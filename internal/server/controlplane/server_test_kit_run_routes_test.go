package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

type runtimeConfigTargetCapture struct {
	Method string
	Path   string
	Header string
	Body   map[string]any
}

func TestServerExecutesTestKitRunFromRuntimeConfig(t *testing.T) {
	ctx := context.Background()
	target, received := newRuntimeConfigTarget(t)
	defer target.Close()

	s := openTestKitSQLiteStore(t, ctx, "sandbox.sqlite")
	seedRuntimeConfigCatalog(t, ctx, s)
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	var result map[string]any
	postJSONInto(t, server.URL+"/api/test-kit/run", fmt.Sprintf(`{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
			"stepId":"step.alpha",
			"baseUrl":%q,
		"overrides":{"id":"runtime-id","mode":"live","header":"selected"},
		"timeoutSeconds":5
	}`, target.URL), http.StatusOK, &result)
	if result["ok"] != true || result["status"] != store.StatusPassed {
		t.Fatalf("test kit run result = %#v", result)
	}
	if received.Method != http.MethodPost || received.Path != "/callback?mode=live" || received.Header != "selected" || received.Body["id"] != "runtime-id" {
		t.Fatalf("target received = %#v", received)
	}
	requireRuntimeConfigRunRecords(t, ctx, s, server.URL)
}

func TestServerTestKitRunHonorsExpectedResponseContains(t *testing.T) {
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result_status":"F"}`))
	}))
	defer target.Close()

	s := openTestKitSQLiteStore(t, ctx, "sandbox.sqlite")
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.case.alpha",
				TemplateID: "template.case.alpha",
				WorkflowID: "workflow.alpha",
				ScopeType:  "step",
				ScopeID:    "step.alpha",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.alpha",
					"caseExecution":{
						"method":"GET",
						"nodeId":"node.alpha",
						"path":"/result",
						"expectedHttpCodes":[200],
						"expectedResponseContains":["\"result_status\":\"S\""]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample", DisplayName: "Sample Profile"}, s))
	defer server.Close()

	var result map[string]any
	postJSONInto(t, server.URL+"/api/test-kit/run", fmt.Sprintf(`{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
		"stepId":"step.alpha",
		"baseUrl":%q
	}`, target.URL), http.StatusOK, &result)
	if result["ok"] != false || !strings.Contains(fmt.Sprint(result["error"]), "response body missing") {
		t.Fatalf("test kit result = %#v", result)
	}
}

func TestServerTestKitRunFailsFastForBodylessWriteRequest(t *testing.T) {
	ctx := context.Background()
	targetCalled := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	s := openTestKitSQLiteStore(t, ctx, "sandbox.sqlite")
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		APICases: []store.CatalogAPICase{
			{ID: "case.bodyless", DisplayName: "Bodyless Case", NodeID: "node.alpha", Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.case.bodyless",
				TemplateID: "template.case.bodyless",
				NodeID:     "node.alpha",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.bodyless",
					"caseExecution":{
						"method":"POST",
						"nodeId":"node.alpha",
						"path":"/submit",
						"expectedHttpCodes":[200]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "sample"}, s))
	defer server.Close()

	var result map[string]any
	postJSONInto(t, server.URL+"/api/test-kit/run", fmt.Sprintf(`{
		"caseId":"case.bodyless",
		"baseUrl":%q
	}`, target.URL), http.StatusOK, &result)
	if targetCalled {
		t.Fatal("bodyless write request should fail before sending HTTP")
	}
	if result["ok"] != false || !strings.Contains(fmt.Sprint(result["error"]), "POST caseExecution.body") {
		t.Fatalf("test kit result = %#v", result)
	}
}

