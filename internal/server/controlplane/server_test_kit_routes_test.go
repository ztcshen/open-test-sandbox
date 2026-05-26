package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerExposesTestKitRunContracts(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
		},
	}
	server := httptest.NewServer(controlplane.NewWithStore(bundle, s))
	defer server.Close()

	var result map[string]any
	postJSONInto(t, server.URL+"/api/test-kit/run", `{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
			"stepId":"step.alpha"
		}`, http.StatusOK, &result)
	if result["ok"] != false || result["caseId"] != "case.alpha" || result["stepId"] != "step.alpha" {
		t.Fatalf("test kit run result = %#v", result)
	}

	runs := decodeJSONResponse(t, server.URL+"/api/runs", http.StatusOK)
	workflowRuns := runs["workflowRuns"].([]any)
	if len(workflowRuns) != 1 || workflowRuns[0].(map[string]any)["workflowId"] != "workflow.alpha" {
		t.Fatalf("test kit run should be indexed in store: %#v", runs)
	}
}

func TestServerExecutesTestKitRunFromRuntimeConfig(t *testing.T) {
	ctx := context.Background()
	var received struct {
		Method string
		Path   string
		Header string
		Body   map[string]any
	}
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
	defer target.Close()

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
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
	var requestSummary map[string]any
	if err := json.Unmarshal([]byte(caseRuns[0].RequestSummaryJSON), &requestSummary); err != nil {
		t.Fatalf("decode request summary: %v", err)
	}
	if requestSummary["method"] != http.MethodPost || requestSummary["fullUrl"] == "" || requestSummary["stepId"] != "step.alpha" {
		t.Fatalf("request summary = %#v", requestSummary)
	}
	records, err := s.ListEvidence(ctx, runs[0].ID)
	if err != nil {
		t.Fatalf("list test-kit evidence: %v", err)
	}
	if len(records) < 3 {
		t.Fatalf("test-kit run should persist request/response/assertion Evidence records, got %#v", records)
	}
	evidence := decodeJSONResponse(t, server.URL+"/api/case/evidence?runId="+runs[0].ID+"&caseId=case.alpha&stepId=step.alpha", http.StatusOK)
	body := evidence["evidence"].(map[string]any)
	requestEvidence := body["request"].(map[string]any)
	responseEvidence := body["response"].(map[string]any)
	if requestEvidence["evidence_uri"] == "" || responseEvidence["evidence_uri"] == "" || responseEvidence["http_code"] != float64(200) {
		t.Fatalf("test-kit Evidence payload = %#v", evidence)
	}
}

func TestServerTestKitRunHonorsExpectedResponseContains(t *testing.T) {
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result_status":"F"}`))
	}))
	defer target.Close()

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
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

