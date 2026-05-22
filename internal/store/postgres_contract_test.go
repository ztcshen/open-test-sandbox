package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"agent-testbench/internal/store/postgres"
)

func TestPostgresStoreContractWithExternalDatabase(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("AGENT_TESTBENCH_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set AGENT_TESTBENCH_POSTGRES_TEST_DSN to run the PostgreSQL Store contract")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres admin connection: %v", err)
	}
	defer admin.Close()
	if err := admin.PingContext(ctx); err != nil {
		t.Fatalf("ping postgres test database: %v", err)
	}
	schemaName := fmt.Sprintf("agent_testbench_contract_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, `create schema `+quotePostgresIdent(schemaName)); err != nil {
		t.Fatalf("create postgres test schema: %v", err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), `drop schema if exists `+quotePostgresIdent(schemaName)+` cascade`)
	}()

	schemaDSN := postgresTestDSNWithSearchPath(t, dsn, schemaName)
	cfg, err := postgres.ParseConfigFromURL(schemaDSN)
	if err != nil {
		t.Fatalf("parse schema postgres dsn: %v", err)
	}
	upgraded, err := postgres.UpgradeSchema(ctx, cfg)
	if err != nil {
		t.Fatalf("upgrade postgres schema: %v", err)
	}
	if upgraded.CurrentVersion != upgraded.TargetVersion || upgraded.AppliedCount == 0 {
		t.Fatalf("initial postgres upgrade = %#v", upgraded)
	}

	s, err := postgres.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("open postgres store: %v", err)
	}
	defer s.Close()
	exerciseStoreContract(t, ctx, s)

	current, err := postgres.UpgradeSchema(ctx, cfg)
	if err != nil {
		t.Fatalf("repeat postgres upgrade: %v", err)
	}
	if current.CurrentVersion != current.TargetVersion || current.AppliedCount != 0 || current.HasPending() {
		t.Fatalf("repeat postgres upgrade = %#v", current)
	}
}

func postgresTestDSNWithSearchPath(t *testing.T, dsn string, schemaName string) string {
	t.Helper()
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse postgres test dsn: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func quotePostgresIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
