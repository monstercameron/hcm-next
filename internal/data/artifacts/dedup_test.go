package artifacts_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/artifacts"
)

// DATA-019 rests on one physical fact migrations/00010_artifacts.sql already
// establishes and internal/data/artifacts/store.go's own doc comment states:
// the artifact table's identity is PRIMARY KEY (tenant_id, content_id), and
// [PutStream]'s idempotency check (`ON CONFLICT (tenant_id, content_id) DO
// NOTHING`) is scoped by that same composite key. Content addressing is
// therefore already tenant-keyed, not global: two tenants who happen to hold
// byte-identical sensitive content never share a row, a reference count, a
// retention decision or a deletion outcome, and no operation in this package
// can observe whether another tenant holds the same bytes. This file proves
// that fact rather than assuming it.
//
// The storage-disposition registry (internal/data/tenancy/storagedisposition,
// definitions/storage/storage-disposition.yaml) deliberately does not carry
// an entry for the artifact table at all -- migrations/00010's own header
// comment and the registry's own header comment both say why: the artifact
// store lives in a companion schema specifically so it falls outside the
// registry-driven live-schema enumeration internal/data/schema's
// TestTodo_DATA_001 walks. Its two declared encryption_class values,
// PLATFORM_MANAGED and FIELD_LEVEL, describe at-rest encryption for the
// tables that *are* registered and are orthogonal to this guarantee: the
// no-leak property proved here comes from the composite tenant-scoped
// primary key plus row level security (migrations/00010's own tenant_isolation
// policy), not from which encryption class a table declares.
func TestTodo_DATA_019(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	other := fixture{db: f.db, schema: f.schema, tenant: insertTenant(t, f.db, "other-tenant-dedup")}

	content := []byte("2026 compensation letter, byte-identical across two tenants")
	wantID := sha256Hex(content)

	t.Run("identical sensitive bytes under two tenants yield two rows, not a shared one", func(t *testing.T) {
		recA := f.mustPut(t, f.putRequest(content))
		recB := other.mustPut(t, other.putRequest(content))

		if recA.ContentID != wantID || recB.ContentID != wantID {
			t.Fatalf("content ids %s / %s, want both %s", recA.ContentID, recB.ContentID, wantID)
		}
		if recA.Tenant == recB.Tenant {
			t.Fatal("fixture generated a colliding tenant identifier")
		}

		// Each tenant's own read succeeds against its own row: this is not
		// one shared artifact visible from two angles.
		gotA, err := artifactsRead(f, wantID)
		if err != nil {
			t.Fatalf("tenant A read its own artifact: %v", err)
		}
		if gotA.Tenant != recA.Tenant {
			t.Fatalf("tenant A's read named tenant %s, want %s", gotA.Tenant, recA.Tenant)
		}
		gotB, err := artifactsRead(other, wantID)
		if err != nil {
			t.Fatalf("tenant B read its own artifact: %v", err)
		}
		if gotB.Tenant != recB.Tenant {
			t.Fatalf("tenant B's read named tenant %s, want %s", gotB.Tenant, recB.Tenant)
		}
	})

	t.Run("a second tenant's put of the same bytes is created=true, never suppressed by another tenant's row", func(t *testing.T) {
		content2 := []byte("a second, independently chosen byte-identical secret")
		_, createdA, err := f.put(f.putRequest(content2))
		if err != nil {
			t.Fatalf("tenant A put: %v", err)
		}
		if !createdA {
			t.Fatal("tenant A's first put of new content reported created=false")
		}

		// If dedup were global (a bug this test exists to catch), tenant B's
		// put would either fail, silently return tenant A's row, or report
		// created=false because "the content already exists" -- any of which
		// would leak the fact that tenant A holds this content.
		recB, createdB, err := other.put(other.putRequest(content2))
		if err != nil {
			t.Fatalf("tenant B put of content tenant A already holds: %v", err)
		}
		if !createdB {
			t.Fatal("tenant B's put reported created=false because another tenant already holds identical bytes (cross-tenant dedup leak)")
		}
		if recB.Tenant != other.tenant {
			t.Fatalf("tenant B's put returned a record for tenant %s", recB.Tenant)
		}
	})

	t.Run("row level security still hides one tenant's row from the other at the storage layer", func(t *testing.T) {
		// This check needs content only one tenant has ever written: wantID
		// (above) is now held by *both* tenants, under their own separate
		// rows, so scoping a query to tenant B and finding a row there would
		// just be tenant B's own legitimate row -- not evidence either way
		// about whether tenant B can see tenant A's. A single-owner content
		// id is what actually exercises the isolation.
		onlyA := []byte("only tenant A ever writes this exact content")
		aOnlyID := sha256Hex(onlyA)
		f.mustPut(t, f.putRequest(onlyA))

		if rlsVisibleCrossTenant(t, f, other.tenant, aOnlyID) {
			t.Fatal("tenant B's RLS-scoped connection saw tenant A's artifact row directly")
		}
		if !rlsVisibleCrossTenant(t, f, f.tenant, aOnlyID) {
			t.Fatal("tenant A's RLS-scoped connection could not see its own artifact row")
		}
	})

	t.Run("reference counts, retention and deletion eligibility are computed per tenant, never shared", func(t *testing.T) {
		f.mustAddReference(t, wantID, artifacts.OwnerRef{Kind: artifacts.OwnerReceipt, ID: "receipt-tenant-a"})

		countA, err := artifactsReferenceCount(f, wantID)
		if err != nil {
			t.Fatalf("reference count for tenant A: %v", err)
		}
		if countA != 1 {
			t.Fatalf("tenant A reference count %d, want 1", countA)
		}

		// Tenant B never referenced this content id under its own tenant
		// scope, so its count must be zero -- not 1 (which would mean the
		// reference log is shared across tenants) and not an error (which
		// would itself be a distinguishing signal).
		countB, err := artifactsReferenceCount(other, wantID)
		if err != nil {
			t.Fatalf("reference count for tenant B: %v", err)
		}
		if countB != 0 {
			t.Fatalf("tenant B reference count %d, want 0 (a shared reference log would leak tenant A's reference)", countB)
		}
	})
}

