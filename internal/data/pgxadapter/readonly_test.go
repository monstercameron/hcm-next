package pgxadapter_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

func TestBeginReadOnlyIsServerEnforcedAndRollbackLeavesConnectionUsable(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()

	t.Run("Conn", func(t *testing.T) {
		conn, err := pgxadapter.Connect(ctx, db.URL, map[string]string{"search_path": db.Schema})
		if err != nil {
			t.Fatalf("Connect: %v", err)
		}
		defer conn.Close(ctx)
		assertReadOnlyTransaction(t, conn, conn.BeginReadOnly)
	})

	t.Run("Pool", func(t *testing.T) {
		pool, err := pgxadapter.NewPool(ctx, db.URL, map[string]string{"search_path": db.Schema})
		if err != nil {
			t.Fatalf("NewPool: %v", err)
		}
		defer pool.Close()
		assertReadOnlyTransaction(t, pool, pool.BeginReadOnly)
	})
}

func assertReadOnlyTransaction(t *testing.T, conn dbport.Conn, begin func(context.Context) (dbport.Tx, error)) {
	t.Helper()
	ctx := context.Background()
	tx, err := begin(ctx)
	if err != nil {
		t.Fatalf("BeginReadOnly: %v", err)
	}

	var readOnly string
	if err := tx.QueryRow(ctx, "SHOW transaction_read_only").Scan(&readOnly); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("SHOW transaction_read_only: %v", err)
	}
	if readOnly != "on" {
		_ = tx.Rollback(ctx)
		t.Fatalf("transaction_read_only = %q, want on", readOnly)
	}
	if _, err := tx.Exec(ctx, "CREATE TEMP TABLE must_not_be_created (id integer)"); err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("write succeeded in a read-only transaction")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback failed read-only transaction: %v", err)
	}

	var one int
	if err := conn.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		t.Fatalf("connection unusable after rollback: %v", err)
	}
	if one != 1 {
		t.Fatalf("post-rollback query = %d, want 1", one)
	}
}
