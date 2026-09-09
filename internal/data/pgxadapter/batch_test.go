package pgxadapter_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_PERFOPT_004_AdapterBatchIntegration(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, `CREATE TABLE batch_probe (id integer PRIMARY KEY, value text NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	counts, err := dbport.ExecAll(ctx, conn, []dbport.Statement{
		{SQL: `INSERT INTO batch_probe (id, value) VALUES ($1, $2)`, Args: []any{1, "one"}},
		{SQL: `INSERT INTO batch_probe (id, value) VALUES ($1, $2)`, Args: []any{2, "two"}},
		{SQL: `INSERT INTO batch_probe (id, value) VALUES ($1, $2)`, Args: []any{3, "three"}},
	})
	if err != nil {
		t.Fatalf("batch insert: %v", err)
	}
	if len(counts) != 3 || counts[0] != 1 || counts[1] != 1 || counts[2] != 1 {
		t.Fatalf("counts = %v, want [1 1 1]", counts)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = dbport.ExecAll(ctx, tx, []dbport.Statement{
		{SQL: `INSERT INTO batch_probe (id, value) VALUES ($1, $2)`, Args: []any{4, "four"}},
		{SQL: `INSERT INTO batch_probe (id, value) VALUES ($1, $2)`, Args: []any{2, "duplicate"}},
	})
	if err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("duplicate batch statement succeeded")
	}
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback after batch error: %v", rollbackErr)
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM batch_probe`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("rows after rollback = %d, want 3", count)
	}
}
