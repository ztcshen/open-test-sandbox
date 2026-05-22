package store_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"agent-testbench/internal/store/schema"
	"agent-testbench/internal/store/sqlite"
)

func TestSQLiteSchemaUpgradesAreIdempotent(t *testing.T) {
	ctx := context.Background()
	cfg := sqlite.Config{Path: filepath.Join(t.TempDir(), "store.sqlite")}

	status, err := sqlite.SchemaStatus(ctx, cfg)
	if err != nil {
		t.Fatalf("initial schema upgrade status: %v", err)
	}
	if status.CurrentVersion != 0 || status.TargetVersion != schema.CurrentVersion || !status.HasPending() {
		t.Fatalf("initial status = %#v", status)
	}

	first, err := sqlite.UpgradeSchema(ctx, cfg)
	if err != nil {
		t.Fatalf("first upgrade: %v", err)
	}
	if first.CurrentVersion != schema.CurrentVersion || first.AppliedCount != len(schema.All()) || first.HasPending() {
		t.Fatalf("first upgraded status = %#v", first)
	}

	second, err := sqlite.UpgradeSchema(ctx, cfg)
	if err != nil {
		t.Fatalf("second upgrade: %v", err)
	}
	if second.CurrentVersion != schema.CurrentVersion || second.AppliedCount != 0 || second.HasPending() {
		t.Fatalf("second upgraded status = %#v", second)
	}
}

func TestSQLiteSchemaIncludesTemplateConfigModel(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	if _, err := sqlite.UpgradeSchema(ctx, sqlite.Config{Path: dbPath}); err != nil {
		t.Fatalf("upgrade schema: %v", err)
	}

	tables := sqliteTableNames(t, dbPath)
	for _, table := range []string{
		"template",
		"template_config",
		"node_config",
		"workflow",
		"interface_node",
		"interface_node_field",
		"interface_node_request_template",
		"interface_node_case",
		"workflow_interface_node",
		"fixture_profile",
		"fixture_table_binding",
		"interface_node_case_dependency",
		"config_versions",
		"config_read_model",
	} {
		if !tables[table] {
			t.Fatalf("missing template config table %q in %#v", table, tables)
		}
	}
}

func TestSQLiteSchemaIncludesEvidenceStepRelation(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	if _, err := sqlite.UpgradeSchema(ctx, sqlite.Config{Path: dbPath}); err != nil {
		t.Fatalf("upgrade schema: %v", err)
	}

	columns := sqliteTableColumns(t, dbPath, "evidence_records")
	if !columns["step_id"] {
		t.Fatalf("missing evidence_records.step_id in %#v", columns)
	}
}

func TestSQLiteSchemaIncludesEnvironmentComponentAssets(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "store.sqlite")
	if _, err := sqlite.UpgradeSchema(ctx, sqlite.Config{Path: dbPath}); err != nil {
		t.Fatalf("upgrade schema: %v", err)
	}

	tables := sqliteTableNames(t, dbPath)
	for _, table := range []string{
		"environment_components",
		"service_dependencies",
		"service_config_assets",
		"component_dependencies",
		"component_config_assets",
	} {
		if !tables[table] {
			t.Fatalf("missing environment component asset table %q in %#v", table, tables)
		}
	}
	assetColumns := sqliteTableColumns(t, dbPath, "service_config_assets")
	for _, column := range []string{
		"service_id",
		"asset_kind",
		"target_component_id",
		"content_inline",
		"remote_ref_json",
		"size_bytes",
		"apply_order",
		"sensitive",
	} {
		if !assetColumns[column] {
			t.Fatalf("missing service_config_assets.%s in %#v", column, assetColumns)
		}
	}
	dependencyColumns := sqliteTableColumns(t, dbPath, "component_dependencies")
	for _, column := range []string{
		"consumer_component_id",
		"provider_component_id",
		"phase",
		"capability",
		"profile_json",
	} {
		if !dependencyColumns[column] {
			t.Fatalf("missing component_dependencies.%s in %#v", column, dependencyColumns)
		}
	}
	componentAssetColumns := sqliteTableColumns(t, dbPath, "component_config_assets")
	for _, column := range []string{
		"owner_component_id",
		"asset_kind",
		"target_component_id",
		"content_inline",
		"remote_ref_json",
		"size_bytes",
		"apply_order",
		"sensitive",
	} {
		if !componentAssetColumns[column] {
			t.Fatalf("missing component_config_assets.%s in %#v", column, componentAssetColumns)
		}
	}
}

func sqliteTableNames(t *testing.T, dbPath string) map[string]bool {
	t.Helper()
	out, err := exec.Command("sqlite3", "-json", dbPath, `select name from sqlite_master where type = 'table';`).CombinedOutput()
	if err != nil {
		t.Fatalf("list sqlite tables: %v: %s", err, out)
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("decode sqlite tables: %v: %s", err, out)
	}
	tables := map[string]bool{}
	for _, row := range rows {
		tables[row.Name] = true
	}
	return tables
}

func sqliteTableColumns(t *testing.T, dbPath string, table string) map[string]bool {
	t.Helper()
	out, err := exec.Command("sqlite3", "-json", dbPath, `pragma table_info(`+table+`);`).CombinedOutput()
	if err != nil {
		t.Fatalf("list sqlite columns: %v: %s", err, out)
	}
	var rows []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("decode sqlite columns: %v: %s", err, out)
	}
	columns := map[string]bool{}
	for _, row := range rows {
		columns[row.Name] = true
	}
	return columns
}
