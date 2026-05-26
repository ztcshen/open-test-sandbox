package controlplane_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerExposesEmptyAgentTestWorkbench(t *testing.T) {
	server := httptest.NewServer(controlplane.New(loadEmptyProfile(t)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/agent-test")
	if err != nil {
		t.Fatalf("get agent test api: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("agent test status = %d", resp.StatusCode)
	}

	var payload struct {
		Summary struct {
			CapabilityCount int `json:"capabilityCount"`
			ProfileCount    int `json:"profileCount"`
			RunCount        int `json:"runCount"`
		} `json:"summary"`
		Capabilities      []map[string]any `json:"capabilities"`
		Profiles          []map[string]any `json:"profiles"`
		AgentRuns         []map[string]any `json:"agentRuns"`
		ConfigEvents      []map[string]any `json:"configEvents"`
		EscalationEvents  []map[string]any `json:"escalationEvents"`
		AcceptanceReports []map[string]any `json:"acceptanceReports"`
		Warnings          []string         `json:"warnings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode agent test api: %v", err)
	}
	if payload.Summary.CapabilityCount != 0 || payload.Summary.ProfileCount != 0 || payload.Summary.RunCount != 0 {
		t.Fatalf("agent test summary = %#v", payload.Summary)
	}
	if payload.Capabilities == nil || payload.Profiles == nil || payload.AgentRuns == nil || payload.ConfigEvents == nil || payload.EscalationEvents == nil || payload.AcceptanceReports == nil || payload.Warnings == nil {
		t.Fatalf("agent test empty arrays = %#v", payload)
	}
}

func TestServerExposesAgentTestRunsFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	first := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	for _, item := range []store.Run{
		{
			ID:           "run.alpha",
			ProfileID:    "sample",
			WorkflowID:   "workflow.alpha",
			Status:       store.StatusPassed,
			EvidenceRoot: ".runtime/evidence/run.alpha",
			SummaryJSON:  `{"diagnosisIndex":{"nextStep":"inspect evidence"}}`,
			StartedAt:    first,
			FinishedAt:   first.Add(time.Second),
			CreatedAt:    first,
		},
		{
			ID:           "run.beta",
			ProfileID:    "sample",
			WorkflowID:   "workflow.beta",
			Status:       store.StatusFailed,
			EvidenceRoot: ".runtime/evidence/run.beta",
			SummaryJSON:  `{"diagnosisIndex":{"failureKind":"dependency_missing","nextStep":"add fixture data"}}`,
			StartedAt:    first.Add(time.Minute),
			FinishedAt:   first.Add(time.Minute + time.Second),
			CreatedAt:    first.Add(time.Minute),
		},
	} {
		if _, err := s.CreateRun(ctx, item); err != nil {
			t.Fatalf("create run %s: %v", item.ID, err)
		}
	}
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
			{ID: "workflow.beta", DisplayName: "Workflow Beta"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/agent-test", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["runCount"] != float64(2) || summary["latestFailureKind"] != "dependency_missing" {
		t.Fatalf("agent test summary = %#v", summary)
	}
	statusCounts := summary["statusCounts"].(map[string]any)
	if statusCounts[store.StatusPassed] != float64(1) || statusCounts[store.StatusFailed] != float64(1) {
		t.Fatalf("agent test status counts = %#v", statusCounts)
	}
	runs := payload["agentRuns"].([]any)
	if len(runs) != 2 {
		t.Fatalf("agent runs = %#v", runs)
	}
	latest := runs[0].(map[string]any)
	if latest["runId"] != "run.beta" || latest["profileId"] != "sample" || latest["workflowId"] != "workflow.beta" || latest["failureKind"] != "dependency_missing" {
		t.Fatalf("latest agent run = %#v", latest)
	}
	diagnosis := latest["diagnosis"].(map[string]any)
	if diagnosis["nextStep"] != "add fixture data" {
		t.Fatalf("latest diagnosis = %#v", diagnosis)
	}
	profiles := payload["profiles"].([]any)
	if len(profiles) != 1 || profiles[0].(map[string]any)["id"] != "sample" || profiles[0].(map[string]any)["stepCount"] != float64(2) {
		t.Fatalf("agent test profiles = %#v", profiles)
	}
	capabilities := payload["capabilities"].([]any)
	if len(capabilities) == 0 {
		t.Fatalf("agent test capabilities = %#v", capabilities)
	}
}

func TestServerExposesProfileAgentValidationConfig(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		AgentTestProfiles: []profile.AgentTestProfile{
			{
				ID:    "baseline",
				Title: "Baseline Chain",
				Steps: []profile.AgentTestStep{
					{Type: "workflow", ID: "workflow.baseline"},
				},
				Probes: []profile.AgentTestProbe{
					{Name: "row_count", Query: "select count(*) from records"},
				},
				EvidencePolicy: map[string]bool{"collectTrace": true, "collectLogs": true},
				ConfigPolicy: profile.AgentConfigPolicy{
					AllowedChanges: []profile.ConfigChange{{Kind: "env", Key: "SANDBOX_FLAG"}},
				},
				RequiredConfig: []profile.RequiredConfig{
					{Kind: "setting", Key: "feature.flag", SuggestedValue: "enabled", Reason: "exercise config application"},
				},
			},
		},
		ConfigAuthoring: profile.ConfigAuthoring{
			SchemaVersion:               "1",
			Role:                        "configuration-subagent",
			Summary:                     "Concrete template configuration is authored by a dedicated subagent.",
			GuidePath:                   "template-config/SKILL.md",
			AllowedWritePaths:           []string{"template-config/"},
			AllowedReadPaths:            []string{"template-config/SKILL.md"},
			MainAgentResponsibilities:   []string{"maintain tools"},
			SubagentResponsibilities:    []string{"author configuration", "report friction"},
			HandoffRequiredFields:       []string{"changedFiles", "friction"},
			FrictionCategories:          []string{"missing-model-capability"},
			RequiresDedicatedSubagent:   true,
			ProhibitsMainAgentAuthoring: true,
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, nil))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/agent-test", http.StatusOK)
	summary := payload["summary"].(map[string]any)
	if summary["profileCount"] != float64(1) || summary["authoringContractCount"] != float64(1) {
		t.Fatalf("agent validation summary = %#v", summary)
	}
	profiles := payload["profiles"].([]any)
	if len(profiles) != 1 {
		t.Fatalf("agent validation profiles = %#v", profiles)
	}
	agentProfile := profiles[0].(map[string]any)
	if agentProfile["id"] != "baseline" || agentProfile["stepCount"] != float64(1) || agentProfile["probeCount"] != float64(1) {
		t.Fatalf("agent validation profile = %#v", agentProfile)
	}
	if len(agentProfile["allowedChanges"].([]any)) != 1 || len(agentProfile["requiredConfig"].([]any)) != 1 {
		t.Fatalf("agent validation profile config = %#v", agentProfile)
	}
	authoring := payload["configAuthoring"].(map[string]any)
	if authoring["role"] != "configuration-subagent" || authoring["requiresDedicatedSubagent"] != true || authoring["prohibitsMainAgentAuthoring"] != true {
		t.Fatalf("config authoring = %#v", authoring)
	}
}

func TestServerExposesAPICaseCapabilities(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{
				ID:             "case.alpha",
				DisplayName:    "Case Alpha",
				NodeID:         "node.alpha",
				CasePath:       "cases/case.alpha.json",
				SourceKind:     "karate",
				SourcePath:     "tests/api.feature",
				ExecutorID:     "executor.karate",
				BaseURL:        "http://127.0.0.1:18080",
				EvidenceDir:    ".runtime/cases",
				TimeoutSeconds: 30,
				DefaultOverrides: map[string]any{
					"itemId": "item-001",
				},
			},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/cases/capabilities")
	if err != nil {
		t.Fatalf("get api case capabilities: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("api case capabilities status = %d", resp.StatusCode)
	}

	var payload struct {
		Cases []struct {
			ID               string         `json:"id"`
			Title            string         `json:"title"`
			Operation        string         `json:"operation"`
			CasePath         string         `json:"casePath"`
			SourceKind       string         `json:"sourceKind"`
			SourcePath       string         `json:"sourcePath"`
			ExecutorID       string         `json:"executorId"`
			BaseURL          string         `json:"baseUrl"`
			EvidenceDir      string         `json:"evidenceDir"`
			TimeoutSeconds   int            `json:"timeoutSeconds"`
			DefaultOverrides map[string]any `json:"defaultOverrides"`
			Graph            struct {
				Nodes []struct {
					ID          string `json:"id"`
					DisplayName string `json:"displayName"`
					Role        string `json:"role"`
				} `json:"nodes"`
			} `json:"graph"`
		} `json:"cases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode api case capabilities: %v", err)
	}
	if len(payload.Cases) != 1 || payload.Cases[0].ID != "case.alpha" || payload.Cases[0].Operation != "Node Alpha" {
		t.Fatalf("api case capabilities = %#v", payload.Cases)
	}
	if payload.Cases[0].CasePath != "cases/case.alpha.json" || payload.Cases[0].SourceKind != "karate" || payload.Cases[0].SourcePath != "tests/api.feature" || payload.Cases[0].ExecutorID != "executor.karate" || payload.Cases[0].BaseURL == "" || payload.Cases[0].EvidenceDir != ".runtime/cases" || payload.Cases[0].TimeoutSeconds != 30 || payload.Cases[0].DefaultOverrides["itemId"] != "item-001" {
		t.Fatalf("api case run config = %#v", payload.Cases[0])
	}
	if len(payload.Cases[0].Graph.Nodes) != 1 || payload.Cases[0].Graph.Nodes[0].ID != "service.alpha" || payload.Cases[0].Graph.Nodes[0].Role != "http" {
		t.Fatalf("api case graph = %#v", payload.Cases[0].Graph)
	}
}

func TestServerExposesAPICaseCapabilitiesFromStoreCatalog(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC),
		Services: []store.CatalogService{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "Alpha", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{
				ID:                   "case.alpha",
				DisplayName:          "Case Alpha",
				NodeID:               "node.alpha",
				CasePath:             "cases/case.alpha.json",
				SourceKind:           "karate",
				SourcePath:           "tests/api.feature",
				ExecutorID:           "executor.karate",
				BaseURL:              "http://127.0.0.1:18080",
				EvidenceDir:          ".runtime/cases",
				TimeoutSeconds:       30,
				DefaultOverridesJSON: `{"itemId":"item-001"}`,
				RequiredForAdmission: true,
				Status:               "active",
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "empty", DisplayName: "Empty Profile"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/cases/capabilities", http.StatusOK)
	cases := payload["cases"].([]any)
	if len(cases) != 1 {
		t.Fatalf("api case capabilities from store = %#v", payload)
	}
	item := cases[0].(map[string]any)
	if item["id"] != "case.alpha" || item["casePath"] != "cases/case.alpha.json" || item["sourceKind"] != "karate" || item["sourcePath"] != "tests/api.feature" || item["executorId"] != "executor.karate" || item["baseUrl"] != "http://127.0.0.1:18080" || item["evidenceDir"] != ".runtime/cases" || item["timeoutSeconds"] != float64(30) {
		t.Fatalf("api case store run config = %#v", item)
	}
	overrides := item["defaultOverrides"].(map[string]any)
	if overrides["itemId"] != "item-001" {
		t.Fatalf("api case store default overrides = %#v", overrides)
	}
	graph := item["graph"].(map[string]any)
	nodes := graph["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["id"] != "service.alpha" {
		t.Fatalf("api case store graph = %#v", graph)
	}
}

func TestServerExposesAPICaseCapabilityRunsFromStore(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	started := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "sample",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusFailed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		CreatedAt:    started,
		UpdatedAt:    started,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	_, err = s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "run.alpha.case",
		RunID:                "run.alpha",
		CaseID:               "case.alpha",
		Status:               store.StatusFailed,
		RequestSummaryJSON:   `{"method":"POST","path":"/alpha"}`,
		AssertionSummaryJSON: `{"status":"failed","errorCount":1}`,
		StartedAt:            started,
		FinishedAt:           started.Add(200 * time.Millisecond),
		CreatedAt:            started,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
			{ID: "case.empty", DisplayName: "Case Empty", NodeID: "node.alpha"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/cases/capabilities", http.StatusOK)
	cases := payload["cases"].([]any)
	alpha := cases[0].(map[string]any)
	if alpha["id"] != "case.alpha" || alpha["runCount"] != float64(1) {
		t.Fatalf("api case run count = %#v", alpha)
	}
	latest := alpha["latestRun"].(map[string]any)
	if latest["runId"] != "run.alpha" || latest["status"] != store.StatusFailed || latest["failureReason"] != "assertion errors: 1" {
		t.Fatalf("api case latest run = %#v", latest)
	}
	empty := cases[1].(map[string]any)
	if empty["id"] != "case.empty" || empty["runCount"] != float64(0) {
		t.Fatalf("empty api case run state = %#v", empty)
	}
	if _, ok := empty["latestRun"]; ok {
		t.Fatalf("empty api case should not expose latestRun: %#v", empty)
	}
}
