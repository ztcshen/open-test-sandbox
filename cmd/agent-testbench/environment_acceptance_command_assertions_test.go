package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type workflowAcceptanceStart struct {
	OK         bool   `json:"ok"`
	BatchRunID string `json:"batchRunId"`
	WorkflowID string `json:"workflowId"`
	Status     string `json:"status"`
}

type workflowAcceptanceReport struct {
	Acceptance struct {
		OK               bool   `json:"ok"`
		TemplateID       string `json:"templateId"`
		TopologyProvider string `json:"topologyProvider"`
	} `json:"acceptance"`
}

type caseBatchStart struct {
	OK         bool   `json:"ok"`
	BatchRunID string `json:"batchRunId"`
	Status     string `json:"status"`
	Total      int    `json:"total"`
}

type caseBatchReport struct {
	OK     bool   `json:"ok"`
	Status string `json:"status"`
	Total  int    `json:"total"`
	Passed int    `json:"passed"`
	Failed int    `json:"failed"`
}

type environmentAcceptanceStart struct {
	OK            bool   `json:"ok"`
	EnvironmentID string `json:"environmentId"`
	BatchRunID    string `json:"batchRunId"`
	WorkflowID    string `json:"workflowId"`
}

type environmentAcceptanceReport struct {
	Acceptance struct {
		OK            bool `json:"ok"`
		HealthSummary struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
		} `json:"healthSummary"`
	} `json:"acceptance"`
}

func newWorkflowAcceptanceCLIServer(t *testing.T, startPayload *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/cases/batch-runs":
			decodeAsyncStartPayload(t, r, startPayload)
			writeTestJSON(t, w, http.StatusAccepted, map[string]any{
				"ok": true, "batchRunId": "batch.acceptance.001", "requestId": "acceptance-001",
				"workflowId": "workflow.core-10", "status": "running", "total": 10,
				"reportUrl": "/api/cases/batch-runs/batch.acceptance.001",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/cases/batch-runs/batch.acceptance.001":
			writeTestJSON(t, w, http.StatusOK, map[string]any{
				"ok": true, "batchRunId": "batch.acceptance.001", "workflowId": "workflow.core-10", "status": "passed", "total": 10,
				"acceptance": map[string]any{
					"ok": true, "templateId": "environment.workflow.skywalking.v1", "workflowId": "workflow.core-10",
					"expectedSteps": 10, "completedSteps": 10, "passedSteps": 10, "failedSteps": 0, "topologyProvider": "skywalking",
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
}

func newCaseBatchCLIServer(t *testing.T, startPayload *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/cases/batch-runs":
			decodeAsyncStartPayload(t, r, startPayload)
			writeTestJSON(t, w, http.StatusAccepted, map[string]any{
				"ok": true, "batchRunId": "batch.case.001", "requestId": "case-batch-001",
				"status": "running", "total": 2, "reportUrl": "/api/cases/batch-runs/batch.case.001",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/cases/batch-runs/batch.case.001":
			writeTestJSON(t, w, http.StatusOK, map[string]any{
				"ok": true, "batchRunId": "batch.case.001", "status": "passed", "total": 2, "passed": 2, "failed": 0,
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
}

func newEnvironmentAcceptanceCLIServer(t *testing.T, startPayload *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/environments/env.team/acceptance-runs":
			decodeAsyncStartPayload(t, r, startPayload)
			writeTestJSON(t, w, http.StatusAccepted, map[string]any{
				"ok": true, "environmentId": "env.team", "batchRunId": "batch.env.acceptance.001",
				"requestId": "env-acceptance-001", "workflowId": "workflow.core-10", "status": "running",
				"reportUrl": "/api/environments/env.team/acceptance-runs/batch.env.acceptance.001",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/environments/env.team/acceptance-runs/batch.env.acceptance.001":
			writeTestJSON(t, w, http.StatusOK, map[string]any{
				"ok": true, "environmentId": "env.team", "batchRunId": "batch.env.acceptance.001",
				"workflowId": "workflow.core-10", "status": "passed",
				"acceptance": map[string]any{
					"ok": true, "templateId": "environment.workflow.skywalking.v1", "workflowId": "workflow.core-10",
					"topologyProvider": "skywalking", "healthSummary": map[string]any{"total": 1, "passed": 1, "failed": 0},
				},
			})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
}

func decodeAsyncStartPayload(t *testing.T, r *http.Request, out *map[string]any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(out); err != nil {
		t.Fatalf("decode start payload: %v", err)
	}
}

func decodeCLIJSON[T any](t *testing.T, raw string) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode cli json: %v\n%s", err, raw)
	}
	return out
}

func assertWorkflowAcceptanceStart(t *testing.T, started workflowAcceptanceStart, startPayload map[string]any) {
	t.Helper()
	if !started.OK || started.BatchRunID != "batch.acceptance.001" || started.WorkflowID != "workflow.core-10" || started.Status != "running" {
		t.Fatalf("workflow acceptance start = %#v", started)
	}
	if startPayload["workflowId"] != "workflow.core-10" || startPayload["requestId"] != "acceptance-001" || startPayload["baseUrl"] != "http://127.0.0.1:18080" || startPayload["timeoutSeconds"] != float64(30) {
		t.Fatalf("workflow acceptance start payload = %#v", startPayload)
	}
}

func assertWorkflowAcceptanceReport(t *testing.T, report workflowAcceptanceReport) {
	t.Helper()
	if !report.Acceptance.OK || report.Acceptance.TemplateID != "environment.workflow.skywalking.v1" || report.Acceptance.TopologyProvider != "skywalking" {
		t.Fatalf("workflow acceptance report = %#v", report.Acceptance)
	}
}

func assertCaseBatchStart(t *testing.T, started caseBatchStart, startPayload map[string]any) {
	t.Helper()
	if !started.OK || started.BatchRunID != "batch.case.001" || started.Status != "running" || started.Total != 2 {
		t.Fatalf("case batch start = %#v", started)
	}
	caseIDs, _ := startPayload["caseIds"].([]any)
	if len(caseIDs) != 2 || caseIDs[0] != "case.alpha" || caseIDs[1] != "case.beta" || startPayload["requestId"] != "case-batch-001" || startPayload["baseUrl"] != "http://127.0.0.1:18080" || startPayload["timeoutSeconds"] != float64(30) {
		t.Fatalf("case batch start payload = %#v", startPayload)
	}
}

func assertCaseBatchReport(t *testing.T, report caseBatchReport) {
	t.Helper()
	if !report.OK || report.Status != "passed" || report.Total != 2 || report.Passed != 2 || report.Failed != 0 {
		t.Fatalf("case batch report = %#v", report)
	}
}

func assertEnvironmentAcceptanceStart(t *testing.T, started environmentAcceptanceStart, startPayload map[string]any) {
	t.Helper()
	if !started.OK || started.EnvironmentID != "env.team" || started.BatchRunID != "batch.env.acceptance.001" || started.WorkflowID != "workflow.core-10" {
		t.Fatalf("environment acceptance start = %#v", started)
	}
	if startPayload["requestId"] != "env-acceptance-001" || startPayload["baseUrl"] != "http://127.0.0.1:18080" {
		t.Fatalf("environment acceptance start payload = %#v", startPayload)
	}
}

func assertEnvironmentAcceptanceReport(t *testing.T, report environmentAcceptanceReport) {
	t.Helper()
	if !report.Acceptance.OK || report.Acceptance.HealthSummary.Total != 1 || report.Acceptance.HealthSummary.Passed != 1 {
		t.Fatalf("environment acceptance report = %#v", report.Acceptance)
	}
}
