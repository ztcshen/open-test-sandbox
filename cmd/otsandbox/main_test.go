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
	"time"

	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/schema"
	"open-test-sandbox/internal/store/sqlite"
)

func TestStoreUpgradeAndStatusCommands(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")

	initial := runCLI(t, "store", "status", "--store-url", dbPath)
	if !strings.Contains(initial, "Version: 0") || !strings.Contains(initial, fmt.Sprintf("Pending: %d", schema.CurrentVersion)) {
		t.Fatalf("initial status output = %q", initial)
	}

	upgraded := runCLI(t, "store", "upgrade", "--store-url", dbPath)
	if !strings.Contains(upgraded, fmt.Sprintf("Upgraded store schema to version %d", schema.CurrentVersion)) {
		t.Fatalf("upgrade output = %q", upgraded)
	}

	current := runCLI(t, "store", "status", "--store-url", dbPath)
	if !strings.Contains(current, fmt.Sprintf("Version: %d", schema.CurrentVersion)) || !strings.Contains(current, "Pending: 0") {
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
	if got := sqliteScalar(t, dbPath, "select value from kv where key = 'active_profile_id';"); got != "empty" {
		t.Fatalf("active profile catalog index = %q", got)
	}
}

func TestConfigPublishCommandIndexesBundleInStore(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")

	out := runCLI(t, "config", "publish", "--from", "../../profiles/empty", "--store-url", dbPath, "--json")

	var report struct {
		ProfileID     string   `json:"profileId"`
		BundleDigest  string   `json:"bundleDigest"`
		ReadModels    []string `json:"readModels"`
		ConfigVersion struct {
			ID           string `json:"id"`
			ProfileID    string `json:"profileId"`
			BundleDigest string `json:"bundleDigest"`
			Active       bool   `json:"active"`
		} `json:"configVersion"`
		CatalogIndex struct {
			ProfileID string `json:"profileId"`
		} `json:"catalogIndex"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode config publish report: %v\n%s", err, out)
	}
	if report.ProfileID != "empty" || report.CatalogIndex.ProfileID != "empty" || !strings.HasPrefix(report.BundleDigest, "sha256:") {
		t.Fatalf("config publish report = %#v", report)
	}
	if report.ConfigVersion.ID == "" || report.ConfigVersion.ProfileID != "empty" || report.ConfigVersion.BundleDigest != report.BundleDigest || !report.ConfigVersion.Active {
		t.Fatalf("config version = %#v", report.ConfigVersion)
	}
	if strings.Join(report.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("config publish read models = %#v", report.ReadModels)
	}
	if got := sqliteScalar(t, dbPath, "select value from kv where key = 'active_profile_id';"); got != "empty" {
		t.Fatalf("active config profile = %q", got)
	}
	if got := sqliteScalar(t, dbPath, "select bundle_digest from config_versions where active = 1;"); got != report.BundleDigest {
		t.Fatalf("active config digest = %q, want %q", got, report.BundleDigest)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'empty' and model_key = 'interface-nodes';"); got != report.ConfigVersion.ID {
		t.Fatalf("interface nodes read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'empty' and model_key = 'catalog';"); got != report.ConfigVersion.ID {
		t.Fatalf("catalog read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
}

func TestConfigPublishCommandMaterializesInterfaceNodeDetailReadModels(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeInterfaceNodeCaseProfile(t)

	out := runCLI(t, "config", "publish", "--from", profileDir, "--store-url", dbPath, "--json")

	var report struct {
		ConfigVersion struct {
			ID string `json:"id"`
		} `json:"configVersion"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode config publish report: %v\n%s", err, out)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'sample' and model_key = 'interface-node:node.alpha';"); got != report.ConfigVersion.ID {
		t.Fatalf("interface node detail read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
	if got := sqliteScalar(t, dbPath, "select json_extract(payload_json, '$.source.kind') from config_read_model where profile_id = 'sample' and model_key = 'interface-node:node.alpha';"); got != "read-model" {
		t.Fatalf("interface node detail source kind = %q", got)
	}
}

func TestConfigPublishCommandMaterializesInterfaceNodeCoverageReadModels(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	profileDir := writeInterfaceNodeCoverageProfile(t)

	out := runCLI(t, "config", "publish", "--from", profileDir, "--store-url", dbPath, "--json")

	var report struct {
		ConfigVersion struct {
			ID string `json:"id"`
		} `json:"configVersion"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode config publish report: %v\n%s", err, out)
	}
	if got := sqliteScalar(t, dbPath, "select config_version_id from config_read_model where profile_id = 'sample' and model_key = 'interface-node-coverage:workflow.alpha';"); got != report.ConfigVersion.ID {
		t.Fatalf("interface node coverage read model version = %q, want %q", got, report.ConfigVersion.ID)
	}
	if got := sqliteScalar(t, dbPath, "select json_extract(payload_json, '$.source.kind') from config_read_model where profile_id = 'sample' and model_key = 'interface-node-coverage-gaps:workflow.alpha';"); got != "read-model" {
		t.Fatalf("interface node coverage gaps source kind = %q", got)
	}
}

func TestProfileImportCommandCanEmitJSONReport(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")

	out := runCLI(t, "profile", "import", "--from", "../../profiles/empty", "--store-url", dbPath, "--json")

	var report struct {
		ProfileID    string `json:"profileId"`
		BundlePath   string `json:"bundlePath"`
		BundleDigest string `json:"bundleDigest"`
		Counts       struct {
			Services         int `json:"services"`
			Workflows        int `json:"workflows"`
			InterfaceNodes   int `json:"interfaceNodes"`
			APICases         int `json:"apiCases"`
			RequestTemplates int `json:"requestTemplates"`
			CaseDependencies int `json:"caseDependencies"`
			WorkflowBindings int `json:"workflowBindings"`
			Fixtures         int `json:"fixtures"`
		} `json:"counts"`
		CatalogIndex struct {
			ProfileID   string `json:"profileId"`
			IndexedAt   string `json:"indexedAt"`
			StoreCounts struct {
				Services        int `json:"services"`
				Workflows       int `json:"workflows"`
				Templates       int `json:"templates"`
				TemplateConfigs int `json:"templateConfigs"`
			} `json:"counts"`
		} `json:"catalogIndex"`
		StorePath  string   `json:"storePath"`
		ImportedAt string   `json:"importedAt"`
		ReadModels []string `json:"readModels"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile import json: %v\n%s", err, out)
	}
	if report.ProfileID != "empty" || report.BundlePath != "../../profiles/empty" {
		t.Fatalf("report profile/path = %#v", report)
	}
	if !strings.HasPrefix(report.BundleDigest, "sha256:") || report.StorePath != dbPath || report.ImportedAt == "" {
		t.Fatalf("report digest/store/import time = %#v", report)
	}
	if report.Counts.Services != 0 || report.Counts.APICases != 0 || report.Counts.WorkflowBindings != 0 {
		t.Fatalf("report counts = %#v", report.Counts)
	}
	if report.CatalogIndex.ProfileID != "empty" || report.CatalogIndex.IndexedAt == "" {
		t.Fatalf("report catalog index identity = %#v", report.CatalogIndex)
	}
	if report.CatalogIndex.StoreCounts.Services != 0 || report.CatalogIndex.StoreCounts.Templates != 0 || report.CatalogIndex.StoreCounts.TemplateConfigs != 0 {
		t.Fatalf("report catalog index counts = %#v", report.CatalogIndex.StoreCounts)
	}
	if strings.Join(report.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("profile import read models = %#v", report.ReadModels)
	}
}

func TestInterfaceNodeCaseAuditReportsMissingExecutionConfigs(t *testing.T) {
	dir := writeInterfaceNodeCaseProfile(t)

	out := runCLI(t, "interface-node", "case", "audit", "--profile", dir, "--node", "node.alpha", "--json")

	var report struct {
		OK     bool   `json:"ok"`
		NodeID string `json:"nodeId"`
		Counts struct {
			Cases      int `json:"cases"`
			Configured int `json:"configured"`
			Missing    int `json:"missing"`
		} `json:"counts"`
		Missing []struct {
			CaseID string `json:"caseId"`
		} `json:"missing"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode interface node case audit json: %v\n%s", err, out)
	}
	if report.OK || report.NodeID != "node.alpha" || report.Counts.Cases != 2 || report.Counts.Configured != 1 || report.Counts.Missing != 1 {
		t.Fatalf("audit report = %#v", report)
	}
	if len(report.Missing) != 1 || report.Missing[0].CaseID != "case.beta" {
		t.Fatalf("missing cases = %#v", report.Missing)
	}
}

func TestInterfaceNodeCaseApplyMergesExecutionConfigsIntoProfileCatalog(t *testing.T) {
	dir := writeInterfaceNodeCaseProfile(t)
	requestPath := filepath.Join(t.TempDir(), "case-config.json")
	writeFile(t, requestPath, `{
  "templateConfigs": [
    {
      "id": "cfg.case.beta",
      "templateId": "case-execution",
      "nodeId": "node.alpha",
      "scopeType": "case",
      "scopeId": "case.beta",
      "title": "Case Beta execution",
      "status": "active",
      "sortOrder": 2,
      "config": {
        "caseId": "case.beta",
        "caseExecution": {
          "method": "GET",
          "nodeId": "service.alpha",
          "path": "/beta",
          "expectedHttpCodes": [200]
        }
      }
    }
  ]
}`)

	out := runCLI(t, "interface-node", "case", "apply", "--profile", dir, "--file", requestPath, "--json")

	var result struct {
		Applied int    `json:"applied"`
		Profile string `json:"profile"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode interface node case apply json: %v\n%s", err, out)
	}
	if result.Applied != 1 || result.Profile != dir {
		t.Fatalf("apply result = %#v", result)
	}
	audit := runCLI(t, "interface-node", "case", "audit", "--profile", dir, "--node", "node.alpha", "--json")
	var auditReport struct {
		OK     bool `json:"ok"`
		Counts struct {
			Missing int `json:"missing"`
		} `json:"counts"`
	}
	if err := json.Unmarshal([]byte(audit), &auditReport); err != nil {
		t.Fatalf("decode audit after apply: %v\n%s", err, audit)
	}
	if !auditReport.OK || auditReport.Counts.Missing != 0 {
		t.Fatalf("audit after apply = %s", audit)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "catalog.json"))
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	var catalog struct {
		TemplateConfigs []struct {
			ConfigJSON string `json:"configJson"`
		} `json:"templateConfigs"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("decode catalog after apply: %v\n%s", err, raw)
	}
	hasBeta := false
	for _, item := range catalog.TemplateConfigs {
		var config struct {
			CaseID string `json:"caseId"`
		}
		if err := json.Unmarshal([]byte(item.ConfigJSON), &config); err != nil {
			t.Fatalf("decode template config after apply: %v\n%s", err, item.ConfigJSON)
		}
		hasBeta = hasBeta || config.CaseID == "case.beta"
	}
	if !hasBeta || strings.Contains(string(raw), "store.sqlite") {
		t.Fatalf("catalog after apply = %s", raw)
	}
}

func TestProfileImportCommandCanAuditImportedProfile(t *testing.T) {
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
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
}`)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "profile", "import", "--from", profileDir, "--store-url", storePath, "--json", "--audit")

	var report struct {
		ProfileID string `json:"profileId"`
		Audit     *struct {
			OK         bool `json:"ok"`
			IssueCount int  `json:"issueCount"`
			Issues     []struct {
				Code      string `json:"code"`
				SubjectID string `json:"subjectId"`
			} `json:"issues"`
			Store *struct {
				ProfileIndexed bool `json:"profileIndexed"`
				DigestMatches  bool `json:"digestMatches"`
			} `json:"store,omitempty"`
		} `json:"audit,omitempty"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode audited profile import json: %v\n%s", err, out)
	}
	if report.ProfileID != "sample" || report.Audit == nil {
		t.Fatalf("report missing audit = %#v", report)
	}
	if report.Audit.OK || report.Audit.IssueCount != 2 || len(report.Audit.Issues) != 2 {
		t.Fatalf("audit summary = %#v", report.Audit)
	}
	if report.Audit.Issues[0].Code != "api-case-node-missing" || report.Audit.Issues[1].Code != "case-dependency-fixture-missing" {
		t.Fatalf("audit issues = %#v", report.Audit.Issues)
	}
	if report.Audit.Store == nil || !report.Audit.Store.ProfileIndexed || !report.Audit.Store.DigestMatches {
		t.Fatalf("audit store = %#v", report.Audit.Store)
	}

	text := runCLI(t, "profile", "import", "--from", profileDir, "--store-url", storePath, "--audit")
	for _, want := range []string{"Imported profile: sample", "Audit OK: false", "Audit Issues: 2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("audited text import output missing %q: %q", want, text)
		}
	}
}

func TestProfileAuditCommandEmitsJSONWithStoreState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
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
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha","casePath":%q},
    {"id":"case.beta","displayName":"Case Beta","nodeId":"node.alpha","casePath":%q}
  ],
  "requestTemplates": [{"id":"template.alpha","nodeId":"node.alpha","method":"POST","path":"/v1/items"}],
  "caseDependencies": [{"id":"dependency.beta","caseId":"case.beta","fixtureId":"fixture.missing"}],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"step.one","nodeId":"node.alpha","caseId":"case.beta","required":true}],
  "fixtures": []
}`, alphaPath, betaPath))

	storePath := filepath.Join(dir, "store.sqlite")
	runCLI(t, "profile", "import", "--from", profileDir, "--store-url", storePath)
	runCLI(t, "case", "run", "--case", alphaPath, "--base-url", server.URL, "--run-id", "run-alpha", "--store-url", storePath, "--profile", "sample")

	out := runCLI(t, "profile", "audit", "--profile", profileDir, "--store-url", storePath, "--json")

	var report struct {
		OK         bool `json:"ok"`
		IssueCount int  `json:"issueCount"`
		Issues     []struct {
			Code      string `json:"code"`
			SubjectID string `json:"subjectId"`
		} `json:"issues"`
		Store *struct {
			ProfileIndexed bool `json:"profileIndexed"`
			DigestMatches  bool `json:"digestMatches"`
			APICases       []struct {
				CaseID       string `json:"caseId"`
				HasPassed    bool   `json:"hasPassed"`
				LatestStatus string `json:"latestStatus"`
			} `json:"apiCases"`
		} `json:"store,omitempty"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode profile audit json: %v\n%s", err, out)
	}
	if report.OK || report.IssueCount != 1 || len(report.Issues) != 1 {
		t.Fatalf("audit report issues = %#v", report)
	}
	if report.Issues[0].Code != "case-dependency-fixture-missing" || report.Issues[0].SubjectID != "dependency.beta" {
		t.Fatalf("audit issue = %#v", report.Issues[0])
	}
	if report.Store == nil || !report.Store.ProfileIndexed || !report.Store.DigestMatches {
		t.Fatalf("audit store state = %#v", report.Store)
	}
	caseState := map[string]struct {
		HasPassed    bool
		LatestStatus string
	}{}
	for _, item := range report.Store.APICases {
		caseState[item.CaseID] = struct {
			HasPassed    bool
			LatestStatus string
		}{HasPassed: item.HasPassed, LatestStatus: item.LatestStatus}
	}
	if !caseState["case.alpha"].HasPassed || caseState["case.alpha"].LatestStatus != "passed" {
		t.Fatalf("case.alpha state = %#v", caseState["case.alpha"])
	}
	if caseState["case.beta"].HasPassed || caseState["case.beta"].LatestStatus != "" {
		t.Fatalf("case.beta state = %#v", caseState["case.beta"])
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

func TestWorkflowAuditCommandEmitsJSONWithScopedStoreState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	writeFile(t, filepath.Join(profileDir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"},
    {"id":"case.beta","displayName":"Case Beta","nodeId":"node.missing"}
  ],
  "requestTemplates": [{"id":"template.alpha","nodeId":"node.alpha","method":"POST","path":"/v1/items"}],
  "caseDependencies": [{"id":"dependency.beta","caseId":"case.beta","fixtureId":"fixture.missing"}],
  "workflowBindings": [
    {"workflowId":"workflow.alpha","stepId":"step.one","nodeId":"node.alpha","caseId":"case.alpha","required":true},
    {"workflowId":"workflow.alpha","stepId":"step.two","nodeId":"node.alpha","caseId":"case.beta","required":true}
  ],
  "fixtures": []
}`)
	storePath := filepath.Join(dir, "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	started := mustParseTime(t, "2026-01-02T03:04:05Z")
	finished := started.Add(2 * time.Second)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.workflow.001",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusFailed,
		StartedAt:  started,
		FinishedAt: finished,
		CreatedAt:  started,
		UpdatedAt:  finished,
	}); err != nil {
		t.Fatalf("create first workflow run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         "run.workflow.001.case.alpha",
		RunID:      "run.workflow.001",
		CaseID:     "case.alpha",
		Status:     store.StatusFailed,
		StartedAt:  started,
		FinishedAt: finished,
		CreatedAt:  started,
	}); err != nil {
		t.Fatalf("record first case run: %v", err)
	}
	laterStarted := started.Add(10 * time.Second)
	laterFinished := laterStarted.Add(3 * time.Second)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         "run.workflow.002",
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  laterStarted,
		FinishedAt: laterFinished,
		CreatedAt:  laterStarted,
		UpdatedAt:  laterFinished,
	}); err != nil {
		t.Fatalf("create second workflow run: %v", err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         "run.workflow.002.case.alpha",
		RunID:      "run.workflow.002",
		CaseID:     "case.alpha",
		Status:     store.StatusPassed,
		StartedAt:  laterStarted,
		FinishedAt: laterFinished,
		CreatedAt:  laterStarted,
	}); err != nil {
		t.Fatalf("record second case run: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	out := runCLI(t, "workflow", "audit", "--profile", profileDir, "--workflow", "workflow.alpha", "--store-url", storePath, "--json")

	var report struct {
		OK         bool   `json:"ok"`
		WorkflowID string `json:"workflowId"`
		IssueCount int    `json:"issueCount"`
		Issues     []struct {
			Code      string `json:"code"`
			SubjectID string `json:"subjectId"`
		} `json:"issues"`
		Store *struct {
			LatestRun *struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"latestRun"`
			BindingCases []struct {
				StepID       string `json:"stepId"`
				CaseID       string `json:"caseId"`
				HasPassed    bool   `json:"hasPassed"`
				LatestStatus string `json:"latestStatus"`
				LatestRunID  string `json:"latestRunId"`
			} `json:"bindingCases"`
		} `json:"store"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode workflow audit json: %v\n%s", err, out)
	}
	if report.OK || report.WorkflowID != "workflow.alpha" || report.IssueCount != 2 {
		t.Fatalf("workflow audit summary = %#v", report)
	}
	if len(report.Issues) != 2 || report.Issues[0].Code != "api-case-node-missing" || report.Issues[1].Code != "case-dependency-fixture-missing" {
		t.Fatalf("workflow audit issues = %#v", report.Issues)
	}
	if report.Store == nil || report.Store.LatestRun == nil || report.Store.LatestRun.ID != "run.workflow.002" || report.Store.LatestRun.Status != store.StatusPassed {
		t.Fatalf("latest workflow run = %#v", report.Store)
	}
	caseState := map[string]struct {
		HasPassed    bool
		LatestStatus string
		LatestRunID  string
	}{}
	for _, item := range report.Store.BindingCases {
		caseState[item.CaseID] = struct {
			HasPassed    bool
			LatestStatus string
			LatestRunID  string
		}{HasPassed: item.HasPassed, LatestStatus: item.LatestStatus, LatestRunID: item.LatestRunID}
	}
	if !caseState["case.alpha"].HasPassed || caseState["case.alpha"].LatestStatus != store.StatusPassed || caseState["case.alpha"].LatestRunID != "run.workflow.002" {
		t.Fatalf("case.alpha workflow state = %#v", caseState["case.alpha"])
	}
	if caseState["case.beta"].HasPassed || caseState["case.beta"].LatestStatus != "" || caseState["case.beta"].LatestRunID != "" {
		t.Fatalf("case.beta workflow state = %#v", caseState["case.beta"])
	}
}

func TestWorkflowAuditCommandPrintsTextSummary(t *testing.T) {
	dir := t.TempDir()
	writeWorkflowProfile(t, dir)

	out := runCLI(t, "workflow", "audit", "--profile", dir, "--workflow", "workflow.alpha")

	for _, want := range []string{
		"Workflow Audit: workflow.alpha",
		"OK: true",
		"Issues: 0",
		"Bindings: 1",
		"Binding: step.one Node: node.alpha Case: case.alpha Required: true",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("workflow audit output missing %q: %q", want, out)
		}
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

func TestCaseRunCommandWritesEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	evidenceDir := filepath.Join(dir, "evidence")

	out := runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-001", "--evidence-dir", evidenceDir)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()
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
	runCLI(t, "case", "run", "--case", alphaPath, "--base-url", server.URL, "--run-id", "run-alpha", "--store-url", storePath, "--profile", "sample")

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

func TestServeHandlerCanBootFromPublishedStoreCatalogWithoutProfilePath(t *testing.T) {
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	sourcePath := filepath.Join(t.TempDir(), "sources", "service-alpha", "main-4e8d26674209")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "team-alpha",
		Services: []store.CatalogService{
			{ID: "service.alpha", DisplayName: "Service Alpha", Kind: "http", SourcePath: sourcePath},
		},
		InterfaceNodes: []store.CatalogInterfaceNode{
			{ID: "node.alpha", DisplayName: "Node Alpha", ServiceID: "service.alpha", Operation: "create", Status: "active"},
		},
		APICases: []store.CatalogAPICase{
			{ID: "case.alpha", DisplayName: "Case Alpha", NodeID: "node.alpha", Status: "active"},
		},
	}); err != nil {
		t.Fatalf("replace profile catalog: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	handler, cleanup, err := serveHandlerFromArgs([]string{"--store-url", storePath})
	if err != nil {
		t.Fatalf("build serve handler from store catalog: %v", err)
	}
	defer cleanup()

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/interface-nodes", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("interface nodes status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Source struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"source"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode interface nodes payload: %v\n%s", err, rec.Body.String())
	}
	if payload.Source.ID != "team-alpha" || payload.Source.Kind != "store" || len(payload.Items) != 1 || payload.Items[0].ID != "node.alpha" {
		t.Fatalf("serve handler did not use published catalog: %#v", payload)
	}

	dashboard := httptest.NewRecorder()
	handler.ServeHTTP(dashboard, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if dashboard.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d body=%s", dashboard.Code, dashboard.Body.String())
	}
	if !strings.Contains(dashboard.Body.String(), sourcePath) || !strings.Contains(dashboard.Body.String(), "4e8d26674209") {
		t.Fatalf("dashboard did not use published runtime source: %s", dashboard.Body.String())
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

func sqliteScalar(t *testing.T, dbPath string, statement string) string {
	t.Helper()
	out, err := exec.Command("sqlite3", dbPath, statement).CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite scalar failed: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
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

func writeInterfaceNodeCaseProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha"}],
  "apiCases": [
    {"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"},
    {"id":"case.beta","displayName":"Case Beta","nodeId":"node.alpha"}
  ],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [],
  "fixtures": []
}`)
	writeFile(t, filepath.Join(dir, "catalog.json"), `{
  "schemaVersion": "1",
  "templateConfigs": [
    {
      "id": "cfg.case.alpha",
      "templateId": "case-execution",
      "nodeId": "node.alpha",
      "scopeType": "case",
      "scopeId": "case.alpha",
      "title": "Case Alpha execution",
      "status": "active",
      "sortOrder": 1,
      "configJson": "{\"caseId\":\"case.alpha\",\"caseExecution\":{\"method\":\"GET\",\"nodeId\":\"service.alpha\",\"path\":\"/alpha\",\"expectedHttpCodes\":[200]}}"
    }
  ]
}`)
	return dir
}

func writeInterfaceNodeCoverageProfile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "profile.json"), `{
  "id": "sample",
  "displayName": "Sample Profile",
  "services": [{"id":"service.alpha","displayName":"Service Alpha"}],
  "workflows": [{"id":"workflow.alpha","displayName":"Workflow Alpha"}],
  "interfaceNodes": [{"id":"node.alpha","displayName":"Node Alpha","serviceId":"service.alpha"}],
  "apiCases": [{"id":"case.alpha","displayName":"Case Alpha","nodeId":"node.alpha"}],
  "requestTemplates": [],
  "caseDependencies": [],
  "workflowBindings": [{"workflowId":"workflow.alpha","stepId":"step.alpha","nodeId":"node.alpha","caseId":"case.alpha","required":true}],
  "fixtures": []
}`)
	return dir
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

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse time %q: %v", value, err)
	}
	return parsed
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