func TestServerExecutesTestKitRunFromStoreRegisteredServicePort(t *testing.T) {
	ctx := context.Background()
	var receivedPath string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"request_id":"req-store-port"}`))
	}))
	defer target.Close()
	targetPort := testServerPort(t, target)

	s := openTestKitSQLiteStore(t, ctx, "sandbox.sqlite")
	seedGatewayTestKitCatalog(t, ctx, s, targetPort, false)

	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "current"}, s))
	defer server.Close()

	var result map[string]any
	postJSONInto(t, server.URL+"/api/test-kit/run", `{
		"caseId":"case.gateway",
		"workflowId":"workflow.gateway",
		"stepId":"step.gateway",
		"timeoutSeconds":5
	}`, http.StatusOK, &result)
	if result["ok"] != true {
		t.Fatalf("test kit run result=%#v", result)
	}
	if receivedPath != "/ready" {
		t.Fatalf("target received path = %q", receivedPath)
	}
}

func TestServerPrefersStoreRegisteredServicePortOverBundleServicePort(t *testing.T) {
	ctx := context.Background()
	var receivedPath string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"request_id":"req-store-first"}`))
	}))
	defer target.Close()
	targetPort := testServerPort(t, target)
	staleBundleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "stale bundle service", http.StatusTeapot)
	}))
	defer staleBundleServer.Close()
	stalePort := testServerPort(t, staleBundleServer)

	s := openTestKitSQLiteStore(t, ctx, "sandbox.sqlite")
	seedGatewayTestKitCatalog(t, ctx, s, targetPort, true)

	bundle := profile.Bundle{
		ID: "current",
		Services: []profile.Service{
			{ID: "service.gateway", DisplayName: "Stale Gateway", Kind: "http", ServicePort: stalePort, Status: "active"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	var result map[string]any
	postJSONInto(t, server.URL+"/api/test-kit/run", `{
		"caseId":"case.gateway",
		"workflowId":"workflow.gateway",
		"stepId":"step.gateway",
		"timeoutSeconds":5
	}`, http.StatusOK, &result)
	if result["ok"] != true {
		t.Fatalf("test kit run result=%#v", result)
	}
	if receivedPath != "/ready" {
		t.Fatalf("Store service port should win over stale bundle service, target received path = %q", receivedPath)
	}
}

func TestServerCatalogUsesStoreCatalogOverStaleReadModel(t *testing.T) {
	ctx := context.Background()
	s := openTestKitSQLiteStore(t, ctx, "sandbox.sqlite")

	stale := store.ProfileCatalog{
		ProfileID: "current",
		Services:  []store.CatalogService{{ID: "service.gateway", Kind: "http"}},
		Workflows: []store.CatalogWorkflow{{ID: "workflow.core", DisplayName: "Core"}},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.query", ServiceID: "service.gateway", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.old", NodeID: "node.query", Status: "active"},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.core", StepID: "query", NodeID: "node.query", CaseID: "case.old", Required: true, SortOrder: 1},
		},
	}
	readModel, err := controlplane.CatalogReadModel(stale, "config.stale", time.Now().UTC())
	if err != nil {
		t.Fatalf("build stale catalog read model: %v", err)
	}
	if _, err := s.UpsertReadModel(ctx, readModel); err != nil {
		t.Fatalf("upsert stale catalog read model: %v", err)
	}

	current := stale
	current.APICases = []store.CatalogAPICase{{ID: "case.current", NodeID: "node.query", Status: "active"}}
	current.WorkflowBindings = []store.CatalogWorkflowBinding{
		{WorkflowID: "workflow.core", StepID: "query", NodeID: "node.query", CaseID: "case.current", Required: true, SortOrder: 1},
	}
	current.TemplateConfigs = []store.CatalogTemplateConfig{
		{
			ID:         "cfg.workflow.core.query",
			WorkflowID: "workflow.core",
			ScopeType:  "step",
			ScopeID:    "query",
			Status:     "active",
			ConfigJSON: `{"caseId":"case.old","caseExecution":{"method":"GET","nodeId":"service.gateway","path":"/query"}}`,
		},
	}
	if err := s.ReplaceProfileCatalog(ctx, current); err != nil {
		t.Fatalf("replace current catalog: %v", err)
	}
	server := httptest.NewServer(controlplane.NewWithStore(profile.Bundle{ID: "current"}, s))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/catalog", http.StatusOK)
	source := payload["source"].(map[string]any)
	if source["kind"] != "store" {
		t.Fatalf("catalog should use store source over stale read model: %#v", source)
	}
	workflows := payload["workflows"].([]any)
	steps := workflows[0].(map[string]any)["steps"].([]any)
	if steps[0].(map[string]any)["caseId"] != "case.current" {
		t.Fatalf("catalog step should use current store binding: %#v", steps[0])
	}
}

