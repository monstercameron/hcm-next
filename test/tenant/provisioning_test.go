package tenant_test

// This file is the pgtest-backed composition TENANT-002's GREEN clause
// still needed: a tenant.ProvisioningRun driven by real, repository-backed
// tenant.PlaneVerifier implementations (internal/data/tenancy's
// IdentityVerifier, KeysVerifier, PoliciesVerifier, SchemaVerifier and
// HealthVerifier) for the five planes this repository can prove today, plus
// tenant.FakeVerifier standing in -- explicitly, at the call site below --
// for the five planes whose own todos (admin directory provisioning,
// signed placement, product/entitlement catalog, recovery contacts, audit
// sink) remain undelivered. ACTIVE is reachable only once every plane in
// tenant.AllPlanes independently reports VERIFIED, exactly as GREEN
// requires; the receipt ledger this file bootstraps through (bootstrapPilot,
// already defined in bootstrap_test.go) is untouched by any of this --
// provisioning composes on top of it, it does not replace it.

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	dataconfigregistry "github.com/monstercameron/hcm-next/internal/data/configregistry"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/tenant"
	"github.com/monstercameron/hcm-next/internal/intent/app/pgstore"
	platformconfig "github.com/monstercameron/hcm-next/internal/platform/configregistry"
	"github.com/monstercameron/hcm-next/internal/trust/custody"
	"github.com/monstercameron/hcm-next/internal/trust/envelope"
)

// storageDispositionPathFromTestTenant is STORE-001's registry file,
// resolved relative to this package's own directory: test/tenant is two
// path segments below the module root.
func storageDispositionPathFromTestTenant() string {
	return filepath.Join("..", "..", "definitions", "storage", "storage-disposition.yaml")
}

// appRoleConn opens a fresh connection on db's schema and assumes
// tenancy.AppRole, exactly as internal/data/tenancy's own fixtures_test.go
// helper of the same name does (unexported there, so this suite carries its
// own copy rather than reaching into another package's test-only code).
func appRoleConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// fakeCustodyProvider is the minimal in-memory custody.Provider this suite
// needs to prove the keys plane: an XOR mask round-trips faithfully and
// rejects a ciphertext bound to the wrong handle. See
// internal/data/tenancy's own copy (planeverification_test.go) for the
// identical fixture used by that package's narrower, single-plane tests;
// this is test/tenant's own copy because it is test-only fixture code, not
// something either package exports for the other to share.
type fakeCustodyProvider struct {
	mu  sync.Mutex
	seq int
}

func xorMaskBytes(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = c ^ 0x5A
	}
	return out
}

func (f *fakeCustodyProvider) receipt(h custody.Handle, op custody.Operation) custody.Receipt {
	f.mu.Lock()
	f.seq++
	id := fmt.Sprintf("fake-custody-receipt-%d", f.seq)
	f.mu.Unlock()
	return custody.Receipt{ID: id, Handle: h, Operation: op, At: time.Now().UTC()}
}

func (f *fakeCustodyProvider) Encrypt(_ custody.Context, h custody.Handle, plaintext []byte) (custody.Ciphertext, custody.Receipt, error) {
	if err := h.Validate(); err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	return custody.Ciphertext{Handle: h, Algorithm: "FAKE-XOR", Data: xorMaskBytes(plaintext)}, f.receipt(h, custody.Encrypt), nil
}

func (f *fakeCustodyProvider) Decrypt(_ custody.Context, h custody.Handle, ct custody.Ciphertext) ([]byte, custody.Receipt, error) {
	if ct.Handle != h {
		return nil, custody.Receipt{}, fmt.Errorf("fake custody: ciphertext is bound to a different handle")
	}
	return xorMaskBytes(ct.Data), f.receipt(h, custody.Decrypt), nil
}

func (f *fakeCustodyProvider) Sign(_ custody.Context, h custody.Handle, message []byte) (custody.Signature, custody.Receipt, error) {
	return custody.Signature{Handle: h, Algorithm: "FAKE-XOR", Data: xorMaskBytes(message)}, f.receipt(h, custody.Sign), nil
}

