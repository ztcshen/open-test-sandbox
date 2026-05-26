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
	"time"
)

func TestServeAndEvidenceTasksUseNamedPostgreSQLActiveStore(t *testing.T) {
	storeName := "daily-serve-pg"
	storeRef := configureNamedPostgreSQLActiveStore(t, storeName)
	runServeAndEvidenceTasksUseNamedActiveStore(t, storeRef, storeName, "postgres", "pg", "PostgreSQL")
}

func TestServeAndEvidenceTasksUseNamedMySQLActiveStore(t *testing.T) {
	storeName := "daily-serve-mysql"
	storeRef := configureNamedMySQLActiveStore(t, storeName)
	runServeAndEvidenceTasksUseNamedActiveStore(t, storeRef, storeName, "mysql", "mysql", "MySQL")
}

func runServeAndEvidenceTasksUseNamedActiveStore(t *testing.T, storeRef string, storeName string, backend string, runLabel string, label string) {
	t.Helper()
	ctx := context.Background()
	runID := "run.tasks." + runLabel + "." + time.Now().UTC().Format("20060102150405.000000000")
	seedNamedActiveServeStore(t, ctx, storeRef, runID, label)
	runCLI(t, "config", "publish", "--from", writeInterfaceNodeBatchReportProfile(t))

	requireNamedActiveEvidenceList(t, runID, label)
	requireNamedActiveEvidenceTasks(t, runID, label)

	handler, cleanup := newNamedActiveServeHandler(t)
	defer cleanup()
	requireNamedActiveStoreCurrent(t, handler, storeName, backend, label)
	requireNamedActiveServeRuns(t, handler, runID, label)
	requireNamedActiveServeCatalog(t, handler, label)

	apiVerifyDir := exerciseNamedActiveProfileAPIs(t, handler, label)
	requireNamedActiveVerifiedProfile(t, ctx, storeRef, apiVerifyDir, label)
	exerciseNamedActiveLegacyEvidenceImport(t, handler, runLabel, label)
}

func seedNamedActiveServeStore(t *testing.T, ctx context.Context, storeRef string, runID string, label string) {
	t.Helper()
	runtime, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open %s task store: %v", label, err)
	}
	seedPostProcessTaskFixture(t, ctx, runtime, runID, runID+".")
	if err := runtime.Close(); err != nil {
		t.Fatalf("close %s task store: %v", label, err)
	}
}

