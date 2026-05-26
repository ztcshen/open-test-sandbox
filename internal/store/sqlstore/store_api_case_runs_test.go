package sqlstore_test

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/sqlstore"
)

func TestStoreRecordsAndReadsAPICaseRunsThroughDatabaseSQL(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, sqlstore.MySQLDialect{})
	started := time.Date(2026, 5, 19, 9, 30, 0, 0, time.UTC)
	finished := started.Add(250 * time.Millisecond)

	created, err := s.RecordAPICaseRun(ctx, store.APICaseRun{
		ID:                   "case-run-001",
		RunID:                "run-001",
		CaseID:               "case.alpha",
		Status:               store.StatusPassed,
		RequestSummaryJSON:   `{"method":"GET"}`,
		AssertionSummaryJSON: `{"passed":1}`,
		StartedAt:            started,
		FinishedAt:           finished,
	})
	if err != nil {
		t.Fatalf("record api case run: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Fatalf("created case run timestamp = %#v", created)
	}
	exec := state.lastExec(t)
	if !strings.Contains(exec.query, "insert into api_case_runs") || strings.Contains(exec.query, "$1") || !strings.Contains(exec.query, "values (?, ?, ?, ?, ?, ?, ?, ?, ?)") {
		t.Fatalf("case run query did not use mysql bind vars:\n%s", exec.query)
	}
	if exec.args[2] != "case.alpha" || exec.args[4] != `{"method":"GET"}` {
		t.Fatalf("case run args = %#v", exec.args)
	}

	state.queueRows(fakeRows{
		columns: []string{"id", "run_id", "case_id", "status", "request_summary_json", "assertion_summary_json", "started_at", "finished_at", "created_at"},
		values: [][]driver.Value{{
			"case-run-001", "run-001", "case.alpha", store.StatusPassed, `{"method":"GET"}`, `{"passed":1}`,
			started.Format(time.RFC3339Nano), finished.Format(time.RFC3339Nano), created.CreatedAt.Format(time.RFC3339Nano),
		}},
	})
	caseRuns, err := s.ListAPICaseRuns(ctx, "run-001")
	if err != nil {
		t.Fatalf("list api case runs: %v", err)
	}
	if len(caseRuns) != 1 || caseRuns[0].ID != "case-run-001" || caseRuns[0].CaseID != "case.alpha" {
		t.Fatalf("case runs = %#v", caseRuns)
	}
	query := state.lastQuery(t)
	if !strings.Contains(query.query, "from api_case_runs where run_id = ?") || query.args[0] != "run-001" {
		t.Fatalf("case run list query = %#v", query)
	}
}

func TestStoreListsLatestAPICaseRunsThroughDatabaseSQL(t *testing.T) {
	tests := []struct {
		name    string
		dialect sqlstore.Dialect
	}{
		{name: "postgres", dialect: sqlstore.PostgresDialect{}},
		{name: "mysql", dialect: sqlstore.MySQLDialect{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, state := openFakeSQLDB(t)
			defer db.Close()
			s := sqlstore.New(db, tt.dialect)
			createdAt := time.Date(2026, 5, 19, 9, 30, 0, 0, time.UTC)

			state.queueRows(fakeRows{
				columns: []string{"id", "run_id", "case_id", "status", "request_summary_json", "assertion_summary_json", "started_at", "finished_at", "created_at"},
				values: [][]driver.Value{{
					"case-run-latest", "run-001", "case.alpha", store.StatusPassed, `{"method":"GET"}`, `{"passed":true}`,
					createdAt.Add(-time.Second), createdAt, createdAt,
				}},
			})

			caseRuns, err := s.ListLatestAPICaseRuns(ctx)
			if err != nil {
				t.Fatalf("list latest api case runs: %v", err)
			}
			if len(caseRuns) != 1 || caseRuns[0].ID != "case-run-latest" || caseRuns[0].CaseID != "case.alpha" {
				t.Fatalf("latest case runs = %#v", caseRuns)
			}
			query := state.lastQuery(t)
			if !strings.Contains(query.query, "row_number() over (partition by case_id order by created_at desc, id desc)") || !strings.Contains(query.query, "where rn = 1") {
				t.Fatalf("latest case run query = %s", query.query)
			}
		})
	}
}
