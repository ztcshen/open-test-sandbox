package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/store/sqlite"
)

type caseRunFileFixture struct {
	serverURL   string
	casePath    string
	evidenceDir string
	storePath   string
	configHome  string
	called      *bool
}

func newCaseRunFileFixture(t *testing.T, trackHTTP bool) caseRunFileFixture {
	t.Helper()

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	fixture := caseRunFileFixture{
		serverURL:   server.URL,
		casePath:    casePath,
		evidenceDir: filepath.Join(dir, "evidence"),
		storePath:   filepath.Join(dir, "store.sqlite"),
		configHome:  filepath.Join(dir, "config"),
	}
	if trackHTTP {
		fixture.called = &called
	}
	return fixture
}

func TestCaseRunCommandWritesEvidence(t *testing.T) {
	fixture := newCaseRunFileFixture(t, false)

	out := runCLI(t, "case", "run", "--case", fixture.casePath, "--base-url", fixture.serverURL, "--run-id", "case-run-001", "--evidence-dir", fixture.evidenceDir, "--store", "sqlite://"+fixture.storePath)
	if !strings.Contains(out, "Case Run: case-run-001") || !strings.Contains(out, "Status: passed") {
		t.Fatalf("case run output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(fixture.evidenceDir, "case-run-001", "summary.json")); err != nil {
		t.Fatalf("summary evidence missing: %v", err)
	}
}

func TestCaseRunCommandRequiresActiveStoreBeforeFileExecution(t *testing.T) {
	fixture := newCaseRunFileFixture(t, true)

	out := runCLIFailsWithEnv(t, []string{"AGENT_TESTBENCH_CONFIG_HOME=" + fixture.configHome}, "case", "run", "--case", fixture.casePath, "--base-url", fixture.serverURL, "--run-id", "case-run-no-store")
	if !strings.Contains(out, errNoActiveStoreConfigured.Error()) {
		t.Fatalf("case run without store output = %q", out)
	}
	if *fixture.called {
		t.Fatal("case run executed request before resolving active Store")
	}
}

func TestCaseRunDryRunPreviewsFileCaseWithoutStoreOrHTTP(t *testing.T) {
	fixture := newCaseRunFileFixture(t, true)

	out := runCLIWithEnv(t, []string{"AGENT_TESTBENCH_CONFIG_HOME=" + fixture.configHome},
		"case", "run",
		"--case", fixture.casePath,
		"--base-url", fixture.serverURL,
		"--run-id", "case-run-dry",
		"--evidence-dir", fixture.evidenceDir,
		"--override", "id=item-override",
		"--dry-run",
		"--json",
	)
	if *fixture.called {
		t.Fatal("dry-run executed the HTTP request")
	}
	if _, err := os.Stat(filepath.Join(fixture.evidenceDir, "case-run-dry")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run should not write evidence, stat err = %v", err)
	}
	assertCaseRunDryRunPlan(t, decodeCaseRunDryRunPlan(t, out), fixture.serverURL, fixture.evidenceDir)
}

func TestCaseRunCommandUsesActiveSQLiteStore(t *testing.T) {
	fixture := newCaseRunFileFixture(t, true)
	t.Setenv("AGENT_TESTBENCH_CONFIG_HOME", fixture.configHome)
	if err := saveStoreConfig(storeConfigFile{
		Active: "legacy-local",
		Stores: map[string]storeConfigEntry{
			"legacy-local": {Name: "legacy-local", URL: "sqlite://" + fixture.storePath, Backend: "sqlite"},
		},
	}); err != nil {
		t.Fatalf("save store config: %v", err)
	}

	out := runCLI(t, "case", "run", "--case", fixture.casePath, "--base-url", fixture.serverURL, "--run-id", "case-run-active-sqlite")
	if !strings.Contains(out, "case-run-active-sqlite") {
		t.Fatalf("case run with active SQLite store output = %q", out)
	}
	if !*fixture.called {
		t.Fatal("case run did not execute request with active SQLite Store")
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
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-002", "--evidence-dir", evidenceDir, "--override", "id=item-override", "--store", "sqlite://"+storePath)
	if !strings.Contains(out, "Case Run: case-run-002") || !strings.Contains(out, "Status: passed") {
		t.Fatalf("case run output = %q", out)
	}
	if _, err := os.Stat(filepath.Join(evidenceDir, "case-run-002", "response.json")); err != nil {
		t.Fatalf("response evidence missing: %v", err)
	}
}

func TestCaseRunCommandIndexesStoreRecords(t *testing.T) {
	storePath, evidenceDir := runStoredFileCase(t, "case-run-003")
	s := openCaseRunSQLiteStore(t, storePath)
	defer s.Close()
	assertIndexedParentRun(t, s, "case-run-003", filepath.Join(evidenceDir, "case-run-003"))
	assertIndexedCaseRunSummaries(t, assertIndexedAPICaseRun(t, s, "case-run-003", "case.alpha"))
	assertIndexedResponseEvidence(t, s, "case-run-003", 5)
}

func TestCaseRunCommandIndexesStoreRecordsWithStoreFlag(t *testing.T) {
	fixture := newCaseRunFileFixture(t, false)

	runCLI(t, "case", "run", "--case", fixture.casePath, "--base-url", fixture.serverURL, "--run-id", "case-run-store-flag", "--evidence-dir", fixture.evidenceDir, "--store", "sqlite://"+fixture.storePath, "--profile", "sample")

	s, err := sqlite.Open(context.Background(), sqlite.Config{Path: fixture.storePath})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	run, err := s.GetRun(context.Background(), "case-run-store-flag")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if run.ProfileID != "sample" || run.Status != "passed" {
		t.Fatalf("run = %#v", run)
	}
}

func TestCaseDiagnoseCommandSummarizesFailedCaseRunEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"status":"rejected"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	storePath := filepath.Join(dir, "store.sqlite")
	evidenceDir := filepath.Join(dir, "evidence")

	runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", "case-run-diagnose", "--evidence-dir", evidenceDir, "--store", "sqlite://"+storePath, "--profile", "sample")
	out := runCLI(t, "case", "diagnose", "--case-run", "case-run-diagnose.case", "--store", "sqlite://"+storePath, "--json")

	var report struct {
		OK              bool     `json:"ok"`
		CaseRunID       string   `json:"caseRunId"`
		Status          string   `json:"status"`
		Category        string   `json:"category"`
		PrimaryFinding  string   `json:"primaryFinding"`
		AssertionErrors []string `json:"assertionErrors"`
		Signals         []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"signals"`
		NextActions []string `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode diagnosis json: %v\n%s", err, out)
	}
	if report.OK || report.CaseRunID != "case-run-diagnose.case" || report.Status != "failed" || report.Category != "assertion-mismatch" {
		t.Fatalf("diagnosis identity = %#v", report)
	}
	if !strings.Contains(report.PrimaryFinding, "status code 400 was not expected") {
		t.Fatalf("primary finding = %q", report.PrimaryFinding)
	}
	if len(report.AssertionErrors) != 2 || !strings.Contains(report.AssertionErrors[0], "status code 400") {
		t.Fatalf("assertion errors = %#v", report.AssertionErrors)
	}
	foundHTTPStatus := false
	for _, signal := range report.Signals {
		if signal.Name == "http.status" && signal.Value == "400" {
			foundHTTPStatus = true
		}
	}
	if !foundHTTPStatus {
		t.Fatalf("diagnosis signals missing http.status=400: %#v", report.Signals)
	}
	joinedActions := strings.Join(report.NextActions, "\n")
	if !strings.Contains(joinedActions, "agent-testbench case evidence --case-run case-run-diagnose.case") {
		t.Fatalf("next actions = %#v", report.NextActions)
	}
}

func TestCaseGateFailsWithActionableReportForFailedCaseRuns(t *testing.T) {
	dir := t.TempDir()
	storePath, runID := seedCaseGateFailureStore(t, dir)
	out := runCLIFails(t, "case", "gate", "--store", "sqlite://"+storePath, "--run", runID, "--require-no-failures", "--require-evidence", "--json")
	assertCaseGateFailureReport(t, decodeCaseGateCLIReport(t, out), runID)
}

func TestCaseRunCommandExecutesStoreCatalogCaseID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/catalog" {
			t.Fatalf("request path = %s", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["id"] != "item-override" {
			t.Fatalf("request overrides = %#v", request)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	defer server.Close()

	dir := t.TempDir()
	storePath := filepath.Join(dir, "store.sqlite")
	evidenceDir := filepath.Join(dir, "evidence")
	seedCatalogCaseStore(t, storePath)

	out := runCLI(t, "case", "run", "--case-id", "case.catalog", "--base-url", server.URL, "--run-id", "catalog-run-001", "--evidence-dir", evidenceDir, "--store", "sqlite://"+storePath, "--profile", "sample", "--override", "id=item-override", "--json")
	assertCatalogCaseRunPayload(t, out)
	assertCatalogCaseRunStore(t, storePath, evidenceDir)
}
