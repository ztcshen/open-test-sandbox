package mysql_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-testbench/internal/store"
	"agent-testbench/internal/store/mysql"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestParseConfigFromURLAcceptsMySQLURL(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/agent_testbench?tls=false")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	if cfg.URL != "mysql://user:secret@example.com:3306/agent_testbench?tls=false" {
		t.Fatalf("mysql config url = %q", cfg.URL)
	}
	for _, want := range []string{
		"user:secret@tcp(example.com:3306)/agent_testbench",
		"parseTime=true",
		"loc=UTC",
		"tls=false",
	} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn missing %q: %q", want, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLKeepsStoreTimeParsingAuthoritative(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/agent_testbench?parseTime=false&loc=Local&tls=false")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"parseTime=true", "loc=UTC", "tls=false"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn missing %q: %q", want, cfg.DSN)
		}
	}
	for _, reject := range []string{"parseTime=false", "loc=Local"} {
		if strings.Contains(cfg.DSN, reject) {
			t.Fatalf("mysql driver dsn should not let URL query override Store time parsing with %q: %q", reject, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLAddsBoundedNetworkTimeouts(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/agent_testbench?tls=false")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"timeout=10s", "readTimeout=30s", "writeTimeout=30s"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn missing bounded network timeout %q: %q", want, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLKeepsExplicitNetworkTimeouts(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/agent_testbench?timeout=2s&readTimeout=3s&writeTimeout=4s")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"timeout=2s", "readTimeout=3s", "writeTimeout=4s"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn should keep explicit network timeout %q: %q", want, cfg.DSN)
		}
	}
	for _, reject := range []string{"timeout=10s", "readTimeout=30s", "writeTimeout=30s"} {
		if strings.Contains(cfg.DSN, reject) {
			t.Fatalf("mysql driver dsn should not duplicate default timeout %q when explicit timeout is set: %q", reject, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLCanonicalizesExplicitNetworkTimeoutKeys(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/agent_testbench?Timeout=2s&READTIMEOUT=3s&writetimeout=4s")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"timeout=2s", "readTimeout=3s", "writeTimeout=4s"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn should canonicalize explicit network timeout key %q: %q", want, cfg.DSN)
		}
	}
	for _, reject := range []string{"Timeout=2s", "READTIMEOUT=3s", "writetimeout=4s", "timeout=10s", "readTimeout=30s", "writeTimeout=30s"} {
		if strings.Contains(cfg.DSN, reject) {
			t.Fatalf("mysql driver dsn should not keep mixed-case or default timeout key %q: %q", reject, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLCanonicalizesCommonDriverParamKeys(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/agent_testbench?TLS=false&CHARSET=utf8mb4&COLLATION=utf8mb4_unicode_ci&MAXALLOWEDPACKET=1048576")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"tls=false", "charset=utf8mb4", "collation=utf8mb4_unicode_ci", "maxAllowedPacket=1048576"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn should canonicalize common driver param key %q: %q", want, cfg.DSN)
		}
	}
	for _, reject := range []string{"TLS=false", "CHARSET=utf8mb4", "COLLATION=utf8mb4_unicode_ci", "MAXALLOWEDPACKET=1048576"} {
		if strings.Contains(cfg.DSN, reject) {
			t.Fatalf("mysql driver dsn should not keep mixed-case driver param key %q: %q", reject, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLCanonicalizesCommonDriverBoolParamKeys(t *testing.T) {
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/agent_testbench?ALLOWNATIVEPASSWORDS=false&CHECKCONNLIVENESS=false&CLIENTFOUNDROWS=true&COLUMNSWITHALIAS=true&INTERPOLATEPARAMS=true&MULTISTATEMENTS=true&REJECTREADONLY=true")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	for _, want := range []string{"allowNativePasswords=false", "checkConnLiveness=false", "clientFoundRows=true", "columnsWithAlias=true", "interpolateParams=true", "multiStatements=true", "rejectReadOnly=true"} {
		if !strings.Contains(cfg.DSN, want) {
			t.Fatalf("mysql driver dsn should canonicalize common bool driver param key %q: %q", want, cfg.DSN)
		}
	}
	for _, reject := range []string{"ALLOWNATIVEPASSWORDS=false", "CHECKCONNLIVENESS=false", "CLIENTFOUNDROWS=true", "COLUMNSWITHALIAS=true", "INTERPOLATEPARAMS=true", "MULTISTATEMENTS=true", "REJECTREADONLY=true"} {
		if strings.Contains(cfg.DSN, reject) {
			t.Fatalf("mysql driver dsn should not keep mixed-case bool driver param key %q: %q", reject, cfg.DSN)
		}
	}
}

func TestParseConfigFromURLRejectsNonMySQLDSN(t *testing.T) {
	_, err := mysql.ParseConfigFromURL("postgres://localhost/agent-testbench")
	if err == nil {
		t.Fatal("expected non-mysql dsn to be rejected")
	}
}

func TestParseConfigFromURLRequiresDatabaseName(t *testing.T) {
	_, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306")
	if err == nil {
		t.Fatal("expected mysql url without database to be rejected")
	}
}

func TestOpenUsesConfiguredSQLDriverAndDelegatesRuntimeStoreMethods(t *testing.T) {
	ctx := context.Background()
	state := openFakeMySQLDriver(t)
	state.queueRows(fakeMySQLRows{
		columns: []string{"table_exists"},
		values:  [][]driver.Value{{int64(0)}},
	})
	state.queueRows(fakeMySQLRows{
		columns: []string{"table_exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeMySQLRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(4)}},
	})

	s, err := mysql.Open(ctx, mysql.Config{
		URL:        "mysql://user:secret@example.com:3306/agent_testbench_test?tls=false",
		DSN:        state.name,
		DriverName: fakeMySQLDriverName,
	})
	if err != nil {
		t.Fatalf("open mysql store: %v", err)
	}
	defer s.Close()
	if state.pings != 1 {
		t.Fatalf("ping count = %d, want 1", state.pings)
	}

	_, err = s.CreateRun(ctx, store.Run{
		ID:           "run-001",
		ProfileID:    "profile.alpha",
		WorkflowID:   "workflow.alpha",
		Status:       store.StatusRunning,
		EvidenceRoot: ".runtime/evidence/run-001",
		SummaryJSON:  `{"stepCount":1}`,
		StartedAt:    time.Date(2026, 5, 21, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create run through mysql store: %v", err)
	}
	exec := state.lastExec(t)
	if !strings.Contains(exec.query, "insert into runs") || !strings.Contains(exec.query, "values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)") {
		t.Fatalf("delegated create run did not use mysql sqlstore dialect:\n%s", exec.query)
	}

	_, err = s.UpsertReadModel(ctx, store.ReadModel{
		ProfileID:       "profile.alpha",
		Key:             "workflow-discovery",
		ConfigVersionID: "config-001",
		PayloadJSON:     `{"workflows":[]}`,
	})
	if err != nil {
		t.Fatalf("upsert read model through mysql store: %v", err)
	}
	exec = state.lastExec(t)
	if !strings.Contains(exec.query, "insert into config_read_model") || !strings.Contains(exec.query, "on duplicate key update") {
		t.Fatalf("delegated read model did not use mysql sqlstore dialect:\n%s", exec.query)
	}

	err = s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "profile.alpha",
		Services:  []store.CatalogService{{ID: "service.alpha"}},
	})
	if err != nil {
		t.Fatalf("replace profile catalog through mysql store: %v", err)
	}
	exec = state.lastExec(t)
	if !strings.Contains(exec.query, "insert into profile_catalogs") || !strings.Contains(exec.query, "values (?, ?, ?") {
		t.Fatalf("delegated profile catalog did not use mysql sqlstore dialect:\n%s", exec.query)
	}
}

func TestSchemaStatusAndUpgradeUseConfiguredSQLDriver(t *testing.T) {
	ctx := context.Background()
	state := openFakeMySQLDriver(t)
	cfg := mysql.Config{
		URL:        "mysql://user:secret@example.com:3306/agent_testbench_test?tls=false",
		DSN:        state.name,
		DriverName: fakeMySQLDriverName,
	}

	state.queueRows(fakeMySQLRows{
		columns: []string{"table_exists"},
		values:  [][]driver.Value{{int64(0)}},
	})
	status, err := mysql.SchemaStatus(ctx, cfg)
	if err != nil {
		t.Fatalf("schema status: %v", err)
	}
	if status.CurrentVersion != 0 || status.TargetVersion == 0 || !status.HasPending() {
		t.Fatalf("mysql schema status = %#v", status)
	}

	state.queueRows(fakeMySQLRows{
		columns: []string{"table_exists"},
		values:  [][]driver.Value{{int64(0)}},
	})
	state.queueRows(fakeMySQLRows{
		columns: []string{"table_exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakeMySQLRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(status.TargetVersion)}},
	})
	upgraded, err := mysql.UpgradeSchema(ctx, cfg)
	if err != nil {
		t.Fatalf("upgrade schema: %v", err)
	}
	if upgraded.CurrentVersion != upgraded.TargetVersion || upgraded.AppliedCount != 1 || upgraded.HasPending() {
		t.Fatalf("mysql upgraded schema = %#v", upgraded)
	}
	execs := state.execsSnapshot()
	if len(execs) == 0 || !strings.Contains(execs[0].query, "create table if not exists schema_versions") || !strings.Contains(execs[0].query, "datetime(6)") {
		t.Fatalf("mysql upgrade execs = %#v", execs)
	}
}

func TestProvisionDatabaseCreatesMissingDatabase(t *testing.T) {
	ctx := context.Background()
	cfg, err := mysql.ParseConfigFromURL("mysql://user:secret@example.com:3306/agent_testbench_test?tls=false")
	if err != nil {
		t.Fatalf("parse mysql url: %v", err)
	}
	driverCfg, err := mysqlDriver.ParseDSN(cfg.DSN)
	if err != nil {
		t.Fatalf("parse driver dsn: %v", err)
	}
	driverCfg.DBName = ""
	serverDSN := driverCfg.FormatDSN()
	registerFakeMySQLDriverOnce.Do(func() {
		sql.Register(fakeMySQLDriverName, fakeMySQLDriver{})
	})
	state := &fakeMySQLState{name: serverDSN}
	fakeMySQLRegistry.put(state)
	state.queueRows(fakeMySQLRows{
		columns: []string{"SCHEMA_NAME"},
		values:  nil,
	})

	result, err := mysql.ProvisionDatabase(ctx, mysql.Config{
		URL:        cfg.URL,
		DSN:        cfg.DSN,
		DriverName: fakeMySQLDriverName,
	})
	if err != nil {
		t.Fatalf("provision database: %v", err)
	}
	if result.Database != "agent_testbench_test" || !result.Created {
		t.Fatalf("provision result = %#v", result)
	}
	exec := state.lastExec(t)
	if !strings.Contains(exec.query, "CREATE DATABASE IF NOT EXISTS `agent_testbench_test`") || !strings.Contains(exec.query, "CHARACTER SET utf8mb4") {
		t.Fatalf("provision exec = %#v", exec)
	}
}

const fakeMySQLDriverName = "agent_testbench_mysql_open_fake"

var registerFakeMySQLDriverOnce sync.Once

func openFakeMySQLDriver(t *testing.T) *fakeMySQLState {
	t.Helper()
	registerFakeMySQLDriverOnce.Do(func() {
		sql.Register(fakeMySQLDriverName, fakeMySQLDriver{})
	})
	state := &fakeMySQLState{name: "fake-mysql"}
	for i := 0; i < len(fakeMySQLRegistry.states)+1; i++ {
		state.name += "x"
	}
	fakeMySQLRegistry.put(state)
	return state
}

type fakeMySQLCall struct {
	query string
	args  []any
}

type fakeMySQLState struct {
	name  string
	mu    sync.Mutex
	pings int
	execs []fakeMySQLCall
	rows  []fakeMySQLRows
}

func (s *fakeMySQLState) lastExec(t *testing.T) fakeMySQLCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.execs) == 0 {
		t.Fatal("no exec calls recorded")
	}
	return s.execs[len(s.execs)-1]
}

func (s *fakeMySQLState) queueRows(rows fakeMySQLRows) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, rows)
}

func (s *fakeMySQLState) execsSnapshot() []fakeMySQLCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]fakeMySQLCall(nil), s.execs...)
}

var fakeMySQLRegistry = &fakeMySQLStateRegistry{states: map[string]*fakeMySQLState{}}

type fakeMySQLStateRegistry struct {
	mu     sync.Mutex
	states map[string]*fakeMySQLState
}

func (r *fakeMySQLStateRegistry) put(state *fakeMySQLState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[state.name] = state
}

func (r *fakeMySQLStateRegistry) get(name string) *fakeMySQLState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.states[name]
}

