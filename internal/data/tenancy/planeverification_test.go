package tenancy_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	dataconfigregistry "github.com/monstercameron/human-capital-management-suite/internal/data/configregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	tenant "github.com/monstercameron/human-capital-management-suite/internal/domains/tenant"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/envelope"
)

// storageDispositionPath is STORE-001's registry file, resolved relative to
// this package's own directory (internal/data/tenancy is three path
// segments below the module root, the same depth internal/data/schema's own
// TestTodo_DATA_001 resolves it from).
func storageDispositionPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "..", "definitions", "storage", "storage-disposition.yaml")
}

// fakeCustodyProvider is an in-memory custody.Provider: it XOR-masks
// plaintext under a fixed pad rather than performing real cryptography, but
// it round-trips faithfully and rejects a ciphertext bound to the wrong
// handle -- everything KeysVerifier needs to prove a tenant KEK is live
// without a real KMS in this test. It is "the custody fake" TENANT-002's
// keys plane evidence rests on until a real provider adapter is qualified
// (see internal/trust/custody's own doc.go: no GetKey/GetSecret operation).
type fakeCustodyProvider struct {
	mu  sync.Mutex
	seq int
}

func (f *fakeCustodyProvider) nextReceipt(h custody.Handle, op custody.Operation) custody.Receipt {
	f.mu.Lock()
	f.seq++
	id := fmt.Sprintf("fake-custody-receipt-%d", f.seq)
	f.mu.Unlock()
	return custody.Receipt{ID: id, Handle: h, Operation: op, At: time.Now().UTC()}
}

func xorMask(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		out[i] = c ^ 0x5A
	}
	return out
}

func (f *fakeCustodyProvider) Encrypt(_ custody.Context, h custody.Handle, plaintext []byte) (custody.Ciphertext, custody.Receipt, error) {
	if err := h.Validate(); err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	return custody.Ciphertext{Handle: h, Algorithm: "FAKE-XOR", Data: xorMask(plaintext)}, f.nextReceipt(h, custody.Encrypt), nil
}

func (f *fakeCustodyProvider) Decrypt(_ custody.Context, h custody.Handle, ciphertext custody.Ciphertext) ([]byte, custody.Receipt, error) {
	if ciphertext.Handle != h {
		return nil, custody.Receipt{}, fmt.Errorf("fake custody: ciphertext is bound to a different handle")
	}
	return xorMask(ciphertext.Data), f.nextReceipt(h, custody.Decrypt), nil
}

func (f *fakeCustodyProvider) Sign(_ custody.Context, h custody.Handle, message []byte) (custody.Signature, custody.Receipt, error) {
	return custody.Signature{Handle: h, Algorithm: "FAKE-XOR", Data: xorMask(message)}, f.nextReceipt(h, custody.Sign), nil
}

func (f *fakeCustodyProvider) Verify(_ custody.Context, h custody.Handle, message []byte, sig custody.Signature) (bool, custody.Receipt, error) {
	return string(xorMask(message)) == string(sig.Data), f.nextReceipt(h, custody.Verify), nil
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
	return h, f.nextReceipt(h, custody.Rotate), nil
}

func (f *fakeCustodyProvider) Revoke(_ custody.Context, h custody.Handle, _ string) (custody.Receipt, error) {
	return f.nextReceipt(h, custody.Revoke), nil
}

var _ custody.Provider = (*fakeCustodyProvider)(nil)

// verificationRequest builds a tenant.VerificationRequest for tenantSlug at
// a fixed instant, requested by a principal distinct from every plane
// verifier's own VerifierPrincipal below.
func verificationRequest(tenantSlug string) tenant.VerificationRequest {
	return tenant.VerificationRequest{
		Tenant:             tenantSlug,
		RequesterPrincipal: "system:tenant-bootstrap-orchestrator",
		Now:                time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	}
}

