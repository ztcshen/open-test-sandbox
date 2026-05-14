package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/sqlite"
)

func TestStoreMigrateAndStatusCommands(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")

	initial := runCLI(t, "store", "status", "--store-url", dbPath)
	if !strings.Contains(initial, "Version: 0") || !strings.Contains(initial, "Pending: 1") {
		t.Fatalf("initial status output = %q", initial)
	}

	migrated := runCLI(t, "store", "migrate", "--store-url", dbPath)
	if !strings.Contains(migrated, "Migrated store to version 1") {
		t.Fatalf("migrate output = %q", migrated)
	}

	current := runCLI(t, "store", "status", "--store-url", dbPath)
	if !strings.Contains(current, "Version: 1") || !strings.Contains(current, "Pending: 0") {
		t.Fatalf("current status output = %q", current)
	}
}

func TestStoreCommandsRejectUnsupportedBackendURLs(t *testing.T) {
	out := runCLIFails(t, "store", "status", "--store-url", "postgres://localhost/open_test_sandbox")
	if !strings.Contains(out, "unsupported store backend") || !strings.Contains(out, "sqlite://") {
		t.Fatalf("unsupported backend output = %q", out)
	}
}

func TestProfileInspectCommand(t *testing.T) {
	out := runCLI(t, "profile", "inspect", "--profile", "../../profiles/empty")
	for _, want := range []string{"Profile: empty", "Display Name: Empty Profile", "Workflows: 0", "API Cases: 0", "Request Templates: 0", "Case Dependencies: 0", "Workflow Bindings: 0"} {
		if !strings.Contains(out, want) {
			t.Fatalf("profile inspect output missing %q: %q", want, out)
		}
	}
}

func TestProfileImportCommandIndexesBundleInStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")

	out := runCLI(t, "profile", "import", "--from", "../../profiles/empty", "--store-url", dbPath)
	if !strings.Contains(out, "Imported profile: empty") {
		t.Fatalf("profile import output = %q", out)
	}

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: dbPath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	index, err := s.GetProfileIndex(context.Background(), "empty")
	if err != nil {
		t.Fatalf("get profile index: %v", err)
	}
	if index.BundlePath == "" || !strings.HasPrefix(index.BundleDigest, "sha256:") {
		t.Fatalf("profile index = %#v", index)
	}
}

func TestBaselineGateCommandsSetAndGetState(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")

	out := runCLI(t, "baseline", "set", "--store-url", storePath, "--profile", "sample", "--subject", "workflow.alpha", "--status", "passed", "--required")
	if !strings.Contains(out, "Baseline Gate: sample workflow.alpha") || !strings.Contains(out, "Status: passed") {
		t.Fatalf("baseline set output = %q", out)
	}

	out = runCLI(t, "baseline", "get", "--store-url", storePath, "--profile", "sample", "--subject", "workflow.alpha")
	for _, want := range []string{"Baseline Gate: sample workflow.alpha", "Status: passed", "Required: true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("baseline get output missing %q: %q", want, out)
		}
	}
}

func TestBaselineGetCommandRejectsMissingGate(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "store.sqlite")

	out := runCLIFails(t, "baseline", "get", "--store-url", storePath, "--profile", "sample", "--subject", "workflow.missing")
	if !strings.Contains(out, "baseline gate not found") || !strings.Contains(out, "sample workflow.missing") {
		t.Fatalf("missing baseline gate output = %q", out)
	}
}

func TestWorkflowPlanCommandPrintsBoundSteps(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowProfile(t, dir)

	out := runCLI(t, "workflow", "plan", "--profile", dir, "--workflow", "workflow.alpha")

	for _, want := range []string{
		"Workflow: workflow.alpha",
		"Step: step.one",
		"Node: node.alpha",
		"Case: case.alpha",
		"Required: true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workflow plan output missing %q: %q", want, out)
		}
	}
}

func TestWorkflowPlanCommandRejectsMissingWorkflow(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowProfile(t, dir)

	out := runCLIFails(t, "workflow", "plan", "--profile", dir, "--workflow", "workflow.missing")
	if !strings.Contains(out, "workflow not found") || !strings.Contains(out, "workflow.missing") {
		t.Fatalf("missing workflow output = %q", out)
	}
}