type fakeMySQLDriver struct{}

func (fakeMySQLDriver) Open(name string) (driver.Conn, error) {
	state := fakeMySQLRegistry.get(name)
	if state == nil {
		return nil, errors.New("unknown fake mysql database")
	}
	return fakeMySQLConn{state: state}, nil
}

type fakeMySQLConn struct {
	state *fakeMySQLState
}

func (c fakeMySQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (c fakeMySQLConn) Close() error              { return nil }
func (c fakeMySQLConn) Begin() (driver.Tx, error) { return nil, errors.New("tx not supported") }

func (c fakeMySQLConn) Ping(context.Context) error {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.pings++
	return nil
}

func (c fakeMySQLConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	out := make([]any, 0, len(args))
	for _, arg := range args {
		out = append(out, arg.Value)
	}
	c.state.execs = append(c.state.execs, fakeMySQLCall{query: query, args: out})
	return driver.RowsAffected(1), nil
}

func (c fakeMySQLConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	if len(c.state.rows) == 0 {
		return &fakeMySQLSQLRows{}, nil
	}
	rows := c.state.rows[0]
	c.state.rows = c.state.rows[1:]
	return &fakeMySQLSQLRows{columns: rows.columns, values: rows.values}, nil
}

type fakeMySQLRows struct {
	columns []string
	values  [][]driver.Value
}

type fakeMySQLSQLRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r fakeMySQLSQLRows) Columns() []string { return r.columns }
func (r fakeMySQLSQLRows) Close() error      { return nil }

func (r *fakeMySQLSQLRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
