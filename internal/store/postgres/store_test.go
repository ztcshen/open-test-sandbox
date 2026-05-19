package postgres_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"open-test-sandbox/internal/store"
	"open-test-sandbox/internal/store/postgres"
)

func TestParseConfigFromURLAcceptsPostgresDSN(t *testing.T) {
	cfg, err := postgres.ParseConfigFromURL("postgres://user:secret@example.com:5432/otsandbox?sslmode=disable")
	if err != nil {
		t.Fatalf("parse postgres dsn: %v", err)
	}
	if cfg.URL != "postgres://user:secret@example.com:5432/otsandbox?sslmode=disable" {
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
	if !strings.Contains(exec.query, "insert into runs") || !strings.Contains(exec.query, "values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)") {
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

const fakePostgresDriverName = "otsandbox_postgres_open_fake"

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
