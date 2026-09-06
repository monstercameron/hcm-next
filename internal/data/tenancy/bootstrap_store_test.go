package tenancy_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/tenant"
)

// bootstrapManifest returns a well-formed manifest for tenantSlug, revision
// rev, mutated by fn (if given) before its digest is fixed by the caller.
func bootstrapManifest(tenantSlug string, rev uint64, fn ...func(*tenant.BootstrapManifest)) tenant.BootstrapManifest {
	m := tenant.BootstrapManifest{
		ManifestID:       "onboard:" + tenantSlug,
		Tenant:           tenantSlug,
		Cell:             "cell-local",
		Region:           "us-east",
		ResidencyProfile: "us-standard",
		IsolationTier:    "SHARED",
		Revision:         rev,
		OwnerRef:         "person:cs-lead",
		CreatedAt:        time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:        "person:cs-lead",
	}
	for _, f := range fn {
		f(&m)
	}
	return m
}

// bootstrapInTx runs tenancy.Bootstrap in its own committed transaction on
// the admin connection, the same elevated identity
// internal/intent/app/pgstore.Store.Bootstrap registers the tenant row
// itself as (see that package's own "Adoption path" note in
// migrations/00008_tenant_isolation.sql).
func bootstrapInTx(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, m tenant.BootstrapManifest) (tenant.BootstrapOutcome, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	outcome, bootErr := tenancy.Bootstrap(ctx, tx, tenantID, m)
	if bootErr != nil {
		_ = tx.Rollback(ctx)
		return outcome, bootErr
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return outcome, nil
}

// TestTodo_TENANT_002_Integration proves internal/data/tenancy.Bootstrap end
// to end against embedded PostgreSQL: a first apply persists exactly one
// receipt, a replay of the identical manifest persists nothing more, a
// same-revision content change is refused and persists nothing, and a new
// revision persists a second receipt -- the full evidence trail
// ListBootstrapReceipts returns.
func TestTodo_TENANT_002_Integration(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := insertTenant(t, db, "acme-pilot")

	m1 := bootstrapManifest("acme-pilot", 1)

	outcome, err := bootstrapInTx(t, db, tenantID, m1)
	if err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if outcome.Decision != tenant.BootstrapApply {
		t.Fatalf("first bootstrap decision %s, want APPLY", outcome.Decision)
	}

	receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
	if err != nil {
		t.Fatalf("list receipts: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("%d receipts after the first bootstrap, want 1", len(receipts))
	}
	if receipts[0].Revision != 1 || receipts[0].Digest != outcome.Digest {
		t.Fatalf("receipt %+v does not match the applied outcome %+v", receipts[0], outcome)
	}

	t.Run("a replay of the identical manifest writes nothing new", func(t *testing.T) {
		again, err := bootstrapInTx(t, db, tenantID, m1)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if again.Decision != tenant.BootstrapNoop {
			t.Fatalf("replay decision %s, want NOOP", again.Decision)
		}
		receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
		if err != nil {
			t.Fatalf("list receipts: %v", err)
		}
		if len(receipts) != 1 {
			t.Fatalf("%d receipts after a replay, want 1 (still)", len(receipts))
		}
	})

	t.Run("a manifest change without a new revision is refused and persists nothing", func(t *testing.T) {
		changed := bootstrapManifest("acme-pilot", 1, func(m *tenant.BootstrapManifest) { m.Region = "eu-west" })
		_, err := bootstrapInTx(t, db, tenantID, changed)
		if err == nil {
			t.Fatal("a same-revision content change was accepted")
		}
		if !errors.Is(err, tenant.ErrBootstrapRejected) {
			t.Fatalf("error %v, want ErrBootstrapRejected", err)
		}
		receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
		if err != nil {
			t.Fatalf("list receipts: %v", err)
		}
		if len(receipts) != 1 {
			t.Fatalf("%d receipts after a rejected apply, want 1 (unchanged)", len(receipts))
		}
	})

	t.Run("a new revision applies and adds a second receipt", func(t *testing.T) {
		m2 := bootstrapManifest("acme-pilot", 2, func(m *tenant.BootstrapManifest) { m.Region = "eu-west" })
		outcome2, err := bootstrapInTx(t, db, tenantID, m2)
		if err != nil {
			t.Fatalf("second bootstrap: %v", err)
		}
		if outcome2.Decision != tenant.BootstrapApply {
			t.Fatalf("second bootstrap decision %s, want APPLY", outcome2.Decision)
		}
		receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
		if err != nil {
			t.Fatalf("list receipts: %v", err)
		}
		if len(receipts) != 2 {
			t.Fatalf("%d receipts after a second revision, want 2", len(receipts))
		}
		if receipts[0].Revision != 1 || receipts[1].Revision != 2 {
			t.Fatalf("receipts are not ordered oldest-first by revision: %+v", receipts)
		}
	})
}

// TestTodo_TENANT_002_Fault proves resumability: a bootstrap attempt that is
// interrupted before it commits (rolled back, exactly as a crashed process or
// a caller that aborts leaves things) records no partial evidence, so a
// retry of the identical manifest still resolves as a first-time APPLY --
// never a false NOOP, and never a spurious REJECTED against a receipt that
// was never actually committed.
func TestTodo_TENANT_002_Fault(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := insertTenant(t, db, "acme-fault")
	m1 := bootstrapManifest("acme-fault", 1)

	t.Run("an interrupted first attempt leaves no receipt", func(t *testing.T) {
		tx, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		outcome, err := tenancy.Bootstrap(ctx, tx, tenantID, m1)
		if err != nil {
			t.Fatalf("bootstrap: %v", err)
		}
		if outcome.Decision != tenant.BootstrapApply {
			t.Fatalf("decision %s, want APPLY", outcome.Decision)
		}
		// Simulate a crash/abort before commit: the caller never gets to
		// call Commit.
		if err := tx.Rollback(ctx); err != nil {
			t.Fatalf("rollback: %v", err)
		}

		receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
		if err != nil {
			t.Fatalf("list receipts: %v", err)
		}
		if len(receipts) != 0 {
			t.Fatalf("%d receipts survived a rolled-back transaction, want 0", len(receipts))
		}
	})

	t.Run("a retry after the interruption applies cleanly, exactly once", func(t *testing.T) {
		outcome, err := bootstrapInTx(t, db, tenantID, m1)
		if err != nil {
			t.Fatalf("retry: %v", err)
		}
		if outcome.Decision != tenant.BootstrapApply {
			t.Fatalf("retry decision %s, want APPLY (the failed attempt left nothing behind)", outcome.Decision)
		}
		receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
		if err != nil {
			t.Fatalf("list receipts: %v", err)
		}
		if len(receipts) != 1 {
			t.Fatalf("%d receipts after the retry, want exactly 1", len(receipts))
		}

		// A further replay is now a true no-op.
		replay, err := bootstrapInTx(t, db, tenantID, m1)
		if err != nil {
			t.Fatalf("replay after retry: %v", err)
		}
		if replay.Decision != tenant.BootstrapNoop {
			t.Fatalf("replay decision %s, want NOOP", replay.Decision)
		}
	})
}

// TestTodo_TENANT_002_Race drives many concurrent bootstrap attempts for the
// identical manifest, each on its own connection and its own transaction, and
// proves the ON CONFLICT DO NOTHING plus re-read path in
// internal/data/tenancy.Bootstrap collapses them all to exactly one applied
// receipt: every caller reports either APPLY (exactly one of them) or NOOP
// (every other one), never an error and never a duplicate row. This machine
// builds windows/arm64 without -race, so the proof is behavioral (every
// goroutine's own reported outcome, plus the one row actually on file)
// rather than relying on the race detector.
func TestTodo_TENANT_002_Race(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	ctx := context.Background()
	tenantID := insertTenant(t, db, "acme-race")
	m1 := bootstrapManifest("acme-race", 1)

	const attempts = 12
	type result struct {
		outcome tenant.BootstrapOutcome
		err     error
	}
	results := make(chan result, attempts)
	var wg sync.WaitGroup

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := db.NewConn(t)
			tx, err := conn.Begin(ctx)
			if err != nil {
				results <- result{err: fmt.Errorf("begin: %w", err)}
				return
			}
			outcome, bootErr := tenancy.Bootstrap(ctx, tx, tenantID, m1)
			if bootErr != nil {
				_ = tx.Rollback(ctx)
				results <- result{outcome: outcome, err: bootErr}
				return
			}
			if err := tx.Commit(ctx); err != nil {
				results <- result{err: fmt.Errorf("commit: %w", err)}
				return
			}
			results <- result{outcome: outcome}
		}()
	}
	wg.Wait()
	close(results)

	var applied, noop int
	for r := range results {
		if r.err != nil {
			t.Fatalf("concurrent bootstrap of the identical manifest failed: %v", r.err)
		}
		switch r.outcome.Decision {
		case tenant.BootstrapApply:
			applied++
		case tenant.BootstrapNoop:
			noop++
		default:
			t.Fatalf("unexpected decision %s under concurrency", r.outcome.Decision)
		}
	}
	if applied != 1 {
		t.Fatalf("%d goroutines reported APPLY, want exactly 1", applied)
	}
	if noop != attempts-1 {
		t.Fatalf("%d goroutines reported NOOP, want %d", noop, attempts-1)
	}

	receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
	if err != nil {
		t.Fatalf("list receipts: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("%d receipts on file after the race, want exactly 1", len(receipts))
	}
}

// TestTodo_TENANT_002_Security proves the bootstrap ledger's row level
// security holds the same way every other tenant-scoped table's does
// (DB-017): a connection scoped to a foreign tenant sees none of another
// tenant's bootstrap receipts, by row count as well as by explicit key,
// and it cannot mutate an already-applied receipt.
func TestTodo_TENANT_002_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	tenantA := insertTenant(t, db, "tenant-a-bootstrap")
	tenantB := insertTenant(t, db, "tenant-b-bootstrap")

	if _, err := bootstrapInTx(t, db, tenantA, bootstrapManifest("tenant-a-bootstrap", 1)); err != nil {
		t.Fatalf("bootstrap tenant A: %v", err)
	}

	app := appRoleConn(t, db)

	t.Run("a connection scoped to a foreign tenant sees no rows", func(t *testing.T) {
		txB := scopedTx(t, ctx, app, tenantB)
		defer func() { _ = txB.Rollback(ctx) }()

		var n int
		if err := txB.QueryRow(ctx, `SELECT count(*) FROM tenant_bootstrap_receipt`).Scan(&n); err != nil {
			t.Fatalf("count as tenant B: %v", err)
		}
		if n != 0 {
			t.Fatalf("tenant B's transaction saw %d bootstrap receipts, want 0", n)
		}

		// Naming tenant A's manifest by exact key must read as empty, not an
		// error and not the row.
		var count int
		if err := txB.QueryRow(ctx, `
			SELECT count(*) FROM tenant_bootstrap_receipt
			WHERE tenant_id = $1 AND manifest_id = 'onboard:tenant-a-bootstrap'`, tenantA).Scan(&count); err != nil {
			t.Fatalf("query tenant A's receipt by key: %v", err)
		}
		if count != 0 {
			t.Fatal("tenant B's transaction found tenant A's bootstrap receipt by explicit key")
		}
	})

	t.Run("hcmnext_app cannot mutate an applied receipt", func(t *testing.T) {
		txA := scopedTx(t, ctx, app, tenantA)
		defer func() { _ = txA.Rollback(ctx) }()

		if _, err := txA.Exec(ctx, `UPDATE tenant_bootstrap_receipt SET digest = 'forged' WHERE tenant_id = $1`, tenantA); err == nil {
			t.Fatal("hcmnext_app updated an applied bootstrap receipt; the table must be append-only")
		}
		if _, err := txA.Exec(ctx, `DELETE FROM tenant_bootstrap_receipt WHERE tenant_id = $1`, tenantA); err == nil {
			t.Fatal("hcmnext_app deleted an applied bootstrap receipt; the table must be append-only")
		}
	})
}
