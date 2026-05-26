package controlplane_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

func TestServerExposesCaseSuiteImpactPlan(t *testing.T) {
	_, s := openCaseSuiteRouteStore(t)
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.create", DisplayName: "Create Item", ServiceID: "service.alpha", Operation: "Create", Method: "POST", Path: "/v1/items"},
			{ID: "node.other", DisplayName: "Other", ServiceID: "service.beta", Operation: "Other", Method: "GET", Path: "/v1/other"},
		},
		Workflows: []profile.Workflow{
			{ID: "workflow.item", DisplayName: "Item Flow"},
		},
		WorkflowBindings: []profile.WorkflowBinding{
			{WorkflowID: "workflow.item", StepID: "create", NodeID: "node.create", CaseID: "case.create", SortOrder: 1},
		},
		APICases: []profile.APICase{
			{ID: "case.create", DisplayName: "Create default", NodeID: "node.create", CasePath: "cases/create.json", Tags: []string{"regression"}, Status: "active", SortOrder: 1},
			{ID: "case.other", DisplayName: "Other default", NodeID: "node.other", CasePath: "cases/other.json", Tags: []string{"regression"}, Status: "active", SortOrder: 2},
		},
	}
	server := serveCaseSuiteRouteBundle(t, bundle, s)

	endpoint := server.URL + "/api/case/suite-impact?signal=/v1/items&status=active&action=run&requestId=change-001&baseUrl=http://127.0.0.1:8080"
	payload := decodeJSONResponse(t, endpoint, http.StatusOK)
	if payload["ok"] != true {
		t.Fatalf("suite impact ok = %#v", payload)
	}
	counts := payload["counts"].(map[string]any)
	if counts["signals"] != float64(1) || counts["nodes"] != float64(1) || counts["workflows"] != float64(1) || counts["cases"] != float64(1) || counts["selected"] != float64(1) {
		t.Fatalf("suite impact counts = %#v", counts)
	}
	batch := payload["batchRequest"].(map[string]any)
	caseIDs := batch["caseIds"].([]any)
	if len(caseIDs) != 1 || caseIDs[0] != "case.create" || batch["requestId"] != "change-001" || batch["baseUrl"] != "http://127.0.0.1:8080" {
		t.Fatalf("suite impact batch request = %#v", batch)
	}
	cases := payload["cases"].([]any)
	if len(cases) != 1 {
		t.Fatalf("suite impact cases = %#v", cases)
	}
	impacted := cases[0].(map[string]any)
	if impacted["caseId"] != "case.create" || len(impacted["reasons"].([]any)) == 0 {
		t.Fatalf("suite impact case = %#v", impacted)
	}
}

func TestServerStartsCaseSuiteImpactBatchRun(t *testing.T) {
	_, s := openCaseSuiteRouteStore(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/items" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Sandbox-Trace-Endpoint", "/v1/env-acceptance")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer target.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case-create.json")
	if err := os.WriteFile(casePath, []byte(`{
  "id": "case.create",
  "title": "Create default",
  "request": {"method": "GET", "path": "/v1/items"},
  "assertions": {"expectedStatusCodes": [200], "responseContains": ["ok"]}
}`), 0o644); err != nil {
		t.Fatalf("write case: %v", err)
	}
	bundle := profile.Bundle{
		ID: "sample",
		InterfaceNodes: []profile.InterfaceNode{
			{ID: "node.create", DisplayName: "Create Item", ServiceID: "service.alpha", Operation: "Create", Method: "GET", Path: "/v1/items"},
		},
		APICases: []profile.APICase{
			{ID: "case.create", DisplayName: "Create default", NodeID: "node.create", CasePath: casePath, BaseURL: target.URL, EvidenceDir: filepath.Join(dir, "evidence"), Tags: []string{"regression"}, Status: "active", SortOrder: 1},
		},
	}
	server := serveCaseSuiteRouteBundle(t, bundle, s)

	body := `{"requestId":"change-004","signals":["/v1/items"],"status":"active","actions":["run"],"baseUrl":"` + target.URL + `"}`
	var created struct {
		OK         bool   `json:"ok"`
		BatchRunID string `json:"batchRunId"`
		ReportURL  string `json:"reportUrl"`
		Impact     struct {
			BatchRequest struct {
				CaseIDs []string `json:"caseIds"`
			} `json:"batchRequest"`
		} `json:"impact"`
	}
	postJSONInto(t, server.URL+"/api/case/suite-impact-runs", body, http.StatusAccepted, &created)
	if !created.OK || created.BatchRunID == "" || created.ReportURL == "" || strings.Join(created.Impact.BatchRequest.CaseIDs, ",") != "case.create" {
		t.Fatalf("suite impact run response = %#v", created)
	}
	report := waitAPICaseBatchReport(t, server.URL+created.ReportURL)
	if !report.OK || report.Status != store.StatusPassed || report.Passed != 1 || report.Failed != 0 || len(report.Cases) != 1 {
		t.Fatalf("suite impact batch report = %#v", report)
	}
}
