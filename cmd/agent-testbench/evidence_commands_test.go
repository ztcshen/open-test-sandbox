package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
)

type evidenceImportCommandReport struct {
	SourcePath      string `json:"sourcePath"`
	ProfileID       string `json:"profileId"`
	RunCount        int    `json:"runCount"`
	APICaseRunCount int    `json:"apiCaseRunCount"`
	EvidenceCount   int    `json:"evidenceCount"`
}

type namedEvidenceImportFixture struct {
	sourcePath       string
	workflowLegacyID int64
	caseLegacyID     int64
	parentRunID      string
}

type evidenceListCommandReport struct {
	Runs []evidenceListCommandRun `json:"runs"`
}

type evidenceListCommandRun struct {
	ID              string                      `json:"id"`
	APICaseRunCount int                         `json:"apiCaseRunCount"`
	EvidenceCount   int                         `json:"evidenceCount"`
	EvidenceRecords []evidenceListCommandRecord `json:"evidenceRecords"`
}

type evidenceListCommandRecord struct {
	ID        string `json:"id"`
	RunID     string `json:"runId"`
	CaseRunID string `json:"caseRunId"`
	Kind      string `json:"kind"`
	URI       string `json:"uri"`
}

type evidenceTasksCommandReport struct {
	RunID  string                     `json:"runId"`
	Counts evidenceTasksCommandCounts `json:"counts"`
	Tasks  []evidenceTasksCommandTask `json:"tasks"`
}

type evidenceTasksCommandCounts struct {
	Total      int   `json:"total"`
	Passed     int   `json:"passed"`
	Failed     int   `json:"failed"`
	Running    int   `json:"running"`
	DurationMs int64 `json:"durationMs"`
}

type evidenceTasksCommandTask struct {
	ID            string `json:"id"`
	RunID         string `json:"runId"`
	StepID        string `json:"stepId"`
	Kind          string `json:"kind"`
	Status        string `json:"status"`
	Outcome       string `json:"outcome"`
	Reason        string `json:"reason"`
	DisplayStatus string `json:"displayStatus"`
	DurationMs    int64  `json:"durationMs"`
}

func TestEvidenceImportCommandIndexesLegacyRuntime(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "evidence", "import", "--from", sourcePath, "--profile", "sample", "--store", "sqlite://"+storePath)
	if !strings.Contains(out, "Imported evidence index") || !strings.Contains(out, "Runs: 2") || !strings.Contains(out, "API Case Runs: 1") {
		t.Fatalf("evidence import output = %q", out)
	}
}

func TestEvidenceImportCommandCanEmitJSONReport(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)
	storePath := filepath.Join(dir, "store.sqlite")

	out := runCLI(t, "evidence", "import", "--from", sourcePath, "--profile", "sample", "--store", "sqlite://"+storePath, "--json")

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
	if report.SourcePath != sourcePath || report.StorePath != "sqlite://"+storePath || report.ProfileID != "sample" {
		t.Fatalf("report paths/profile = %#v", report)
	}
	if report.RunCount != 2 || report.APICaseRunCount != 1 || report.EvidenceCount != 1 {
		t.Fatalf("report counts = %#v", report)
	}
}

func TestEvidenceImportUsesNamedPostgreSQLActiveStore(t *testing.T) {
	storeRef := configureNamedPostgreSQLActiveStore(t, "daily-evidence-import-pg")
	runEvidenceImportUsesNamedActiveStore(t, storeRef, "pg", "PostgreSQL")
}

func TestEvidenceImportUsesNamedMySQLActiveStore(t *testing.T) {
	storeRef := configureNamedMySQLActiveStore(t, "daily-evidence-import-mysql")
	runEvidenceImportUsesNamedActiveStore(t, storeRef, "mysql", "MySQL")
}

func runEvidenceImportUsesNamedActiveStore(t *testing.T, storeRef string, runLabel string, label string) {
	t.Helper()
	fixture := createNamedEvidenceImportFixture(t, runLabel)
	report := runEvidenceImportJSON(t, fixture.sourcePath, label)
	requireEvidenceImportSummary(t, label, report, fixture.sourcePath)
	requireImportedEvidenceStoreRecords(t, storeRef, label, fixture)
	requireImportedEvidenceListOutput(t, label, fixture)
}

