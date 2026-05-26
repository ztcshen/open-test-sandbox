package sqlstore_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store/sqlstore"
)

type migrationDB struct {
	ctx   context.Context
	db    *sql.DB
	state *fakeSQLState
}

func newMigrationDB(t *testing.T) *migrationDB {
	t.Helper()

	db, state := openFakeSQLDB(t)
	t.Cleanup(func() {
		_ = db.Close()
	})

	return &migrationDB{
		ctx:   context.Background(),
		db:    db,
		state: state,
	}
}

func (m *migrationDB) queueMissingSchemaVersionTable() {
	m.state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(0)}},
	})
}

func (m *migrationDB) queueExistingSchemaVersion(version int) {
	m.state.queueRows(fakeRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	m.state.queueRows(fakeRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(version)}},
	})
}

func (m *migrationDB) queueBootstrapFromEmptySchema() {
	m.queueMissingSchemaVersionTable()
	m.queueExistingSchemaVersion(sqlstore.CurrentSchemaVersion)
}

func (m *migrationDB) queueUpgradeFromSchemaVersion(version int) {
	m.queueExistingSchemaVersion(version)
	m.queueExistingSchemaVersion(sqlstore.CurrentSchemaVersion)
}

func (m *migrationDB) schemaStatus(t *testing.T, dialect sqlstore.Dialect) sqlstore.SchemaStatusResult {
	t.Helper()

	status, err := sqlstore.SchemaStatus(m.ctx, m.db, dialect)
	if err != nil {
		t.Fatalf("schema status: %v", err)
	}
	return status
}

func (m *migrationDB) upgradeSchema(t *testing.T, dialect sqlstore.Dialect, context string) sqlstore.SchemaStatusResult {
	t.Helper()

	status, err := sqlstore.UpgradeSchema(m.ctx, m.db, dialect)
	if err != nil {
		t.Fatalf("%s: %v", context, err)
	}
	return status
}

func (m *migrationDB) execSQL() string {
	return joinedSQLExecs(m.state.execsSnapshot())
}

func assertPendingCoreSchema(t *testing.T, status sqlstore.SchemaStatusResult, context string) {
	t.Helper()

	if status.CurrentVersion != 0 || status.TargetVersion != sqlstore.CurrentSchemaVersion || !status.HasPending() {
		t.Fatalf("%s = %#v", context, status)
	}
}

func assertAppliedCoreSchema(t *testing.T, status sqlstore.SchemaStatusResult, context string) {
	t.Helper()

	if status.CurrentVersion != sqlstore.CurrentSchemaVersion || status.AppliedCount != 1 || status.HasPending() {
		t.Fatalf("%s = %#v", context, status)
	}
}

func assertLatestCoreSchema(t *testing.T, status sqlstore.SchemaStatusResult, context string) {
	t.Helper()

	if status.AppliedCount != 0 || status.HasPending() {
		t.Fatalf("%s = %#v", context, status)
	}
}

func assertCoreSchemaVersionInsertArgs(t *testing.T, call fakeSQLCall) {
	t.Helper()

	if fmt.Sprint(call.args[0]) != fmt.Sprint(sqlstore.CurrentSchemaVersion) || call.args[1] != sqlstore.CoreSchemaName {
		t.Fatalf("version insert args = %#v", call.args)
	}
	if _, ok := call.args[2].(time.Time); !ok {
		t.Fatalf("version insert applied_at arg = %#v, want time.Time", call.args[2])
	}
}

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
			assertSQLContains(t, joined, tt.name+" core schema", tt.want...)
			assertSQLOmits(t, joined, tt.name+" core schema", tt.mustNot...)
		})
	}
}

func TestCoreSchemaSQLKeepsSharedIndexesStable(t *testing.T) {
	statements := sqlstore.CoreSchemaSQL(sqlstore.PostgresDialect{})
	joined := strings.Join(statements, "\n")
	assertSQLContains(t, joined, "core schema shared indexes",
		"idx_api_case_runs_run_id_created_at",
		"idx_evidence_records_run_id_created_at",
		"idx_trace_topologies_workflow_run_id_created_at",
		"idx_post_process_tasks_run_id_created_at",
		"idx_config_versions_active_published",
		"idx_environments_verified_status",
		"idx_environments_verification",
	)
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
	assertSQLContains(t, joined, "core schema environment catalog",
		"create table if not exists environments",
		"id text primary key",
		"verified boolean not null",
		"services_json jsonb not null",
		"repos_json jsonb not null",
		"compose_json jsonb not null",
		"health_checks_json jsonb not null",
		"last_verified_at timestamptz",
		"summary_json jsonb not null",
	)
}

