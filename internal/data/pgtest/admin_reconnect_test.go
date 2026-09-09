package pgtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Under a loaded machine a DROP SCHEMA ... CASCADE overran its deadline, pgx
// closed the shared admin connection, and every later CREATE SCHEMA in the
// package failed with "conn closed". withAdmin must recover from a closed
// admin connection rather than hand it to the next caller.
func TestWithAdminReconnectsAfterTheAdminConnectionCloses(t *testing.T) {
	if err := ensureServer(); err != nil {
		t.Fatalf("server: %v", err)
	}
	ctx := context.Background()

	adminMu.Lock()
	closeErr := adminConn.Close(ctx)
	closed := adminConn.IsClosed()
	adminMu.Unlock()
	if closeErr != nil || !closed {
		t.Fatalf("closing the admin connection: err=%v closed=%v", closeErr, closed)
	}

	var one int
	err := withAdmin(ctx, func(conn dbport.Conn) error {
		return conn.QueryRow(ctx, "SELECT 1").Scan(&one)
	})
	if err != nil || one != 1 {
		t.Fatalf("withAdmin after a closed admin connection: err=%v one=%d, want a reconnect and 1", err, one)
	}
}

func TestWithAdminRetriesOnceWhenTheStatementClosedTheConnection(t *testing.T) {
	if err := ensureServer(); err != nil {
		t.Fatalf("server: %v", err)
	}
	ctx := context.Background()
	calls := 0
	err := withAdmin(ctx, func(conn dbport.Conn) error {
		calls++
		if calls == 1 {
			// Simulate a statement that pgx aborted: the connection is closed
			// underneath the caller and the call returns an error.
			_ = conn.(interface{ Close(context.Context) error }).Close(ctx)
			return errors.New("conn closed")
		}
		var n int
		return conn.QueryRow(ctx, "SELECT 2").Scan(&n)
	})
	if err != nil || calls != 2 {
		t.Fatalf("withAdmin: err=%v calls=%d, want one reconnect-and-retry", err, calls)
	}
}

func TestWithAdminDoesNotRetryWithASpentContext(t *testing.T) {
	if err := ensureServer(); err != nil {
		t.Fatalf("server: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	calls := 0
	err := withAdmin(ctx, func(conn dbport.Conn) error {
		calls++
		_ = conn.(interface{ Close(context.Context) error }).Close(context.Background())
		cancel()
		return errors.New("conn closed")
	})
	if err == nil || calls != 1 {
		t.Fatalf("withAdmin with a spent context: err=%v calls=%d, want the original error and no retry", err, calls)
	}
	// The admin connection was still replaced, so the next caller is fine.
	var n int
	if err := withAdmin(context.Background(), func(conn dbport.Conn) error {
		return conn.QueryRow(context.Background(), "SELECT 3").Scan(&n)
	}); err != nil || n != 3 {
		t.Fatalf("next caller after a spent-context failure: err=%v n=%d", err, n)
	}
}