func createNamedEvidenceImportFixture(t *testing.T, runLabel string) namedEvidenceImportFixture {
	t.Helper()

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "legacy.sqlite")
	suffix := time.Now().UTC().UnixNano()
	workflowLegacyID := suffix
	caseLegacyID := suffix + 1
	parentRunID := fmt.Sprintf("case-run-parent-%s-%d", runLabel, suffix)
	createLegacyRuntimeDBWithIDs(t, sourcePath, workflowLegacyID, caseLegacyID, parentRunID)
	return namedEvidenceImportFixture{
		sourcePath:       sourcePath,
		workflowLegacyID: workflowLegacyID,
		caseLegacyID:     caseLegacyID,
		parentRunID:      parentRunID,
	}
}

func runEvidenceImportJSON(t *testing.T, sourcePath string, label string) evidenceImportCommandReport {
	t.Helper()

	out := runCLI(t, "evidence", "import", "--from", sourcePath, "--profile", "sample", "--json")
	var report evidenceImportCommandReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode %s evidence import json: %v\n%s", label, err, out)
	}
	return report
}

func requireEvidenceImportSummary(t *testing.T, label string, report evidenceImportCommandReport, sourcePath string) {
	t.Helper()

	if report.SourcePath != sourcePath || report.ProfileID != "sample" || report.RunCount != 2 || report.APICaseRunCount != 1 || report.EvidenceCount != 1 {
		t.Fatalf("%s evidence import report = %#v", label, report)
	}
}

func requireImportedEvidenceStoreRecords(t *testing.T, storeRef string, label string, fixture namedEvidenceImportFixture) {
	t.Helper()

	ctx := context.Background()
	runtime, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open %s evidence import Store: %v", label, err)
	}
	defer runtime.Close()
	workflowRunID := fmt.Sprintf("legacy-workflow-%d", fixture.workflowLegacyID)
	workflowRun, err := runtime.GetRun(ctx, workflowRunID)
	if err != nil {
		t.Fatalf("get imported %s workflow run: %v", label, err)
	}
	if workflowRun.ProfileID != "sample" || workflowRun.WorkflowID != "workflow.alpha" || workflowRun.Status != store.StatusPassed {
		t.Fatalf("imported %s workflow run = %#v", label, workflowRun)
	}
	parentRun, err := runtime.GetRun(ctx, fixture.parentRunID)
	if err != nil {
		t.Fatalf("get imported %s parent run: %v", label, err)
	}
	if parentRun.ProfileID != "sample" || parentRun.Status != store.StatusFailed {
		t.Fatalf("imported %s parent run = %#v", label, parentRun)
	}
	caseRuns, err := runtime.ListAPICaseRuns(ctx, fixture.parentRunID)
	if err != nil {
		t.Fatalf("list imported %s case runs: %v", label, err)
	}
	if len(caseRuns) != 1 || caseRuns[0].ID != fmt.Sprintf("legacy-case-run-%d", fixture.caseLegacyID) || caseRuns[0].CaseID != "case.alpha" || caseRuns[0].Status != store.StatusFailed {
		t.Fatalf("imported %s case runs = %#v", label, caseRuns)
	}
	records, err := runtime.ListEvidence(ctx, fixture.parentRunID)
	if err != nil {
		t.Fatalf("list imported %s Evidence: %v", label, err)
	}
	if len(records) != 1 || records[0].ID != fmt.Sprintf("legacy-evidence-%d", fixture.caseLegacyID) || records[0].Kind != "case-run" {
		t.Fatalf("imported %s Evidence = %#v", label, records)
	}
}

