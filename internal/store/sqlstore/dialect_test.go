package sqlstore_test

import (
	"testing"

	"agent-testbench/internal/store/sqlstore"
)

func TestDialectRegistryRecognizesOpenSourceDatabaseFamilies(t *testing.T) {
	tests := []struct {
		name       string
		reference  string
		wantName   string
		wantDriver string
	}{
		{name: "postgres", reference: "postgres://user:pass@localhost:5432/agent-testbench", wantName: "postgres", wantDriver: "pgx"},
		{name: "postgresql", reference: "postgresql://user:pass@localhost:5432/agent-testbench", wantName: "postgres", wantDriver: "pgx"},
		{name: "mysql", reference: "mysql://user:pass@localhost:3306/agent-testbench", wantName: "mysql", wantDriver: "mysql"},
		{name: "sqlite", reference: "sqlite:///tmp/agent-testbench.sqlite", wantName: "sqlite", wantDriver: "sqlite"},
		{name: "file", reference: "file:/tmp/agent-testbench.sqlite", wantName: "sqlite", wantDriver: "sqlite"},
		{name: "path", reference: "/tmp/agent-testbench.sqlite", wantName: "sqlite", wantDriver: "sqlite"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialect, err := sqlstore.DialectFromReference(tt.reference)
			if err != nil {
				t.Fatalf("dialect from reference: %v", err)
			}
			if dialect.Name() != tt.wantName || dialect.DriverName() != tt.wantDriver {
				t.Fatalf("dialect = %s/%s want %s/%s", dialect.Name(), dialect.DriverName(), tt.wantName, tt.wantDriver)
			}
		})
	}
}

func TestDialectCapturesSQLDifferences(t *testing.T) {
	tests := []struct {
		name        string
		dialect     sqlstore.Dialect
		wantBind3   string
		wantText    string
		wantJSON    string
		wantTime    string
		wantKeyText string
		wantUpsert  string
		wantQuoteID string
	}{
		{name: "postgres", dialect: sqlstore.PostgresDialect{}, wantBind3: "$3", wantText: "text", wantJSON: "jsonb", wantTime: "timestamptz", wantKeyText: "text", wantUpsert: "on conflict(id) do update set name = excluded.name", wantQuoteID: `"runs"`},
		{name: "mysql", dialect: sqlstore.MySQLDialect{}, wantBind3: "?", wantText: "mediumtext", wantJSON: "json", wantTime: "datetime(6)", wantKeyText: "varchar(128)", wantUpsert: "on duplicate key update name = values(name)", wantQuoteID: "`runs`"},
		{name: "sqlite", dialect: sqlstore.SQLiteDialect{}, wantBind3: "?", wantText: "text", wantJSON: "text", wantTime: "text", wantKeyText: "text", wantUpsert: "on conflict(id) do update set name = excluded.name", wantQuoteID: `"runs"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.dialect.BindVar(3); got != tt.wantBind3 {
				t.Fatalf("bind var = %q want %q", got, tt.wantBind3)
			}
			if got := tt.dialect.TextType(); got != tt.wantText {
				t.Fatalf("text type = %q want %q", got, tt.wantText)
			}
			if got := tt.dialect.JSONType(); got != tt.wantJSON {
				t.Fatalf("json type = %q want %q", got, tt.wantJSON)
			}
			if got := tt.dialect.TimeType(); got != tt.wantTime {
				t.Fatalf("time type = %q want %q", got, tt.wantTime)
			}
			if got := tt.dialect.KeyTextType(); got != tt.wantKeyText {
				t.Fatalf("key text type = %q want %q", got, tt.wantKeyText)
			}
			if got := tt.dialect.UpsertClause("id", []string{"name"}); got != tt.wantUpsert {
				t.Fatalf("upsert = %q want %q", got, tt.wantUpsert)
			}
			if got := tt.dialect.QuoteIdent("runs"); got != tt.wantQuoteID {
				t.Fatalf("quote ident = %q want %q", got, tt.wantQuoteID)
			}
		})
	}
}

func TestConfigFromReferenceCarriesDialectAndDSN(t *testing.T) {
	cfg, err := sqlstore.ConfigFromReference("postgres://user:pass@localhost:5432/agent_testbench?sslmode=disable")
	if err != nil {
		t.Fatalf("config from reference: %v", err)
	}
	if cfg.Backend != "postgres" || cfg.DriverName != "pgx" || cfg.DSN != "postgres://user:pass@localhost:5432/agent_testbench?sslmode=disable" {
		t.Fatalf("postgres config = %#v", cfg)
	}

	sqliteCfg, err := sqlstore.ConfigFromReference("/tmp/agent-testbench.sqlite")
	if err != nil {
		t.Fatalf("sqlite config from path: %v", err)
	}
	if sqliteCfg.Backend != "sqlite" || sqliteCfg.DriverName != "sqlite" || sqliteCfg.DSN != "/tmp/agent-testbench.sqlite" {
		t.Fatalf("sqlite path config = %#v", sqliteCfg)
	}

	mysqlCfg, err := sqlstore.ConfigFromReference("mysql://user:pass@localhost:3306/agent-testbench")
	if err != nil {
		t.Fatalf("mysql config from reference: %v", err)
	}
	if mysqlCfg.Backend != "mysql" || mysqlCfg.DriverName != "mysql" || mysqlCfg.DSN != "mysql://user:pass@localhost:3306/agent-testbench" {
		t.Fatalf("mysql config = %#v", mysqlCfg)
	}
}
