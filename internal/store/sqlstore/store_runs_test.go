package sqlstore_test

import (
	"context"
	"database/sql/driver"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlstore"
)

func TestStoreRecordsAndReadsRunsThroughDatabaseSQL(t *testing.T) {
	exerciseStoreRecordsAndReadsRuns(t, runDialectExpectation{
		dialect: sqlstore.PostgresDialect{},
	})
}

func TestStoreRecordsAndReadsRunsUseMySQLDialect(t *testing.T) {
	exerciseStoreRecordsAndReadsRuns(t, runDialectExpectation{
		dialect: sqlstore.MySQLDialect{},
		reject:  "$1",
	})
}

type runDialectExpectation struct {
	dialect sqlstore.Dialect
	reject  string
}

func exerciseStoreRecordsAndReadsRuns(t *testing.T, tt runDialectExpectation) {
	t.Helper()

	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, tt.dialect)
	started := time.Date(2026, 5, 19, 9, 30, 0, 0, time.UTC)

	created, err := s.CreateRun(ctx, store.Run{
		ID:            "run-001",
		ProfileID:     "profile.alpha",
		EnvironmentID: "env.alpha",
		WorkflowID:    "workflow.alpha",
		Status:        store.StatusRunning,
		EvidenceRoot:  ".runtime/evidence/run-001",
		SummaryJSON:   `{"stepCount":1}`,
		StartedAt:     started,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("created run timestamps = %#v", created)
	}
	exec := state.lastExec(t)
	assertSQLContains(t, exec.query, "create run query", "insert into runs", sqlValuesClause(tt.dialect, 11))
	assertSQLOmits(t, exec.query, "create run query", tt.reject)
	if exec.args[0] != "run-001" || exec.args[6] != `{"stepCount":1}` {
		t.Fatalf("create run args = %#v", exec.args)
	}

	queueRunRow(state, created, started, `{"stepCount": 1}`)
	loaded, err := s.GetRun(ctx, "run-001")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if loaded.ID != "run-001" || loaded.EnvironmentID != "env.alpha" || loaded.Status != store.StatusPassed || loaded.SummaryJSON != `{"stepCount":1}` || !loaded.StartedAt.Equal(started) {
		t.Fatalf("loaded run = %#v", loaded)
	}
	query := state.lastQuery(t)
	assertSQLContains(t, query.query, "get run query", "from runs where id = "+tt.dialect.BindVar(1))
	if query.args[0] != "run-001" {
		t.Fatalf("get run query = %#v", query)
	}

	queueRunRow(state, created, started, `{"stepCount":1}`)
	runs, err := s.ListRuns(ctx)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "run-001" {
		t.Fatalf("runs = %#v", runs)
	}
}

func queueRunRow(state *fakeSQLState, run store.Run, started time.Time, summaryJSON string) {
	state.queueRows(fakeRows{
		columns: []string{"id", "profile_id", "environment_id", "workflow_id", "status", "evidence_root", "summary_json", "started_at", "finished_at", "created_at", "updated_at"},
		values: [][]driver.Value{{
			"run-001", "profile.alpha", "env.alpha", "workflow.alpha", store.StatusPassed, ".runtime/evidence/run-001", summaryJSON,
			started.Format(time.RFC3339Nano), "", run.CreatedAt.Format(time.RFC3339Nano), run.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
}

func TestPostgresStoreUsesNullForZeroTimestampArgs(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, sqlstore.PostgresDialect{})
	started := time.Date(2026, 5, 19, 9, 30, 0, 0, time.UTC)

	_, err := s.CreateRun(ctx, store.Run{
		ID:            "run-001",
		ProfileID:     "profile.alpha",
		EnvironmentID: "env.alpha",
		WorkflowID:    "workflow.alpha",
		Status:        store.StatusRunning,
		EvidenceRoot:  ".runtime/evidence/run-001",
		SummaryJSON:   `{"stepCount":1}`,
		StartedAt:     started,
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	exec := state.lastExec(t)
	if exec.args[7] != started {
		t.Fatalf("started_at arg = %#v, want time.Time", exec.args[7])
	}
	if exec.args[8] != nil {
		t.Fatalf("finished_at arg = %#v, want nil for zero time", exec.args[8])
	}
}