func TestCoreSchemaSQLIncludesEnvironmentComponentAssets(t *testing.T) {
	statements := sqlstore.CoreSchemaSQL(sqlstore.PostgresDialect{})
	joined := strings.Join(statements, "\n")
	assertSQLContains(t, joined, "core schema environment component assets",
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
	)
	assertSQLOmits(t, joined, "core schema environment component assets",
		"create table if not exists service_dependencies",
		"create table if not exists service_config_assets",
		"idx_service_config_assets_target",
		"idx_service_config_assets_service_order",
	)
}

func TestCoreSchemaSQLDoesNotApplyCommentsBeforeIncrementalMigrations(t *testing.T) {
	for _, dialect := range []sqlstore.Dialect{sqlstore.PostgresDialect{}, sqlstore.MySQLDialect{}} {
		t.Run(dialect.Name(), func(t *testing.T) {
			statements := sqlstore.CoreSchemaSQL(dialect)
			joined := strings.Join(statements, "\n")
			assertSQLOmits(t, joined, dialect.Name()+" core schema comments",
				"comment on table",
				"comment on column",
				" comment = ",
				" comment '",
			)
		})
	}
}

func TestSchemaDDLIncludesPostgreSQLComments(t *testing.T) {
	statements := sqlstore.SchemaDDL(sqlstore.PostgresDialect{})
	joined := strings.Join(statements, "\n")
	assertSQLContains(t, joined, "postgres schema comments",
		`comment on table "runs" is 'Workflow run records and their execution summary.'`,
		`comment on column "runs"."summary_json" is 'Machine-readable run summary used by APIs and reports.'`,
		`comment on table "environment_components" is 'Runtime components that make up a registered environment.'`,
		`comment on column "component_config_assets"."content_inline" is 'Inline asset content when the asset is stored directly in the Store.'`,
	)
}

func TestSchemaDDLIncludesMySQLComments(t *testing.T) {
	statements := sqlstore.SchemaDDL(sqlstore.MySQLDialect{})
	joined := strings.Join(statements, "\n")
	assertSQLContains(t, joined, "mysql schema comments",
		"alter table `runs` comment = 'Workflow run records and their execution summary.'",
		"alter table `runs` modify column `environment_id` varchar(128) not null default '' comment 'Environment where the workflow run executed.'",
		"alter table `runs` modify column `summary_json` json not null comment 'Machine-readable run summary used by APIs and reports.'",
		"alter table `environment_components` comment = 'Runtime components that make up a registered environment.'",
		"alter table `component_config_assets` modify column `content_inline` mediumtext not null comment 'Inline asset content when the asset is stored directly in the Store.'",
	)
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
			migration := newMigrationDB(t)
			migration.queueUpgradeFromSchemaVersion(9)

			status := migration.upgradeSchema(t, tt.dialect, "upgrade v9 schema")
			assertAppliedCoreSchema(t, status, "upgraded v9 schema status")
			assertSQLContains(t, migration.execSQL(), tt.name+" v9 upgrade comments", tt.want...)
		})
	}
}

func TestUpgradeSchemaAppliesCommentsAfterLegacyColumnMigrations(t *testing.T) {
	tests := []struct {
		name             string
		dialect          sqlstore.Dialect
		addColumnSQL     string
		columnCommentSQL string
	}{
		{
			name:             "postgres",
			dialect:          sqlstore.PostgresDialect{},
			addColumnSQL:     `alter table "runs" add column "environment_id" text not null default ''`,
			columnCommentSQL: `comment on column "runs"."environment_id" is 'Environment where the workflow run executed.'`,
		},
		{
			name:             "mysql",
			dialect:          sqlstore.MySQLDialect{},
			addColumnSQL:     "alter table `runs` add column `environment_id` varchar(128) not null default ''",
			columnCommentSQL: "alter table `runs` modify column `environment_id` varchar(128) not null default '' comment 'Environment where the workflow run executed.'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migration := newMigrationDB(t)
			migration.queueUpgradeFromSchemaVersion(5)

			migration.upgradeSchema(t, tt.dialect, "upgrade v5 schema")

			execs := migration.state.execsSnapshot()
			addIndex := -1
			commentIndex := -1
			commentCount := 0
			for i, exec := range execs {
				if strings.Contains(exec.query, tt.addColumnSQL) && addIndex == -1 {
					addIndex = i
				}
				if strings.Contains(exec.query, tt.columnCommentSQL) {
					if commentIndex == -1 {
						commentIndex = i
					}
					commentCount++
				}
			}
			if addIndex == -1 {
				t.Fatalf("%s v5 upgrade missing add-column migration:\n%s", tt.name, joinedSQLExecs(execs))
			}
			if commentIndex == -1 {
				t.Fatalf("%s v5 upgrade missing environment_id comment migration:\n%s", tt.name, joinedSQLExecs(execs))
			}
			if commentIndex < addIndex {
				t.Fatalf("%s v5 upgrade commented environment_id before adding it: add=%d comment=%d\n%s", tt.name, addIndex, commentIndex, joinedSQLExecs(execs))
			}
			if commentCount != 1 {
				t.Fatalf("%s v5 upgrade applied environment_id comment %d times, want 1:\n%s", tt.name, commentCount, joinedSQLExecs(execs))
			}
		})
	}
}

