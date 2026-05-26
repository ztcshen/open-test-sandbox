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

func TestStoreUpsertsAndReadsBaselineGateThroughDatabaseSQL(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	s := sqlstore.New(db, sqlstore.MySQLDialect{})
	checkedAt := time.Date(2026, 5, 19, 11, 0, 0, 0, time.UTC)

	gate, err := s.UpsertBaselineGate(ctx, store.BaselineGate{
		ProfileID:   "profile.alpha",
		SubjectID:   "workflow.alpha",
		Status:      store.StatusPassed,
		Required:    true,
		SummaryJSON: `{"required":true}`,
		CheckedAt:   checkedAt,
	})
	if err != nil {
		t.Fatalf("upsert baseline gate: %v", err)
	}
	exec := state.lastExec(t)
	if !strings.Contains(exec.query, "insert into baseline_gates") || strings.Contains(exec.query, "$1") || !strings.Contains(exec.query, "values (?, ?, ?, ?, ?, ?, ?)") {
		t.Fatalf("baseline gate query did not use mysql bind vars:\n%s", exec.query)
	}
	if !strings.Contains(exec.query, "on duplicate key update") || !strings.Contains(exec.query, "status = values(status)") {
		t.Fatalf("baseline gate query did not use mysql upsert:\n%s", exec.query)
	}
	if gate.UpdatedAt.IsZero() || exec.args[0] != "profile.alpha" || exec.args[3] != true {
		t.Fatalf("baseline gate/args = %#v %#v", gate, exec.args)
	}

	state.queueRows(fakeRows{
		columns: []string{"profile_id", "subject_id", "status", "required", "summary_json", "checked_at", "updated_at"},
		values: [][]driver.Value{{
			"profile.alpha", "workflow.alpha", store.StatusPassed, true, `{"required":true}`,
			checkedAt.Format(time.RFC3339Nano), gate.UpdatedAt.Format(time.RFC3339Nano),
		}},
	})
	loaded, err := s.GetBaselineGate(ctx, "profile.alpha", "workflow.alpha")
	if err != nil {
		t.Fatalf("get baseline gate: %v", err)
	}
	if loaded.ProfileID != "profile.alpha" || !loaded.Required || !loaded.CheckedAt.Equal(checkedAt) {
		t.Fatalf("loaded baseline gate = %#v", loaded)
	}
	query := state.lastQuery(t)
	if !strings.Contains(query.query, "from baseline_gates where profile_id = ? and subject_id = ?") || query.args[0] != "profile.alpha" || query.args[1] != "workflow.alpha" {
		t.Fatalf("baseline gate get query = %#v", query)
	}
}
