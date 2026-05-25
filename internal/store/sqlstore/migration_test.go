package sqlstore_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store/sqlstore"
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
				"id varchar(255) primary key",
				"profile_id varchar(128) not null",
				"environment_id varchar(128) not null",
				"evidence_root mediumtext not null",
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
		"idx_api_case_runs_run_id_created_at",
		"idx_evidence_records_run_id_created_at",
		"idx_trace_topologies_workflow_run_id_created_at",
		"idx_post_process_tasks_run_id_created_at",
		"idx_config_versions_active_published",
		"idx_environments_verified_status",
		"idx_environments_verification",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("core schema missing index %q:\n%s", want, joined)
		}
	}
}

func TestCoreSchemaSQLUsesMySQLCompatibleIndexDDL(t *testing.T) {
	statements := sqlstore.CoreSchemaSQL(sqlstore.MySQLDialect{})
	joined := strings.Join(statements, "\n")
	if strings.Contains(joined, "create index if not exists") {
		t.Fatalf("mysql schema should not use index-if-not-exists syntax:\n%s", joined)
	}
	if !strings.Contains(joined, "create index `idx_api_case_runs_run_id_created_at`") {
		t.Fatalf("mysql schema missing quoted index DDL:\n%s", joined)
	}
	if !strings.Contains(joined, "consumer_component_id varchar(128) not null") || !strings.Contains(joined, "provider_component_id varchar(128) not null") {
		t.Fatalf("mysql component graph keys should stay bounded for composite indexes:\n%s", joined)
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
		"create table if not exists component_dependencies",
		"consumer_component_id text not null",
		"provider_component_id text not null",
		"phase text not null",
		"capability text not null",
		"profile_json jsonb not null",
		"idx_component_dependencies_provider",
		"create table if not exists component_config_assets",
		"owner_component_id text not null",
		"asset_kind text not null",
		"target_component_id text not null",
		"content_inline text not null",
		"remote_ref_json jsonb not null",
		"size_bytes integer not null",
		`"sensitive" boolean not null`,
		"idx_component_config_assets_target",
		"idx_component_config_assets_owner_order",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("core schema missing environment component asset DDL %q:\n%s", want, joined)
		}
	}
	for _, unwanted := range []string{
		"create table if not exists service_dependencies",
		"create table if not exists service_config_assets",
		"idx_service_config_assets_target",
		"idx_service_config_assets_service_order",
	} {
		if strings.Contains(joined, unwanted) {
			t.Fatalf("core schema should not contain legacy service asset DDL %q:\n%s", unwanted, joined)
		}
	}
}

func TestCoreSchemaSQLIncludesPostgreSQLComments(t *testing.T) {
	statements := sqlstore.CoreSchemaSQL(sqlstore.PostgresDialect{})
	joined := strings.Join(statements, "\n")
	for _, want := range []string{
		`comment on table "runs" is 'Workflow run records and their execution summary.'`,
		`comment on column "runs"."summary_json" is 'Machine-readable run summary used by APIs and reports.'`,
		`comment on table "environment_components" is 'Runtime components that make up a registered environment.'`,
		`comment on column "component_config_assets"."content_inline" is 'Inline asset content when the asset is stored directly in the Store.'`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("postgres schema comments missing %q:\n%s", want, joined)
		}
	}
}

func TestCoreSchemaSQLIncludesMySQLComments(t *testing.T) {
	statements := sqlstore.CoreSchemaSQL(sqlstore.MySQLDialect{})
	joined := strings.Join(statements, "\n")
	for _, want := range []string{
		"alter table `runs` comment = 'Workflow run records and their execution summary.'",
		"alter table `runs` modify column `summary_json` json not null comment 'Machine-readable run summary used by APIs and reports.'",
		"alter table `environment_components` comment = 'Runtime components that make up a registered environment.'",
		"alter table `component_config_assets` modify column `content_inline` mediumtext not null comment 'Inline asset content when the asset is stored directly in the Store.'",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("mysql schema comments missing %q:\n%s", want, joined)
		}
	}
}

