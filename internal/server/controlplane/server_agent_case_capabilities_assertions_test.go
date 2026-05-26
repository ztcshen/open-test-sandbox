package controlplane_test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func seedAgentTestRunsStore(t *testing.T) (store.Store, profile.Bundle) {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	first := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	for _, item := range agentTestRunRecords(first) {
		if _, err := s.CreateRun(ctx, item); err != nil {
			t.Fatalf("create run %s: %v", item.ID, err)
		}
	}
	return s, profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Workflows: []profile.Workflow{
			{ID: "workflow.alpha", DisplayName: "Workflow Alpha"},
			{ID: "workflow.beta", DisplayName: "Workflow Beta"},
		},
	}
}

func agentTestRunRecords(first time.Time) []store.Run {
	return []store.Run{
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
	}
}

func assertAgentTestRunsPayload(t *testing.T, payload map[string]any) {
	t.Helper()
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
	assertLatestAgentTestRun(t, runs[0].(map[string]any))
	assertAgentTestProfileSummary(t, payload)
}

func assertLatestAgentTestRun(t *testing.T, latest map[string]any) {
	t.Helper()
	if latest["runId"] != "run.beta" || latest["profileId"] != "sample" || latest["workflowId"] != "workflow.beta" || latest["failureKind"] != "dependency_missing" {
		t.Fatalf("latest agent run = %#v", latest)
	}
	diagnosis := latest["diagnosis"].(map[string]any)
	if diagnosis["nextStep"] != "add fixture data" {
		t.Fatalf("latest diagnosis = %#v", diagnosis)
	}
}

func assertAgentTestProfileSummary(t *testing.T, payload map[string]any) {
	t.Helper()
	profiles := payload["profiles"].([]any)
	if len(profiles) != 1 || profiles[0].(map[string]any)["id"] != "sample" || profiles[0].(map[string]any)["stepCount"] != float64(2) {
		t.Fatalf("agent test profiles = %#v", profiles)
	}
	capabilities := payload["capabilities"].([]any)
	if len(capabilities) == 0 {
		t.Fatalf("agent test capabilities = %#v", capabilities)
	}
}

func profileAgentValidationBundle() profile.Bundle {
	return profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		AgentTestProfiles: []profile.AgentTestProfile{{
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
		}},
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
}

func assertProfileAgentValidationPayload(t *testing.T, payload map[string]any) {
	t.Helper()
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

func apiCaseCapabilityBundle() profile.Bundle {
	return profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		Services: []profile.Service{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http"},
		},
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha"},
		},
		APICases: []profile.APICase{{
			ID:               "case.alpha",
			DisplayName:      "Case Alpha",
			NodeID:           "node.alpha",
			CasePath:         "cases/case.alpha.json",
			SourceKind:       "karate",
			SourcePath:       "tests/api.feature",
			ExecutorID:       "executor.karate",
			BaseURL:          "http://127.0.0.1:18080",
			EvidenceDir:      ".runtime/cases",
			TimeoutSeconds:   30,
			DefaultOverrides: map[string]any{"itemId": "item-001"},
		}},
	}
}

func assertAPICaseCapabilitiesPayload(t *testing.T, payload map[string]any) {
	t.Helper()
	cases := payload["cases"].([]any)
	if len(cases) != 1 {
		t.Fatalf("api case capabilities = %#v", cases)
	}
	item := cases[0].(map[string]any)
	if item["id"] != "case.alpha" || item["operation"] != "Node Alpha" {
		t.Fatalf("api case capabilities = %#v", cases)
	}
	if item["casePath"] != "cases/case.alpha.json" || item["sourceKind"] != "karate" || item["sourcePath"] != "tests/api.feature" || item["executorId"] != "executor.karate" || item["baseUrl"] == "" || item["evidenceDir"] != ".runtime/cases" || item["timeoutSeconds"] != float64(30) {
		t.Fatalf("api case run config = %#v", item)
	}
	if item["defaultOverrides"].(map[string]any)["itemId"] != "item-001" {
		t.Fatalf("api case default overrides = %#v", item)
	}
	graph := item["graph"].(map[string]any)
	nodes := graph["nodes"].([]any)
	if len(nodes) != 1 || nodes[0].(map[string]any)["id"] != "service.alpha" || nodes[0].(map[string]any)["role"] != "http" {
		t.Fatalf("api case graph = %#v", graph)
	}
}

func seedAPICaseCapabilityRunStore(t *testing.T) store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	started := time.Date(2026, 5, 14, 9, 0, 0, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{ID: "run.alpha", ProfileID: "sample", WorkflowID: "workflow.alpha", Status: store.StatusFailed, EvidenceRoot: ".runtime/evidence/run.alpha", CreatedAt: started, UpdatedAt: started}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{ID: "run.alpha.case", RunID: "run.alpha", CaseID: "case.alpha", Status: store.StatusFailed, RequestSummaryJSON: `{"method":"POST","path":"/alpha"}`, AssertionSummaryJSON: `{"status":"failed","errorCount":1}`, StartedAt: started, FinishedAt: started.Add(200 * time.Millisecond), CreatedAt: started}); err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	return s
}

func apiCaseCapabilityRunsBundle() profile.Bundle {
	bundle := apiCaseCapabilityBundle()
	bundle.APICases = append(bundle.APICases, profile.APICase{ID: "case.empty", DisplayName: "Case Empty", NodeID: "node.alpha"})
	return bundle
}

func assertAPICaseCapabilityRunsPayload(t *testing.T, payload map[string]any) {
	t.Helper()
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

func getJSONMap(t *testing.T, url string) map[string]any {
	t.Helper()
	return decodeJSONResponse(t, url, http.StatusOK)
}
