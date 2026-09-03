package idempotency_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/transaction/idempotency"
)

// TestTodo_TX_006_Security proves the tenant boundary is physical, not a
// convention, the same way internal/workflow/runtime's
// TestTodo_WF_RUN_001_Security proves it for workflow_instance: under the
// least-privilege role, one tenant's transaction cannot read, reserve over,
// or even learn about another tenant's idempotency record, and a
// transaction that forgot to declare a tenant sees nothing at all.
func TestTodo_TX_006_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	alice := insertTenant(t, db, "tx006-security-a")
	bob := insertTenant(t, db, "tx006-security-b")
	store := idempotency.PostgresStore{}
	conn := appConn(t, db)

	aliceScope := idempotency.Scope{
		Tenant:      alice,
		Capability:  "promotion.approve.v1",
		EffectScope: "worker:employment_change",
		Key:         "alice-key",
	}
	digest := digestOf("security-fixture")

	inTenantTx(t, conn, alice, func(tx dbport.Tx) error {
		_, _, err := store.Reserve(ctx, tx, aliceScope, digest, defaultPolicy, fixedInstant)
		if err != nil {
			return err
		}
		_, err = store.Complete(ctx, tx, aliceScope, idempotency.ResultIdentity{ResultRef: "alice-result"}, fixedInstant)
		return err
	})

	t.Run("another tenant cannot look up the record", func(t *testing.T) {
		// bob's transaction addresses alice's own tenant id in the Scope; the
		// tenant isolation policy still confines it to bob's own rows
		// because the session's app.tenant_id is bob's, not alice's -- a
		// forged Scope.Tenant field cannot reach across the boundary.
		var found bool
		inTenantTx(t, conn, bob, func(tx dbport.Tx) error {
			var err error
			_, found, err = store.Lookup(ctx, tx, aliceScope)
			return err
		})
		if found {
			t.Fatal("bob's transaction found alice's idempotency record")
		}
	})

	t.Run("a transaction with no tenant scope sees nothing", func(t *testing.T) {
		var found bool
		inTx(t, conn, func(tx dbport.Tx) error {
			var err error
			_, found, err = store.Lookup(ctx, tx, aliceScope)
			return err
		})
		if found {
			t.Fatal("an unscoped transaction found alice's idempotency record")
		}
	})

	t.Run("another tenant can reserve the identical key/capability/effect scope as its own row", func(t *testing.T) {
		// The primary key is (tenant, capability, effect_scope, key), so
		// bob reserving the same capability/effect_scope/key alice already
		// completed must not collide with alice's row -- the tenant
		// dimension is what keeps the two namespaces apart, exactly as
		// platform-foundation-gap-closure.md section 6 requires ("tenant
		// namespaces cannot collide").
		bobScope := idempotency.Scope{
			Tenant:      bob,
			Capability:  aliceScope.Capability,
			EffectScope: aliceScope.EffectScope,
			Key:         aliceScope.Key,
		}
		var rec idempotency.Record
		var created bool
		inTenantTx(t, conn, bob, func(tx dbport.Tx) error {
			var err error
			rec, created, err = store.Reserve(ctx, tx, bobScope, digest, defaultPolicy, fixedInstant)
			return err
		})
		if !created {
			t.Fatalf("bob's reservation collided with alice's tenant-scoped row: %+v", rec)
		}
	})

	t.Run("a tenant cannot forge a reservation under another tenant's row level security context", func(t *testing.T) {
		// bob's session tenant is bob, but the Scope names alice: RLS's
		// WITH CHECK clause must refuse the INSERT rather than let it land
		// under alice's tenant_id.
		forged := idempotency.Scope{
			Tenant:      alice,
			Capability:  "forged.capability.v1",
			EffectScope: "forged:scope",
			Key:         "forged-key",
		}
		err := inTenantTxErr(conn, bob, func(tx dbport.Tx) error {
			_, _, err := store.Reserve(ctx, tx, forged, digestOf("forged"), defaultPolicy, fixedInstant)
			return err
		})
		if err == nil {
			t.Fatal("bob's tenant-scoped transaction inserted a record under alice's tenant id")
		}
	})
}
