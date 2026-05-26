package main

import (
	"agent-testbench/internal/store"
	"agent-testbench/internal/store/mysql"
	"agent-testbench/internal/store/postgres"
	"agent-testbench/internal/store/sqlite"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func configureNamedPostgreSQLActiveStore(t *testing.T, name string) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("AGENT_TESTBENCH_TEST_PG_DSN"))
	if dsn == "" {
		t.Skip("set AGENT_TESTBENCH_TEST_PG_DSN to run named PostgreSQL daily path coverage")
	}
	t.Setenv("AGENT_TESTBENCH_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	runCLI(t, "store", "config", "set", name, "--url", dsn)
	runCLI(t, "store", "use", name)
	runCLI(t, "store", "upgrade")
	return dsn
}

func configureNamedMySQLActiveStore(t *testing.T, name string) string {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("AGENT_TESTBENCH_MYSQL_TEST_DSN"))
	if dsn == "" {
		t.Skip("set AGENT_TESTBENCH_MYSQL_TEST_DSN to run named MySQL daily path coverage")
	}
	t.Setenv("AGENT_TESTBENCH_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	runCLI(t, "store", "config", "set", name, "--url", dsn)
	runCLI(t, "store", "use", name)
	runCLI(t, "store", "upgrade")
	resetNamedMySQLActiveStore(t, dsn)
	return dsn
}

