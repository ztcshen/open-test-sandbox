package controlplane_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/domain/profile"
	"agent-testbench/internal/store"
)

type apiCaseBatchReportForTest struct {
	OK                   bool   `json:"ok"`
	BatchRunID           string `json:"batchRunId"`
	Status               string `json:"status"`
	WorkflowID           string `json:"workflowId"`
	HTMLReportPath       string `json:"htmlReportPath"`
	HTMLReportURL        string `json:"htmlReportUrl"`
	JUnitReportPath      string `json:"junitReportPath"`
	JUnitReportURL       string `json:"junitReportUrl"`
	ArtifactManifestPath string `json:"artifactManifestPath"`
	ArtifactManifestURL  string `json:"artifactManifestUrl"`
	Completed            int    `json:"completed"`
	Total                int    `json:"total"`
	Passed               int    `json:"passed"`
	Failed               int    `json:"failed"`
	Acceptance           struct {
		OK               bool   `json:"ok"`
		TemplateID       string `json:"templateId"`
		WorkflowID       string `json:"workflowId"`
		ExpectedSteps    int    `json:"expectedSteps"`
		CompletedSteps   int    `json:"completedSteps"`
		PassedSteps      int    `json:"passedSteps"`
		TopologyProvider string `json:"topologyProvider"`
		HealthSummary    struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
			Failed int `json:"failed"`
		} `json:"healthSummary"`
		NodeHealth []struct {
			ID     string `json:"id"`
			Kind   string `json:"kind"`
			URL    string `json:"url"`
			OK     bool   `json:"ok"`
			Status string `json:"status"`
		} `json:"nodeHealth"`
		Steps []struct {
			StepID           string `json:"stepId"`
			CaseID           string `json:"caseId"`
			Status           string `json:"status"`
			ElapsedMs        int64  `json:"elapsedMs"`
			EvidenceComplete bool   `json:"evidenceComplete"`
			TopologyComplete bool   `json:"topologyComplete"`
		} `json:"steps"`
	} `json:"acceptance"`
	Nodes []struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Operation   string `json:"operation"`
		Method      string `json:"method"`
		Path        string `json:"path"`
	} `json:"nodes"`
	Cases []struct {
		CaseID          string `json:"caseId"`
		DisplayName     string `json:"displayName"`
		Scenario        string `json:"scenario"`
		NodeID          string `json:"nodeId"`
		NodeDisplayName string `json:"nodeDisplayName"`
		Operation       string `json:"operation"`
		Method          string `json:"method"`
		Path            string `json:"path"`
		StepID          string `json:"stepId"`
		CaseRunID       string `json:"caseRunId"`
		Status          string `json:"status"`
		RunID           string `json:"runId"`
		ViewerURL       string `json:"viewerUrl"`
		DetailURL       string `json:"detailUrl"`
		ElapsedMs       int64  `json:"elapsedMs"`
		Error           string `json:"error"`
	} `json:"cases"`
}

func waitAPICaseBatchReport(t *testing.T, reportURL string) apiCaseBatchReportForTest {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(reportURL)
		if err != nil {
			t.Fatalf("get batch report: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("batch report status = %d body=%s", resp.StatusCode, raw)
		}
		var report apiCaseBatchReportForTest
		if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
			resp.Body.Close()
			t.Fatalf("decode batch report: %v", err)
		}
		resp.Body.Close()
		if report.Status != store.StatusRunning || time.Now().After(deadline) {
			return report
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func loadEmptyProfile(t *testing.T) profile.Bundle {
	t.Helper()
	return profile.EmptyBundle()
}

func writeEmptyProfileBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir empty profile: %v", err)
	}
	raw := `{
  "id": "empty",
  "displayName": "Empty Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`
	if err := os.WriteFile(filepath.Join(dir, "profile.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write empty profile: %v", err)
	}
	return dir
}

func createLegacyRuntimeDB(t *testing.T, path string) {
	t.Helper()
	statement := `
create table workflow_runs (
  id integer primary key,
  workflow_id text not null,
  status text not null,
  summary_json text not null default '',
  created_at text not null
);
create table interface_node_case_run (
  id integer primary key,
  node_id text not null,
  case_id text not null,
  run_id text not null,
  status text not null,
  failure_kind text not null default '',
  failure_reason text not null default '',
  evidence_path text not null default '',
  elapsed_ms integer not null default 0,
  summary_json text not null default '',
  created_at text not null
);
insert into workflow_runs(id, workflow_id, status, summary_json, created_at)
values (7, 'workflow.alpha', 'passed', '{"steps":1}', '2026-05-14T01:02:03Z');
insert into interface_node_case_run(id, node_id, case_id, run_id, status, evidence_path, summary_json, created_at)
values (11, 'node.alpha', 'case.alpha', 'case-run-parent', 'failed', '.runtime/cases/case-run-parent', '{"failure":"expected"}', '2026-05-14T01:03:03Z');
`
	cmd := exec.Command("sqlite3", path, statement)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create legacy db: %v\n%s", err, out)
	}
}

func decodeJSONResponse(t *testing.T, url string, wantStatus int) map[string]any {
	t.Helper()
	var payload map[string]any
	getJSONInto(t, url, wantStatus, &payload)
	return payload
}