func TestTemplateRenderCommandPrintsRequestPreview(t *testing.T) {
	dir := t.TempDir()
	writeTemplateProfile(t, dir)

	out := runCLI(t, "template", "render", "--profile", dir, "--template", "template.create", "--fixture", "fixture.item")

	var rendered struct {
		Method string         `json:"method"`
		Path   string         `json:"path"`
		Body   map[string]any `json:"body"`
	}
	if err := json.Unmarshal([]byte(out), &rendered); err != nil {
		t.Fatalf("decode template render output: %v\n%s", err, out)
	}
	if rendered.Method != "POST" || rendered.Path != "/v1/items/item-001" {
		t.Fatalf("rendered request identity = %#v", rendered)
	}
	if rendered.Body["id"] != "item-001" || rendered.Body["quantity"].(float64) != 3 {
		t.Fatalf("rendered request body = %#v", rendered.Body)
	}
}

func TestEvidenceImportCommandIndexesLegacyRuntime(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "evidence", "import", "--from", sourcePath, "--profile", "sample", "--store-url", storePath)
	if !strings.Contains(out, "Imported evidence index") || !strings.Contains(out, "Runs: 2") || !strings.Contains(out, "API Case Runs: 1") {
		t.Fatalf("evidence import output = %q", out)
	}
}

func TestEvidenceImportCommandCanEmitJSONReport(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "evidence", "import", "--from", sourcePath, "--profile", "sample", "--store-url", storePath, "--json")

	var report struct {
		SourcePath      string `json:"sourcePath"`
		StorePath       string `json:"storePath"`
		ProfileID       string `json:"profileId"`
		RunCount        int    `json:"runCount"`
		APICaseRunCount int    `json:"apiCaseRunCount"`
		EvidenceCount   int    `json:"evidenceCount"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode evidence import json report: %v\n%s", err, out)
	}
	if report.SourcePath != sourcePath || report.StorePath != storePath || report.ProfileID != "sample" {
		t.Fatalf("report paths/profile = %#v", report)
	}
	if report.RunCount != 2 || report.APICaseRunCount != 1 || report.EvidenceCount != 1 {
		t.Fatalf("report counts = %#v", report)
	}
}

func TestEvidenceListCommandPrintsStoreRecords(t *testing.T) {
	storePath := createStoredCaseRun(t, "case-run-004")

	out := runCLI(t, "evidence", "list", "--store-url", storePath)

	for _, want := range []string{"Run: case-run-004", "Case Run: case-run-004.case", "Case: case.alpha", "Evidence: response"} {
		if !strings.Contains(out, want) {
			t.Fatalf("evidence list output missing %q: %q", want, out)
		}
	}
}

func TestEvidenceListCommandCanEmitJSON(t *testing.T) {
	storePath := createStoredCaseRun(t, "case-run-005")

	out := runCLI(t, "evidence", "list", "--store-url", storePath, "--json")

	var report struct {
		Runs []struct {
			ID              string `json:"id"`
			APICaseRunCount int    `json:"apiCaseRunCount"`
			EvidenceCount   int    `json:"evidenceCount"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode evidence list json: %v\n%s", err, out)
	}
	if len(report.Runs) != 1 || report.Runs[0].ID != "case-run-005" {
		t.Fatalf("json runs = %#v", report.Runs)
	}
	if report.Runs[0].APICaseRunCount != 1 || report.Runs[0].EvidenceCount != 5 {
		t.Fatalf("json run counts = %#v", report.Runs[0])
	}
}

func TestEvidenceListCommandRejectsMissingRun(t *testing.T) {
	storePath := createStoredCaseRun(t, "case-run-006")

	out := runCLIFails(t, "evidence", "list", "--store-url", storePath, "--run", "case-run-missing")
	if !strings.Contains(out, "run not found") || !strings.Contains(out, "case-run-missing") {
		t.Fatalf("missing run output = %q", out)
	}
}