func TestUpgradeSchemaAddsCommentsFromVersionNine(t *testing.T) {
	tests := []struct {
		name    string
		dialect sqlstore.Dialect
		want    []string
	}{
		{
			name:    "postgres",
			dialect: sqlstore.PostgresDialect{},
			want: []string{
				`comment on table "runs" is 'Workflow run records and their execution summary.'`,
				`comment on column "runs"."summary_json" is 'Machine-readable run summary used by APIs and reports.'`,
			},
		},
		{
			name:    "mysql",
			dialect: sqlstore.MySQLDialect{},
			want: []string{
				"alter table `runs` comment = 'Workflow run records and their execution summary.'",
				"alter table `runs` modify column `summary_json` json not null comment 'Machine-readable run summary used by APIs and reports.'",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, state := openFakeSQLDB(t)
			defer db.Close()

			state.queueRows(fakeRows{
				columns: []string{"exists"},
				values:  [][]driver.Value{{int64(1)}},
			})
			state.queueRows(fakeRows{
				columns: []string{"version"},
				values:  [][]driver.Value{{int64(9)}},
			})
			state.queueRows(fakeRows{
				columns: []string{"exists"},
				values:  [][]driver.Value{{int64(1)}},
			})
			state.queueRows(fakeRows{
				columns: []string{"version"},
				values:  [][]driver.Value{{int64(sqlstore.CurrentSchemaVersion)}},
			})

			status, err := sqlstore.UpgradeSchema(ctx, db, tt.dialect)
			if err != nil {
				t.Fatalf("upgrade v9 schema: %v", err)
			}
			if status.CurrentVersion != sqlstore.CurrentSchemaVersion || status.AppliedCount != 1 || status.HasPending() {
				t.Fatalf("upgraded v9 schema status = %#v", status)
			}
			joinedExecs := strings.Builder{}
			for _, exec := range state.execsSnapshot() {
				joinedExecs.WriteString(exec.query)
				joinedExecs.WriteByte('\n')
			}
			for _, want := range tt.want {
				if !strings.Contains(joinedExecs.String(), want) {
					t.Fatalf("%s v9 upgrade missing schema comment %q:\n%s", tt.name, want, joinedExecs.String())
				}
			}
		})
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

func TestSchemaStatusAndUpgradeSchemaUseMySQLMigrations(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	dialect := sqlstore.MySQLDialect{}

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
	if !strings.Contains(query.query, "information_schema.tables") || !strings.Contains(query.query, "table_schema = database()") || !strings.Contains(query.query, "schema_versions") {
		t.Fatalf("mysql schema status table existence query = %#v", query)
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
	if !strings.Contains(execs[0].query, "create table if not exists schema_versions") || !strings.Contains(execs[0].query, "applied_at datetime(6) not null") {
		t.Fatalf("first mysql upgrade statement = %s", execs[0].query)
	}
	last := execs[len(execs)-1]
	if !strings.Contains(last.query, "insert into schema_versions") || !strings.Contains(last.query, "values (?, ?, ?)") || strings.Contains(last.query, "$1") {
		t.Fatalf("mysql version insert query = %s", last.query)
	}
	if !strings.Contains(last.query, "on duplicate key update") || !strings.Contains(last.query, "applied_at = values(applied_at)") {
		t.Fatalf("mysql version insert should use mysql upsert: %s", last.query)
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

func TestUpgradeSchemaWidensMySQLRuntimeIdentifiersFromVersionFour(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	dialect := sqlstore.MySQLDialect{}

	state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(4)}},
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
		t.Fatalf("upgrade mysql v4 schema: %v", err)
	}
	if status.CurrentVersion != sqlstore.CurrentSchemaVersion || status.AppliedCount != 1 || status.HasPending() {
		t.Fatalf("upgraded mysql v4 schema status = %#v", status)
	}

	joinedExecs := strings.Builder{}
	for _, exec := range state.execsSnapshot() {
		joinedExecs.WriteString(exec.query)
		joinedExecs.WriteByte('\n')
	}
	for _, want := range []string{
		"alter table `runs` modify column `id` varchar(255) not null",
		"alter table `api_case_runs` modify column `id` varchar(255) not null, modify column `run_id` varchar(255) not null",
		"alter table `evidence_records` modify column `id` varchar(255) not null",
		"modify column `workflow_run_id` varchar(255) not null",
		"alter table `environments` modify column `last_verification_run_id` varchar(255) not null",
		"alter table `runs` add column `environment_id` varchar(128) not null default ''",
		"drop table if exists `service_config_assets`",
		"drop table if exists `service_dependencies`",
	} {
		if !strings.Contains(joinedExecs.String(), want) {
			t.Fatalf("mysql v4 upgrade missing %q:\n%s", want, joinedExecs.String())
		}
	}
}

func TestUpgradeSchemaAddsRunEnvironmentAndDropsLegacyServiceGraphTables(t *testing.T) {
	tests := []struct {
		name    string
		dialect sqlstore.Dialect
		want    []string
	}{
		{
			name:    "postgres",
			dialect: sqlstore.PostgresDialect{},
			want: []string{
				`alter table "runs" add column "environment_id" text not null default ''`,
				`drop table if exists "service_config_assets"`,
				`drop table if exists "service_dependencies"`,
			},
		},
		{
			name:    "mysql",
			dialect: sqlstore.MySQLDialect{},
			want: []string{
				"alter table `runs` add column `environment_id` varchar(128) not null default ''",
				"drop table if exists `service_config_assets`",
				"drop table if exists `service_dependencies`",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, state := openFakeSQLDB(t)
			defer db.Close()

			state.queueRows(fakeRows{
				columns: []string{"exists"},
				values:  [][]driver.Value{{int64(1)}},
			})
			state.queueRows(fakeRows{
				columns: []string{"version"},
				values:  [][]driver.Value{{int64(5)}},
			})
			state.queueRows(fakeRows{
				columns: []string{"exists"},
				values:  [][]driver.Value{{int64(1)}},
			})
			state.queueRows(fakeRows{
				columns: []string{"version"},
				values:  [][]driver.Value{{int64(sqlstore.CurrentSchemaVersion)}},
			})

			status, err := sqlstore.UpgradeSchema(ctx, db, tt.dialect)
			if err != nil {
				t.Fatalf("upgrade v5 schema: %v", err)
			}
			if status.CurrentVersion != sqlstore.CurrentSchemaVersion || status.AppliedCount != 1 || status.HasPending() {
				t.Fatalf("upgraded v5 schema status = %#v", status)
			}
			joinedExecs := strings.Builder{}
			for _, exec := range state.execsSnapshot() {
				joinedExecs.WriteString(exec.query)
				joinedExecs.WriteByte('\n')
			}
			for _, want := range tt.want {
				if !strings.Contains(joinedExecs.String(), want) {
					t.Fatalf("%s v5 upgrade missing %q:\n%s", tt.name, want, joinedExecs.String())
				}
			}
		})
	}
}

func TestUpgradeSchemaWidensMySQLTextColumnsFromVersionSix(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	dialect := sqlstore.MySQLDialect{}

	state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(6)}},
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
		t.Fatalf("upgrade mysql v6 schema: %v", err)
	}
	if status.CurrentVersion != sqlstore.CurrentSchemaVersion || status.AppliedCount != 1 || status.HasPending() {
		t.Fatalf("upgraded mysql v6 schema status = %#v", status)
	}
	joinedExecs := strings.Builder{}
	for _, exec := range state.execsSnapshot() {
		joinedExecs.WriteString(exec.query)
		joinedExecs.WriteByte('\n')
	}
	for _, want := range []string{
		"alter table `runs` modify column `evidence_root` mediumtext not null",
		"alter table `component_config_assets` modify column `target_path` mediumtext not null",
		"alter table `component_config_assets` modify column `content_inline` mediumtext not null",
		"alter table `component_config_assets` modify column `sha256` mediumtext not null",
	} {
		if !strings.Contains(joinedExecs.String(), want) {
			t.Fatalf("mysql v6 upgrade missing %q:\n%s", want, joinedExecs.String())
		}
	}
}