// TestTodo_DATA_019_Security proves the equality-oracle case DATA-019's own
// RED clause names: a retrieval attempt naming a content id that exists, but
// only under a different tenant, must be refused in exactly the same shape --
// same error type, same denial reason, same durable refusal evidence -- as a
// retrieval naming a content id that has never been written by anyone. If the
// two cases differed in any observable way, a caller could use Retrieve as an
// existence oracle against another tenant's content by digest alone.
func TestTodo_DATA_019_Security(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	victim := fixture{db: f.db, schema: f.schema, tenant: insertTenant(t, f.db, "victim-tenant")}

	secret := []byte("victim tenant's special-category content")
	victimRec := victim.mustPut(t, victim.putRequest(secret))

	neverWritten := sha256Hex([]byte("nobody, anywhere, has ever put these bytes"))

	t.Run("retrieving another tenant's real content id and a guessed one deny identically", func(t *testing.T) {
		_, _, crossErr := f.retrieve(victimRec.ContentID, allowAll("attacker"), fixedNow)
		_, _, guessErr := f.retrieve(neverWritten, allowAll("attacker"), fixedNow)

		var crossDenied, guessDenied artifacts.ErrRetrievalDenied
		if !errors.As(crossErr, &crossDenied) {
			t.Fatalf("cross-tenant retrieval returned %v, want ErrRetrievalDenied", crossErr)
		}
		if !errors.As(guessErr, &guessDenied) {
			t.Fatalf("guessed-digest retrieval returned %v, want ErrRetrievalDenied", guessErr)
		}
		if crossDenied.Reason != guessDenied.Reason {
			t.Fatalf("denial reasons differ: cross-tenant %q vs guessed %q (an oracle)", crossDenied.Reason, guessDenied.Reason)
		}
		if crossDenied.Reason != "no such artifact" {
			t.Fatalf("denial reason %q, want %q", crossDenied.Reason, "no such artifact")
		}
	})

	t.Run("both denials are recorded as durable refusal evidence, symmetrically", func(t *testing.T) {
		beforeCross := refusalCount(t, f, victimRec.ContentID)
		beforeGuess := refusalCount(t, f, neverWritten)

		if _, _, err := f.retrieve(victimRec.ContentID, allowAll("attacker-2"), fixedNow); err == nil {
			t.Fatal("a second cross-tenant retrieval unexpectedly succeeded")
		}
		if _, _, err := f.retrieve(neverWritten, allowAll("attacker-2"), fixedNow); err == nil {
			t.Fatal("a second guessed-digest retrieval unexpectedly succeeded")
		}

		if got := refusalCount(t, f, victimRec.ContentID); got != beforeCross+1 {
			t.Fatalf("cross-tenant refusal count %d, want %d", got, beforeCross+1)
		}
		if got := refusalCount(t, f, neverWritten); got != beforeGuess+1 {
			t.Fatalf("guessed-digest refusal count %d, want %d", got, beforeGuess+1)
		}
	})

	t.Run("Read (no-authorization lookup) also reports the same not-found shape either way", func(t *testing.T) {
		_, crossErr := artifactsRead(f, victimRec.ContentID)
		_, guessErr := artifactsRead(f, neverWritten)

		var crossNotFound, guessNotFound artifacts.ErrNotFound
		if !errors.As(crossErr, &crossNotFound) {
			t.Fatalf("cross-tenant Read returned %v, want ErrNotFound", crossErr)
		}
		if !errors.As(guessErr, &guessNotFound) {
			t.Fatalf("guessed-digest Read returned %v, want ErrNotFound", guessErr)
		}
	})
}

