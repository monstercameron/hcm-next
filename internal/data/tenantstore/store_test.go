package tenantstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenantstore"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/tenant/govauth"
)

var fixedAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type fixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
	key    [32]byte
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, "tenant-"+id.String(), "tenant "+id.String())
	return fixture{db: db, tenant: id, key: [32]byte{9, 8, 7}}
}

func appConnection(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func transaction(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID) dbport.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("scope tenant: %v", err)
	}
	return tx
}

func commit(t *testing.T, tx dbport.Tx) {
	t.Helper()
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func placement(t *testing.T, f fixture, epoch uint64) tenant.Placement {
	t.Helper()
	p, err := tenant.Sign(tenant.Placement{
		Tenant: f.tenant.String(), Cell: "cell-a", Region: "us-east",
		ResidencyProfile: "US", IsolationTier: "dedicated", Epoch: epoch,
	}, f.key)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func profile(t *testing.T, f fixture, revision uint64) govauth.GovernmentAuthorizationProfile {
	t.Helper()
	p, err := govauth.NewProfile(govauth.GovernmentAuthorizationProfile{
		SchemaVersion: 1, Revision: revision, TenantID: f.tenant.String(), IntegrationID: "integration-a",
		ApplicablePrograms: []govauth.Program{govauth.ProgramGovRAMP}, SystemBoundary: "platform",
		AssessorStatus: govauth.AssessorPending, ReviewDate: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func event(f fixture) tenant.ProvisioningEvent {
	return tenant.ProvisioningEvent{
		Kind: tenant.EventPlaneVerified, Tenant: f.tenant.String(), Plane: tenant.PlanePlacement,
		VerifierPrincipal: "verifier", At: fixedAt,
	}
}

func TestTodo_PERSIST_TENANT_001(t *testing.T) {
	f := newFixture(t)
	conn := appConnection(t, f.db)
	ctx := context.Background()
	tx := transaction(t, conn, f.tenant)
	store := tenantstore.New(tx)
	if err := store.SavePlacement(ctx, f.tenant.String(), placement(t, f, 1), 0); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendProvisioningEvent(ctx, f.tenant.String(), event(f), 1); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGovernmentAuthorizationProfile(ctx, f.tenant.String(), profile(t, f, 1)); err != nil {
		t.Fatal(err)
	}
	commit(t, tx)
}

func TestTodo_PERSIST_TENANT_001_Integration(t *testing.T) {
	f := newFixture(t)
	conn := appConnection(t, f.db)
	tx := transaction(t, conn, f.tenant)
	store := tenantstore.New(tx)
	ctx := context.Background()
	if err := store.SavePlacement(ctx, f.tenant.String(), placement(t, f, 1), 0); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendProvisioningEvent(ctx, f.tenant.String(), event(f), 1); err != nil {
		t.Fatal(err)
	}
	profile := profile(t, f, 1)
	if err := store.SaveGovernmentAuthorizationProfile(ctx, f.tenant.String(), profile); err != nil {
		t.Fatal(err)
	}
	loadedPlacement, err := store.LoadPlacement(ctx, f.tenant.String())
	if err != nil || loadedPlacement.Epoch != 1 {
		t.Fatalf("placement = %+v, err = %v", loadedPlacement, err)
	}
	events, err := store.ListProvisioningEvents(ctx, f.tenant.String())
	if err != nil || len(events) != 1 || events[0].Sequence != 1 {
		t.Fatalf("events = %+v, err = %v", events, err)
	}
	loadedProfile, err := store.LoadGovernmentAuthorizationProfile(ctx, f.tenant.String(), "integration-a", 1)
	if err != nil || loadedProfile.RevisionDigest != profile.RevisionDigest {
		t.Fatalf("profile = %+v, err = %v", loadedProfile, err)
	}
	commit(t, tx)
}

func TestTodo_PERSIST_TENANT_001_Fault(t *testing.T) {
	f := newFixture(t)
	conn := appConnection(t, f.db)
	ctx := context.Background()
	tx := transaction(t, conn, f.tenant)
	store := tenantstore.New(tx)
	if err := store.SavePlacement(ctx, f.tenant.String(), placement(t, f, 2), 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlacement(ctx, f.tenant.String(), placement(t, f, 1), 2); !errors.Is(err, tenantstore.ErrStaleCAS) {
		t.Fatalf("stale placement error = %v, want ErrStaleCAS", err)
	}
	if err := store.SaveGovernmentAuthorizationProfile(ctx, f.tenant.String(), profile(t, f, 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGovernmentAuthorizationProfile(ctx, f.tenant.String(), profile(t, f, 1)); !errors.Is(err, tenantstore.ErrDuplicate) {
		t.Fatalf("duplicate profile error = %v, want ErrDuplicate", err)
	}
	if err := store.AppendProvisioningEvent(ctx, f.tenant.String(), event(f), 1); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendProvisioningEvent(ctx, f.tenant.String(), event(f), 1); !errors.Is(err, tenantstore.ErrDuplicate) {
		t.Fatalf("duplicate event error = %v, want ErrDuplicate", err)
	}
	_ = tx.Rollback(ctx)
}

func TestTodo_PERSIST_TENANT_001_Security(t *testing.T) {
	f := newFixture(t)
	other := newFixture(t)
	conn := appConnection(t, f.db)
	tx := transaction(t, conn, f.tenant)
	store := tenantstore.New(tx)
	if err := store.SavePlacement(context.Background(), f.tenant.String(), placement(t, f, 1), 0); err != nil {
		t.Fatal(err)
	}
	commit(t, tx)
	otherConn := appConnection(t, f.db)
	otherTx := transaction(t, otherConn, other.tenant)
	defer otherTx.Rollback(context.Background())
	otherStore := tenantstore.New(otherTx)
	if _, err := otherStore.LoadPlacement(context.Background(), f.tenant.String()); !errors.Is(err, tenantstore.ErrNotFound) {
		t.Fatalf("cross-tenant placement error = %v, want ErrNotFound", err)
	}
	var count int
	if err := otherTx.QueryRow(context.Background(), `SELECT count(*) FROM tenant_placement`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("cross-tenant visible placement rows = %d, want 0", count)
	}
}

func TestTodo_PERSIST_TENANT_001_Recovery(t *testing.T) {
	f := newFixture(t)
	conn := appConnection(t, f.db)
	tx := transaction(t, conn, f.tenant)
	store := tenantstore.New(tx)
	if err := store.SavePlacement(context.Background(), f.tenant.String(), placement(t, f, 1), 0); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendProvisioningEvent(context.Background(), f.tenant.String(), event(f), 1); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGovernmentAuthorizationProfile(context.Background(), f.tenant.String(), profile(t, f, 1)); err != nil {
		t.Fatal(err)
	}
	commit(t, tx)
	fresh := appConnection(t, f.db)
	freshTx := transaction(t, fresh, f.tenant)
	defer freshTx.Rollback(context.Background())
	freshStore := tenantstore.New(freshTx)
	if _, err := freshStore.LoadPlacement(context.Background(), f.tenant.String()); err != nil {
		t.Fatal(err)
	}
	if rows, err := freshStore.ListProvisioningEvents(context.Background(), f.tenant.String()); err != nil || len(rows) != 1 {
		t.Fatalf("reloaded events = %+v, err = %v", rows, err)
	}
	if _, err := freshStore.LatestGovernmentAuthorizationProfile(context.Background(), f.tenant.String(), "integration-a"); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PERSIST_TENANT_001_Mutation(t *testing.T) {
	f := newFixture(t)
	conn := appConnection(t, f.db)
	tx := transaction(t, conn, f.tenant)
	store := tenantstore.New(tx)
	if err := store.AppendProvisioningEvent(context.Background(), f.tenant.String(), event(f), 1); err != nil {
		t.Fatal(err)
	}
	commit(t, tx)
	updateTx := transaction(t, conn, f.tenant)
	if _, err := updateTx.Exec(context.Background(), `UPDATE tenant_provisioning_event SET reason = 'changed' WHERE tenant_id = $1 AND event_sequence = 1`, f.tenant); err == nil {
		t.Fatal("append-only event UPDATE succeeded")
	}
	_ = updateTx.Rollback(context.Background())
	deleteTx := transaction(t, conn, f.tenant)
	if _, err := deleteTx.Exec(context.Background(), `DELETE FROM tenant_provisioning_event WHERE tenant_id = $1 AND event_sequence = 1`, f.tenant); err == nil {
		t.Fatal("append-only event DELETE succeeded")
	}
	_ = deleteTx.Rollback(context.Background())
	placementTx := transaction(t, conn, f.tenant)
	placementStore := tenantstore.New(placementTx)
	if err := placementStore.SavePlacement(context.Background(), f.tenant.String(), placement(t, f, 1), 0); err != nil {
		t.Fatal(err)
	}
	commit(t, placementTx)
	fencedTx := transaction(t, conn, f.tenant)
	if _, err := fencedTx.Exec(context.Background(), `UPDATE tenant_placement SET epoch = 1 WHERE tenant_id = $1`, f.tenant); err == nil {
		t.Fatal("same-epoch placement UPDATE succeeded")
	}
	_ = fencedTx.Rollback(context.Background())
}
