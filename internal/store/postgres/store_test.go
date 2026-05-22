package postgres_test

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
	"agent-testbench/internal/store/postgres"
	"agent-testbench/internal/store/sqlstore"
)

func TestParseConfigFromURLAcceptsPostgresDSN(t *testing.T) {
	cfg, err := postgres.ParseConfigFromURL("postgres://user:secret@example.com:5432/agent_testbench?sslmode=disable")
	if err != nil {
		t.Fatalf("parse postgres dsn: %v", err)
	}
	if cfg.URL != "postgres://user:secret@example.com:5432/agent_testbench?sslmode=disable" {
		t.Fatalf("postgres config url = %q", cfg.URL)
	}
}

func TestParseConfigFromURLRejectsNonPostgresDSN(t *testing.T) {
	_, err := postgres.ParseConfigFromURL("sqlite://tmp/store.sqlite")
	if err == nil {
		t.Fatal("expected non-postgres dsn to be rejected")
	}
}

func TestOpenUsesConfiguredSQLDriverAndDelegatesRuntimeStoreMethods(t *testing.T) {
	ctx := context.Background()
	state := openFakePostgresDriver(t)
	state.queueRows(fakePostgresRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakePostgresRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(sqlstore.CurrentSchemaVersion)}},
	})
	s, err := postgres.Open(ctx, postgres.Config{
		URL:        state.name,
		DriverName: fakePostgresDriverName,
	})
	if err != nil {
		t.Fatalf("open postgres store: %v", err)
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
		StartedAt:    time.Date(2026, 5, 19, 9, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("create run through postgres store: %v", err)
	}
	exec := state.lastExec(t)
	if !strings.Contains(exec.query, "insert into runs") || !strings.Contains(exec.query, "values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)") {
		t.Fatalf("delegated create run did not use postgres sqlstore dialect:\n%s", exec.query)
	}

	_, err = s.UpsertReadModel(ctx, store.ReadModel{
		ProfileID:       "profile.alpha",
		Key:             "workflow-discovery",
		ConfigVersionID: "config-001",
		PayloadJSON:     `{"workflows":[]}`,
	})
	if err != nil {
		t.Fatalf("upsert read model through postgres store: %v", err)
	}
	exec = state.lastExec(t)
	if !strings.Contains(exec.query, "insert into config_read_model") || !strings.Contains(exec.query, "values ($1, $2, $3, $4, $5, $6)") {
		t.Fatalf("delegated read model did not use postgres sqlstore dialect:\n%s", exec.query)
	}

	err = s.ReplaceProfileCatalog(ctx, store.ProfileCatalog{
		ProfileID: "profile.alpha",
		Services:  []store.CatalogService{{ID: "service.alpha"}},
	})
	if err != nil {
		t.Fatalf("replace profile catalog through postgres store: %v", err)
	}
	exec = state.lastExec(t)
	if !strings.Contains(exec.query, "insert into profile_catalogs") || !strings.Contains(exec.query, "values ($1, $2, $3") {
		t.Fatalf("delegated profile catalog did not use postgres sqlstore dialect:\n%s", exec.query)
	}
}

func TestSchemaStatusAndUpgradeUseConfiguredSQLDriver(t *testing.T) {
	ctx := context.Background()
	state := openFakePostgresDriver(t)
	cfg := postgres.Config{URL: state.name, DriverName: fakePostgresDriverName}

	state.queueRows(fakePostgresRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(0)}},
	})
	status, err := postgres.SchemaStatus(ctx, cfg)
	if err != nil {
		t.Fatalf("schema status: %v", err)
	}
	if status.CurrentVersion != 0 || status.TargetVersion == 0 || !status.HasPending() {
		t.Fatalf("postgres schema status = %#v", status)
	}

	state.queueRows(fakePostgresRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(0)}},
	})
	state.queueRows(fakePostgresRows{
		columns: []string{"exists"},
		values:  [][]driver.Value{{int64(1)}},
	})
	state.queueRows(fakePostgresRows{
		columns: []string{"version"},
		values:  [][]driver.Value{{int64(status.TargetVersion)}},
	})
	upgraded, err := postgres.UpgradeSchema(ctx, cfg)
	if err != nil {
		t.Fatalf("upgrade schema: %v", err)
	}
	if upgraded.CurrentVersion != upgraded.TargetVersion || upgraded.AppliedCount != 1 || upgraded.HasPending() {
		t.Fatalf("postgres upgraded schema = %#v", upgraded)
	}
	execs := state.execsSnapshot()
	if len(execs) == 0 || !strings.Contains(execs[0].query, "create table if not exists schema_versions") {
		t.Fatalf("postgres upgrade execs = %#v", execs)
	}
}

