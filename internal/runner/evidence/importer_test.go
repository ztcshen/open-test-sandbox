package evidence_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"open-test-sandbox/internal/runner/evidence"
	"open-test-sandbox/internal/store/sqlite"
)

func TestImportLegacyRuntimeIndexesRunsCasesAndEvidence(t *testing.T) {
	ctx := context.Background()
	sourcePath := filepath.Join(t.TempDir(), "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)

	target, err := sqlite.Open(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "store.sqlite")})
	if err != nil {
		t.Fatalf("open target store: %v", err)
	}
	defer target.Close()

	result, err := evidence.ImportLegacyRuntime(ctx, evidence.ImportOptions{
		SourcePath: sourcePath,
		ProfileID:  "sample",
		Store:      target,
	})
	if err != nil {
		t.Fatalf("import legacy runtime: %v", err)
	}
	if result.RunCount != 2 || result.APICaseRunCount != 1 || result.EvidenceCount != 1 {
		t.Fatalf("result = %#v", result)
	}

	run, err := target.GetRun(ctx, "legacy-workflow-7")
	if err != nil {
		t.Fatalf("get imported workflow run: %v", err)
	}
	if run.ProfileID != "sample" || run.WorkflowID != "workflow.alpha" || run.Status != "passed" {
		t.Fatalf("imported run = %#v", run)
	}

	caseRuns, err := target.ListAPICaseRuns(ctx, "case-run-parent")
	if err != nil {
		t.Fatalf("list case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].ID != "legacy-case-run-11" || caseRuns[0].CaseID != "case.alpha" {
		t.Fatalf("case runs = %#v", caseRuns)
	}

	records, err := target.ListEvidence(ctx, "case-run-parent")
	if err != nil {
		t.Fatalf("list evidence: %v", err)
	}
	if len(records) != 1 || records[0].URI != ".runtime/cases/case-run-parent" {
		t.Fatalf("evidence records = %#v", records)
	}
}

func TestImportLegacyRuntimeSQLiteIsIdempotent(t *testing.T) {
	ctx := context.Background()
	sourcePath := filepath.Join(t.TempDir(), "legacy.sqlite")
	createLegacyRuntimeDB(t, sourcePath)
	targetPath := filepath.Join(t.TempDir(), "store.sqlite")

	result, err := evidence.ImportLegacyRuntimeSQLite(ctx, evidence.SQLiteImportOptions{
		SourcePath: sourcePath,
		TargetPath: targetPath,
		ProfileID:  "sample",
	})
	if err != nil {
		t.Fatalf("import sqlite legacy runtime: %v", err)
	}
	if result.RunCount != 2 || result.APICaseRunCount != 1 || result.EvidenceCount != 1 {
		t.Fatalf("result = %#v", result)
	}
	second, err := evidence.ImportLegacyRuntimeSQLite(ctx, evidence.SQLiteImportOptions{
		SourcePath: sourcePath,
		TargetPath: targetPath,
		ProfileID:  "sample",
	})
	if err != nil {
		t.Fatalf("import sqlite legacy runtime again: %v", err)
	}
	if second.RunCount != 2 || second.APICaseRunCount != 1 || second.EvidenceCount != 1 {
		t.Fatalf("second result = %#v", second)
	}

	target, err := sqlite.Open(ctx, sqlite.Config{Path: targetPath})
	if err != nil {
		t.Fatalf("open target store: %v", err)
	}
	defer target.Close()
	caseRuns, err := target.ListAPICaseRuns(ctx, "case-run-parent")
	if err != nil {
		t.Fatalf("list case runs: %v", err)
	}
	if len(caseRuns) != 1 {
		t.Fatalf("case runs should not duplicate: %#v", caseRuns)
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
