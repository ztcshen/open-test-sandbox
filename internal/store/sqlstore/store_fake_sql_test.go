package sqlstore_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"
)

const fakeDriverName = "agent_testbench_sqlstore_fake"

var registerFakeDriverOnce sync.Once

func openFakeSQLDB(t *testing.T) (*sql.DB, *fakeSQLState) {
	t.Helper()
	registerFakeDriverOnce.Do(func() {
		sql.Register(fakeDriverName, fakeSQLDriver{})
	})
	state := &fakeSQLState{}
	name := fakeSQLStateRegistry.put(state)
	db, err := sql.Open(fakeDriverName, name)
	if err != nil {
		t.Fatalf("open fake sql db: %v", err)
	}
	return db, state
}

type fakeSQLCall struct {
	query string
	args  []any
}

type fakeRows struct {
	columns []string
	values  [][]driver.Value
}

type fakeSQLState struct {
	mu       sync.Mutex
	execs    []fakeSQLCall
	queries  []fakeSQLCall
	rows     []fakeRows
	execErrs []error
}

func (s *fakeSQLState) queueRows(rows fakeRows) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows = append(s.rows, rows)
}

func (s *fakeSQLState) queueExecError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execErrs = append(s.execErrs, err)
}

func (s *fakeSQLState) lastExec(t *testing.T) fakeSQLCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.execs) == 0 {
		t.Fatal("no exec calls recorded")
	}
	return s.execs[len(s.execs)-1]
}

func (s *fakeSQLState) lastExecs(t *testing.T, count int) []fakeSQLCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.execs) < count {
		t.Fatalf("recorded exec calls = %d, want at least %d", len(s.execs), count)
	}
	return append([]fakeSQLCall(nil), s.execs[len(s.execs)-count:]...)
}

func (s *fakeSQLState) execsSnapshot() []fakeSQLCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]fakeSQLCall(nil), s.execs...)
}

func (s *fakeSQLState) clearExecs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.execs = nil
}

func (s *fakeSQLState) lastQuery(t *testing.T) fakeSQLCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) == 0 {
		t.Fatal("no query calls recorded")
	}
	return s.queries[len(s.queries)-1]
}

var fakeSQLStateRegistry = &fakeRegistry{states: map[string]*fakeSQLState{}}

type fakeRegistry struct {
	mu     sync.Mutex
	next   int
	states map[string]*fakeSQLState
}

func (r *fakeRegistry) put(state *fakeSQLState) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	name := "fake-db"
	for i := 0; i < r.next; i++ {
		name += "x"
	}
	r.states[name] = state
	return name
}

func (r *fakeRegistry) get(name string) *fakeSQLState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.states[name]
}

type fakeSQLDriver struct{}

func (fakeSQLDriver) Open(name string) (driver.Conn, error) {
	state := fakeSQLStateRegistry.get(name)
	if state == nil {
		return nil, errors.New("unknown fake database")
	}
	return fakeSQLConn{state: state}, nil
}

type fakeSQLConn struct {
	state *fakeSQLState
}

func (c fakeSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare not supported")
}
func (c fakeSQLConn) Close() error              { return nil }
func (c fakeSQLConn) Begin() (driver.Tx, error) { return fakeSQLTx{}, nil }

func (c fakeSQLConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.execs = append(c.state.execs, fakeSQLCall{query: query, args: namedValues(args)})
	if len(c.state.execErrs) > 0 {
		err := c.state.execErrs[0]
		c.state.execErrs = c.state.execErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	return driver.RowsAffected(1), nil
}

func (c fakeSQLConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.queries = append(c.state.queries, fakeSQLCall{query: query, args: namedValues(args)})
	if len(c.state.rows) == 0 {
		return &fakeSQLRows{}, nil
	}
	rows := c.state.rows[0]
	c.state.rows = c.state.rows[1:]
	return &fakeSQLRows{columns: rows.columns, values: rows.values}, nil
}

type fakeSQLTx struct{}

func (fakeSQLTx) Commit() error   { return nil }
func (fakeSQLTx) Rollback() error { return nil }

func namedValues(values []driver.NamedValue) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value.Value)
	}
	return out
}

type fakeSQLRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r fakeSQLRows) Columns() []string {
	return r.columns
}

func (r fakeSQLRows) Close() error {
	return nil
}

func (r *fakeSQLRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
