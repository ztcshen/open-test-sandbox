package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlite"
)

func TestCaseIncompleteBatchesCommandReportsNotRunCases(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-incomplete-batches-pg")
	runCaseIncompleteBatchesCommandReportsNotRunCases(t, storeRef, "PostgreSQL")
}

func TestCaseIncompleteBatchesUsesNamedMySQLActiveStore(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-incomplete-batches-mysql")
	runCaseIncompleteBatchesCommandReportsNotRunCases(t, storeRef, "MySQL")
}

func runCaseIncompleteBatchesCommandReportsNotRunCases(t *testing.T, _ string, label string) {
	t.Helper()
	fixture := newIncompleteBatchFixture(t)

	runCompletedIncompleteBatchCase(t, fixture)
	requireIncompleteBatchTextReport(t, fixture, label)
	requireIncompleteBatchJSONReport(t, fixture, label)

	storeOnlyRef := writeStoreOnlyIncompleteBatchCatalog(t, fixture)
	requireStoreOnlyIncompleteBatchReport(t, storeOnlyRef, fixture, label)
}

type incompleteBatchFixture struct {
	profileDir string
	alphaPath  string
	betaPath   string
	betaID     string
	runID      string
	serverURL  string
	workDir    string
}

func newIncompleteBatchFixture(t *testing.T) incompleteBatchFixture {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	profileDir := filepath.Join(dir, "profile")
	alphaPath := filepath.Join(dir, "case.alpha.json")
	betaPath := filepath.Join(dir, "case.beta.json")
	alphaCaseID := uniqueTestID(t, "case.alpha")
	betaCaseID := uniqueTestID(t, "case.beta")
	runID := uniqueTestID(t, "run-alpha")

	writeIncompleteBatchCaseFiles(t, alphaPath, betaPath, alphaCaseID, betaCaseID)
	writeIncompleteBatchProfile(t, profileDir, alphaPath, betaPath, alphaCaseID, betaCaseID)

	return incompleteBatchFixture{
		profileDir: profileDir,
		alphaPath:  alphaPath,
		betaPath:   betaPath,
		betaID:     betaCaseID,
		runID:      runID,
		serverURL:  server.URL,
		workDir:    dir,
	}
}

func writeIncompleteBatchCaseFiles(t *testing.T, alphaPath string, betaPath string, alphaCaseID string, betaCaseID string) {
	t.Helper()
	writeFile(t, alphaPath, fmt.Sprintf(`{
  "id": %q,
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
}`, alphaCaseID))
	writeFile(t, betaPath, fmt.Sprintf(`{
  "id": %q,
  "title": "Read Item",
	  "request": {"method": "GET", "path": "/v1/items/item-001"},
	  "assertions": {"expectedStatusCodes": [200]}
	}`, betaCaseID))
}

func writeIncompleteBatchProfile(t *testing.T, profileDir string, alphaPath string, betaPath string, alphaCaseID string, betaCaseID string) {
	t.Helper()
	writeFile(t, filepath.Join(profileDir, "profile.json"), fmt.Sprintf(`{
	  "id": "sample",
	  "displayName": "Sample Profile",
  "services": [],
  "workflows": [],
  "interfaceNodes": [],
  "apiCases": [
    {"id":%q,"displayName":"Case Alpha","casePath":%q},
    {"id":%q,"displayName":"Case Beta","casePath":%q}
	  ],
	  "requestTemplates": [],
	  "caseDependencies": [],
	  "workflowBindings": [],
	  "fixtures": []
	}`, alphaCaseID, alphaPath, betaCaseID, betaPath))
}

func runCompletedIncompleteBatchCase(t *testing.T, fixture incompleteBatchFixture) {
	t.Helper()
	runCLI(t, "case", "run", "--case", fixture.alphaPath, "--base-url", fixture.serverURL, "--run-id", fixture.runID, "--profile", "sample")
}