func postJSONResponse(t *testing.T, url string, body string, wantStatus int) map[string]any {
	t.Helper()
	var payload map[string]any
	postJSONInto(t, url, body, wantStatus, &payload)
	return payload
}

func getJSONInto(t *testing.T, url string, wantStatus int, target any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	decodeResponseJSON(t, url, resp, wantStatus, target)
}

func postJSONInto(t *testing.T, url string, body string, wantStatus int, target any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	decodeResponseJSON(t, url, resp, wantStatus, target)
}

func decodeResponseJSON(t *testing.T, label string, resp *http.Response, wantStatus int, target any) {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", label, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s status = %d body=%s", label, resp.StatusCode, raw)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode %s: %v body=%s", label, err, raw)
	}
}

func writeAuditSampleProfile(t *testing.T) string {
	t.Helper()
	profileDir := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("create profile dir: %v", err)
	}
	raw := `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"}],
  "requestTemplates": [],
  "caseDependencies": [{"id":"dependency.alpha","caseId":"case.alpha","fixtureId":"fixture.missing"}],
  "workflowBindings": [],
  "fixtures": []
}`
	if err := os.WriteFile(filepath.Join(profileDir, "profile.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write audit sample profile: %v", err)
	}
	return profileDir
}

func writeWorkbenchSampleProfile(t *testing.T) string {
	t.Helper()
	profileDir := filepath.Join(t.TempDir(), "profile")
	writeWorkbenchSampleProfileAt(t, profileDir)
	return profileDir
}

func writeWorkbenchSampleProfileAt(t *testing.T, profileDir string) {
	t.Helper()
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("create profile dir: %v", err)
	}
	raw := `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha","kind":"http"}],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha","serviceId":"service.alpha"}],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"}],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"step.alpha","nodeId":"node.alpha","caseId":"case.alpha","required":true}],
  "fixtures": []
}`
	if err := os.WriteFile(filepath.Join(profileDir, "profile.json"), []byte(raw), 0o644); err != nil {
		t.Fatalf("write workbench sample profile: %v", err)
	}
}

type failingListRunsStore struct {
	store.Store
}

func (failingListRunsStore) ListRuns(context.Context) ([]store.Run, error) {
	return nil, errors.New("list runs failed")
}

type latestCaseRunCatalogStore struct {
	store.Store
	catalog    store.ProfileCatalog
	latest     []store.APICaseRun
	readModels map[string]store.ReadModel
}

func (s latestCaseRunCatalogStore) GetProfileCatalog(context.Context) (store.ProfileCatalog, error) {
	return s.catalog, nil
}

func (s latestCaseRunCatalogStore) GetReadModel(_ context.Context, _ string, key string) (store.ReadModel, error) {
	if s.readModels != nil {
		if model, ok := s.readModels[key]; ok {
			return model, nil
		}
	}
	return store.ReadModel{}, store.ErrNotFound
}

func (s latestCaseRunCatalogStore) ListRuns(context.Context) ([]store.Run, error) {
	return nil, errors.New("full run scan should not be used")
}

func (s latestCaseRunCatalogStore) ListLatestAPICaseRuns(context.Context) ([]store.APICaseRun, error) {
	return s.latest, nil
}

type interfaceNodeCaseRunCatalogStore struct {
	store.Store
	catalog store.ProfileCatalog
	records []store.APICaseRunRecord
}

func (s interfaceNodeCaseRunCatalogStore) GetProfileCatalog(context.Context) (store.ProfileCatalog, error) {
	return s.catalog, nil
}

func (s interfaceNodeCaseRunCatalogStore) GetReadModel(context.Context, string, string) (store.ReadModel, error) {
	return store.ReadModel{}, store.ErrNotFound
}

func (s interfaceNodeCaseRunCatalogStore) ListRuns(context.Context) ([]store.Run, error) {
	return nil, errors.New("full run scan should not be used")
}

func (s interfaceNodeCaseRunCatalogStore) ListAPICaseRunRecordsForCaseIDs(context.Context, []string) ([]store.APICaseRunRecord, error) {
	return s.records, nil
}

func (s interfaceNodeCaseRunCatalogStore) ListTraceTopologies(context.Context, string) ([]store.TraceTopology, error) {
	return []store.TraceTopology{}, nil
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return string(raw)
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
}

func hasJSONCheck(items []any, name string) bool {
	for _, item := range items {
		check, ok := item.(map[string]any)
		if ok && check["name"] == name && check["ok"] == true {
			return true
		}
	}
	return false
}

func hasJSONFailedCheck(items []any, name string) bool {
	for _, item := range items {
		check, ok := item.(map[string]any)
		if ok && check["name"] == name && check["ok"] == false {
			return true
		}
	}
	return false
}

func jsonStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func sqliteCountRows(t *testing.T, dbPath string, table string) int {
	t.Helper()
	out, err := exec.Command("sqlite3", dbPath, "select count(*) from "+table+";").CombinedOutput()
	if err != nil {
		t.Fatalf("count %s: %v: %s", table, err, out)
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("parse %s count %q: %v", table, out, err)
	}
	return value
}
