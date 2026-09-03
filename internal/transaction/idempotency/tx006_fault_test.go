package idempotency_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
)

// TestTodo_TX_006_Fault proves the RED clause "retention expires before
// retry/redelivery window" is refused at Reserve time before anything is
// written, and that an effect which fails leaves nothing durable behind: the
// whole point of running Reserve/effect/Complete inside the caller's own
// transaction is that a caller who rolls back on error also erases the
// reservation, so a later retry starts clean.
func TestTodo_TX_006_Fault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "tx006-fault")
	conn := appConn(t, db)
	store := idempotency.PostgresStore{}

	t.Run("a retention shorter than the retry window is refused before any row is written", func(t *testing.T) {
		scope := defaultScope(tenant, "fault-retention")
		digest := digestOf("fault-retention")
		badPolicy := idempotency.RetentionPolicy{
			Retention:   1 * time.Hour,
			RetryWindow: 24 * time.Hour,
		}

		ran := false
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, guardErr := idempotency.Guard(ctx, tx, store, scope, digest, badPolicy, fixedInstant,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					ran = true
					return idempotency.ResultIdentity{ResultRef: "should-never-run"}, nil
				})
			return guardErr
		})
		if code := idempotency.CodeOf(err); code != idempotency.CodeRetentionTooShort {
			t.Fatalf("code = %q, want %q (%v)", code, idempotency.CodeRetentionTooShort, err)
		}
		if ran {
			t.Fatal("the effect ran despite an illegal retention policy")
		}

		// Nothing was written: a completely fresh Guard call under a legal
		// policy for the same scope and digest still gets to run the
		// effect, which it could not if a row had been left behind.
		var completed bool
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, err := idempotency.Guard(ctx, tx, store, scope, digest, defaultPolicy, fixedInstant,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					completed = true
					return idempotency.ResultIdentity{ResultRef: "recovered"}, nil
				})
			return err
		})
		if !completed {
			t.Fatal("a legal retry could not run its effect; a row must have survived the refused reservation")
		}
	})

	t.Run("an effect that fails leaves no reservation behind for a retry to trip over", func(t *testing.T) {
		scope := defaultScope(tenant, "fault-effect-error")
		digest := digestOf("fault-effect-error")
		boom := errors.New("boom: the connector call failed")

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, guardErr := idempotency.Guard(ctx, tx, store, scope, digest, defaultPolicy, fixedInstant,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					return idempotency.ResultIdentity{}, boom
				})
			return guardErr
		})
		if !errors.Is(err, boom) {
			t.Fatalf("Guard did not propagate the effect's own error: %v", err)
		}

		// The transaction rolled back, so the reservation the failed attempt
		// made is gone: Lookup outside that transaction sees nothing, and a
		// retry with the same scope and digest gets to run and succeed.
		var found bool
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var lookupErr error
			_, found, lookupErr = store.Lookup(ctx, tx, scope)
			return lookupErr
		})
		if found {
			t.Fatal("a record survived a rolled-back transaction")
		}

		var ranRetry bool
		var rec idempotency.Record
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			rec, err = idempotency.Guard(ctx, tx, store, scope, digest, defaultPolicy, fixedInstant,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					ranRetry = true
					return idempotency.ResultIdentity{ResultRef: "retry-succeeded"}, nil
				})
			return err
		})
		if !ranRetry {
			t.Fatal("the retry never ran its effect")
		}
		if rec.Identity.ResultRef != "retry-succeeded" {
			t.Fatalf("retry identity = %+v, want retry-succeeded", rec.Identity)
		}
	})

	t.Run("Complete refuses a scope with no matching reservation", func(t *testing.T) {
		scope := defaultScope(tenant, "fault-complete-orphan")
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, completeErr := store.Complete(ctx, tx, scope, idempotency.ResultIdentity{ResultRef: "orphan"}, fixedInstant)
			return completeErr
		})
		if code := idempotency.CodeOf(err); code != idempotency.CodeNotReserved {
			t.Fatalf("code = %q, want %q (%v)", code, idempotency.CodeNotReserved, err)
		}
	})

	t.Run("Complete refuses an empty result identity", func(t *testing.T) {
		scope := defaultScope(tenant, "fault-complete-empty")
		digest := digestOf("fault-complete-empty")
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			if _, _, resErr := store.Reserve(ctx, tx, scope, digest, defaultPolicy, fixedInstant); resErr != nil {
				return resErr
			}
			_, completeErr := store.Complete(ctx, tx, scope, idempotency.ResultIdentity{}, fixedInstant)
			return completeErr
		})
		if code := idempotency.CodeOf(err); code != idempotency.CodeInvalidRecord {
			t.Fatalf("code = %q, want %q (%v)", code, idempotency.CodeInvalidRecord, err)
		}
	})
}