// TestTodo_DATA_019_Race drives many tenants concurrently, each Put-ing the
// exact same byte-identical sensitive content on its own connection and its
// own transaction, and proves the ON CONFLICT (tenant_id, content_id)
// idempotency in internal/data/artifacts.PutStream never collapses two
// different tenants' writes into one row: every tenant reports created=true
// exactly once for its own first write, and the final row count is exactly
// one per tenant, not one shared row. This machine builds windows/arm64
// without -race, so the proof is behavioral (each goroutine's own reported
// outcome, plus the rows actually on file) rather than relying on the race
// detector.
func TestTodo_DATA_019_Race(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ctx := context.Background()

	const tenantCount = 10
	content := []byte("concurrently written, byte-identical across every tenant")
	wantID := sha256Hex(content)

	tenants := make([]fixture, tenantCount)
	for i := range tenants {
		tenants[i] = fixture{
			db:     f.db,
			schema: f.schema,
			tenant: insertTenant(t, f.db, "race-dedup-tenant-"+strconv.Itoa(i)),
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, tenantCount)
	for _, tf := range tenants {
		wg.Add(1)
		// Each goroutine drives its own connection and its own transaction --
		// the same discipline internal/data/tenancy's own
		// TestTodo_DB_017_Race uses -- since a single pgx connection is not
		// safe for concurrent use from multiple goroutines.
		go func(tf fixture) {
			defer wg.Done()
			conn := tf.db.NewConn(t)
			tx, err := conn.Begin(ctx)
			if err != nil {
				errs <- fmt.Errorf("tenant %s begin: %w", tf.tenant, err)
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()

			_, created, err := artifacts.Put(ctx, tx, tf.schema, tf.putRequest(content))
			if err != nil {
				errs <- fmt.Errorf("tenant %s put: %w", tf.tenant, err)
				return
			}
			if !created {
				errs <- fmt.Errorf("tenant %s reported created=false on its own first put (cross-tenant dedup leak under concurrency)", tf.tenant)
				return
			}
			if err := tx.Commit(ctx); err != nil {
				errs <- fmt.Errorf("tenant %s commit: %w", tf.tenant, err)
			}
		}(tf)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	for _, tf := range tenants {
		rec, err := artifactsRead(tf, wantID)
		if err != nil {
			t.Fatalf("tenant %s cannot read its own row after the race: %v", tf.tenant, err)
		}
		if rec.Tenant != tf.tenant {
			t.Fatalf("tenant %s read back a row for tenant %s", tf.tenant, rec.Tenant)
		}
	}
}
