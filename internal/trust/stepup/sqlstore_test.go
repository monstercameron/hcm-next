package stepup_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/trust/stepup"
)

type scriptedConnector struct{ conn *scriptedConn }

func (c scriptedConnector) Connect(context.Context) (driver.Conn, error) { return c.conn, nil }
func (c scriptedConnector) Driver() driver.Driver                        { return scriptedDriver{} }

type scriptedDriver struct{}

func (scriptedDriver) Open(string) (driver.Conn, error) { return nil, errors.New("use connector") }

type queryStep struct {
	value  string
	noRows bool
	err    error
}

type execStep struct{ err error }

type scriptedConn struct {
	mu      sync.Mutex
	queries []queryStep
	execs   []execStep
}

func (c *scriptedConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *scriptedConn) Close() error                        { return nil }
func (c *scriptedConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (c *scriptedConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.queries) == 0 {
		return nil, errors.New("unexpected query")
	}
	step := c.queries[0]
	c.queries = c.queries[1:]
	if step.err != nil {
		return nil, step.err
	}
	if step.noRows {
		return &scriptedRows{}, nil
	}
	return &scriptedRows{value: step.value, pending: true}, nil
}

func (c *scriptedConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.execs) == 0 {
		return nil, errors.New("unexpected exec")
	}
	step := c.execs[0]
	c.execs = c.execs[1:]
	if step.err != nil {
		return nil, step.err
	}
	return driver.RowsAffected(1), nil
}

type scriptedRows struct {
	value   string
	pending bool
}

func (r *scriptedRows) Columns() []string { return []string{"outcome"} }
func (r *scriptedRows) Close() error      { return nil }
func (r *scriptedRows) Next(dest []driver.Value) error {
	if !r.pending {
		return io.EOF
	}
	r.pending = false
	dest[0] = r.value
	return nil
}

func openScriptedDB(conn *scriptedConn) *sql.DB {
	return sql.OpenDB(scriptedConnector{conn: conn})
}

func TestSQLProofStore_ConsumeOutcomesAndFailures(t *testing.T) {
	dbErr := errors.New("query unavailable")
	insertErr := errors.New("unique conflict")
	cases := []struct {
		name     string
		queries  []queryStep
		execs    []execStep
		wantUsed bool
		wantErr  error
	}{
		{name: "already consumed", queries: []queryStep{{value: "executed"}}, wantUsed: true},
		{name: "initial query failure", queries: []queryStep{{err: dbErr}}, wantErr: dbErr},
		{name: "fresh insert", queries: []queryStep{{noRows: true}}, execs: []execStep{{}}, wantUsed: false},
		{name: "insert collision then committed reread", queries: []queryStep{{noRows: true}, {value: "executed"}}, execs: []execStep{{err: insertErr}}, wantUsed: true},
		{name: "insert failure without committed reread", queries: []queryStep{{noRows: true}, {err: dbErr}}, execs: []execStep{{err: insertErr}}, wantErr: stepup.ErrAmbiguous},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn := &scriptedConn{queries: tc.queries, execs: tc.execs}
			db := openScriptedDB(conn)
			t.Cleanup(func() { _ = db.Close() })
			store := stepup.NewSQLProofStore(db)
			used, err := store.Consume(context.Background(), "proof-1", string(stepup.OutcomeExecuted))
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Consume error = %v, want %v", err, tc.wantErr)
				}
			} else if err != nil || used != tc.wantUsed {
				t.Fatalf("Consume = (%v, %v), want (%v, nil)", used, err, tc.wantUsed)
			}
		})
	}
}

func TestNewSQLProofStore_RequiresDatabaseHandle(t *testing.T) {
	if stepup.NewSQLProofStore(nil) == nil {
		t.Fatal("NewSQLProofStore(nil) returned nil")
	}
}