func (f *fakeCustodyProvider) Verify(_ custody.Context, h custody.Handle, message []byte, sig custody.Signature) (bool, custody.Receipt, error) {
	return string(xorMaskBytes(message)) == string(sig.Data), f.receipt(h, custody.Verify), nil
}

func (f *fakeCustodyProvider) IssueLease(_ custody.Context, h custody.Handle, op custody.Operation, ttl time.Duration) (custody.Lease, error) {
	return custody.Lease{ID: fmt.Sprintf("lease-%d", f.seq), Handle: h, Operation: op, ExpiresAt: time.Now().UTC().Add(ttl)}, nil
}

func (f *fakeCustodyProvider) RenewLease(_ custody.Context, lease custody.Lease, ttl time.Duration) (custody.Lease, error) {
	lease.ExpiresAt = time.Now().UTC().Add(ttl)
	return lease, nil
}

func (f *fakeCustodyProvider) Rotate(_ custody.Context, h custody.Handle) (custody.Handle, custody.Receipt, error) {
	h.Version = h.Version + "+1"
	return h, f.receipt(h, custody.Rotate), nil
}

func (f *fakeCustodyProvider) Revoke(_ custody.Context, h custody.Handle, _ string) (custody.Receipt, error) {
	return f.receipt(h, custody.Revoke), nil
}

var _ custody.Provider = (*fakeCustodyProvider)(nil)

