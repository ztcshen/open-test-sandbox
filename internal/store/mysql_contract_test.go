package store_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store/mysql"
)

func TestMySQLStoreContractWithExternalDatabase(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("AGENT_TESTBENCH_MYSQL_TEST_DSN"))
	if dsn == "" {
		t.Skip("set AGENT_TESTBENCH_MYSQL_TEST_DSN to run the MySQL Store contract")
	}
	mode, err := parseMySQLTestDSNMode(os.Getenv("AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	adminCfg, err := mysql.ParseConfigFromURL(dsn)
	if err != nil {
		t.Fatalf("parse mysql admin dsn: %v", err)
	}
	admin, err := sql.Open(adminCfg.DriverName, adminCfg.DSN)
	if err != nil {
		t.Fatalf("open mysql admin connection: %v", err)
	}
	defer admin.Close()
	if err := admin.PingContext(ctx); err != nil {
		t.Fatalf("ping mysql test database: %v", err)
	}
	cfg := adminCfg
	if mode == "existing" {
		databaseName := mysqlTestDatabaseName(t, dsn)
		if !isDedicatedMySQLTestDatabase(databaseName) {
			t.Fatalf("existing MySQL contract database %q is not a dedicated sandbox/smoke/test/ci database", databaseName)
		}
		resetExistingMySQLTestDatabase(t, ctx, admin)
	} else if mode == "create-drop" {
		databaseName := fmt.Sprintf("agent_testbench_contract_%d", time.Now().UnixNano())
		if _, err := admin.ExecContext(ctx, `create database `+quoteMySQLIdent(databaseName)+` character set utf8mb4 collate utf8mb4_unicode_ci`); err != nil {
			t.Fatalf("create mysql test database: %v", err)
		}
		defer func() {
			_, _ = admin.ExecContext(context.Background(), `drop database if exists `+quoteMySQLIdent(databaseName))
		}()
		var parseErr error
		cfg, parseErr = mysql.ParseConfigFromURL(mysqlTestDSNWithDatabase(t, dsn, databaseName))
		if parseErr != nil {
			t.Fatalf("parse database mysql dsn: %v", parseErr)
		}
	}
	if cfg.URL == "" {
		parsedCfg, err := mysql.ParseConfigFromURL(dsn)
		if err != nil {
			t.Fatalf("parse mysql dsn: %v", err)
		}
		cfg = parsedCfg
	}
	upgraded, err := mysql.UpgradeSchema(ctx, cfg)
	if err != nil {
		t.Fatalf("upgrade mysql schema: %v", err)
	}
	if upgraded.CurrentVersion != upgraded.TargetVersion || upgraded.AppliedCount == 0 {
		t.Fatalf("initial mysql upgrade = %#v", upgraded)
	}

	s, err := mysql.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open mysql store: %v", err)
	}
	defer s.Close()
	exerciseStoreContract(t, ctx, s)

	current, err := mysql.UpgradeSchema(ctx, cfg)
	if err != nil {
		t.Fatalf("repeat mysql upgrade: %v", err)
	}
	if current.CurrentVersion != current.TargetVersion || current.AppliedCount != 0 || current.HasPending() {
		t.Fatalf("repeat mysql upgrade = %#v", current)
	}
}

func parseMySQLTestDSNMode(raw string) (string, error) {
	mode := strings.TrimSpace(raw)
	switch mode {
	case "existing", "create-drop":
		return mode, nil
	case "":
		return "", errors.New("set AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE=existing for a dedicated existing MySQL Store database, or AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE=create-drop for local admin-only contract tests")
	default:
		return "", fmt.Errorf("unsupported AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE %q; use existing or create-drop", mode)
	}
}

func TestParseMySQLTestDSNModeRequiresExplicitMode(t *testing.T) {
	_, err := parseMySQLTestDSNMode("")
	if err == nil {
		t.Fatal("expected empty MySQL contract mode to be rejected")
	}
	if !strings.Contains(err.Error(), "AGENT_TESTBENCH_MYSQL_TEST_DSN_MODE=existing") || !strings.Contains(err.Error(), "create-drop") {
		t.Fatalf("unexpected empty mode error: %v", err)
	}
}

func TestParseMySQLTestDSNModeAcceptsExplicitModes(t *testing.T) {
	for _, mode := range []string{"existing", "create-drop"} {
		got, err := parseMySQLTestDSNMode(mode)
		if err != nil {
			t.Fatalf("parse mode %q: %v", mode, err)
		}
		if got != mode {
			t.Fatalf("parse mode %q = %q", mode, got)
		}
	}
}

func TestParseMySQLTestDSNModeRejectsUnknownModes(t *testing.T) {
	_, err := parseMySQLTestDSNMode("admin")
	if err == nil {
		t.Fatal("expected unknown MySQL contract mode to be rejected")
	}
	if !strings.Contains(err.Error(), "use existing or create-drop") {
		t.Fatalf("unexpected unknown mode error: %v", err)
	}
}

func TestIsDedicatedMySQLTestDatabaseAcceptsRenamedProductToken(t *testing.T) {
	for _, name := range []string{"agent_testbench", "agent_testbench_local", "agent-testbench-local"} {
		if !isDedicatedMySQLTestDatabase(name) {
			t.Fatalf("expected %q to be treated as a dedicated AgentTestBench database", name)
		}
	}
}

func mysqlTestDSNWithDatabase(t *testing.T, dsn string, databaseName string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse mysql test dsn: %v", err)
	}
	parsed.Path = "/" + databaseName
	return parsed.String()
}

func mysqlTestDatabaseName(t *testing.T, dsn string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse mysql test dsn: %v", err)
	}
	return strings.TrimPrefix(parsed.EscapedPath(), "/")
}

func isDedicatedMySQLTestDatabase(databaseName string) bool {
	value, err := url.PathUnescape(strings.TrimSpace(databaseName))
	if err != nil {
		value = strings.TrimSpace(databaseName)
	}
	value = strings.ToLower(value)
	return strings.Contains(value, "agent-testbench") || strings.Contains(value, "agent_testbench") ||
		strings.Contains(value, "_smoke") || strings.Contains(value, "smoke_") ||
		strings.Contains(value, "_test") || strings.Contains(value, "test_") ||
		strings.Contains(value, "_ci") || strings.Contains(value, "ci_")
}

func resetExistingMySQLTestDatabase(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rows, err := db.QueryContext(ctx, `select table_name from information_schema.tables where table_schema = database()`)
	if err != nil {
		t.Fatalf("list existing mysql contract tables: %v", err)
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatalf("scan mysql contract table: %v", err)
		}
		if strings.TrimSpace(table) != "" {
			tables = append(tables, table)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate mysql contract tables: %v", err)
	}
	if len(tables) == 0 {
		return
	}
	if _, err := db.ExecContext(ctx, `set foreign_key_checks = 0`); err != nil {
		t.Fatalf("disable mysql foreign key checks: %v", err)
	}
	defer func() {
		if _, err := db.ExecContext(context.Background(), `set foreign_key_checks = 1`); err != nil {
			t.Errorf("restore mysql foreign key checks: %v", err)
		}
	}()
	for _, table := range tables {
		if _, err := db.ExecContext(ctx, `drop table if exists `+quoteMySQLIdent(table)); err != nil {
			t.Fatalf("drop mysql contract table %s: %v", table, err)
		}
	}
}

func quoteMySQLIdent(value string) string {
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}
