package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "."}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
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