func TestSchemaStatusAndUpgradeSchemaUseDialectMigrations(t *testing.T) {
	tests := []struct {
		name                  string
		dialect               sqlstore.Dialect
		tableProbeFragments   []string
		firstDDLFragments     []string
		versionInsertContains []string
		versionInsertOmits    []string
		versionUpsertContains []string
	}{
		{
			name:                  "postgres",
			dialect:               sqlstore.PostgresDialect{},
			tableProbeFragments:   []string{"information_schema.tables", "schema_versions"},
			firstDDLFragments:     []string{"create table if not exists schema_versions"},
			versionInsertContains: []string{"insert into schema_versions", "values ($1, $2, $3)"},
		},
		{
			name:                  "mysql",
			dialect:               sqlstore.MySQLDialect{},
			tableProbeFragments:   []string{"information_schema.tables", "table_schema = database()", "schema_versions"},
			firstDDLFragments:     []string{"create table if not exists schema_versions", "applied_at datetime(6) not null"},
			versionInsertContains: []string{"insert into schema_versions", "values (?, ?, ?)"},
			versionInsertOmits:    []string{"$1"},
			versionUpsertContains: []string{"on duplicate key update", "applied_at = values(applied_at)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migration := newMigrationDB(t)

			migration.queueMissingSchemaVersionTable()
			status := migration.schemaStatus(t, tt.dialect)
			assertPendingCoreSchema(t, status, "empty schema status")
			assertSQLContains(t, migration.state.lastQuery(t).query, tt.name+" schema status table existence query", tt.tableProbeFragments...)

			migration.queueBootstrapFromEmptySchema()
			upgraded := migration.upgradeSchema(t, tt.dialect, "upgrade schema")
			assertAppliedCoreSchema(t, upgraded, "upgraded schema status")
			execs := migration.state.execsSnapshot()
			if len(execs) < len(sqlstore.CoreSchemaSQL(tt.dialect))+1 {
				t.Fatalf("exec count = %d, want at least ddl + version insert", len(execs))
			}
			assertSQLContains(t, execs[0].query, tt.name+" first upgrade statement", tt.firstDDLFragments...)
			last := execs[len(execs)-1]
			assertSQLContains(t, last.query, tt.name+" version insert query", tt.versionInsertContains...)
			assertSQLOmits(t, last.query, tt.name+" version insert query", tt.versionInsertOmits...)
			assertSQLContains(t, last.query, tt.name+" version insert upsert", tt.versionUpsertContains...)
			assertCoreSchemaVersionInsertArgs(t, last)

			migration.state.clearExecs()
			migration.queueExistingSchemaVersion(sqlstore.CurrentSchemaVersion)
			latest := migration.upgradeSchema(t, tt.dialect, "upgrade latest schema")
			assertLatestCoreSchema(t, latest, "latest schema status")
			if execs := migration.state.execsSnapshot(); len(execs) != 0 {
				t.Fatalf("latest schema should not execute DDL: %#v", execs)
			}
		})
	}
}