func requireImportedEvidenceListOutput(t *testing.T, label string, fixture namedEvidenceImportFixture) {
	t.Helper()

	listOut := runCLI(t, "evidence", "list", "--run", fixture.parentRunID, "--json")
	var evidenceReport evidenceListCommandReport
	if err := json.Unmarshal([]byte(listOut), &evidenceReport); err != nil {
		t.Fatalf("decode imported %s evidence list json: %v\n%s", label, err, listOut)
	}
	if len(evidenceReport.Runs) != 1 || evidenceReport.Runs[0].ID != fixture.parentRunID || evidenceReport.Runs[0].APICaseRunCount != 1 || evidenceReport.Runs[0].EvidenceCount != 1 {
		t.Fatalf("imported %s evidence list = %#v", label, evidenceReport.Runs)
	}
	if len(evidenceReport.Runs[0].EvidenceRecords) != 1 {
		t.Fatalf("imported %s evidence list records = %#v", label, evidenceReport.Runs[0].EvidenceRecords)
	}
	record := evidenceReport.Runs[0].EvidenceRecords[0]
	if record.ID != fmt.Sprintf("legacy-evidence-%d", fixture.caseLegacyID) || record.RunID != fixture.parentRunID || record.CaseRunID != fmt.Sprintf("legacy-case-run-%d", fixture.caseLegacyID) || record.Kind != "case-run" || record.URI != ".runtime/cases/"+fixture.parentRunID {
		t.Fatalf("imported %s evidence list record = %#v", label, record)
	}
}

func TestEvidenceListCommandPrintsStoreRecords(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-evidence-list-pg")
	runEvidenceListCommandPrintsStoreRecords(t, "PostgreSQL")
}

func TestEvidenceListCommandPrintsStoreRecordsUsesNamedMySQLActiveStore(t *testing.T) {
	configureNamedMySQLActiveStore(t, "daily-evidence-list-mysql")
	runEvidenceListCommandPrintsStoreRecords(t, "MySQL")
}

func runEvidenceListCommandPrintsStoreRecords(t *testing.T, label string) {
	t.Helper()
	runID := uniqueTestID(t, "case-run-004")
	createStoredCaseRun(t, runID, label)

	out := runCLI(t, "evidence", "list", "--run", runID)

	for _, want := range []string{"Run: " + runID, "Case Run: " + runID + ".case", "Case: case.alpha", "Evidence: response"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%s evidence list output missing %q: %q", label, want, out)
		}
	}
}

func TestEvidenceListCommandCanEmitJSON(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-evidence-list-json-pg")
	runEvidenceListCommandCanEmitJSON(t, "PostgreSQL")
}

func TestEvidenceListCommandCanEmitJSONUsesNamedMySQLActiveStore(t *testing.T) {
	configureNamedMySQLActiveStore(t, "daily-evidence-list-json-mysql")
	runEvidenceListCommandCanEmitJSON(t, "MySQL")
}

