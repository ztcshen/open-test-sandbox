package sqlstore_test

import (
	"strings"
	"testing"

	"open-test-sandbox/internal/store/sqlstore"
)

func TestCoreSchemaSQLUsesDialectColumnTypes(t *testing.T) {
	tests := []struct {
		name       string
		dialect    sqlstore.Dialect
		want       []string
		mustNot    []string
		statementN int
	}{
		{
			name:    "postgres",
			dialect: sqlstore.PostgresDialect{},
			want: []string{
				"applied_at timestamptz not null",
				"summary_json jsonb not null",
				"started_at timestamptz",
				"labels_json jsonb not null",
				"payload_json jsonb not null",
				"active boolean not null",
			},
			mustNot: []string{"summary_json text not null"},
		},
		{
			name:    "mysql",
			dialect: sqlstore.MySQLDialect{},
			want: []string{
				"applied_at datetime(6) not null",
				"summary_json json not null",
				"started_at datetime(6)",
				"labels_json json not null",
				"payload_json json not null",
				"active boolean not null",
			},
			mustNot: []string{"timestamptz", "jsonb"},
		},
		{
			name:    "sqlite",
			dialect: sqlstore.SQLiteDialect{},
			want: []string{
				"applied_at text not null",
				"summary_json text not null",
				"started_at text",
				"labels_json text not null",
				"payload_json text not null",
				"active integer not null",
			},
			mustNot: []string{"timestamptz", "datetime(6)", "jsonb"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statements := sqlstore.CoreSchemaSQL(tt.dialect)
			if len(statements) < 4 {
				t.Fatalf("core schema statements = %d, want at least 4", len(statements))
			}
			joined := strings.Join(statements, "\n")
			for _, want := range tt.want {
				if !strings.Contains(joined, want) {
					t.Fatalf("core schema missing %q:\n%s", want, joined)
				}
			}
			for _, unwanted := range tt.mustNot {
				if strings.Contains(joined, unwanted) {
					t.Fatalf("core schema unexpectedly contained %q:\n%s", unwanted, joined)
				}
			}
		})
	}
}

func TestCoreSchemaSQLKeepsSharedIndexesStable(t *testing.T) {
	statements := sqlstore.CoreSchemaSQL(sqlstore.PostgresDialect{})
	joined := strings.Join(statements, "\n")
	for _, want := range []string{
		"create index if not exists idx_api_case_runs_run_id_created_at",
		"create index if not exists idx_evidence_records_run_id_created_at",
		"create index if not exists idx_trace_topologies_workflow_run_id_created_at",
		"create index if not exists idx_post_process_tasks_run_id_created_at",
		"create index if not exists idx_config_versions_active_published",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("core schema missing index %q:\n%s", want, joined)
		}
	}
}
