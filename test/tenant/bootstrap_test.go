// Package tenant_test is the cross-package pilot tenant bootstrap
// conformance suite (definitions/architecture/repository-layout.yaml, root
// "test").
//
// TENANT-002, in the reduced slice this lane implements, is a composition of
// two independently owned pieces: internal/intent/app/pgstore.Store.Bootstrap
// registers the tenant's own row (idempotent by tenant id, `ON CONFLICT
// (tenant_id) DO NOTHING`), and internal/data/tenancy.Bootstrap layers a
// revision-guarded manifest ledger on top of it (idempotent by manifest
// identity, revision and content digest; TENANT-002's own addition). Neither
// package's own test suite proves the two compose correctly against the same
// live tenant, which is exactly what this suite exists to do -- the same
// reason test/bootstrap exists instead of trusting each package's unit tests
// alone.
package tenant_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const testCellID = "cell-tenant-bootstrap-test"

func pilotManifest(rev uint64, mutate ...func(*tenant.BootstrapManifest)) tenant.BootstrapManifest {
	m := tenant.BootstrapManifest{
		ManifestID:       "onboard:pilot-partner",
		Tenant:           "pilot-partner",
		Cell:             testCellID,
		Region:           "us-east",
		ResidencyProfile: "us-standard",
		IsolationTier:    "SHARED",
		Revision:         rev,
		OwnerRef:         "person:design-partner-owner",
		CreatedAt:        time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC),
		CreatedBy:        "person:design-partner-owner",
	}
	for _, f := range mutate {
		f(&m)
	}
	return m
}

// bootstrapPilot composes both halves of a pilot tenant bootstrap: the
// tenant row itself (pgstore.Store.Bootstrap, owned by the intent-and-
// capability lane and not edited here) plus this lane's manifest ledger
// (tenancy.Bootstrap), inside one committed transaction so the two either
// both land or neither does.
func bootstrapPilot(t *testing.T, store *pgstore.Store, db *pgtest.DB, manifest tenant.BootstrapManifest) tenant.BootstrapOutcome {
	t.Helper()
	ctx := context.Background()

	if err := store.Bootstrap(ctx, manifest.Tenant); err != nil {
		t.Fatalf("pgstore bootstrap of tenant %q: %v", manifest.Tenant, err)
	}

	tenantID := pgstore.TenantID(manifest.Tenant)
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	outcome, err := tenancy.Bootstrap(ctx, tx, tenantID, manifest)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("tenancy bootstrap of tenant %q: %v", manifest.Tenant, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return outcome
}

// TestPilotTenantBootstrapsIdempotentlyFromAManifest is supplemental
// end-to-end evidence for TENANT-002 (not itself a named TEST MATRIX kind):
// it proves the composed bootstrap -- pgstore's tenant row plus this lane's
// manifest ledger -- is idempotent as a whole for one pilot tenant, and that
// a manifest change without a declared revision bump is refused even after
// the tenant row itself already exists and is ACTIVE.
func TestPilotTenantBootstrapsIdempotentlyFromAManifest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	store, err := pgstore.New(db.Conn, pgstore.WithCellID(testCellID))
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}

	m1 := pilotManifest(1)
	tenantID := pgstore.TenantID(m1.Tenant)

	first := bootstrapPilot(t, store, db, m1)
	if first.Decision != tenant.BootstrapApply {
		t.Fatalf("first bootstrap decision %s, want APPLY", first.Decision)
	}

	t.Run("the tenant row is registered and ACTIVE", func(t *testing.T) {
		var status string
		if err := db.Conn.QueryRow(ctx, `SELECT status FROM tenant WHERE tenant_id = $1`, tenantID).Scan(&status); err != nil {
			t.Fatalf("read tenant row: %v", err)
		}
		if status != "ACTIVE" {
			t.Fatalf("tenant status %q, want ACTIVE", status)
		}
	})

	t.Run("evidence receipts name the applied manifest", func(t *testing.T) {
		receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
		if err != nil {
			t.Fatalf("list receipts: %v", err)
		}
		if len(receipts) != 1 {
			t.Fatalf("%d bootstrap receipts, want 1", len(receipts))
		}
		if receipts[0].ManifestID != m1.ManifestID || receipts[0].Revision != 1 {
			t.Fatalf("receipt %+v does not name manifest %s revision 1", receipts[0], m1.ManifestID)
		}
	})

	t.Run("the whole composed bootstrap is a no-op on replay", func(t *testing.T) {
		replay := bootstrapPilot(t, store, db, m1)
		if replay.Decision != tenant.BootstrapNoop {
			t.Fatalf("replay decision %s, want NOOP", replay.Decision)
		}
		receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
		if err != nil {
			t.Fatalf("list receipts: %v", err)
		}
		if len(receipts) != 1 {
			t.Fatalf("%d bootstrap receipts after a replay, want 1 (still)", len(receipts))
		}
		var tenantRows int
		if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM tenant WHERE tenant_id = $1`, tenantID).Scan(&tenantRows); err != nil {
			t.Fatalf("count tenant rows: %v", err)
		}
		if tenantRows != 1 {
			t.Fatalf("%d tenant rows after a replay, want 1 (still)", tenantRows)
		}
	})

	t.Run("a manifest change without a new revision is refused after the tenant already exists", func(t *testing.T) {
		changed := pilotManifest(1, func(m *tenant.BootstrapManifest) { m.Region = "eu-west" })

		// pgstore's own half is unaffected either way (it never inspects
		// manifest content), so only the ledger half is exercised directly
		// here to isolate the refusal to what actually refuses it.
		tx, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		_, bootErr := tenancy.Bootstrap(ctx, tx, tenantID, changed)
		_ = tx.Rollback(ctx)
		if bootErr == nil {
			t.Fatal("a same-revision content change was accepted after the tenant already existed")
		}
		if !errors.Is(bootErr, tenant.ErrBootstrapRejected) {
			t.Fatalf("error %v, want ErrBootstrapRejected", bootErr)
		}

		receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
		if err != nil {
			t.Fatalf("list receipts: %v", err)
		}
		if len(receipts) != 1 {
			t.Fatalf("%d bootstrap receipts after a rejected apply, want 1 (unchanged)", len(receipts))
		}
	})

	t.Run("a declared new revision applies and the tenant row is still exactly one row", func(t *testing.T) {
		m2 := pilotManifest(2, func(m *tenant.BootstrapManifest) { m.Region = "eu-west" })
		second := bootstrapPilot(t, store, db, m2)
		if second.Decision != tenant.BootstrapApply {
			t.Fatalf("second bootstrap decision %s, want APPLY", second.Decision)
		}

		receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
		if err != nil {
			t.Fatalf("list receipts: %v", err)
		}
		if len(receipts) != 2 {
			t.Fatalf("%d bootstrap receipts after a second revision, want 2", len(receipts))
		}

		var tenantRows int
		if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM tenant WHERE tenant_id = $1`, tenantID).Scan(&tenantRows); err != nil {
			t.Fatalf("count tenant rows: %v", err)
		}
		if tenantRows != 1 {
			t.Fatalf("%d tenant rows after a second manifest revision, want 1 (a manifest revision names a new manifest state, never a new tenant)", tenantRows)
		}
	})
}