func TestUpgradeSchemaWidensMySQLRuntimeIdentifiersFromVersionFour(t *testing.T) {
	migration := newMigrationDB(t)
	dialect := sqlstore.MySQLDialect{}
	migration.queueUpgradeFromSchemaVersion(4)

	status := migration.upgradeSchema(t, dialect, "upgrade mysql v4 schema")
	assertAppliedCoreSchema(t, status, "upgraded mysql v4 schema status")
	assertSQLContains(t, migration.execSQL(), "mysql v4 upgrade",
		"alter table `runs` modify column `id` varchar(255) not null",
		"alter table `api_case_runs` modify column `id` varchar(255) not null, modify column `run_id` varchar(255) not null",
		"alter table `evidence_records` modify column `id` varchar(255) not null",
		"modify column `workflow_run_id` varchar(255) not null",
		"alter table `environments` modify column `last_verification_run_id` varchar(255) not null",
		"alter table `runs` add column `environment_id` varchar(128) not null default ''",
		"drop table if exists `service_config_assets`",
		"drop table if exists `service_dependencies`",
	)
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
			migration := newMigrationDB(t)
			migration.queueUpgradeFromSchemaVersion(5)

			status := migration.upgradeSchema(t, tt.dialect, "upgrade v5 schema")
			assertAppliedCoreSchema(t, status, "upgraded v5 schema status")
			assertSQLContains(t, migration.execSQL(), tt.name+" v5 upgrade", tt.want...)
		})
	}
}

func TestUpgradeSchemaWidensMySQLTextColumnsFromVersionSix(t *testing.T) {
	migration := newMigrationDB(t)
	dialect := sqlstore.MySQLDialect{}
	migration.queueUpgradeFromSchemaVersion(6)

	status := migration.upgradeSchema(t, dialect, "upgrade mysql v6 schema")
	assertAppliedCoreSchema(t, status, "upgraded mysql v6 schema status")
	assertSQLContains(t, migration.execSQL(), "mysql v6 upgrade",
		"alter table `runs` modify column `evidence_root` mediumtext not null",
		"alter table `component_config_assets` modify column `target_path` mediumtext not null",
		"alter table `component_config_assets` modify column `content_inline` mediumtext not null",
		"alter table `component_config_assets` modify column `sha256` mediumtext not null",
	)
}

func TestUpgradeSchemaWidensMySQLConfigVersionIdentifiersFromVersionSeven(t *testing.T) {
	migration := newMigrationDB(t)
	dialect := sqlstore.MySQLDialect{}
	migration.queueUpgradeFromSchemaVersion(7)

	status := migration.upgradeSchema(t, dialect, "upgrade mysql v7 schema")
	assertAppliedCoreSchema(t, status, "upgraded mysql v7 schema status")
	assertSQLContains(t, migration.execSQL(), "mysql v7 upgrade",
		"alter table `config_versions` modify column `id` varchar(255) not null",
		"alter table `config_read_model` modify column `config_version_id` varchar(255) not null",
	)
}

func TestUpgradeSchemaTreatsDuplicateMySQLIndexAsIdempotent(t *testing.T) {
	migration := newMigrationDB(t)
	dialect := sqlstore.MySQLDialect{}
	migration.queueExistingSchemaVersion(4)

	for i := 0; i < 3; i++ {
		migration.state.queueExecError(nil)
	}
	migration.state.queueExecError(fmt.Errorf("Error 1061 (42000): Duplicate key name 'idx_api_case_runs_run_id_created_at'"))
	migration.queueExistingSchemaVersion(sqlstore.CurrentSchemaVersion)

	status := migration.upgradeSchema(t, dialect, "upgrade mysql schema should tolerate duplicate index replay")
	assertAppliedCoreSchema(t, status, "upgraded mysql schema status")
}

func TestUpgradeSchemaIgnoresExistingMySQLIndexesDuringReplay(t *testing.T) {
	migration := newMigrationDB(t)
	dialect := sqlstore.MySQLDialect{}
	migration.queueExistingSchemaVersion(4)

	migration.state.queueExecError(nil)
	migration.state.queueExecError(nil)
	migration.state.queueExecError(nil)
	migration.state.queueExecError(errors.New("Error 1061 (42000): Duplicate key name 'idx_api_case_runs_run_id_created_at'"))
	migration.queueExistingSchemaVersion(sqlstore.CurrentSchemaVersion)

	status := migration.upgradeSchema(t, dialect, "upgrade mysql v4 schema with existing index")
	assertAppliedCoreSchema(t, status, "upgraded mysql v4 schema status")
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
			migration := newMigrationDB(t)
			migration.queueUpgradeFromSchemaVersion(1)

			status := migration.upgradeSchema(t, tt.dialect, "upgrade v1 schema")
			assertAppliedCoreSchema(t, status, "upgraded v1 schema status")
			assertSQLContains(t, migration.execSQL(), tt.name+" v1 environment catalog DDL",
				"create table if not exists environments",
				tt.wantColumn,
			)
		})
	}
}
