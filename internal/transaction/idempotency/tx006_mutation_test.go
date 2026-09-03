package idempotency_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
)

// TestTodo_TX_006_Mutation pins boundary and dimension behavior a weakened
// implementation could pass the other tests while getting wrong: retention
// exactly equal to the retry window is legal ("shorter than" is a strict
// inequality); every one of the four scope dimensions is load-bearing in the
// uniqueness key, not just the idempotency key by itself; a malformed digest
// is refused before any row is written; and a conflicting attempt changes
// nothing about the row it did not win, including its timestamps.
func TestTodo_TX_006_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "tx006-mutation")
	conn := appConn(t, db)
	store := idempotency.PostgresStore{}

	t.Run("retention exactly equal to the retry window is legal, not refused", func(t *testing.T) {
		scope := defaultScope(tenant, "mutation-equal-retention")
		digest := digestOf("mutation-equal-retention")
		policy := idempotency.RetentionPolicy{Retention: 24 * time.Hour, RetryWindow: 24 * time.Hour}

		var ran bool
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, guardErr := idempotency.Guard(ctx, tx, store, scope, digest, policy, fixedInstant,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					ran = true
					return idempotency.ResultIdentity{ResultRef: "equal-boundary"}, nil
				})
			return guardErr
		})
		if err != nil {
			t.Fatalf("retention == retry window was refused: %v", err)
		}
		if !ran {
			t.Fatal("the effect never ran under a legal equal-boundary policy")
		}
	})

	t.Run("capability, effect scope and key are each load-bearing in the uniqueness tuple", func(t *testing.T) {
		base := idempotency.Scope{
			Tenant:      tenant,
			Capability:  "cap-a",
			EffectScope: "scope-a",
			Key:         "key-a",
		}
		digest := digestOf("mutation-dimensions")

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, created, err := store.Reserve(ctx, tx, base, digest, defaultPolicy, fixedInstant)
			if err != nil {
				return err
			}
			if !created {
				t.Fatal("base scope was not freshly created")
			}
			return nil
		})

		variants := []idempotency.Scope{
			{Tenant: tenant, Capability: "cap-b", EffectScope: "scope-a", Key: "key-a"},
			{Tenant: tenant, Capability: "cap-a", EffectScope: "scope-b", Key: "key-a"},
			{Tenant: tenant, Capability: "cap-a", EffectScope: "scope-a", Key: "key-b"},
		}
		for _, v := range variants {
			v := v
			inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
				_, created, err := store.Reserve(ctx, tx, v, digest, defaultPolicy, fixedInstant)
				if err != nil {
					return err
				}
				if !created {
					t.Fatalf("scope %+v collided with the base scope; a dimension is not load-bearing", v)
				}
				return nil
			})
		}
	})

	t.Run("a malformed digest is refused before any row is written", func(t *testing.T) {
		scope := defaultScope(tenant, "mutation-bad-digest")
		badDigest := "not-a-sha256-digest"

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, _, resErr := store.Reserve(ctx, tx, scope, badDigest, defaultPolicy, fixedInstant)
			return resErr
		})
		if code := idempotency.CodeOf(err); code != idempotency.CodeInvalidRecord {
			t.Fatalf("code = %q, want %q (%v)", code, idempotency.CodeInvalidRecord, err)
		}

		var found bool
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var lookupErr error
			_, found, lookupErr = store.Lookup(ctx, tx, scope)
			return lookupErr
		})
		if found {
			t.Fatal("a row was written despite a refused, malformed digest")
		}
	})

	t.Run("a conflicting attempt leaves the winning row's timestamps untouched", func(t *testing.T) {
		scope := defaultScope(tenant, "mutation-timestamps")
		digest := digestOf("mutation-timestamps-a")
		otherDigest := digestOf("mutation-timestamps-b")

		var original idempotency.Record
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			original, err = idempotency.Guard(ctx, tx, store, scope, digest, defaultPolicy, fixedInstant,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					return idempotency.ResultIdentity{ResultRef: "original"}, nil
				})
			return err
		})

		later := fixedInstant.Add(48 * time.Hour)
		_ = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, guardErr := idempotency.Guard(ctx, tx, store, scope, otherDigest, defaultPolicy, later,
				func(context.Context, dbport.Tx) (idempotency.ResultIdentity, error) {
					t.Fatal("a conflicting digest ran the effect")
					return idempotency.ResultIdentity{}, nil
				})
			return guardErr
		})

		var after idempotency.Record
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var lookupErr error
			var found bool
			after, found, lookupErr = store.Lookup(ctx, tx, scope)
			if lookupErr == nil && !found {
				t.Fatal("the winning record disappeared after a conflicting attempt")
			}
			return lookupErr
		})

		if !after.CreatedAt.Equal(original.CreatedAt) {
			t.Fatalf("created_at changed after a conflict: %s -> %s", original.CreatedAt, after.CreatedAt)
		}
		if !after.ExpiresAt.Equal(original.ExpiresAt) {
			t.Fatalf("expires_at changed after a conflict: %s -> %s", original.ExpiresAt, after.ExpiresAt)
		}
		if after.RequestDigest != digest {
			t.Fatalf("request digest changed after a conflict: %q -> %q", digest, after.RequestDigest)
		}
		if after.Identity != original.Identity {
			t.Fatalf("identity changed after a conflict: %+v -> %+v", original.Identity, after.Identity)
		}
	})
}