func requireIncompleteBatchTextReport(t *testing.T, fixture incompleteBatchFixture, label string) {
	t.Helper()
	out := runCLI(t, "case", "incomplete-batches", "--profile", fixture.profileDir)
	for _, want := range []string{"Incomplete API Cases: 1", fixture.betaID, "not-run", fixture.betaPath} {
		if !strings.Contains(out, want) {
			t.Fatalf("%s incomplete case output missing %q: %q", label, want, out)
		}
	}
}

func requireIncompleteBatchJSONReport(t *testing.T, fixture incompleteBatchFixture, label string) {
	t.Helper()
	report := decodeIncompleteCaseReportForTest(t, label, runCLI(t, "case", "incomplete-batches", "--profile", fixture.profileDir, "--json"))
	if !report.OK || report.Count != 1 || len(report.Items) != 1 {
		t.Fatalf("%s incomplete cases report = %#v", label, report)
	}
	if report.Items[0].ID != fixture.betaID || report.Items[0].Reason != "not-run" {
		t.Fatalf("%s incomplete case item = %#v", label, report.Items[0])
	}
	if !strings.Contains(report.Items[0].SuggestedCommand, fixture.betaPath) {
		t.Fatalf("%s suggested command = %q", label, report.Items[0].SuggestedCommand)
	}
}

func writeStoreOnlyIncompleteBatchCatalog(t *testing.T, fixture incompleteBatchFixture) string {
	t.Helper()
	ctx := context.Background()
	storeOnlyPath := filepath.Join(fixture.workDir, "store-only.sqlite")
	storeOnly, err := sqlite.Open(ctx, sqlite.Config{Path: storeOnlyPath})
	if err != nil {
		t.Fatalf("open store-only catalog: %v", err)
	}
	if err := storeOnly.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "current",
		APICases: []store.CatalogAPICase{
			{ID: "case.store.passed", DisplayName: "Passed Store Case", Status: "active"},
			{ID: "case.store.pending", DisplayName: "Pending Store Case", CasePath: fixture.betaPath, Status: "active"},
		},
	}); err != nil {
		t.Fatalf("seed store-only catalog: %v", err)
	}
	if _, err := storeOnly.CreateRun(ctx, store.Run{ID: "run.store.passed", ProfileID: "current", WorkflowID: "case.store.passed", Status: store.StatusPassed}); err != nil {
		t.Fatalf("create store-only run: %v", err)
	}
	if _, err := storeOnly.RecordAPICaseRun(ctx, store.APICaseRun{ID: "run.store.passed.case", RunID: "run.store.passed", CaseID: "case.store.passed", Status: store.StatusPassed}); err != nil {
		t.Fatalf("record store-only case run: %v", err)
	}
	if err := storeOnly.Close(); err != nil {
		t.Fatalf("close store-only catalog: %v", err)
	}
	return "sqlite://" + storeOnlyPath
}

func requireStoreOnlyIncompleteBatchReport(t *testing.T, storeRef string, fixture incompleteBatchFixture, label string) {
	t.Helper()
	storeOnlyReport := decodeIncompleteCaseReportForTest(t, label, runCLI(t, "case", "incomplete-batches", "--store", storeRef, "--json"))
	if !storeOnlyReport.OK || storeOnlyReport.Count != 1 || len(storeOnlyReport.Items) != 1 {
		t.Fatalf("%s store-only incomplete report = %#v", label, storeOnlyReport)
	}
	if storeOnlyReport.Items[0].ID != "case.store.pending" || storeOnlyReport.Items[0].Reason != "not-run" || storeOnlyReport.Items[0].Source != "profile:current" {
		t.Fatalf("%s store-only incomplete item = %#v", label, storeOnlyReport.Items[0])
	}
	if !strings.Contains(storeOnlyReport.Items[0].SuggestedCommand, fixture.betaPath) {
		t.Fatalf("%s store-only suggested command = %q", label, storeOnlyReport.Items[0].SuggestedCommand)
	}
}

func decodeIncompleteCaseReportForTest(t *testing.T, label string, raw string) incompleteCaseReport {
	t.Helper()
	var report incompleteCaseReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		t.Fatalf("decode %s incomplete cases report: %v\n%s", label, err, raw)
	}
	return report
}