// planeVerifiersFor composes the full closed set of tenant.PlaneVerifier
// this suite drives a tenant.ProvisioningRun with: real, repository-backed
// implementations for the five planes internal/data/tenancy can prove today
// (identity+RLS, keys, policies, schemas, health), and an explicit
// tenant.FakeVerifier for the five that remain -- each one named here as
// standing in for an undelivered todo, never presented as real proof.
func planeVerifiersFor(t *testing.T, db *pgtest.DB, tenantSlug string, tenantID uuid.UUID) map[tenant.Plane]tenant.PlaneVerifier {
	t.Helper()

	// Keys plane: an envelope.Manager over the fake custody provider,
	// carrying a KEK registered specifically for this tenant.
	provider := &fakeCustodyProvider{}
	root := custody.Handle{ID: "root-key", Kind: custody.Key, Version: "v1", Tenant: "*", Region: "us-east"}
	mgr, err := envelope.New(root, provider)
	if err != nil {
		t.Fatalf("envelope.New: %v", err)
	}
	kek := custody.Handle{ID: "kek-" + tenantSlug, Kind: custody.Key, Version: "v1", Tenant: tenantSlug, Region: "us-east"}
	registerCtx := custody.Context{RequestContext: custody.RequestContext{
		Workload: "test", Tenant: tenantSlug, Region: "us-east", Purpose: "register", Destination: "internal",
	}}
	if err := mgr.RegisterTenant(registerCtx, tenantSlug, kek); err != nil {
		t.Fatalf("RegisterTenant for %s: %v", tenantSlug, err)
	}

	// Policies plane: publish and activate one configuration object scoped
	// to exactly this tenant through the durable Postgres-backed registry.
	configStore := dataconfigregistry.New(appRoleConn(t, db))
	scope := platformconfig.Scope{TenantID: tenantID.String()}
	obj, err := platformconfig.Publish(configStore, platformconfig.ConfigurationObject{
		Kind: platformconfig.KindPolicy, ID: "pilot-onboarding", Revision: 1,
		Body: []byte(`{"rule":"pilot-default"}`), SchemaRef: "hcmnext.policy/v1",
		Scope: scope, PublisherPrincipal: "person:policy-owner", PublishedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Publish policy for %s: %v", tenantSlug, err)
	}
	if _, err := platformconfig.Activate(configStore, obj.Ref(), platformconfig.ActivationEvidence{
		ActivatedBy: "person:policy-owner", ActivatedAt: time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Activate policy for %s: %v", tenantSlug, err)
	}

	return map[tenant.Plane]tenant.PlaneVerifier{
		tenant.PlaneIdentity: tenancy.IdentityVerifier{TenantID: tenantID, Admin: db.Conn, AppConn: appRoleConn(t, db)},
		tenant.PlaneKeys:     tenancy.KeysVerifier{Manager: mgr, Region: "us-east"},
		tenant.PlanePolicies: tenancy.PoliciesVerifier{Store: configStore, Scope: scope, Kind: platformconfig.KindPolicy, ID: "pilot-onboarding"},
		tenant.PlaneSchemas: tenancy.SchemaVerifier{
			RegistryPath: storageDispositionPathFromTestTenant(),
			Tables:       []string{"tenant", "tenant_bootstrap_receipt"},
			Live:         db.Conn,
		},
		tenant.PlaneHealth: tenancy.HealthVerifier{TenantID: tenantID, AppConn: appRoleConn(t, db)},

		// The remaining five planes have no repository-backed proof yet --
		// each depends on its own undelivered todo (an admin directory,
		// signed placement, a product/entitlement catalog, recovery
		// contacts, an audit sink). Standing in with an explicit fake here
		// is what lets this composition demonstrate the whole
		// tenant.AllPlanes gate reaching ACTIVE without pretending any of
		// these five is real evidence.
		tenant.PlaneAdmin:            tenant.NewFakeVerifier(tenant.PlaneAdmin, "system:admin-plane-fake", "todo:admin-directory-provisioning"),
		tenant.PlanePlacement:        tenant.NewFakeVerifier(tenant.PlanePlacement, "system:placement-plane-fake", "todo:signed-placement"),
		tenant.PlaneProducts:         tenant.NewFakeVerifier(tenant.PlaneProducts, "system:products-plane-fake", "todo:product-entitlement-catalog"),
		tenant.PlaneRecoveryContacts: tenant.NewFakeVerifier(tenant.PlaneRecoveryContacts, "system:recovery-contacts-plane-fake", "todo:recovery-contacts"),
		tenant.PlaneAudit:            tenant.NewFakeVerifier(tenant.PlaneAudit, "system:audit-plane-fake", "todo:audit-sink"),
	}
}

const requesterPrincipal = "system:tenant-bootstrap-orchestrator"

// TestPilotTenantProvisioningReachesActiveAcrossAllPlanes is the
// TENANT-002 GREEN composition: once the tenant row and manifest ledger are
// bootstrapped (bootstrapPilot), a tenant.ProvisioningRun for the same
// tenant stays PENDING until every plane in tenant.AllPlanes independently
// reports VERIFIED, and reaches ACTIVE in the very call that supplies the
// last one -- never before.
func TestPilotTenantProvisioningReachesActiveAcrossAllPlanes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	store, err := pgstore.New(db.Conn, pgstore.WithCellID(testCellID))
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}
	m := pilotManifest(1, func(m *tenant.BootstrapManifest) {
		m.Tenant = "provisioning-full-pilot"
		m.ManifestID = "onboard:provisioning-full-pilot"
	})
	if outcome := bootstrapPilot(t, store, db, m); outcome.Decision != tenant.BootstrapApply {
		t.Fatalf("bootstrap decision %s, want APPLY", outcome.Decision)
	}
	tenantID := pgstore.TenantID(m.Tenant)

	verifiers := planeVerifiersFor(t, db, m.Tenant, tenantID)

	run, err := tenant.NewProvisioningRun(m.Tenant)
	if err != nil {
		t.Fatalf("NewProvisioningRun: %v", err)
	}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	for i, plane := range tenant.AllPlanes {
		verifier, ok := verifiers[plane]
		if !ok {
			t.Fatalf("no verifier composed for plane %s", plane)
		}
		outcome, err := tenant.VerifyPlane(ctx, run, verifier, requesterPrincipal, now)
		if err != nil {
			t.Fatalf("verify plane %s: %v", plane, err)
		}
		if outcome.Decision != tenant.ProvisioningApply {
			t.Fatalf("plane %s decision %s, want APPLY", plane, outcome.Decision)
		}

		isLast := i == len(tenant.AllPlanes)-1
		if outcome.Activated != isLast {
			t.Fatalf("plane %s (index %d of %d) Activated=%v, want %v", plane, i, len(tenant.AllPlanes), outcome.Activated, isLast)
		}
	}

	if run.Status() != tenant.ProvisioningActive {
		t.Fatalf("final status %s, want ACTIVE", run.Status())
	}
	if got := run.VerifiedPlanes(); len(got) != len(tenant.AllPlanes) {
		t.Fatalf("%d verified planes, want %d", len(got), len(tenant.AllPlanes))
	}

	t.Run("re-running every plane's verification is idempotent", func(t *testing.T) {
		eventsBefore := len(run.Events())
		for _, plane := range tenant.AllPlanes {
			outcome, err := tenant.VerifyPlane(ctx, run, verifiers[plane], requesterPrincipal, now.Add(time.Minute))
			if err != nil {
				t.Fatalf("re-verify plane %s: %v", plane, err)
			}
			if outcome.Decision != tenant.ProvisioningNoop {
				t.Fatalf("re-verify plane %s decision %s, want NOOP", plane, outcome.Decision)
			}
		}
		if run.Status() != tenant.ProvisioningActive {
			t.Fatalf("status after a full idempotent re-run is %s, want still ACTIVE", run.Status())
		}
		if got := len(run.Events()); got != eventsBefore {
			t.Fatalf("%d events after an idempotent re-run, want %d (unchanged)", got, eventsBefore)
		}
	})

	t.Run("a later health-plane failure degrades the tenant by name", func(t *testing.T) {
		ev, err := run.RecordFailure(tenant.PlaneHealth, "governed read timed out during a routine check", now.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("RecordFailure: %v", err)
		}
		if ev.Digest() == "" {
			t.Fatal("the degradation event carries no digest")
		}
		if run.Status() != tenant.ProvisioningDegraded {
			t.Fatalf("status after a plane failure is %s, want DEGRADED", run.Status())
		}
		plane, _, ok := run.DegradedPlane()
		if !ok || plane != tenant.PlaneHealth {
			t.Fatalf("DegradedPlane returned (%s, %v), want (HEALTH, true)", plane, ok)
		}

		// Repair: re-running the real HealthVerifier (the governed read
		// still succeeds; nothing on the database side actually broke)
		// clears the degradation.
		outcome, err := tenant.VerifyPlane(ctx, run, verifiers[tenant.PlaneHealth], requesterPrincipal, now.Add(3*time.Hour))
		if err != nil {
			t.Fatalf("repair verify plane HEALTH: %v", err)
		}
		if outcome.Decision != tenant.ProvisioningApply {
			t.Fatalf("repair decision %s, want APPLY", outcome.Decision)
		}
		if run.Status() != tenant.ProvisioningActive {
			t.Fatalf("status after repair is %s, want ACTIVE again", run.Status())
		}
	})
}

