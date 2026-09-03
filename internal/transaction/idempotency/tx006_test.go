package idempotency_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
)

// TestTodo_TX_006 proves the GREEN clause end to end: a first call runs its
// effect and stores the identity; a replay under the same digest returns
// that stored identity without running the effect again; the same key under
// a different digest is refused as IDEMPOTENCY_CONFLICT and mutates nothing.
func TestTodo_TX_006(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "tx006-primary")
	conn := appConn(t, db)
	store := idempotency.PostgresStore{}
	scope := defaultScope(tenant, "primary")
	digest := digestOf("promotion:worker-1:effective-date:2026-10-01")

	var calls int32

	t.Run("first call runs the effect and stores its identity", func(t *testing.T) {
		var rec idempotency.Record
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			rec, err = idempotency.Guard(ctx, tx, store, scope, digest, defaultPolicy, fixedInstant,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					atomic.AddInt32(&calls, 1)
					return idempotency.ResultIdentity{ResultRef: "promotion-result-1", EventRef: "worker-1@7"}, nil
				})
			return err
		})
		if rec.Status != idempotency.StatusCompleted {
			t.Fatalf("status = %q, want COMPLETED", rec.Status)
		}
		if rec.Identity.ResultRef != "promotion-result-1" || rec.Identity.EventRef != "worker-1@7" {
			t.Fatalf("stored identity = %+v, want the effect's own identity", rec.Identity)
		}
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Fatalf("effect ran %d times, want 1", got)
		}
	})

	t.Run("replay under the same digest returns the stored identity without rerunning the effect", func(t *testing.T) {
		var rec idempotency.Record
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			rec, err = idempotency.Guard(ctx, tx, store, scope, digest, defaultPolicy, fixedInstant.Add(time.Hour),
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					atomic.AddInt32(&calls, 1)
					t.Fatal("the effect ran a second time on an exact replay")
					return idempotency.ResultIdentity{}, nil
				})
			return err
		})
		if rec.Identity.ResultRef != "promotion-result-1" {
			t.Fatalf("replayed identity = %+v, want the original", rec.Identity)
		}
		if got := atomic.LoadInt32(&calls); got != 1 {
			t.Fatalf("effect ran %d times after replay, want 1 (never a second effect)", got)
		}
	})

	t.Run("the same key under a different digest conflicts and mutates nothing", func(t *testing.T) {
		otherDigest := digestOf("promotion:worker-1:effective-date:2026-11-01")
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := idempotency.Guard(ctx, tx, store, scope, otherDigest, defaultPolicy, fixedInstant,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					atomic.AddInt32(&calls, 1)
					t.Fatal("the effect ran under a conflicting digest")
					return idempotency.ResultIdentity{}, nil
				})
			return err
		})
		if code := idempotency.CodeOf(err); code != idempotency.CodeConflict {
			t.Fatalf("code = %q, want %q (%v)", code, idempotency.CodeConflict, err)
		}
		var idErr *idempotency.Error
		if !errors.As(err, &idErr) {
			t.Fatalf("error is not *idempotency.Error: %v", err)
		}
		if idErr.RecordedDigest != digest || idErr.RequestDigest != otherDigest {
			t.Fatalf("conflict digests = (%q, %q), want (%q, %q)",
				idErr.RecordedDigest, idErr.RequestDigest, digest, otherDigest)
		}

		// Nothing mutated: the record on disk still carries the original
		// digest and identity.
		var rec idempotency.Record
		var found bool
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var lookupErr error
			rec, found, lookupErr = store.Lookup(ctx, tx, scope)
			return lookupErr
		})
		if !found {
			t.Fatal("the original record disappeared after a conflicting attempt")
		}
		if rec.RequestDigest != digest || rec.Identity.ResultRef != "promotion-result-1" {
			t.Fatalf("record after conflict = %+v, want the untouched original", rec)
		}
	})
}