// TestPilotTenantBootstrapRejectsAnotherPilotsManifest proves that composing
// pgstore's tenant registration with this lane's manifest ledger does not
// quietly let a second, differently-identified manifest hijack an
// already-bootstrapped tenant: the ledger's ManifestID check
// (tenant.ResolveBootstrap) holds even once the tenant row is real and
// ACTIVE, not only against the in-memory fixtures the pure unit tests use.
func TestPilotTenantBootstrapRejectsAnotherPilotsManifest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	store, err := pgstore.New(db.Conn, pgstore.WithCellID(testCellID))
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}

	m1 := pilotManifest(1, func(m *tenant.BootstrapManifest) {
		m.Tenant = "second-pilot"
		m.ManifestID = "onboard:second-pilot"
	})
	tenantID := pgstore.TenantID(m1.Tenant)
	if outcome := bootstrapPilot(t, store, db, m1); outcome.Decision != tenant.BootstrapApply {
		t.Fatalf("first bootstrap decision %s, want APPLY", outcome.Decision)
	}

	hijack := pilotManifest(1, func(m *tenant.BootstrapManifest) {
		m.Tenant = "second-pilot"
		m.ManifestID = "onboard:someone-elses-manifest"
	})
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	_, bootErr := tenancy.Bootstrap(ctx, tx, tenantID, hijack)
	_ = tx.Rollback(ctx)
	if bootErr == nil {
		t.Fatal("a differently-identified manifest was accepted against an already-bootstrapped tenant")
	}
	if !errors.Is(bootErr, tenant.ErrBootstrapRejected) {
		t.Fatalf("error %v, want ErrBootstrapRejected", bootErr)
	}

	receipts, err := tenancy.ListBootstrapReceipts(ctx, db.Conn, tenantID)
	if err != nil {
		t.Fatalf("list receipts: %v", err)
	}
	if len(receipts) != 1 || receipts[0].ManifestID != m1.ManifestID {
		t.Fatalf("receipts %+v changed after a rejected hijack attempt", receipts)
	}
}