// TestPilotTenantProvisioningNeverSeesAnotherTenantsReceipts is the
// composition-level proof that provisioning two pilot tenants side by side
// never lets one see the other's evidence: the identity plane's own row
// level security check and the policies plane's config-object resolution
// are both genuinely scoped per tenant, not merely per manifest.
func TestPilotTenantProvisioningNeverSeesAnotherTenantsReceipts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	store, err := pgstore.New(db.Conn, pgstore.WithCellID(testCellID))
	if err != nil {
		t.Fatalf("pgstore.New: %v", err)
	}

	first := pilotManifest(1, func(m *tenant.BootstrapManifest) {
		m.Tenant = "provisioning-isolation-first"
		m.ManifestID = "onboard:provisioning-isolation-first"
	})
	second := pilotManifest(1, func(m *tenant.BootstrapManifest) {
		m.Tenant = "provisioning-isolation-second"
		m.ManifestID = "onboard:provisioning-isolation-second"
	})
	if outcome := bootstrapPilot(t, store, db, first); outcome.Decision != tenant.BootstrapApply {
		t.Fatalf("bootstrap %s decision %s, want APPLY", first.Tenant, outcome.Decision)
	}
	if outcome := bootstrapPilot(t, store, db, second); outcome.Decision != tenant.BootstrapApply {
		t.Fatalf("bootstrap %s decision %s, want APPLY", second.Tenant, outcome.Decision)
	}

	firstID := pgstore.TenantID(first.Tenant)
	secondID := pgstore.TenantID(second.Tenant)

	// Only the first tenant gets the full plane-verifier composition
	// (which, as a side effect, publishes and activates its own
	// "pilot-onboarding" policy). The second tenant deliberately gets
	// nothing published for that same id: a genuine cross-tenant leak
	// through the policies plane would show up as that id resolving
	// anyway when asked about the second tenant's own scope.
	firstVerifiers := planeVerifiersFor(t, db, first.Tenant, firstID)

	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

	// Each tenant's identity plane must see only its own bootstrap
	// receipt, never the other's, even though both are provisioned in the
	// same process against the same schema at the same time.
	firstIdentity, err := firstVerifiers[tenant.PlaneIdentity].Verify(ctx, tenant.VerificationRequest{Tenant: first.Tenant, RequesterPrincipal: requesterPrincipal, Now: now})
	if err != nil {
		t.Fatalf("verify identity plane for %s: %v", first.Tenant, err)
	}
	secondIdentityVerifier := tenancy.IdentityVerifier{TenantID: secondID, Admin: db.Conn, AppConn: appRoleConn(t, db)}
	secondIdentity, err := secondIdentityVerifier.Verify(ctx, tenant.VerificationRequest{Tenant: second.Tenant, RequesterPrincipal: requesterPrincipal, Now: now})
	if err != nil {
		t.Fatalf("verify identity plane for %s: %v", second.Tenant, err)
	}
	if firstIdentity.Tenant == secondIdentity.Tenant {
		t.Fatal("both tenants' identity plane evidence names the same tenant")
	}

	// A policy published and activated only for the first tenant must not
	// resolve when the policies plane is asked about the second tenant's
	// own scope -- even reusing the exact same Store instance (Store is a
	// stateless wrapper; nothing about it is pinned to one tenant) and the
	// exact same (kind, id).
	crossTenant := tenancy.PoliciesVerifier{
		Store: firstVerifiers[tenant.PlanePolicies].(tenancy.PoliciesVerifier).Store,
		Scope: platformconfig.Scope{TenantID: secondID.String()},
		Kind:  platformconfig.KindPolicy,
		ID:    "pilot-onboarding",
	}
	if _, err := crossTenant.Verify(ctx, tenant.VerificationRequest{Tenant: second.Tenant, RequesterPrincipal: requesterPrincipal, Now: now}); err == nil {
		t.Fatal("the second tenant's policies plane resolved the first tenant's activated policy")
	}

	// Two independent tenant.ProvisioningRun values for these tenants never
	// share state -- verifying one never marks the other's plane VERIFIED.
	runA, err := tenant.NewProvisioningRun(first.Tenant)
	if err != nil {
		t.Fatalf("NewProvisioningRun: %v", err)
	}
	runB, err := tenant.NewProvisioningRun(second.Tenant)
	if err != nil {
		t.Fatalf("NewProvisioningRun: %v", err)
	}
	if _, err := tenant.VerifyPlane(ctx, runA, firstVerifiers[tenant.PlaneIdentity], requesterPrincipal, now); err != nil {
		t.Fatalf("verify runA identity: %v", err)
	}
	if _, ok := runB.Verified(tenant.PlaneIdentity); ok {
		t.Fatal("verifying tenant A's identity plane also marked tenant B's run verified")
	}
}
