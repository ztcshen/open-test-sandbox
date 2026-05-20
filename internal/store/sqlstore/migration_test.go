package sqlstore_test

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strings"
	"testing"
	"time"

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
				"catalog_json jsonb not null",
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
				"catalog_json json not null",
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
				"catalog_json text not null",
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
		"create index if not exists idx_environments_verified_status",
		"create index if not exists idx_environments_verification",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("core schema missing index %q:\n%s", want, joined)
		}
	}
}

func TestCoreSchemaSQLIncludesEnvironmentCatalog(t *testing.T) {
	statements := sqlstore.CoreSchemaSQL(sqlstore.PostgresDialect{})
	joined := strings.Join(statements, "\n")
	for _, want := range []string{
		"create table if not exists environments",
		"id text primary key",
		"verified boolean not null",
		"services_json jsonb not null",
		"repos_json jsonb not null",
		"compose_json jsonb not null",
		"health_checks_json jsonb not null",
		"last_verified_at timestamptz",
		"summary_json jsonb not null",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("core schema missing environment catalog DDL %q:\n%s", want, joined)
		}
	}
}

func TestCoreSchemaSQLIncludesEnvironmentComponentAssets(t *testing.T) {
	statements := sqlstore.CoreSchemaSQL(sqlstore.PostgresDialect{})
	joined := strings.Join(statements, "\n")
	for _, want := range []string{
		"create table if not exists environment_components",
		"component_id text not null",
		"kind text not null",
		"role text not null",
		"runtime_json jsonb not null",
		"healthcheck_json jsonb not null",
		"create table if not exists service_dependencies",
		"dependency_component_id text not null",
		"profile_json jsonb not null",
		"create table if not exists service_config_assets",
		"asset_kind text not null",
		"target_component_id text not null",
		"content_inline text not null",
		"remote_ref_json jsonb not null",
		"size_bytes integer not null",
		"sensitive boolean not null",
		"idx_service_config_assets_target",
		"idx_service_config_assets_service_order",
		"create table if not exists component_dependencies",
		"consumer_component_id text not null",
		"provider_component_id text not null",
		"phase text not null",
		"capability text not null",
		"idx_component_dependencies_provider",
		"create table if not exists component_config_assets",
		"owner_component_id text not null",
		"idx_component_config_assets_target",
		"idx_component_config_assets_owner_order",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("core schema missing environment component asset DDL %q:\n%s", want, joined)
		}
	}
}

func TestSchemaStatusAndUpgradeSchemaUseSharedDatabaseSQLMigrations(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	dialect := sqlstore.PostgresDialect{}

	state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(0)}},
	})
	status, err := sqlstore.SchemaStatus(ctx, db, dialect)
	if err != nil {
		t.Fatalf("schema status: %v", err)
	}
	if status.CurrentVersion != 0 || status.TargetVersion != sqlstore.CurrentSchemaVersion || !status.HasPending() {
		t.Fatalf("empty schema status = %#v", status)
	}
	query := state.lastQuery(t)
	if !strings.Contains(query.query, "information_schema.tables") || !strings.Contains(query.query, "schema_versions") {
		t.Fatalf("schema status table existence query = %#v", query)
	}

	state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(0)}},
	})
	state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(sqlstore.CurrentSchemaVersion)}},
	})
	upgraded, err := sqlstore.UpgradeSchema(ctx, db, dialect)
	if err != nil {
		t.Fatalf("upgrade schema: %v", err)
	}
	if upgraded.CurrentVersion != sqlstore.CurrentSchemaVersion || upgraded.AppliedCount != 1 || upgraded.HasPending() {
		t.Fatalf("upgraded schema status = %#v", upgraded)
	}
	execs := state.execsSnapshot()
	if len(execs) < len(sqlstore.CoreSchemaSQL(dialect))+1 {
		t.Fatalf("exec count = %d, want at least ddl + version insert", len(execs))
	}
	if !strings.Contains(execs[0].query, "create table if not exists schema_versions") {
		t.Fatalf("first upgrade statement = %s", execs[0].query)
	}
	last := execs[len(execs)-1]
	if !strings.Contains(last.query, "insert into schema_versions") || !strings.Contains(last.query, "values ($1, $2, $3)") {
		t.Fatalf("version insert query = %s", last.query)
	}
	if fmt.Sprint(last.args[0]) != fmt.Sprint(sqlstore.CurrentSchemaVersion) || last.args[1] != sqlstore.CoreSchemaName {
		t.Fatalf("version insert args = %#v", last.args)
	}
	if _, ok := last.args[2].(time.Time); !ok {
		t.Fatalf("version insert applied_at arg = %#v, want time.Time", last.args[2])
	}

	state.clearExecs()
	state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(sqlstore.CurrentSchemaVersion)}},
	})
	latest, err := sqlstore.UpgradeSchema(ctx, db, dialect)
	if err != nil {
		t.Fatalf("upgrade latest schema: %v", err)
	}
	if latest.AppliedCount != 0 || latest.HasPending() {
		t.Fatalf("latest schema status = %#v", latest)
	}
	if execs := state.execsSnapshot(); len(execs) != 0 {
		t.Fatalf("latest schema should not execute DDL: %#v", execs)
	}
}

func TestUpgradeSchemaAppliesEnvironmentCatalogToVersionOneDatabase(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	dialect := sqlstore.PostgresDialect{}

	state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(sqlstore.CurrentSchemaVersion)}},
	})

	status, err := sqlstore.UpgradeSchema(ctx, db, dialect)
	if err != nil {
		t.Fatalf("upgrade v1 schema: %v", err)
	}
	if status.CurrentVersion != sqlstore.CurrentSchemaVersion || status.AppliedCount != 1 || status.HasPending() {
		t.Fatalf("upgraded v1 schema status = %#v", status)
	}
	joinedExecs := strings.Builder{}
	for _, exec := range state.execsSnapshot() {
		joinedExecs.WriteString(exec.query)
		joinedExecs.WriteByte('\n')
	}
	if !strings.Contains(joinedExecs.String(), "create table if not exists environments") {
		t.Fatalf("v1 upgrade did not apply environment catalog DDL:\n%s", joinedExecs.String())
	}
}