func TestCaseRunDryRunCommandWritesEvidence(t *testing.T) {
	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	evidenceDir := filepath.Join(dir, "evidence")

	out := runCLI(t, "case", "run", "--case", casePath, "--dry-run", "--run-id", "case-run-001", "--evidence-dir", evidenceDir)
	if !strings.Contains(out, "Case Run: case-run-001") || !strings.Contains(out, "Status: passed") {
		t.Fatalf("case run output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "case-run-001", "summary.json")); err != nil {
		t.Fatalf("summary evidence missing: %v", err)
	}
}

func TestCaseRunCommandExecutesHTTPCase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["id"] != "item-override" {
			t.Fatalf("request overrides = %#v", request)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	evidenceDir := filepath.Join(dir, "evidence")

	out := runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-002", "--evidence-dir", evidenceDir, "--override", "id=item-override")
	if !strings.Contains(out, "Case Run: case-run-002") || !strings.Contains(out, "Status: passed") {
		t.Fatalf("case run output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "case-run-002", "response.json")); err != nil {
		t.Fatalf("response evidence missing: %v", err)
	}
}

func TestCaseRunCommandIndexesStoreRecords(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	storePath := filepath.Join(dir, "store.sqlite")
	evidenceDir := filepath.Join(dir, "evidence")

	runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-003", "--evidence-dir", evidenceDir, "--store-url", storePath, "--profile", "sample")

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	run, err := s.GetRun(context.Background(), "case-run-003")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.ProfileID != "sample" || run.Status != "passed" {
		t.Fatalf("run = %#v", run)
	}
	if !run.FinishedAt.After(run.StartedAt) {
		t.Fatalf("run timing was not indexed: %#v", run)
	}
	var runSummary struct {
		RunID  string `json:"runId"`
		CaseID string `json:"caseId"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(run.SummaryJSON), &runSummary); err != nil {
		t.Fatalf("decode run summary: %v", err)
	}
	if runSummary.RunID != "case-run-003" || runSummary.CaseID != "case.alpha" || runSummary.Status != "passed" {
		t.Fatalf("run summary = %#v", runSummary)
	}
	caseRuns, err := s.ListAPICaseRuns(context.Background(), "case-run-003")
	if err != nil {
		t.Fatalf("list api case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].CaseID != "case.alpha" {
		t.Fatalf("case runs = %#v", caseRuns)
	}
	if !caseRuns[0].FinishedAt.After(caseRuns[0].StartedAt) {
		t.Fatalf("case run timing was not indexed: %#v", caseRuns[0])
	}
	var requestSummary struct {
		Method  string `json:"method"`
		Path    string `json:"path"`
		HasBody bool   `json:"hasBody"`
	}
	if err := json.Unmarshal([]byte(caseRuns[0].RequestSummaryJSON), &requestSummary); err != nil {
		t.Fatalf("decode request summary: %v", err)
	}
	if requestSummary.Method != "POST" || requestSummary.Path != "/v1/items" || !requestSummary.HasBody {
		t.Fatalf("request summary = %#v", requestSummary)
	}
	var assertionSummary struct {
		Status     string `json:"status"`
		ErrorCount int    `json:"errorCount"`
	}
	if err := json.Unmarshal([]byte(caseRuns[0].AssertionSummaryJSON), &assertionSummary); err != nil {
		t.Fatalf("decode assertion summary: %v", err)
	}
	if assertionSummary.Status != "passed" || assertionSummary.ErrorCount != 0 {
		t.Fatalf("assertion summary = %#v", assertionSummary)
	}
	records, err := s.ListEvidence(context.Background(), "case-run-003")
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("evidence records = %#v", records)
	}
	var responseSummary string
	for _, record := range records {
		if record.Kind == "response" {
			responseSummary = record.Summary
		}
	}
	var response struct {
		StatusCode int `json:"statusCode"`
		BodyBytes  int `json:"bodyBytes"`
	}
	if err := json.Unmarshal([]byte(responseSummary), &response); err != nil {
		t.Fatalf("decode response evidence summary: %v", err)
	}
	if response.StatusCode != http.StatusOK || response.BodyBytes == 0 {
		t.Fatalf("response evidence summary = %#v", response)
	}
}

func TestCaseIncompleteBatchesCommandReportsNotRunCases(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	alphaPath := filepath.Join(dir, "case.alpha.json")
	betaPath := filepath.Join(dir, "case.beta.json")
	writeAPICaseFile(t, alphaPath)
	writeFile(t, betaPath, `{
  "id": "case.beta",
  "title": "Read Item",
  "request": {"method": "GET", "path": "/v1/items/item-001"},
  "assertions": {"expectedStatusCodes": [200]}
}`)
	writeFile(t, filepath.Join(profileDir, "profile.json"), fmt.Sprintf(`{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","casePath":%q},
    {"id":"case.beta","displayName":"Case Beta","casePath":%q}
  ],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`, alphaPath, betaPath))

	storePath := filepath.Join(dir, "store.sqlite")
	runCLI(t, "case", "run", "--case", alphaPath, "--dry-run", "--run-id", "run-alpha", "--store-url", storePath, "--profile", "sample")

	out := runCLI(t, "case", "incomplete-batches", "--profile", profileDir, "--store-url", storePath)
	for _, want := range []string{"Incomplete API Cases: 1", "case.beta", "not-run", betaPath} {
		if !strings.Contains(out, want) {
			t.Fatalf("incomplete case output missing %q: %q", want, out)
		}
	}

	jsonOut := runCLI(t, "case", "incomplete-batches", "--profile", profileDir, "--store-url", storePath, "--json")
	var report struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
		Items []struct {
			ID      string `json:"id"`
			Reason  string `json:"reason"`
			Command string `json:"suggestedCommand"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("decode incomplete cases report: %v\n%s", err, jsonOut)
	}
	if !report.OK || report.Count != 1 || len(report.Items) != 1 {
		t.Fatalf("incomplete cases report = %#v", report)
	}
	if report.Items[0].ID != "case.beta" || report.Items[0].Reason != "not-run" {
		t.Fatalf("incomplete case item = %#v", report.Items[0])
	}
	if !strings.Contains(report.Items[0].Command, betaPath) {
		t.Fatalf("suggested command = %q", report.Items[0].Command)
	}
}

func TestServeHandlerUsesConfiguredStore(t *testing.T) {
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run.alpha",
		ProfileID:    "empty",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusPassed,
		EvidenceRoot: ".runtime/evidence/run.alpha",
		SummaryJSON:  `{"steps":[{"stepId":"step.alpha","ok":true}]}`,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	handler, cleanup, err := serveHandlerFromArgs([]string{
		"--profile", "../../profiles/empty",
		"--store-url", storePath,
	})
	if err != nil {
		t.Fatalf("build serve handler: %v", err)
	}
	defer cleanup()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("runs status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "run.alpha") {
		t.Fatalf("serve handler did not use configured store: %s", rec.Body.String())
	}
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func runCLIFails(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("go run . %s unexpectedly succeeded:\n%s", strings.Join(args, " "), out)
	}
	return string(out)
}

func createStoredCaseRun(t *testing.T, runID string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	storePath := filepath.Join(dir, "store.sqlite")
	evidenceDir := filepath.Join(dir, "evidence")

	runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", runID, "--evidence-dir", evidenceDir, "--store-url", storePath, "--profile", "sample")
	return storePath
}

func writeAPICaseFile(t *testing.T, path string) {
	t.Helper()
	raw := []byte(`{
  "id": "case.alpha",
  "title": "Create Item",
  "request": {
    "method": "POST",
    "path": "/v1/items",
    "headers": {"Content-Type": "application/json"},
    "body": {"id": "item-001"}
  },
  "assertions": {
    "expectedStatusCodes": [200],
    "responseContains": ["created"]
  }
}`)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write api case: %v", err)
	}
}

func writeWorkflowProfile(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "workflows", "workflow.json"), `{"id":"workflow.alpha","displayName":"Workflow Alpha"}`)
	writeFile(t, filepath.Join(dir, "interface-nodes", "node.json"), `{"id":"node.alpha","displayName":"Node Alpha"}`)
	writeFile(t, filepath.Join(dir, "cases", "case.json"), `{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"}`)
	writeFile(t, filepath.Join(dir, "workflow-bindings", "binding.json"), `{"workflowId":"workflow.alpha","stepId":"step.one","nodeId":"node.alpha","caseId":"case.alpha","required":true}`)
}

func writeTemplateProfile(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "request-templates", "template.json"), `{
  "id": "template.create",
  "method": "POST",
  "path": "/v1/items/{{.itemId}}",
  "templateJson": "{\"id\":\"{{.itemId}}\",\"quantity\":{{.quantity}}}"
}`)
	writeFile(t, filepath.Join(dir, "fixtures", "fixture.json"), `{
  "id": "fixture.item",
  "kind": "json",
  "dataJson": "{\"itemId\":\"item-001\",\"quantity\":3}"
}`)
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
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