func runEvidenceListCommandCanEmitJSON(t *testing.T, label string) {
	t.Helper()
	runID := uniqueTestID(t, "case-run-005")
	createStoredCaseRun(t, runID, label)

	out := runCLI(t, "evidence", "list", "--run", runID, "--json")

	var report struct {
		Runs []struct {
			ID              string `json:"id"`
			APICaseRunCount int    `json:"apiCaseRunCount"`
			EvidenceCount   int    `json:"evidenceCount"`
		} `json:"runs"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode %s evidence list json: %v\n%s", label, err, out)
	}
	if len(report.Runs) != 1 || report.Runs[0].ID != runID {
		t.Fatalf("%s json runs = %#v", label, report.Runs)
	}
	if report.Runs[0].APICaseRunCount != 1 || report.Runs[0].EvidenceCount != 5 {
		t.Fatalf("%s json run counts = %#v", label, report.Runs[0])
	}
}

func TestEvidenceListCommandRejectsMissingRun(t *testing.T) {
	configureNamedPostgreSQLActiveStore(t, "daily-evidence-list-missing-pg")
	runEvidenceListCommandRejectsMissingRun(t, "PostgreSQL")
}

func TestEvidenceListCommandRejectsMissingRunUsesNamedMySQLActiveStore(t *testing.T) {
	configureNamedMySQLActiveStore(t, "daily-evidence-list-missing-mysql")
	runEvidenceListCommandRejectsMissingRun(t, "MySQL")
}

func runEvidenceListCommandRejectsMissingRun(t *testing.T, label string) {
	t.Helper()
	runID := uniqueTestID(t, "case-run-006")
	createStoredCaseRun(t, runID, label)

	out := runCLIFails(t, "evidence", "list", "--run", "case-run-missing")
	if !strings.Contains(out, "run not found") || !strings.Contains(out, "case-run-missing") {
		t.Fatalf("%s missing run output = %q", label, out)
	}
}

func TestEvidenceCommandsUseNamedSQLiteActiveStore(t *testing.T) {
	configureNamedSQLiteActiveStore(t, "daily-evidence-sqlite")
	runEvidenceListCommandPrintsStoreRecords(t, "SQLite")
}

func TestEvidenceTasksCommandListsPostProcessTasks(t *testing.T) {
	storePath := createPostProcessTaskStore(t)
	report := runEvidenceTasksJSON(t, storePath)
	requireEvidenceTasksReport(t, report)
	requireEvidenceTasksTextFilters(t, storePath)
	requireEvidenceCommandsUseExplicitStore(t, storePath)
}

func runEvidenceTasksJSON(t *testing.T, storePath string) evidenceTasksCommandReport {
	t.Helper()

	out := runCLI(t,
		"evidence", "tasks",
		"--store", "sqlite://"+storePath,
		"--run", "run.tasks",
		"--step", "step-a",
		"--kind", "trace_topology_collect",
		"--json",
	)
	var report evidenceTasksCommandReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode evidence tasks json: %v\n%s", err, out)
	}
	return report
}

func requireEvidenceTasksReport(t *testing.T, report evidenceTasksCommandReport) {
	t.Helper()

	if report.RunID != "run.tasks" || report.Counts.Total != 1 || report.Counts.Passed != 1 || report.Counts.DurationMs != 125 {
		t.Fatalf("evidence tasks report = %#v", report)
	}
	if len(report.Tasks) != 1 || report.Tasks[0].ID != "task.trace" || report.Tasks[0].StepID != "step-a" || report.Tasks[0].Kind != "trace_topology_collect" {
		t.Fatalf("evidence tasks = %#v", report.Tasks)
	}
	if report.Tasks[0].Outcome != "success" || report.Tasks[0].Reason != "completed" || report.Tasks[0].DisplayStatus != "passed: completed" {
		t.Fatalf("evidence task readable status = %#v", report.Tasks[0])
	}
}

func requireEvidenceTasksTextFilters(t *testing.T, storePath string) {
	t.Helper()

	textOut := runCLI(t, "evidence", "tasks", "--store", "sqlite://"+storePath, "--run", "run.tasks", "--status", "failed")
	for _, want := range []string{"Post Process Tasks: run.tasks", "task.logs", "runtime_log_collect", "300 ms", "log source missing"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("evidence tasks text missing %q:\n%s", want, textOut)
		}
	}
	skippedOut := runCLI(t, "evidence", "tasks", "--store", "sqlite://"+storePath, "--run", "run.tasks", "--status", "skipped")
	for _, want := range []string{"task.trace.skip", "skipped: SkyWalking provider unavailable"} {
		if !strings.Contains(skippedOut, want) {
			t.Fatalf("evidence skipped task text missing %q:\n%s", want, skippedOut)
		}
	}
}

func requireEvidenceCommandsUseExplicitStore(t *testing.T, storePath string) {
	t.Helper()

	storeRef := "sqlite://" + storePath
	listOut := runCLI(t, "evidence", "list", "--store", storeRef, "--json")
	if !strings.Contains(listOut, "run.tasks") {
		t.Fatalf("evidence list --store output = %q", listOut)
	}
	tasksOut := runCLI(t, "evidence", "tasks", "--store", storeRef, "--run", "run.tasks", "--json")
	if !strings.Contains(tasksOut, "task.trace") || !strings.Contains(tasksOut, "task.logs") {
		t.Fatalf("evidence tasks --store output = %q", tasksOut)
	}
}
