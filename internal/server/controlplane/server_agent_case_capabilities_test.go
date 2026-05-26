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
	s, bundle := seedAgentTestRunsStore(t)
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	assertAgentTestRunsPayload(t, getJSONMap(t, server.URL+"/api/agent-test"))
}

func TestServerExposesProfileAgentValidationConfig(t *testing.T) {
	server := httptest.NewServer(controlplane.NewWithStore(profileAgentValidationBundle(), nil))
	defer server.Close()

	assertProfileAgentValidationPayload(t, getJSONMap(t, server.URL+"/api/agent-test"))
}

func TestServerExposesAPICaseCapabilities(t *testing.T) {
	server := httptest.NewServer(controlplane.New(apiCaseCapabilityBundle()))
	defer server.Close()

	assertAPICaseCapabilitiesPayload(t, getJSONMap(t, server.URL+"/api/cases/capabilities"))
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
	s := seedAPICaseCapabilityRunStore(t)
	server := httptest.NewServer(controlplane.NewWithStore(apiCaseCapabilityRunsBundle(), s))
	defer server.Close()

	assertAPICaseCapabilityRunsPayload(t, getJSONMap(t, server.URL+"/api/cases/capabilities"))
}
