package store_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	"open-test-sandbox/internal/store/schema"
	"open-test-sandbox/internal/store/sqlite"
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
