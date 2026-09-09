package queryplans

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Call is one statement a [Spy] observed a caller run, exactly as that
// caller sent it: the SQL text and the positional arguments bound to it.
type Call struct {
	SQL  string
	Args []any
}

// Spy wraps a [dbport.Conn] and records every statement run through Query or
// QueryRow before forwarding it unchanged. It exists so a catalogue entry's
// Prepare closure can drive a store's real, exported method and recover the
// exact statement that method sent -- the "SQL as used by the store" DB-020
// asks for -- without the catalogue ever copying that SQL text by hand and
// risking it drifting from the method it was copied out of.
//
// Exec is not recorded: every catalogue entry in this package proves a read,
// and Spy exists to capture the query a read issues, not a write a fixture
// seeds.
type Spy struct {
	dbport.Conn
	Calls []Call
}

// NewSpy wraps conn.
func NewSpy(conn dbport.Conn) *Spy {
	return &Spy{Conn: conn}
}

// Query records the call and forwards it to the wrapped connection.
func (s *Spy) Query(ctx context.Context, sqlText string, args ...any) (dbport.Rows, error) {
	s.Calls = append(s.Calls, Call{SQL: sqlText, Args: args})
	return s.Conn.Query(ctx, sqlText, args...)
}

// QueryRow records the call and forwards it to the wrapped connection.
func (s *Spy) QueryRow(ctx context.Context, sqlText string, args ...any) dbport.Row {
	s.Calls = append(s.Calls, Call{SQL: sqlText, Args: args})
	return s.Conn.QueryRow(ctx, sqlText, args...)
}

// CallAgainst returns the last recorded call whose statement text names
// table, and reports whether one was found. "Last" rather than "first"
// matters for a store method that reads more than one table before the one a
// catalogue entry is proving (internal/data/ledger/temporal's delegated
// modes read ledger_event and then, only when an assertion carries an
// authority reference, authority_assignment): the call this package explains
// is always the one against the table the entry names, never an earlier or
// later statement the same call happened to also issue.
func (s *Spy) CallAgainst(table string) (Call, bool) {
	for i := len(s.Calls) - 1; i >= 0; i-- {
		if strings.Contains(s.Calls[i].SQL, table) {
			return s.Calls[i], true
		}
	}
	return Call{}, false
}
