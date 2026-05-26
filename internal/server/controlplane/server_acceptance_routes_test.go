package controlplane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestServerAsyncWorkflowAcceptancePassesWithSkyWalkingTopology(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "request.acceptance")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
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
			_, _ = w.Write([]byte(`{"data":{"queryBasicTraces":{"traces":[{"endpointNames":["GET:/v1/acceptance"],"duration":80,"start":"2026-05-20 0320","isError":false,"traceIds":["trace.acceptance"]}]}}}`))
		case strings.Contains(payload.Query, "queryTrace"):
			_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.acceptance","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/v1/acceptance","type":"Entry","component":"Tomcat"},{"traceId":"trace.acceptance","segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":"trace.acceptance","parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"GET:/v1/acceptance","type":"Entry","component":"Server"}]}}}`))
		default:
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
	}))
	defer provider.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case-acceptance.json")
	if err := os.WriteFile(casePath, []byte(`{
  "id": "case.acceptance",
  "title": "Acceptance Step",
  "request": {"method": "GET", "path": "/v1/acceptance"},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`), 0o644); err != nil {
		t.Fatalf("write api case: %v", err)
	}
	bundle := profile.Bundle{
		ID:             "sample",
		Workflows:      []profile.Workflow{{ID: "workflow.acceptance", DisplayName: "Acceptance Workflow"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.acceptance", DisplayName: "Acceptance Node"}},
		APICases: []profile.APICase{{
			ID:          "case.acceptance",
			DisplayName: "Acceptance Step",
			NodeID:      "node.acceptance",
			CasePath:    casePath,
			BaseURL:     target.URL,
			EvidenceDir: filepath.Join(dir, "evidence"),
		}},
		WorkflowBindings: []profile.WorkflowBinding{{
			WorkflowID: "workflow.acceptance",
			StepID:     "step.acceptance",
			NodeID:     "node.acceptance",
			CaseID:     "case.acceptance",
			Required:   true,
			SortOrder:  1,
		}},
	}
	server := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{
		Runtime:         s,
		TraceGraphQLURL: provider.URL,
	}))
	defer server.Close()

	var created struct {
		ReportURL string `json:"reportUrl"`
	}
	postJSONInto(t, server.URL+"/api/cases/batch-runs", `{"requestId":"workflow-acceptance-001","workflowId":"workflow.acceptance"}`, http.StatusAccepted, &created)
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.Acceptance.OK || len(report.Acceptance.Steps) != 1 || !report.Acceptance.Steps[0].EvidenceComplete || !report.Acceptance.Steps[0].TopologyComplete {
		t.Fatalf("workflow acceptance with SkyWalking = %#v", report.Acceptance)
	}
}

func TestServerRunsWorkflowCaseWithStoreExecutionConfigWithoutCaseFile(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/store-execution" {
			t.Fatalf("unexpected execution path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer target.Close()

	now := time.Now().UTC()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Workflows: []store.CatalogWorkflow{{ID: "workflow.store-execution", DisplayName: "Store Execution Workflow"}},
		APICases:  []store.CatalogAPICase{{ID: "case.store-execution", DisplayName: "Store Execution Case", NodeID: "node.store-execution", Status: "active"}},
		Services:  []store.CatalogService{{ID: "service.store-execution", Kind: "app", ServicePort: 18080, Status: "active"}},
		InterfaceNodes: []store.CatalogInterfaceNode{{
			ID: "node.store-execution", ServiceID: "service.store-execution", Method: "POST", Path: "/store-execution", Status: "active",
		}},
		WorkflowBindings: []store.CatalogWorkflowBinding{{
			WorkflowID: "workflow.store-execution", StepID: "store-step", NodeID: "node.store-execution", CaseID: "case.store-execution", Required: true, SortOrder: 1,
		}},
		TemplateConfigs: []store.CatalogTemplateConfig{{
			ID: "cfg.case.store-execution", TemplateID: "TPL-CASE-EXECUTION-V1", ScopeType: "api-case", ScopeID: "case.store-execution", Status: "active",
			ConfigJSON: `{"caseId":"case.store-execution","caseExecution":{"method":"POST","nodeId":"node.store-execution","path":"/store-execution","body":{"hello":"store"},"expectedHttpCodes":[200],"headers":{"Content-Type":"application/json"}}}`,
		}},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	bundle := profile.Bundle{
		ID:             "sample",
		Workflows:      []profile.Workflow{{ID: "workflow.store-execution"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.store-execution", Method: "POST", Path: "/store-execution"}},
		APICases:       []profile.APICase{{ID: "case.store-execution", NodeID: "node.store-execution"}},
		WorkflowBindings: []profile.WorkflowBinding{{
			WorkflowID: "workflow.store-execution", StepID: "store-step", NodeID: "node.store-execution", CaseID: "case.store-execution", Required: true, SortOrder: 1,
		}},
	}
	server := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: s}))
	defer server.Close()

	var created struct {
		ReportURL string `json:"reportUrl"`
		Total     int    `json:"total"`
	}
	postJSONInto(t, server.URL+"/api/cases/batch-runs", fmt.Sprintf(`{"requestId":"store-execution-001","workflowId":"workflow.store-execution","baseUrl":%q}`, target.URL), http.StatusAccepted, &created)
	if created.Total != 1 {
		t.Fatalf("store execution workflow should plan one case, got %d", created.Total)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if report.Total != 1 || report.Passed != 1 {
		t.Fatalf("store execution workflow report = %#v", report)
	}
}

func TestServerBatchRunHonorsStoreExecutionExpectedResponseContains(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result_status":"F"}`))
	}))
	defer target.Close()

	now := time.Now().UTC()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Workflows: []store.CatalogWorkflow{{ID: "workflow.store-execution", DisplayName: "Store Execution Workflow"}},
		APICases:  []store.CatalogAPICase{{ID: "case.store-execution", DisplayName: "Store Execution Case", NodeID: "node.store-execution", Status: "active"}},
		InterfaceNodes: []store.CatalogInterfaceNode{{
			ID: "node.store-execution", Method: "GET", Path: "/store-execution", Status: "active",
		}},
		WorkflowBindings: []store.CatalogWorkflowBinding{{
			WorkflowID: "workflow.store-execution", StepID: "store-step", NodeID: "node.store-execution", CaseID: "case.store-execution", Required: true, SortOrder: 1,
		}},
		TemplateConfigs: []store.CatalogTemplateConfig{{
			ID: "cfg.case.store-execution", TemplateID: "TPL-CASE-EXECUTION-V1", ScopeType: "api-case", ScopeID: "case.store-execution", Status: "active",
			ConfigJSON: `{"caseId":"case.store-execution","caseExecution":{"method":"GET","nodeId":"node.store-execution","path":"/store-execution","expectedHttpCodes":[200],"expectedResponseContains":["\"result_status\":\"S\""]}}`,
		}},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	bundle := profile.Bundle{
		ID:             "sample",
		Workflows:      []profile.Workflow{{ID: "workflow.store-execution"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.store-execution", Method: "GET", Path: "/store-execution"}},
		APICases:       []profile.APICase{{ID: "case.store-execution", NodeID: "node.store-execution"}},
		WorkflowBindings: []profile.WorkflowBinding{{
			WorkflowID: "workflow.store-execution", StepID: "store-step", NodeID: "node.store-execution", CaseID: "case.store-execution", Required: true, SortOrder: 1,
		}},
	}
	server := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: s}))
	defer server.Close()

	var created struct {
		ReportURL string `json:"reportUrl"`
	}
	postJSONInto(t, server.URL+"/api/cases/batch-runs", fmt.Sprintf(`{"requestId":"store-execution-assertion-001","workflowId":"workflow.store-execution","baseUrl":%q}`, target.URL), http.StatusAccepted, &created)
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if report.OK || report.Status != store.StatusFailed || report.Passed != 0 || report.Failed != 1 || !strings.Contains(report.Cases[0].Error, "response did not contain") {
		t.Fatalf("store execution assertion report = %#v", report)
	}
}

