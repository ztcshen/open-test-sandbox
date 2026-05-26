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

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/server/controlplane"
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func openAcceptanceRouteStore(t *testing.T, ctx context.Context) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "sandbox.sqlite")})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return s
}

func newEnvironmentAcceptanceTarget(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "request.env.acceptance")
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"ready":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
}

func newEnvironmentAcceptanceTraceProvider(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}

func environmentAcceptanceBundle(t *testing.T, targetURL string) profile.Bundle {
	t.Helper()
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
	return profile.Bundle{
		ID:             "sample",
		Workflows:      []profile.Workflow{{ID: "workflow.env.acceptance"}},
		InterfaceNodes: []profile.InterfaceNode{{ID: "node.env.acceptance"}},
		APICases: []profile.APICase{{
			ID: "case.env.acceptance", NodeID: "node.env.acceptance", CasePath: casePath,
			BaseURL: targetURL, EvidenceDir: filepath.Join(dir, "evidence"),
		}},
		WorkflowBindings: []profile.WorkflowBinding{{
			WorkflowID: "workflow.env.acceptance", StepID: "step.env.acceptance",
			NodeID: "node.env.acceptance", CaseID: "case.env.acceptance", Required: true, SortOrder: 1,
		}},
	}
}

func registerEnvironmentAcceptance(t *testing.T, serverURL string) {
	t.Helper()
	registered := postJSONResponse(t, serverURL+"/api/environments", `{
  "id": "env.acceptance",
  "verificationWorkflowId": "workflow.env.acceptance"
}`, http.StatusOK)
	if registered["ok"] != true {
		t.Fatalf("register environment = %#v", registered)
	}
}

func replaceEnvironmentAcceptanceGraph(t *testing.T, ctx context.Context, s store.Store, targetURL string) {
	t.Helper()
	err := s.ReplaceEnvironmentComponentGraph(ctx, "env.acceptance", store.EnvironmentComponentGraph{
		Components: []store.EnvironmentComponent{{
			ComponentID: "service.gateway", Kind: "app", Role: "business-service",
			ComposeService: "service-gateway", Required: true,
			HealthCheckJSON: fmt.Sprintf(`{"kind":"url","url":%q}`, targetURL+"/health"),
		}},
	})
	if err != nil {
		t.Fatalf("replace component graph: %v", err)
	}
}

func startEnvironmentAcceptanceRun(t *testing.T, serverURL string) string {
	t.Helper()
	started := postJSONResponse(t, serverURL+"/api/environments/env.acceptance/acceptance-runs", `{"requestId":"env-acceptance-001"}`, http.StatusAccepted)
	reportURL := fmt.Sprint(started["reportUrl"])
	if started["environmentId"] != "env.acceptance" || started["workflowId"] != "workflow.env.acceptance" || reportURL == "" {
		t.Fatalf("environment acceptance start = %#v", started)
	}
	return reportURL
}

func requireEnvironmentAcceptanceHealth(t *testing.T, report apiCaseBatchReportForTest) {
	t.Helper()
	health := report.Acceptance.HealthSummary
	if !report.Acceptance.OK || health.Total != 1 || health.Passed != 1 || len(report.Acceptance.NodeHealth) != 1 || !report.Acceptance.NodeHealth[0].OK {
		t.Fatalf("environment acceptance health summary = %#v", report.Acceptance)
	}
}

func requireEnvironmentAcceptanceStoreState(t *testing.T, ctx context.Context, s store.Store, report apiCaseBatchReportForTest) {
	t.Helper()
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
}

func requirePersistedEnvironmentAcceptanceReport(t *testing.T, bundle profile.Bundle, s store.Store, reportURL, batchRunID string) {
	t.Helper()
	restarted := httptest.NewServer(controlplane.NewWithOptions(bundle, controlplane.Options{Runtime: s}))
	defer restarted.Close()
	persisted := decodeJSONResponse(t, restarted.URL+reportURL, http.StatusOK)
	if persisted["environmentId"] != "env.acceptance" || persisted["batchRunId"] != batchRunID {
		t.Fatalf("persisted environment acceptance report = %#v", persisted)
	}
}