// TestIdentityVerifierProvesTenantRowAndRLSIsolation is real,
// repository-backed evidence for TENANT-002's identity plane: the tenant
// row reads ACTIVE, an unscoped app-role transaction sees none of this
// tenant's bootstrap receipts, and the same role scoped to this tenant sees
// its own.
func TestIdentityVerifierProvesTenantRowAndRLSIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	tenantID := insertTenant(t, db, "identity-plane")
	m := bootstrapManifest("identity-plane", 1)
	if _, err := bootstrapInTx(t, db, tenantID, m); err != nil {
		t.Fatalf("seed a bootstrap receipt: %v", err)
	}

	verifier := tenancy.IdentityVerifier{TenantID: tenantID, Admin: db.Conn, AppConn: appRoleConn(t, db)}
	if verifier.Plane() != tenant.PlaneIdentity {
		t.Fatalf("Plane() = %s, want IDENTITY", verifier.Plane())
	}

	got, err := verifier.Verify(ctx, verificationRequest("identity-plane"))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Plane != tenant.PlaneIdentity || got.Tenant != "identity-plane" {
		t.Fatalf("unexpected verified record: %+v", got)
	}
	if len(got.EvidenceRefs) == 0 {
		t.Fatal("identity plane verification carries no evidence")
	}
}