func TestServerBatchRunAppliesWorkflowStepExportsAsOverrides(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	var appliedAmount string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/trial":
			_, _ = w.Write([]byte(`{"total_amount":500000}`))
		case "/apply":
			var request map[string]any
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode apply request: %v", err)
			}
			appliedAmount = strings.TrimSpace(fmt.Sprint(request["requested_amount"]))
			if appliedAmount != "500000" {
				_, _ = w.Write([]byte(`{"result_status":"F"}`))
				return
			}
			_, _ = w.Write([]byte(`{"result_status":"S"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer target.Close()

	now := time.Now().UTC()
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "sample",
		IndexedAt: now,
		Workflows: []store.CatalogWorkflow{{ID: "workflow.exports", DisplayName: "Export Workflow"}},
		APICases: []store.CatalogAPICase{
			{ID: "case.trial", DisplayName: "Trial", NodeID: "node.trial", Status: "active"},
			{ID: "case.apply", DisplayName: "Apply", NodeID: "node.apply", Status: "active"},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.trial", Method: "GET", Path: "/trial", Status: "active"},
			{ID: "node.apply", Method: "POST", Path: "/apply", Status: "active"},
		},
		WorkflowBindings: []store.CatalogWorkflowBinding{
			{WorkflowID: "workflow.exports", StepID: "trial", NodeID: "node.trial", CaseID: "case.trial", Required: true, SortOrder: 1},
			{WorkflowID: "workflow.exports", StepID: "apply", NodeID: "node.apply", CaseID: "case.apply", Required: true, SortOrder: 2},
		},
		TemplateConfigs: []store.CatalogTemplateConfig{
			{
				ID: "cfg.trial", TemplateID: "TPL-CASE-EXECUTION-V1", WorkflowID: "workflow.exports", ScopeType: "step", ScopeID: "trial", Status: "active",
				ConfigJSON: `{"caseId":"case.trial","caseExecution":{"method":"GET","nodeId":"node.trial","path":"/trial","expectedHttpCodes":[200]},"exports":[{"from":"responseBody","name":"requested_amount","path":"total_amount"}]}`,
			},
			{
				ID: "cfg.apply", TemplateID: "TPL-CASE-EXECUTION-V1", WorkflowID: "workflow.exports", ScopeType: "step", ScopeID: "apply", Status: "active",
				ConfigJSON: `{"caseId":"case.apply","caseExecution":{"method":"POST","nodeId":"node.apply","path":"/apply","body":{"requested_amount":"{{override:requested_amount|}}"},"expectedHttpCodes":[200],"expectedResponseContains":["\"result_status\":\"S\""]}}`,
			},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}

	bundle := profile.Bundle{
		ID:             "sample",
		Workflows:      []profile.Workflow{{ID: "workflow.exports"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.trial", Method: "GET", Path: "/trial"}, {ID: "node.apply", Method: "POST", Path: "/apply"}},
		APICases:       []profile.APICase{{ID: "case.trial", NodeID: "node.trial"}, {ID: "case.apply", NodeID: "node.apply"}},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.exports", StepID: "trial", NodeID: "node.trial", CaseID: "case.trial", Required: true, SortOrder: 1},
			{WorkflowID: "workflow.exports", StepID: "apply", NodeID: "node.apply", CaseID: "case.apply", Required: true, SortOrder: 2},
		},
	}
	server := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: s}))
	defer server.Close()

	var created struct {
		ReportURL string `json:"reportUrl"`
	}
	postJSONInto(t, server.URL+"/api/cases/batch-runs", fmt.Sprintf(`{"requestId":"workflow-exports-001","workflowId":"workflow.exports","baseUrl":%q}`, target.URL), http.StatusAccepted, &created)
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Passed != 2 || appliedAmount != "500000" {
		t.Fatalf("workflow export report = %#v appliedAmount=%q", report, appliedAmount)
	}
}

func TestServerStartsEnvironmentAcceptanceRunWithHealthSummary(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "request.env.acceptance")
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"ready":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer target.Close()

	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode provider request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(payload.Query, "queryBasicTraces"):
			_, _ = w.Write([]byte(`{"data":{"queryBasicTraces":{"traces":[{"endpointNames":["GET:/v1/env-acceptance"],"duration":80,"start":"2026-05-20 0320","isError":false,"traceIds":["trace.env.acceptance"]}]}}}`))
		case strings.Contains(payload.Query, "queryTrace"):
			_, _ = w.Write([]byte(`{"data":{"queryTrace":{"spans":[{"traceId":"trace.env.acceptance","segmentId":"segment.entry","spanId":0,"parentSpanId":-1,"refs":[],"serviceCode":"service.entry","endpointName":"/v1/env-acceptance","type":"Entry","component":"Tomcat"},{"traceId":"trace.env.acceptance","segmentId":"segment.worker","spanId":0,"parentSpanId":-1,"refs":[{"traceId":"trace.env.acceptance","parentSegmentId":"segment.entry","parentSpanId":0,"type":"CrossProcess"}],"serviceCode":"service.worker","endpointName":"GET:/v1/env-acceptance","type":"Entry","component":"Server"}]}}}`))
		default:
			t.Fatalf("unexpected provider query: %s", payload.Query)
		}
	}))
	defer provider.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case-env-acceptance.json")
	if err := os.WriteFile(casePath, []byte(`{
  "id": "case.env.acceptance",
  "title": "Environment Acceptance Step",
  "request": {"method": "GET", "path": "/v1/env-acceptance"},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`), 0o644); err != nil {
		t.Fatalf("write api case: %v", err)
	}
	bundle := profile.Bundle{
		ID:             "sample",
		Workflows:      []profile.Workflow{{ID: "workflow.env.acceptance"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.env.acceptance"}},
		APICases: []profile.APICase{{
			ID:          "case.env.acceptance",
			NodeID:      "node.env.acceptance",
			CasePath:    casePath,
			BaseURL:     target.URL,
			EvidenceDir: filepath.Join(dir, "evidence"),
		}},
		WorkflowBindings: []profile.WorkflowBinding{{
			WorkflowID: "workflow.env.acceptance",
			StepID:     "step.env.acceptance",
			NodeID:     "node.env.acceptance",
			CaseID:     "case.env.acceptance",
			Required:   true,
			SortOrder:  1,
		}},
	}
	server := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: s, TraceGraphQLURL: provider.URL}))
	defer server.Close()

	registered := postJSONResponse(t, server.URL+"/api/environments", fmt.Sprintf(`{
  "id": "env.acceptance",
  "verificationWorkflowId": "workflow.env.acceptance"
}`), http.StatusOK)
	if registered["ok"] != true {
		t.Fatalf("register environment = %#v", registered)
	}
	if err := s.ReplaceEnvironmentComponentGraph(ctx, "env.acceptance", store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{
			{
				ComponentID:     "service.gateway",
				Kind:            "app",
				Role:            "business-service",
				ComposeService:  "service-gateway",
				Required:        true,
				HealthCheckJSON: fmt.Sprintf(`{"kind":"url","url":%q}`, target.URL+"/health"),
			},
		},
	}); err != nil {
		t.Fatalf("replace component graph: %v", err)
	}

	started := postJSONResponse(t, server.URL+"/api/environments/env.acceptance/acceptance-runs", `{"requestId":"env-acceptance-001"}`, http.StatusAccepted)
	reportURL := fmt.Sprint(started["reportUrl"])
	if started["environmentId"] != "env.acceptance" || started["workflowId"] != "workflow.env.acceptance" || reportURL == "" {
		t.Fatalf("environment acceptance start = %#v", started)
	}
	report := waitAPICaseBatchReport(t, server.URL+reportURL)
	if !report.Acceptance.OK || report.Acceptance.HealthSummary.Total != 1 || report.Acceptance.HealthSummary.Passed != 1 || len(report.Acceptance.NodeHealth) != 1 || !report.Acceptance.NodeHealth[0].OK {
		t.Fatalf("environment acceptance health summary = %#v", report.Acceptance)
	}
	env, err := s.GetEnvironment(ctx, "env.acceptance")
	if err != nil {
		t.Fatalf("get environment after acceptance: %v", err)
	}
	if env.Status != "verified-ready" || env.LastVerificationRunID != report.BatchRunID || env.LastVerificationStatus != store.StatusPassed || !env.EvidenceComplete || !env.TopologyComplete {
		t.Fatalf("environment after acceptance = %#v", env)
	}
	batchRun, err := s.GetRun(ctx, report.BatchRunID)
	if err != nil {
		t.Fatalf("get batch run after acceptance: %v", err)
	}
	if batchRun.EnvironmentID != "env.acceptance" {
		t.Fatalf("batch run environment id = %#v", batchRun)
	}
	topologies, err := s.ListTraceTopologies(ctx, report.BatchRunID)
	if err != nil {
		t.Fatalf("list batch topology: %v", err)
	}
	if len(topologies) != 1 || topologies[0].WorkflowRunID != report.BatchRunID || topologies[0].StepID != "step.env.acceptance" {
		t.Fatalf("batch topology copies = %#v", topologies)
	}

	restarted := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: s}))
	defer restarted.Close()
	persisted := decodeJSONResponse(t, restarted.URL+reportURL, http.StatusOK)
	if persisted["environmentId"] != "env.acceptance" || persisted["batchRunId"] != report.BatchRunID {
		t.Fatalf("persisted environment acceptance report = %#v", persisted)
	}
}