// TestTodo_TX_006_Golden pins the exact stored shape of one guarded call
// against fixed inputs: the scope, the digest, the policy and the clock are
// all fixed, so the resulting record's every field is a golden value.
func TestTodo_TX_006_Golden(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "tx006-golden")
	conn := appConn(t, db)
	store := idempotency.PostgresStore{}

	scope := idempotency.Scope{
		Tenant:      tenant,
		Capability:  "promotion.approve.v1",
		EffectScope: "worker:employment_change",
		Key:         "golden-key",
	}
	digest := digestOf("golden-fixture")
	policy := idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour}
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	identity := idempotency.ResultIdentity{
		ResultRef:      "res:golden-1",
		EventRef:       "worker:golden@1",
		EffectIdentity: "effect:golden-1",
		EvidenceID:     "evidence:golden-1",
	}

	var rec idempotency.Record
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		rec, err = idempotency.Guard(ctx, tx, store, scope, digest, policy, now,
			func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
				return identity, nil
			})
		return err
	})

	want := idempotency.Record{
		Scope:         scope,
		RequestDigest: digest,
		Status:        idempotency.StatusCompleted,
		Identity:      identity,
		CreatedAt:     now,
		ExpiresAt:     now.Add(72 * time.Hour),
	}
	if rec.Scope != want.Scope {
		t.Fatalf("scope = %+v, want %+v", rec.Scope, want.Scope)
	}
	if rec.RequestDigest != want.RequestDigest {
		t.Fatalf("request digest = %q, want %q", rec.RequestDigest, want.RequestDigest)
	}
	if rec.Status != want.Status {
		t.Fatalf("status = %q, want %q", rec.Status, want.Status)
	}
	if rec.Identity != want.Identity {
		t.Fatalf("identity = %+v, want %+v", rec.Identity, want.Identity)
	}
	if !rec.CreatedAt.Equal(want.CreatedAt) {
		t.Fatalf("created_at = %s, want %s", rec.CreatedAt, want.CreatedAt)
	}
	if !rec.ExpiresAt.Equal(want.ExpiresAt) {
		t.Fatalf("expires_at = %s, want %s", rec.ExpiresAt, want.ExpiresAt)
	}
}

// TestTodo_TX_006_Race proves the RED clause "concurrent same-key calls
// commit twice" is impossible: two real, independent connections race
// Guard.Reserve on the exact same scope and digest at once, and the effect
// still runs exactly once. The loser observes the winner's own stored
// identity, never a second one of its own.
func TestTodo_TX_006_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "tx006-race")
	store := idempotency.PostgresStore{}
	scope := defaultScope(tenant, "race")
	digest := digestOf("race-fixture")

	const attempts = 8
	var calls int32
	results := make([]idempotency.Record, attempts)
	errs := make([]error, attempts)

	var start sync.WaitGroup
	start.Add(1)
	var done sync.WaitGroup
	done.Add(attempts)

	for i := 0; i < attempts; i++ {
		i := i
		conn := appConn(t, db)
		go func() {
			defer done.Done()
			start.Wait()

			tx, err := conn.Begin(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			runErr := func() error {
				if e := tenancy.WithTenant(ctx, tx, tenant); e != nil {
					return e
				}
				rec, guardErr := idempotency.Guard(ctx, tx, store, scope, digest, defaultPolicy, fixedInstant,
					func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
						atomic.AddInt32(&calls, 1)
						return idempotency.ResultIdentity{
							ResultRef: "race-result",
							EventRef:  "attempt-" + uuid.NewString(),
						}, nil
					})
				results[i] = rec
				return guardErr
			}()
			if runErr != nil {
				_ = tx.Rollback(ctx)
				errs[i] = runErr
				return
			}
			errs[i] = tx.Commit(ctx)
		}()
	}

	start.Done()
	done.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("effect ran %d times across %d concurrent attempts, want exactly 1", got, attempts)
	}

	var succeeded, conflicted, inProgress int
	var winningEventRef string
	for i, err := range errs {
		switch {
		case err == nil:
			succeeded++
			if results[i].Identity.EventRef == "" {
				t.Fatalf("attempt %d succeeded with no stored identity", i)
			}
			if winningEventRef == "" {
				winningEventRef = results[i].Identity.EventRef
			} else if results[i].Identity.EventRef != winningEventRef {
				t.Fatalf("attempt %d succeeded with a different identity (%q) than the winner (%q); two effects committed",
					i, results[i].Identity.EventRef, winningEventRef)
			}
		case idempotency.CodeOf(err) == idempotency.CodeInProgress:
			inProgress++
		case idempotency.CodeOf(err) == idempotency.CodeConflict:
			conflicted++
		default:
			t.Fatalf("attempt %d failed unexpectedly: %v", i, err)
		}
	}

	if succeeded == 0 {
		t.Fatal("no concurrent attempt ever succeeded")
	}
	if conflicted != 0 {
		t.Fatalf("%d attempt(s) saw a digest conflict, but every attempt used the same digest", conflicted)
	}
	t.Logf("attempts=%d succeeded=%d in_progress=%d", attempts, succeeded, inProgress)
}