func requireNamedActiveEvidenceList(t *testing.T, runID string, label string) {
	t.Helper()
	listOut := runCLI(t, "evidence", "list", "--run", runID, "--json")
	var report struct {
		Runs []struct {
			ID            string `json:"id"`
			EvidenceCount int    `json:"evidenceCount"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(listOut), &report); err != nil {
		t.Fatalf("decode %s evidence list json: %v\n%s", label, err, listOut)
	}
	if len(report.Runs) != 1 || report.Runs[0].ID != runID || report.Runs[0].EvidenceCount != 1 {
		t.Fatalf("%s evidence list report = %#v", label, report.Runs)
	}
}

func requireNamedActiveEvidenceTasks(t *testing.T, runID string, label string) {
	t.Helper()
	tasksOut := runCLI(t, "evidence", "tasks", "--run", runID, "--step", "step-a", "--kind", "trace_topology_collect", "--json")
	var report struct {
		RunID  string `json:"runId"`
		Counts struct {
			Total  int `json:"total"`
			Passed int `json:"passed"`
		} `json:"counts"`
		Tasks []struct {
			ID            string `json:"id"`
			StepID        string `json:"stepId"`
			DisplayStatus string `json:"displayStatus"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(tasksOut), &report); err != nil {
		t.Fatalf("decode %s evidence tasks json: %v\n%s", label, err, tasksOut)
	}
	if report.RunID != runID || report.Counts.Total != 1 || report.Counts.Passed != 1 || len(report.Tasks) != 1 {
		t.Fatalf("%s evidence tasks report = %#v", label, report)
	}
	task := report.Tasks[0]
	if !strings.Contains(task.ID, "task.trace") || task.StepID != "step-a" || task.DisplayStatus != "passed: completed" {
		t.Fatalf("%s evidence task = %#v", label, task)
	}
}

func newNamedActiveServeHandler(t *testing.T) (http.Handler, func() error) {
	t.Helper()
	handler, cleanup, err := serveHandlerFromArgs(nil)
	if err != nil {
		t.Fatalf("build serve handler from active SQL Store: %v", err)
	}
	return handler, cleanup
}

func requireNamedActiveStoreCurrent(t *testing.T, handler http.Handler, storeName string, backend string, label string) {
	t.Helper()
	rec := serveGET(t, handler, "/api/store/current", "store current")
	var payload struct {
		OK         bool   `json:"ok"`
		Configured bool   `json:"configured"`
		Name       string `json:"name"`
		Backend    string `json:"backend"`
		Source     string `json:"source"`
	}
	decodeServeJSON(t, rec, &payload, label+" store current payload")
	if !payload.OK || !payload.Configured || payload.Name != storeName || payload.Backend != backend || payload.Source != "active-config" {
		t.Fatalf("%s store current payload = %#v", label, payload)
	}
}

func requireNamedActiveServeRuns(t *testing.T, handler http.Handler, runID string, label string) {
	t.Helper()
	rec := serveGET(t, handler, "/api/runs", "runs")
	if !strings.Contains(rec.Body.String(), runID) {
		t.Fatalf("%s serve runs via active SQL Store = %d %s", label, rec.Code, rec.Body.String())
	}
}

func requireNamedActiveServeCatalog(t *testing.T, handler http.Handler, label string) {
	t.Helper()
	rec := serveGET(t, handler, "/api/interface-nodes", "interface nodes")
	var payload struct {
		Source struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"source"`
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decodeServeJSON(t, rec, &payload, label+" interface nodes payload")
	if payload.Source.ID != "sample" || payload.Source.Kind != "read-model" || len(payload.Items) != 1 || payload.Items[0].ID != "node.alpha" {
		t.Fatalf("%s serve catalog payload = %#v", label, payload)
	}
}

func exerciseNamedActiveProfileAPIs(t *testing.T, handler http.Handler, label string) string {
	t.Helper()
	apiImportDir := writeEmptyProfileBundle(t)
	requireNamedActiveProfileImport(t, handler, apiImportDir, label)
	apiVerifyDir := writeInterfaceNodeCaseProfile(t)
	requireNamedActiveProfileVerify(t, handler, apiVerifyDir, label)
	return apiVerifyDir
}

func requireNamedActiveProfileImport(t *testing.T, handler http.Handler, apiImportDir string, label string) {
	t.Helper()
	rec := servePOST(t, handler, "/api/profile/import", `{"path":`+mustJSON(t, apiImportDir)+`}`, "profile import")
	var payload struct {
		ProfileID  string   `json:"profileId"`
		BundlePath string   `json:"bundlePath"`
		ReadModels []string `json:"readModels"`
	}
	decodeServeJSON(t, rec, &payload, label+" serve profile import payload")
	if payload.ProfileID != "empty" || payload.BundlePath != apiImportDir || strings.Join(payload.ReadModels, ",") != "interface-nodes,catalog,dashboard" {
		t.Fatalf("%s serve profile import payload = %#v", label, payload)
	}
}

func requireNamedActiveProfileVerify(t *testing.T, handler http.Handler, apiVerifyDir string, label string) {
	t.Helper()
	rec := servePOST(t, handler, "/api/profile/verify", `{"path":`+mustJSON(t, apiVerifyDir)+`}`, "profile verify")
	var payload struct {
		OK        bool   `json:"ok"`
		ProfileID string `json:"profileId"`
		Publish   struct {
			ProfileID  string   `json:"profileId"`
			BundlePath string   `json:"bundlePath"`
			ReadModels []string `json:"readModels"`
		} `json:"publish"`
		Summary struct {
			FailedChecks int `json:"failedChecks"`
		} `json:"summary"`
	}
	decodeServeJSON(t, rec, &payload, label+" serve profile verify payload")
	if !payload.OK || payload.ProfileID != "sample" || payload.Publish.ProfileID != "sample" || payload.Publish.BundlePath != apiVerifyDir || !hasReadModels(payload.Publish.ReadModels, "interface-nodes", "catalog", "dashboard") || payload.Summary.FailedChecks != 0 {
		t.Fatalf("%s serve profile verify payload = %#v", label, payload)
	}
}

func requireNamedActiveVerifiedProfile(t *testing.T, ctx context.Context, storeRef string, apiVerifyDir string, label string) {
	t.Helper()
	runtime, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("reopen %s serve profile Store: %v", label, err)
	}
	defer runtime.Close()
	verifiedIndex, err := runtime.GetProfileIndex(ctx, "sample")
	if err != nil {
		t.Fatalf("get %s serve profile index: %v", label, err)
	}
	if verifiedIndex.BundlePath != apiVerifyDir || !strings.HasPrefix(verifiedIndex.BundleDigest, "sha256:") {
		t.Fatalf("%s serve profile index = %#v", label, verifiedIndex)
	}
	verifiedCatalog, err := runtime.GetProfileCatalog(ctx)
	if err != nil {
		t.Fatalf("get %s serve profile catalog: %v", label, err)
	}
	if verifiedCatalog.ProfileID != "sample" || len(verifiedCatalog.APICases) != 2 {
		t.Fatalf("%s serve profile catalog = %#v", label, verifiedCatalog)
	}
}

func exerciseNamedActiveLegacyEvidenceImport(t *testing.T, handler http.Handler, runLabel string, label string) {
	t.Helper()
	apiLegacyPath, apiLegacyCaseID, apiLegacyParentRunID := createServeLegacyRuntimeDB(t, runLabel)
	requireNamedActiveEvidenceImport(t, handler, apiLegacyPath, label)
	requireNamedActiveImportedEvidenceList(t, handler, apiLegacyParentRunID, apiLegacyCaseID, label)
}

func createServeLegacyRuntimeDB(t *testing.T, runLabel string) (string, int64, string) {
	t.Helper()
	apiLegacyPath := filepath.Join(t.TempDir(), "legacy-api.sqlite")
	apiLegacySuffix := time.Now().UTC().UnixNano()
	apiLegacyWorkflowID := apiLegacySuffix
	apiLegacyCaseID := apiLegacySuffix + 1
	apiLegacyParentRunID := fmt.Sprintf("case-run-parent-api-%s-%d", runLabel, apiLegacySuffix)
	createLegacyRuntimeDBWithIDs(t, apiLegacyPath, apiLegacyWorkflowID, apiLegacyCaseID, apiLegacyParentRunID)
	return apiLegacyPath, apiLegacyCaseID, apiLegacyParentRunID
}

func requireNamedActiveEvidenceImport(t *testing.T, handler http.Handler, apiLegacyPath string, label string) {
	t.Helper()
	body := `{"sourcePath":` + mustJSON(t, apiLegacyPath) + `,"profileId":"sample"}`
	rec := servePOST(t, handler, "/api/evidence/import", body, "evidence import")
	var payload struct {
		OK              bool   `json:"ok"`
		SourcePath      string `json:"sourcePath"`
		ProfileID       string `json:"profileId"`
		RunCount        int    `json:"runCount"`
		APICaseRunCount int    `json:"apiCaseRunCount"`
		EvidenceCount   int    `json:"evidenceCount"`
	}
	decodeServeJSON(t, rec, &payload, label+" serve evidence import payload")
	if !payload.OK || payload.SourcePath != apiLegacyPath || payload.ProfileID != "sample" || payload.RunCount != 2 || payload.APICaseRunCount != 1 || payload.EvidenceCount != 1 {
		t.Fatalf("%s serve evidence import payload = %#v", label, payload)
	}
}

func requireNamedActiveImportedEvidenceList(t *testing.T, handler http.Handler, parentRunID string, caseID int64, label string) {
	t.Helper()
	rec := serveGET(t, handler, "/api/evidence/list?run="+parentRunID, "evidence list")
	var payload struct {
		Runs []struct {
			ID              string `json:"id"`
			APICaseRunCount int    `json:"apiCaseRunCount"`
			EvidenceCount   int    `json:"evidenceCount"`
			EvidenceRecords []struct {
				ID        string `json:"id"`
				CaseRunID string `json:"caseRunId"`
				Kind      string `json:"kind"`
				URI       string `json:"uri"`
			} `json:"evidenceRecords"`
		} `json:"runs"`
	}
	decodeServeJSON(t, rec, &payload, label+" serve evidence list payload")
	if len(payload.Runs) != 1 || payload.Runs[0].ID != parentRunID || payload.Runs[0].APICaseRunCount != 1 || payload.Runs[0].EvidenceCount != 1 || len(payload.Runs[0].EvidenceRecords) != 1 {
		t.Fatalf("%s serve evidence list payload = %#v", label, payload.Runs)
	}
	record := payload.Runs[0].EvidenceRecords[0]
	if record.ID != fmt.Sprintf("legacy-evidence-%d", caseID) || record.CaseRunID != fmt.Sprintf("legacy-case-run-%d", caseID) || record.Kind != "case-run" || record.URI != ".runtime/cases/"+parentRunID {
		t.Fatalf("%s serve evidence list record = %#v", label, record)
	}
}

func serveGET(t *testing.T, handler http.Handler, path string, label string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d body=%s", label, rec.Code, rec.Body.String())
	}
	return rec
}

func servePOST(t *testing.T, handler http.Handler, path string, body string, label string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d body=%s", label, rec.Code, rec.Body.String())
	}
	return rec
}

func decodeServeJSON(t *testing.T, rec *httptest.ResponseRecorder, dest any, label string) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), dest); err != nil {
		t.Fatalf("decode %s: %v\n%s", label, err, rec.Body.String())
	}
}