func seedGatewayTestKitCatalog(t *testing.T, ctx context.Context, s *sqlite.Store, servicePort int, includeInterfaceNode bool) {
	t.Helper()

	catalog := store.ProfileCatalog{
		ProfileID: "current",
		IndexedAt: time.Now().UTC(),
		Services: []store.CatalogService{
			{ID: "service.gateway", DisplayName: "Gateway", Kind: "http", ServicePort: servicePort, Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.gateway", DisplayName: "Gateway Case", NodeID: "node.gateway", Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.case.gateway",
				TemplateID: "template.case.gateway",
				NodeID:     "node.gateway",
				WorkflowID: "workflow.gateway",
				ScopeType:  "step",
				ScopeID:    "step.gateway",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.gateway",
					"caseExecution":{
						"method":"GET",
						"nodeId":"service.gateway",
						"path":"/ready",
						"expectedHttpCodes":[200]
					}
				}`,
			},
		},
	}
	if includeInterfaceNode {
		catalog.InterfaceNodes = []store.CatalogInterfaceNode{
			{ID: "node.gateway", ServiceID: "service.gateway", Method: "GET", Path: "/ready", Status: "active"},
		}
	}
	if err := s.ReplaceProfileCatalog(ctx, catalog); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
}

func newRuntimeConfigTarget(t *testing.T) (*httptest.Server, *runtimeConfigTargetCapture) {
	t.Helper()

	received := &runtimeConfigTargetCapture{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Method = r.Method
		received.Path = r.URL.String()
		received.Header = r.Header.Get("X-Case")
		if err := json.NewDecoder(r.Body).Decode(&received.Body); err != nil {
			t.Fatalf("decode target body: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"request_id":"req-001"}`))
	}))
	return target, received
}

func seedRuntimeConfigCatalog(t *testing.T, ctx context.Context, s *sqlite.Store) {
	t.Helper()

	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: time.Now().UTC(),
		Services: []store.CatalogService{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "app"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", Status: "active"},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID:         "cfg.case.alpha",
				TemplateID: "template.case.alpha",
				NodeID:     "node.alpha",
				WorkflowID: "workflow.alpha",
				ScopeType:  "step",
				ScopeID:    "step.alpha",
				Title:      "Case Alpha Runtime",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.alpha",
					"caseExecution":{
						"method":"POST",
						"nodeId":"service.alpha",
						"path":"/callback",
						"query":{"mode":"{{override:mode|default}}"},
						"headers":{"X-Case":"{{override:header|defaultValue}}"},
						"body":{"id":"{{override:id|default-id}}","serial":"{{serial:TST}}"},
						"expectedHttpCodes":[200],
						"requireRequestId":true,
						"traceCorrelatorFields":["request_id"]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
}

func requireRuntimeConfigRunRecords(t *testing.T, ctx context.Context, s *sqlite.Store, serverURL string) {
	t.Helper()

	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].WorkflowID != "workflow.alpha" {
		t.Fatalf("runs = %#v", runs)
	}
	caseRuns, err := s.ListAPICaseRuns(ctx, runs[0].ID)
	if err != nil {
		t.Fatalf("list api case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].Status != store.StatusPassed {
		t.Fatalf("case runs = %#v", caseRuns)
	}
	if !caseRuns[0].FinishedAt.After(caseRuns[0].StartedAt) || caseRuns[0].FinishedAt.Sub(caseRuns[0].StartedAt) < 10*time.Millisecond {
		t.Fatalf("case run timing = %#v", caseRuns[0])
	}
	requireRuntimeConfigRequestSummary(t, caseRuns[0])
	requireRuntimeConfigEvidence(t, ctx, s, serverURL, runs[0].ID)
}

func requireRuntimeConfigRequestSummary(t *testing.T, caseRun store.APICaseRun) {
	t.Helper()

	var requestSummary map[string]any
	if err := json.Unmarshal([]byte(caseRun.RequestSummaryJSON), &requestSummary); err != nil {
		t.Fatalf("decode request summary: %v", err)
	}
	if requestSummary["method"] != http.MethodPost || requestSummary["fullUrl"] == "" || requestSummary["stepId"] != "step.alpha" {
		t.Fatalf("request summary = %#v", requestSummary)
	}
}

func requireRuntimeConfigEvidence(t *testing.T, ctx context.Context, s *sqlite.Store, serverURL, runID string) {
	t.Helper()

	records, err := s.ListEvidence(ctx, runID)
	if err != nil {
		t.Fatalf("list test-kit evidence: %v", err)
	}
	if len(records) < 3 {
		t.Fatalf("test-kit run should persist request/response/assertion Evidence records, got %#v", records)
	}
	evidence := decodeJSONResponse(t, serverURL+"/api/case/evidence?runId="+runID+"&caseId=case.alpha&stepId=step.alpha", http.StatusOK)
	body := evidence["evidence"].(map[string]any)
	requestEvidence := body["request"].(map[string]any)
	responseEvidence := body["response"].(map[string]any)
	if requestEvidence["evidence_uri"] == "" || responseEvidence["evidence_uri"] == "" || responseEvidence["http_code"] != float64(200) {
		t.Fatalf("test-kit Evidence payload = %#v", evidence)
	}
}

func testServerPort(t *testing.T, server *httptest.Server) int {
	t.Helper()

	targetURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse target url: %v", err)
	}
	targetPort, err := strconv.Atoi(targetURL.Port())
	if err != nil {
		t.Fatalf("parse target port: %v", err)
	}
	return targetPort
}