func TestServerExecutesTestKitRunFromStoreRegisteredServicePort(t *testing.T) {
	ctx := context.Background()
	var receivedPath string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"request_id":"req-store-port"}`))
	}))
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target url: %v", err)
	}
	targetPort, err := strconv.Atoi(targetURL.Port())
	if err != nil {
		t.Fatalf("parse target port: %v", err)
	}

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		IndexedAt: time.Now().UTC(),
		Services: []store.CatalogService{
			{ID: "service.gateway", DisplayName: "Gateway", Kind: "http", ServicePort: targetPort, Status: "active"},
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
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

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
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatalf("parse target url: %v", err)
	}
	targetPort, err := strconv.Atoi(targetURL.Port())
	if err != nil {
		t.Fatalf("parse target port: %v", err)
	}
	staleBundleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "stale bundle service", http.StatusTeapot)
	}))
	defer staleBundleServer.Close()
	staleURL, err := url.Parse(staleBundleServer.URL)
	if err != nil {
		t.Fatalf("parse stale url: %v", err)
	}
	stalePort, err := strconv.Atoi(staleURL.Port())
	if err != nil {
		t.Fatalf("parse stale port: %v", err)
	}

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		IndexedAt: time.Now().UTC(),
		Services: []store.CatalogService{
			{ID: "service.gateway", DisplayName: "Gateway", Kind: "http", ServicePort: targetPort, Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.gateway", DisplayName: "Gateway Case", NodeID: "node.gateway", Status: "active"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.gateway", ServiceID: "service.gateway", Method: "GET", Path: "/ready", Status: "active"},
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
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

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
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()

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

func TestServerCollectsTraceTopologyForSingleTestKitRun(t *testing.T) {
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "request.alpha")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "queryBasicTraces"):
			_, _ = w.Write([]byte(`{"data":{"queryBasicTraces":{"traces":[{"endpointNames":["GET:/callback"],"duration":80,"start":"2026-05-15 0830","isError":false,"traceIds":["trace.alpha"]}]}}}`))
		case strings.Contains(payload.Query, "queryTrace"):
			if payload.Variables["traceId"] != "trace.alpha" {
				t.Fatalf("trace id variable = %#v", payload.Variables)
			}
			_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.alpha","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/callback","type":"Entry","component":"Tomcat"}]}}}`))
		default:
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
	}))
	defer provider.Close()

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
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
				NodeID:     "node.alpha",
				WorkflowID: "workflow.alpha",
				ScopeType:  "step",
				ScopeID:    "step.alpha",
				Title:      "Case Alpha Runtime",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.alpha",
					"caseExecution":{
						"method":"GET",
						"nodeId":"service.alpha",
						"path":"/callback",
						"expectedHttpCodes":[200]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithOptions(profile.Bundle{ID: "sample"}, controlplane.Options{
		Runtime:         s,
		TraceGraphQLURL: provider.URL,
	}))
	defer server.Close()

	var result map[string]any
	postJSONInto(t, server.URL+"/api/test-kit/run", fmt.Sprintf(`{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
			"stepId":"step.alpha",
			"baseUrl":%q,
		"timeoutSeconds":5
	}`, target.URL), http.StatusOK, &result)
	if result["ok"] != true {
		t.Fatalf("test kit run result = %#v", result)
	}

	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %#v", runs)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		topologies, err := s.ListTraceTopologies(ctx, runs[0].ID)
		if err != nil {
			t.Fatalf("list trace topologies: %v", err)
		}
		if len(topologies) == 1 && topologies[0].CaseID == "case.alpha" && topologies[0].StepID == "step.alpha" && topologies[0].RequestID == "request.alpha" {
			tasks, err := s.ListPostProcessTasks(ctx, runs[0].ID)
			if err != nil {
				t.Fatalf("list post process tasks: %v", err)
			}
			if len(tasks) != 1 || tasks[0].Kind != "trace_topology_collect" || tasks[0].Status != store.StatusPassed || tasks[0].DurationMs < 0 {
				t.Fatalf("trace post process tasks = %#v", tasks)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("stored trace topology was not collected asynchronously")
}

func TestServerReturnsTraceTopologyForWorkflowStepTestKitRun(t *testing.T) {
	ctx := context.Background()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "request.alpha")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "queryBasicTraces"):
			_, _ = w.Write([]byte(`{"data":{"queryBasicTraces":{"traces":[{"endpointNames":["GET:/callback"],"duration":80,"start":"2026-05-15 0830","isError":false,"traceIds":["trace.alpha"]}]}}}`))
		case strings.Contains(payload.Query, "queryTrace"):
			_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.alpha","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/callback","type":"Entry","component":"Tomcat"},{"traceId":"trace.alpha","segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":"trace.alpha","parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"GET:/callback","type":"Entry","component":"Server"}]}}}`))
		default:
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
	}))
	defer provider.Close()

	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open sqlite store: %v", err)
	}
	defer s.Close()
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
				NodeID:     "node.alpha",
				WorkflowID: "workflow.alpha",
				ScopeType:  "step",
				ScopeID:    "step.alpha",
				Title:      "Case Alpha Runtime",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.alpha",
					"caseExecution":{
						"method":"GET",
						"nodeId":"service.alpha",
						"path":"/callback",
						"expectedHttpCodes":[200]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithOptions(profile.Bundle{ID: "sample"}, controlplane.Options{
		Runtime:         s,
		TraceGraphQLURL: provider.URL,
	}))
	defer server.Close()

	var result map[string]any
	postJSONInto(t, server.URL+"/api/test-kit/run", fmt.Sprintf(`{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
		"stepId":"step.alpha",
		"baseUrl":%q,
		"timeoutSeconds":5
	}`, target.URL), http.StatusOK, &result)
	topology := result["traceTopology"].(map[string]any)
	if topology["provider"] != "skywalking" || topology["status"] != "complete" || topology["traceId"] != "trace.alpha" {
		t.Fatalf("trace topology should be returned inline: %#v", topology)
	}
	if edges := topology["confirmedEdges"].([]any); len(edges) != 1 {
		t.Fatalf("trace topology edges = %#v", edges)
	}
}

func TestServerRecordsSkippedTraceTopologyTaskWhenTraceProviderMissing(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "store.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Request-Id", "request.alpha")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

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
				NodeID:     "node.alpha",
				WorkflowID: "workflow.alpha",
				ScopeType:  "step",
				ScopeID:    "step.alpha",
				Title:      "Case Alpha Runtime",
				Status:     "active",
				ConfigJSON: `{
					"caseId":"case.alpha",
					"caseExecution":{
						"method":"GET",
						"nodeId":"service.alpha",
						"path":"/callback",
						"expectedHttpCodes":[200]
					}
				}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithOptions(profile.Bundle{ID: "sample"}, controlplane.Options{Runtime: s}))
	defer server.Close()

	var result map[string]any
	postJSONInto(t, server.URL+"/api/test-kit/run", fmt.Sprintf(`{
		"caseId":"case.alpha",
		"workflowId":"workflow.alpha",
		"stepId":"step.alpha",
		"baseUrl":%q,
		"timeoutSeconds":5
	}`, target.URL), http.StatusOK, &result)

	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %#v", runs)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tasks, err := s.ListPostProcessTasks(ctx, runs[0].ID)
		if err != nil {
			t.Fatalf("list post process tasks: %v", err)
		}
		if len(tasks) == 1 {
			if tasks[0].Kind != "trace_topology_collect" || tasks[0].Status != store.StatusSkipped || tasks[0].StepID != "step.alpha" {
				t.Fatalf("trace task should record skipped collection: %#v", tasks)
			}
			if !strings.Contains(tasks[0].Error, "TraceGraphQLURL") {
				t.Fatalf("trace skipped task should explain missing provider config: %#v", tasks[0])
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("missing trace topology skipped task")
}

func TestServerExposesPostProcessTasks(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "store.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	base := time.Date(2026, 5, 17, 2, 3, 4, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.tasks",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  base,
		FinishedAt: base.Add(time.Second),
		CreatedAt:  base,
		UpdatedAt:  base.Add(time.Second),
	}); err != nil {
		t.Fatalf("create task run: %v", err)
	}
	if _, err := s.RecordPostProcessTask(ctx, store.PostProcessTask{
		ID:         "task.trace",
		RunID:      "run.tasks",
		WorkflowID: "workflow.alpha",
		StepID:     "step-a",
		CaseID:     "case.alpha",
		Kind:       "trace_topology_collect",
		Status:     store.StatusPassed,
		StartedAt:  base.Add(10 * time.Millisecond),
		FinishedAt: base.Add(135 * time.Millisecond),
		CreatedAt:  base.Add(10 * time.Millisecond),
	}); err != nil {
		t.Fatalf("record task: %v", err)
	}
	if _, err := s.RecordPostProcessTask(ctx, store.PostProcessTask{
		ID:          "task.logs",
		RunID:       "run.tasks",
		WorkflowID:  "workflow.alpha",
		StepID:      "step-b",
		CaseID:      "case.beta",
		Kind:        "runtime_log_collect",
		Status:      store.StatusFailed,
		StartedAt:   base.Add(200 * time.Millisecond),
		FinishedAt:  base.Add(500 * time.Millisecond),
		Error:       "log source missing",
		SummaryJSON: `{"source":"runtime-log"}`,
		CreatedAt:   base.Add(200 * time.Millisecond),
	}); err != nil {
		t.Fatalf("record failed task: %v", err)
	}
	if _, err := s.RecordPostProcessTask(ctx, store.PostProcessTask{
		ID:          "task.trace.skip",
		RunID:       "run.tasks",
		WorkflowID:  "workflow.alpha",
		StepID:      "step-c",
		CaseID:      "case.gamma",
		Kind:        "trace_topology_collect",
		Status:      store.StatusSkipped,
		StartedAt:   base.Add(600 * time.Millisecond),
		FinishedAt:  base.Add(600 * time.Millisecond),
		SummaryJSON: `{"reason":"SkyWalking provider unavailable"}`,
		CreatedAt:   base.Add(600 * time.Millisecond),
	}); err != nil {
		t.Fatalf("record skipped task: %v", err)
	}

	server := httptest.NewServer(controlplane.NewWithOptions(profile.Bundle{ID: "sample"}, controlplane.Options{Runtime: s}))
	defer server.Close()

	payload := decodeJSONResponse(t, server.URL+"/api/post-process-tasks?runId=run.tasks&stepId=step-a&kind=trace_topology_collect", http.StatusOK)
	if payload["ok"] != true || payload["runId"] != "run.tasks" {
		t.Fatalf("post process task payload = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["total"].(float64) != 1 || counts["passed"].(float64) != 1 || counts["durationMs"].(float64) != 125 {
		t.Fatalf("post process task counts = %#v", counts)
	}
	tasks := payload["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("post process tasks = %#v", tasks)
	}
	task := tasks[0].(map[string]any)
	if task["id"] != "task.trace" || task["kind"] != "trace_topology_collect" || task["stepId"] != "step-a" {
		t.Fatalf("post process task = %#v", task)
	}
	if task["outcome"] != "success" || task["reason"] != "completed" || task["displayStatus"] != "passed: completed" {
		t.Fatalf("post process task readable status = %#v", task)
	}

	all := decodeJSONResponse(t, server.URL+"/api/post-process-tasks?runId=run.tasks", http.StatusOK)
	allTasks := all["tasks"].([]any)
	if len(allTasks) != 3 {
		t.Fatalf("all post process tasks = %#v", allTasks)
	}
	byID := map[string]map[string]any{}
	for _, raw := range allTasks {
		task := raw.(map[string]any)
		byID[task["id"].(string)] = task
	}
	if byID["task.logs"]["outcome"] != "failed" || byID["task.logs"]["reason"] != "log source missing" || byID["task.logs"]["displayStatus"] != "failed: log source missing" {
		t.Fatalf("failed task readable status = %#v", byID["task.logs"])
	}
	if byID["task.trace.skip"]["outcome"] != "skipped" || byID["task.trace.skip"]["reason"] != "SkyWalking provider unavailable" || byID["task.trace.skip"]["displayStatus"] != "skipped: SkyWalking provider unavailable" {
		t.Fatalf("skipped task readable status = %#v", byID["task.trace.skip"])
	}

	missing := decodeJSONResponse(t, server.URL+"/api/post-process-tasks", http.StatusBadRequest)
	if missing["ok"] != false || !strings.Contains(missing["error"].(string), "runId") {
		t.Fatalf("missing runId response = %#v", missing)
	}
}

func TestServerExposesTestKitBatchContract(t *testing.T) {
	bundle := profile.Bundle{
		ID:          "sample",
		DisplayName: "Sample Profile",
		APICases: []profile.APICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha"},
			{ID: "case.beta", DisplayName: "Case Beta", NodeID: "node.alpha"},
		},
	}
	server := httptest.NewServer(controlplane.New(bundle))
	defer server.Close()

	var payload struct {
		OK      bool             `json:"ok"`
		Results []map[string]any `json:"results"`
		Summary struct {
			CaseCount int `json:"caseCount"`
			Passed    int `json:"passed"`
		} `json:"summary"`
	}
	postJSONInto(t, server.URL+"/api/test-kit/run-batch", `{
			"caseIds":["case.alpha","case.beta"]
		}`, http.StatusOK, &payload)
	if payload.OK || len(payload.Results) != 2 || payload.Summary.CaseCount != 2 || payload.Summary.Passed != 0 {
		t.Fatalf("test kit batch payload = %#v", payload)
	}
}
