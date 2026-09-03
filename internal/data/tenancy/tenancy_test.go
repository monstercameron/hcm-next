package tenancy_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

// TestTodo_DB_017_NilTenantRejected is a pure Go-layer guard: WithTenant must
// refuse the nil UUID before it ever reaches PostgreSQL, because a nil tenant
// id would otherwise be indistinguishable at the SQL level from a (however
// unlikely) real tenant identifier and would fail open instead of loud.
func TestTodo_DB_017_NilTenantRejected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := tenancy.WithTenant(ctx, tx, uuid.Nil); err == nil {
		t.Fatal("WithTenant accepted the nil UUID")
	}
}