const fakePostgresDriverName = "agent_testbench_postgres_open_fake"

var registerFakePostgresDriverOnce sync.Once

func openFakePostgresDriver(t *testing.T) *fakePostgresState {
	t.Helper()
	registerFakePostgresDriverOnce.Do(func() {
		sql.Register(fakePostgresDriverName, fakePostgresDriver{})
	})
	state := &fakePostgresState{name: "fake-postgres"}
	for i := 0; i < len(fakePostgresRegistry.states)+1; i++ {
		state.name += "x"
	}
	fakePostgresRegistry.put(state)
	return state
}

type fakePostgresCall struct {
	query string
	args  []any
}

type fakePostgresState struct {
	name  string
	mu    sync.Mutex
	pings int
	execs []fakePostgresCall
	rows  []fakePostgresRows
}

func (s *fakePostgresState) lastExec(t *testing.T) fakePostgresCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.execs) == 0 {
		t.Fatal("no exec calls recorded")
	}
	return s.execs[len(s.execs)-1]
}

func (s *fakePostgresState) queueRows(rows fakePostgresRows) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, rows)
}

func (s *fakePostgresState) execsSnapshot() []fakePostgresCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]fakePostgresCall(nil), s.execs...)
}

var fakePostgresRegistry = &fakePostgresStateRegistry{states: map[string]*fakePostgresState{}}

type fakePostgresStateRegistry struct {
	mu     sync.Mutex
	states map[string]*fakePostgresState
}

func (r *fakePostgresStateRegistry) put(state *fakePostgresState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[state.name] = state
}

func (r *fakePostgresStateRegistry) get(name string) *fakePostgresState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.states[name]
}

type fakePostgresDriver struct{}

func (fakePostgresDriver) Open(name string) (driver.Conn, error) {
	state := fakePostgresRegistry.get(name)
	if state == nil {
		return nil, errors.New("unknown fake postgres database")
	}
	return fakePostgresConn{state: state}, nil
}

type fakePostgresConn struct {
	state *fakePostgresState
}

func (c fakePostgresConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (c fakePostgresConn) Close() error              { return nil }
func (c fakePostgresConn) Begin() (driver.Tx, error) { return nil, errors.New("tx not supported") }

func (c fakePostgresConn) Ping(context.Context) error {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.pings++
	return nil
}

func (c fakePostgresConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	out := make([]any, 0, len(args))
	for _, arg := range args {
		out = append(out, arg.Value)
	}
	c.state.execs = append(c.state.execs, fakePostgresCall{query: query, args: out})
	return driver.RowsAffected(1), nil
}

func (c fakePostgresConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	if len(c.state.rows) == 0 {
		return &fakePostgresSQLRows{}, nil
	}
	rows := c.state.rows[0]
	c.state.rows = c.state.rows[1:]
	return &fakePostgresSQLRows{columns: rows.columns, values: rows.values}, nil
}

type fakePostgresRows struct {
	columns []string
	values  [][]driver.Value
}

type fakePostgresSQLRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r fakePostgresSQLRows) Columns() []string { return r.columns }
func (r fakePostgresSQLRows) Close() error      { return nil }

func (r *fakePostgresSQLRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