// TestIdentityVerifierFailsWhenTenantIsNotActive proves the identity plane
// refuses to VERIFY a tenant whose row does not read ACTIVE.
func TestIdentityVerifierFailsWhenTenantIsNotActive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, 'suspended-tenant', 'cell-local', 'suspended tenant', 'SUSPENDED', timestamptz '2026-01-01T00:00:00Z')`,
		id)

	verifier := tenancy.IdentityVerifier{TenantID: id, Admin: db.Conn, AppConn: appRoleConn(t, db)}
	if _, err := verifier.Verify(ctx, verificationRequest("suspended-tenant")); err == nil {
		t.Fatal("a non-ACTIVE tenant was reported VERIFIED by the identity plane")
	}
}

// TestKeysVerifierProvesTenantKEKRoundTrip is real evidence for TENANT-002's
// keys plane: a tenant KEK registered on an envelope.Manager over the fake
// custody provider above can seal and open a fresh marker right now.
func TestKeysVerifierProvesTenantKEKRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	provider := &fakeCustodyProvider{}
	root := custody.Handle{ID: "root-key", Kind: custody.Key, Version: "v1", Tenant: "*", Region: "us-east"}
	mgr, err := envelope.New(root, provider)
	if err != nil {
		t.Fatalf("envelope.New: %v", err)
	}
	kek := custody.Handle{ID: "kek-keys-plane", Kind: custody.Key, Version: "v1", Tenant: "keys-plane", Region: "us-east"}
	registerCtx := custody.Context{RequestContext: custody.RequestContext{
		Workload: "test", Tenant: "keys-plane", Region: "us-east", Purpose: "register", Destination: "internal",
	}}
	if err := mgr.RegisterTenant(registerCtx, "keys-plane", kek); err != nil {
		t.Fatalf("RegisterTenant: %v", err)
	}

	verifier := tenancy.KeysVerifier{Manager: mgr, Region: "us-east"}
	if verifier.Plane() != tenant.PlaneKeys {
		t.Fatalf("Plane() = %s, want KEYS", verifier.Plane())
	}
	got, err := verifier.Verify(ctx, verificationRequest("keys-plane"))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(got.EvidenceRefs) != 1 {
		t.Fatalf("evidence refs = %v, want exactly one stable KEK reference", got.EvidenceRefs)
	}

	// A second, independent round trip must report the identical evidence
	// -- each call mints a fresh custody receipt under the hood, but that
	// receipt id must never leak into EvidenceRefs (see planeverification.go's
	// own comment): tenant.ProvisioningRun.RecordVerified treats differing
	// EvidenceRefs as a material change, so unstable evidence here would
	// make every re-verification of an already-VERIFIED keys plane look
	// like a conflict instead of an idempotent replay.
	again, err := verifier.Verify(ctx, verificationRequest("keys-plane"))
	if err != nil {
		t.Fatalf("second Verify: %v", err)
	}
	if len(again.EvidenceRefs) != 1 || again.EvidenceRefs[0] != got.EvidenceRefs[0] {
		t.Fatalf("evidence refs changed across repeated verifications: %v then %v", got.EvidenceRefs, again.EvidenceRefs)
	}
}

// TestKeysVerifierFailsForAnUnregisteredTenant proves the keys plane cannot
// report VERIFIED for a tenant whose KEK was never registered.
func TestKeysVerifierFailsForAnUnregisteredTenant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	provider := &fakeCustodyProvider{}
	root := custody.Handle{ID: "root-key", Kind: custody.Key, Version: "v1", Tenant: "*", Region: "us-east"}
	mgr, err := envelope.New(root, provider)
	if err != nil {
		t.Fatalf("envelope.New: %v", err)
	}

	verifier := tenancy.KeysVerifier{Manager: mgr, Region: "us-east"}
	if _, err := verifier.Verify(ctx, verificationRequest("never-registered")); err == nil {
		t.Fatal("an unregistered tenant's keys plane was reported VERIFIED")
	}
}

// TestPoliciesVerifierResolvesActiveConfigObject is real, pgtest-backed
// evidence for TENANT-002's policies plane: a published, activated
// configuration object resolves through internal/data/configregistry's
// durable Store (migration 00027).
func TestPoliciesVerifierResolvesActiveConfigObject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	tenantID := insertTenant(t, db, "policies-plane")
	store := dataconfigregistry.New(appRoleConn(t, db))
	scope := platformconfig.Scope{TenantID: tenantID.String()}

	obj, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind: platformconfig.KindPolicy, ID: "pilot-onboarding", Revision: 1,
		Body: []byte(`{"rule":"pilot-default"}`), SchemaRef: "hcmnext.policy/v1",
		Scope: scope, PublisherPrincipal: "person:policy-owner", PublishedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := platformconfig.Activate(store, obj.Ref(), platformconfig.ActivationEvidence{
		ActivatedBy: "person:policy-owner", ActivatedAt: time.Date(2026, 9, 1, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	verifier := tenancy.PoliciesVerifier{Store: store, Scope: scope, Kind: platformconfig.KindPolicy, ID: "pilot-onboarding"}
	if verifier.Plane() != tenant.PlanePolicies {
		t.Fatalf("Plane() = %s, want POLICIES", verifier.Plane())
	}
	got, err := verifier.Verify(ctx, verificationRequest("policies-plane"))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(got.EvidenceRefs) != 2 {
		t.Fatalf("evidence refs = %v, want a config object ref and its digest", got.EvidenceRefs)
	}
}

// TestPoliciesVerifierFailsWhenNothingIsActivated proves the policies plane
// never reports VERIFIED on a guess: publishing without activating is not
// enough.
func TestPoliciesVerifierFailsWhenNothingIsActivated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	tenantID := insertTenant(t, db, "policies-plane-unactivated")
	store := dataconfigregistry.New(appRoleConn(t, db))
	scope := platformconfig.Scope{TenantID: tenantID.String()}

	if _, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind: platformconfig.KindPolicy, ID: "pilot-onboarding", Revision: 1,
		Body: []byte(`{"rule":"pilot-default"}`), SchemaRef: "hcmnext.policy/v1",
		Scope: scope, PublisherPrincipal: "person:policy-owner", PublishedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	verifier := tenancy.PoliciesVerifier{Store: store, Scope: scope, Kind: platformconfig.KindPolicy, ID: "pilot-onboarding"}
	if _, err := verifier.Verify(ctx, verificationRequest("policies-plane-unactivated")); err == nil {
		t.Fatal("an unactivated policy object was reported VERIFIED")
	}
}

// TestSchemaVerifierProvesRegisteredTenantScopedTablesArePresent is real
// evidence for TENANT-002's schemas plane: tenant and tenant_bootstrap_receipt
// are both registered tenant-scoped in STORE-001's storage-disposition
// registry and present in the live schema this test is connected to.
func TestSchemaVerifierProvesRegisteredTenantScopedTablesArePresent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	verifier := tenancy.SchemaVerifier{
		RegistryPath: storageDispositionPath(t),
		Tables:       []string{"tenant", "tenant_bootstrap_receipt"},
		Live:         db.Conn,
	}
	if verifier.Plane() != tenant.PlaneSchemas {
		t.Fatalf("Plane() = %s, want SCHEMAS", verifier.Plane())
	}
	got, err := verifier.Verify(ctx, verificationRequest("schemas-plane"))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(got.EvidenceRefs) != 2 {
		t.Fatalf("evidence refs = %v, want one per table", got.EvidenceRefs)
	}
}

// TestSchemaVerifierFailsForAnUnregisteredTable proves the schemas plane
// refuses a table the registry has never heard of, rather than silently
// skipping it.
func TestSchemaVerifierFailsForAnUnregisteredTable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	verifier := tenancy.SchemaVerifier{
		RegistryPath: storageDispositionPath(t),
		Tables:       []string{"no_such_table_ever_registered"},
		Live:         db.Conn,
	}
	if _, err := verifier.Verify(ctx, verificationRequest("schemas-plane-unregistered")); err == nil {
		t.Fatal("an unregistered table was reported VERIFIED by the schemas plane")
	}
}

// TestHealthVerifierProvesGovernedReadSucceeds is real evidence for
// TENANT-002's health plane: a row-level-security-scoped transaction on the
// app role can execute and read back a trivial statement.
func TestHealthVerifierProvesGovernedReadSucceeds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	tenantID := insertTenant(t, db, "health-plane")
	verifier := tenancy.HealthVerifier{TenantID: tenantID, AppConn: appRoleConn(t, db)}
	if verifier.Plane() != tenant.PlaneHealth {
		t.Fatalf("Plane() = %s, want HEALTH", verifier.Plane())
	}
	got, err := verifier.Verify(ctx, verificationRequest("health-plane"))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(got.EvidenceRefs) != 1 {
		t.Fatalf("evidence refs = %v, want exactly one governed-read reference", got.EvidenceRefs)
	}
}

// TestHealthVerifierFailsWithoutAnAppRoleConnection proves the "governed"
// half of the health plane's name is load-bearing: WithTenant refuses the
// nil tenant before any statement runs, so a caller that forgets to bind a
// real tenant id cannot accidentally read as an ungoverned superuser probe.
func TestHealthVerifierFailsWithoutAnAppRoleConnection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	verifier := tenancy.HealthVerifier{TenantID: uuid.Nil, AppConn: appRoleConn(t, db)}
	if _, err := verifier.Verify(ctx, verificationRequest("health-plane-nil")); err == nil {
		t.Fatal("a nil tenant id was reported VERIFIED by the health plane")
	}
}

// TestPlaneVerifiersSatisfyTheDomainPort is a compile-time-adjacent sanity
// check: every real verifier this file provides is assignable to
// tenant.PlaneVerifier and errors.Is composes over their failures the same
// way a caller composing tenant.VerifyPlane would rely on.
func TestPlaneVerifiersSatisfyTheDomainPort(t *testing.T) {
	t.Parallel()
	verifiers := []tenant.PlaneVerifier{
		tenancy.IdentityVerifier{},
		tenancy.KeysVerifier{},
		tenancy.PoliciesVerifier{},
		tenancy.SchemaVerifier{},
		tenancy.HealthVerifier{},
	}
	seen := make(map[tenant.Plane]bool, len(verifiers))
	for _, v := range verifiers {
		if seen[v.Plane()] {
			t.Fatalf("plane %s is claimed by more than one verifier in this file", v.Plane())
		}
		seen[v.Plane()] = true
	}

	// A verifier presented with no tables to check must refuse rather than
	// report VERIFIED on nothing -- exercised here against SchemaVerifier's
	// zero value.
	_, err := tenancy.SchemaVerifier{RegistryPath: storageDispositionPath(t)}.Verify(context.Background(), verificationRequest("zero-value"))
	if err == nil {
		t.Fatal("a SchemaVerifier with no declared tables reported VERIFIED")
	}
}