func TestUpgradeSchemaWidensMySQLConfigVersionIdentifiersFromVersionSeven(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	dialect := sqlstore.MySQLDialect{}

	state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(7)}},
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
		t.Fatalf("upgrade mysql v7 schema: %v", err)
	}
	if status.CurrentVersion != sqlstore.CurrentSchemaVersion || status.AppliedCount != 1 || status.HasPending() {
		t.Fatalf("upgraded mysql v7 schema status = %#v", status)
	}
	joinedExecs := strings.Builder{}
	for _, exec := range state.execsSnapshot() {
		joinedExecs.WriteString(exec.query)
		joinedExecs.WriteByte('\n')
	}
	for _, want := range []string{
		"alter table `config_versions` modify column `id` varchar(255) not null",
		"alter table `config_read_model` modify column `config_version_id` varchar(255) not null",
	} {
		if !strings.Contains(joinedExecs.String(), want) {
			t.Fatalf("mysql v7 upgrade missing %q:\n%s", want, joinedExecs.String())
		}
	}
}

func TestUpgradeSchemaTreatsDuplicateMySQLIndexAsIdempotent(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	dialect := sqlstore.MySQLDialect{}

	state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(4)}},
	})
	for i := 0; i < 3; i++ {
		state.queueExecError(nil)
	}
	state.queueExecError(fmt.Errorf("Error 1061 (42000): Duplicate key name 'idx_api_case_runs_run_id_created_at'"))
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
		t.Fatalf("upgrade mysql schema should tolerate duplicate index replay: %v", err)
	}
	if status.CurrentVersion != sqlstore.CurrentSchemaVersion || status.AppliedCount != 1 || status.HasPending() {
		t.Fatalf("upgraded mysql schema status = %#v", status)
	}
}