func configureNamedSQLiteActiveStore(t *testing.T, name string) string {
	t.Helper()
	storeRef := "sqlite://" + filepath.Join(t.TempDir(), "store.sqlite")
	t.Setenv("AGENT_TESTBENCH_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	runCLI(t, "store", "config", "set", name, "--url", storeRef)
	runCLI(t, "store", "use", name)
	runCLI(t, "store", "upgrade")
	return storeRef
}

func uniqueTestID(t *testing.T, prefix string) string {
	t.Helper()
	slug := strings.ToLower(t.Name())
	slug = strings.NewReplacer("/", "-", "_", "-", " ", "-").Replace(slug)
	return fmt.Sprintf("%s.%s.%d", prefix, slug, time.Now().UTC().UnixNano())
}

func resetNamedMySQLActiveStore(t *testing.T, dsn string) {
	t.Helper()
	cfg, err := mysql.ParseConfigFromURL(dsn)
	if err != nil {
		t.Fatalf("parse MySQL test store DSN: %v", err)
	}
	db, err := sql.Open(cfg.DriverName, cfg.DSN)
	if err != nil {
		t.Fatalf("open MySQL test store for reset: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx := context.Background()
	rows, err := db.QueryContext(ctx, `
select table_name
from information_schema.tables
where table_schema = database()
  and table_type = 'BASE TABLE'
  and table_name <> 'schema_versions'
order by table_name;`)
	if err != nil {
		t.Fatalf("list MySQL test store tables: %v", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan MySQL test store table: %v", err)
		}
		tables = append(tables, table)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate MySQL test store tables: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close MySQL test store table cursor: %v", err)
	}
	if _, err := db.ExecContext(ctx, "set foreign_key_checks = 0"); err != nil {
		t.Fatalf("disable MySQL foreign key checks: %v", err)
	}
	defer db.ExecContext(context.Background(), "set foreign_key_checks = 1")
	for _, table := range tables {
		if _, err := db.ExecContext(ctx, "delete from "+quoteMySQLTestIdent(table)); err != nil {
			t.Fatalf("clear MySQL test table %q: %v", table, err)
		}
	}
}

func quoteMySQLTestIdent(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func seedEnvironmentVerificationArtifacts(t *testing.T, storeRef string, runID string) {
	t.Helper()
	ctx := context.Background()
	runtime, err := openStore(ctx, storeRef)
	if err != nil {
		t.Fatalf("open verification artifact Store: %v", err)
	}
	defer runtime.Close()
	now := time.Now().UTC()
	if _, err := runtime.CreateRun(ctx, store.Run{
		ID:         runID,
		ProfileID:  "sample",
		WorkflowID: "workflow.core-10",
		Status:     store.StatusPassed,
		SummaryJSON: `{"acceptance":{"templateId":"environment.workflow.skywalking.v1","ok":true,"workflowId":"workflow.core-10",
"expectedSteps":1,"completedSteps":1,"passedSteps":1,"failedSteps":0,"topologyProvider":"skywalking",
"steps":[{"stepId":"step.core-10","caseId":"case.core-10","status":"passed","elapsedMs":12,"evidenceComplete":true,"topologyComplete":true}]}}`,
		StartedAt:  now.Add(-time.Second),
		FinishedAt: now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}); err != nil {
		t.Fatalf("seed verification run: %v", err)
	}
	if _, err := runtime.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         runID + ".summary",
		RunID:      runID,
		Kind:       "summary",
		URI:        "store://verification/" + runID + "/summary.json",
		MediaType:  "application/json",
		SHA256:     "verification-summary-sha256",
		SizeBytes:  2,
		Summary:    `{"status":"passed"}`,
		Category:   "verification",
		Visibility: "internal",
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("seed verification Evidence: %v", err)
	}
	if _, err := runtime.SaveTraceTopology(ctx, store.TraceTopology{
		ID:            runID + ".topology.skywalking",
		WorkflowRunID: runID,
		WorkflowID:    "workflow.core-10",
		StepID:        "step.core-10",
		CaseID:        "case.core-10",
		RequestID:     "request.core-10",
		TraceID:       "trace.core-10",
		Status:        "complete",
		TopologyJSON:  `{"provider":"skywalking","status":"complete","traceId":"trace.core-10","spanCount":2,"confirmedEdges":[{"source":"service.entry","target":"service.worker"}],"observedNodes":["service.entry","service.worker"]}`,
		TextTopology:  "service.entry -> service.worker",
		CreatedAt:     now,
	}); err != nil {
		t.Fatalf("seed verification topology: %v", err)
	}
}

func withPostgresSchemaStatus(t *testing.T, fn func(context.Context, postgres.Config) (postgres.SchemaStatusResult, error)) {
	t.Helper()
	original := postgresSchemaStatus
	postgresSchemaStatus = fn
	t.Cleanup(func() {
		postgresSchemaStatus = original
	})
}

func withMySQLSchemaStatus(t *testing.T, fn func(context.Context, mysql.Config) (mysql.SchemaStatusResult, error)) {
	t.Helper()
	original := mysqlSchemaStatus
	mysqlSchemaStatus = fn
	t.Cleanup(func() {
		mysqlSchemaStatus = original
	})
}

func withMySQLProvisionDatabase(t *testing.T, fn func(context.Context, mysql.Config) (mysql.ProvisionDatabaseResult, error)) {
	t.Helper()
	original := mysqlProvisionDatabase
	mysqlProvisionDatabase = fn
	t.Cleanup(func() {
		mysqlProvisionDatabase = original
	})
}

func sqliteScalar(t *testing.T, dbPath string, statement string) string {
	t.Helper()
	out, err := exec.Command("sqlite3", dbPath, statement).CombinedOutput()
	if err != nil {
		t.Fatalf("sqlite scalar failed: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func createStoredCaseRun(t *testing.T, runID string, label string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"created"}`)
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	casePath := filepath.Join(dir, "case.json")
	writeAPICaseFile(t, casePath)
	evidenceDir := filepath.Join(dir, "evidence")

	runCLI(t, "case", "run", "--case", casePath, "--base-url", server.URL, "--run-id", runID, "--evidence-dir", evidenceDir, "--profile", "sample")
	t.Logf("created %s stored case run %s", label, runID)
}

func createPostProcessTaskStore(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	storePath := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := sqlite.Open(ctx, sqlite.Config{Path: storePath})
	if err != nil {
		t.Fatalf("open post process task store: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatalf("close post process task store: %v", err)
		}
	})
	seedPostProcessTaskFixture(t, ctx, s, "run.tasks", "")
	return storePath
}

func seedPostProcessTaskFixture(t *testing.T, ctx context.Context, s store.Store, runID string, idPrefix string) {
	t.Helper()
	base := time.Date(2026, 5, 17, 1, 2, 3, 0, time.UTC)
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         runID,
		ProfileID:  "sample",
		WorkflowID: "workflow.alpha",
		Status:     store.StatusPassed,
		StartedAt:  base,
		FinishedAt: base.Add(time.Second),
		CreatedAt:  base,
		UpdatedAt:  base.Add(time.Second),
	}); err != nil {
		t.Fatalf("create task run: %v", err)
	}
	if _, err := s.RecordEvidence(ctx, store.EvidenceRecord{
		ID:         idPrefix + "evidence.response",
		RunID:      runID,
		CaseRunID:  runID + ".case",
		StepID:     "step-a",
		Kind:       "response",
		URI:        "store://evidence/" + runID + "/response.json",
		MediaType:  "application/json",
		SHA256:     "response-sha256",
		SizeBytes:  2,
		Summary:    `{"statusCode":200}`,
		Category:   "http",
		Visibility: "internal",
		CreatedAt:  base.Add(5 * time.Millisecond),
	}); err != nil {
		t.Fatalf("record task Evidence: %v", err)
	}
	records := []store.PostProcessTask{
		{
			ID:         idPrefix + "task.trace",
			RunID:      runID,
			WorkflowID: "workflow.alpha",
			StepID:     "step-a",
			CaseID:     "case.alpha",
			Kind:       "trace_topology_collect",
			Status:     store.StatusPassed,
			StartedAt:  base.Add(10 * time.Millisecond),
			FinishedAt: base.Add(135 * time.Millisecond),
			CreatedAt:  base.Add(10 * time.Millisecond),
		},
		{
			ID:          idPrefix + "task.logs",
			RunID:       runID,
			WorkflowID:  "workflow.alpha",
			StepID:      "step-b",
			CaseID:      "case.beta",
			Kind:        "runtime_log_collect",
			Status:      store.StatusFailed,
			StartedAt:   base.Add(200 * time.Millisecond),
			FinishedAt:  base.Add(500 * time.Millisecond),
			Error:       "log source missing",
			SummaryJSON: `{"source":"runtime-log"}`,
			CreatedAt:   base.Add(200 * time.Millisecond),
		},
		{
			ID:          idPrefix + "task.trace.skip",
			RunID:       runID,
			WorkflowID:  "workflow.alpha",
			StepID:      "step-c",
			CaseID:      "case.gamma",
			Kind:        "trace_topology_collect",
			Status:      store.StatusSkipped,
			StartedAt:   base.Add(600 * time.Millisecond),
			FinishedAt:  base.Add(600 * time.Millisecond),
			SummaryJSON: `{"reason":"SkyWalking provider unavailable"}`,
			CreatedAt:   base.Add(600 * time.Millisecond),
		},
	}
	for _, record := range records {
		if _, err := s.RecordPostProcessTask(ctx, record); err != nil {
			t.Fatalf("record post process task %s: %v", record.ID, err)
		}
	}
}

func recordCaseRunForCoverage(t *testing.T, ctx context.Context, s store.Store, runID string, caseID string, status string, at time.Time) {
	t.Helper()
	if _, err := s.CreateRun(ctx, store.Run{
		ID:         runID,
		ProfileID:  "sample",
		WorkflowID: caseID,
		Status:     status,
		StartedAt:  at,
		FinishedAt: at.Add(time.Second),
		CreatedAt:  at,
		UpdatedAt:  at.Add(time.Second),
	}); err != nil {
		t.Fatalf("create coverage run %s: %v", runID, err)
	}
	if _, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:         runID + ".case",
		RunID:      runID,
		CaseID:     caseID,
		Status:     status,
		StartedAt:  at,
		FinishedAt: at.Add(time.Second),
		CreatedAt:  at,
	}); err != nil {
		t.Fatalf("record coverage case run %s: %v", runID, err)
	}
}

func createLegacyRuntimeDB(t *testing.T, path string) {
	t.Helper()
	createLegacyRuntimeDBWithIDs(t, path, 7, 11, "case-run-parent")
}

func createLegacyRuntimeDBWithIDs(t *testing.T, path string, workflowLegacyID int64, caseLegacyID int64, parentRunID string) {
	t.Helper()
	statement := fmt.Sprintf(`
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
values (%d, 'workflow.alpha', 'passed', '{"steps":1}', '2026-05-14T01:02:03Z');
insert into interface_node_case_run(id, node_id, case_id, run_id, status, evidence_path, summary_json, created_at)
values (%d, 'node.alpha', 'case.alpha', '%s', 'failed', '.runtime/cases/%s', '{"failure":"expected"}', '2026-05-14T01:03:03Z');
`, workflowLegacyID, caseLegacyID, parentRunID, parentRunID)
	cmd := exec.Command("sqlite3", path, statement)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create legacy db: %v\n%s", err, out)
	}
}