func TestUpgradeSchemaIgnoresExistingMySQLIndexesDuringReplay(t *testing.T) {
	ctx := context.Background()
	db, state := openFakeSQLDB(t)
	defer db.Close()
	dialect := sqlstore.MySQLDialect{}

	state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(4)}},
	})
	state.queueExecError(nil)
	state.queueExecError(nil)
	state.queueExecError(nil)
	state.queueExecError(errors.New("Error 1061 (42000): Duplicate key name 'idx_api_case_runs_run_id_created_at'"))
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
		t.Fatalf("upgrade mysql v4 schema with existing index: %v", err)
	}
	if status.CurrentVersion != sqlstore.CurrentSchemaVersion || status.AppliedCount != 1 || status.HasPending() {
		t.Fatalf("upgraded mysql v4 schema status = %#v", status)
	}
}

func TestUpgradeSchemaAppliesEnvironmentCatalogToVersionOneDatabase(t *testing.T) {
	tests := []struct {
		name       string
		dialect    sqlstore.Dialect
		wantColumn string
	}{
		{name: "postgres", dialect: sqlstore.PostgresDialect{}, wantColumn: "services_json jsonb not null"},
		{name: "mysql", dialect: sqlstore.MySQLDialect{}, wantColumn: "services_json json not null"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			db, state := openFakeSQLDB(t)
			defer db.Close()

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

			status, err := sqlstore.UpgradeSchema(ctx, db, tt.dialect)
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
			if !strings.Contains(joinedExecs.String(), "create table if not exists environments") || !strings.Contains(joinedExecs.String(), tt.wantColumn) {
				t.Fatalf("%s v1 upgrade did not apply environment catalog DDL:\n%s", tt.name, joinedExecs.String())
			}
		})
	}
}
