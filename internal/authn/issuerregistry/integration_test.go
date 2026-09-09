package issuerregistry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/authn/issuerregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

// TestTodo_AUTHN_001_Integration is this todo's INTEGRATION matrix test:
// the full publish -> distinct-approver activate -> suspend -> reactivate
// -> retire lifecycle, driven entirely through [issuerregistry.PGStore]
// against a real, migrated PostgreSQL schema (migration 00038), proving
// the same governed behavior [TestTodo_AUTHN_001] proves against
// [issuerregistry.MemoryStore] survives an actual round trip through
// Postgres: row level security, the append-only triggers, and the
// gap-free event sequence.
func TestTodo_AUTHN_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "acme-corp")
	store := issuerregistry.NewPGStore(appConn(t, db))
	tenant := values.TenantId(tenantID.String())
	const issuerURL = "https://login.acme.invalid/"

	issuer := issuerregistry.Issuer{
		Tenant:    tenant,
		IssuerURL: issuerURL,
		Audience:  "hcm-next-api",
		JWKS: issuerregistry.JWKSSource{
			Kind:       issuerregistry.JWKSSourcePinnedKeys,
			PinnedKeys: []issuerregistry.PinnedKey{validPinnedKey(t, "kid-1")},
		},
		Algorithms:         []trustfederation.Algorithm{trustfederation.AlgRS256},
		ClaimMappings:      []issuerregistry.ClaimMapping{{SourceClaim: "grp", Target: issuerregistry.PrincipalFieldRoles}},
		ClockSkew:          30 * time.Second,
		MetadataStaleness:  24 * time.Hour,
		Revision:           1,
		PublisherPrincipal: "alice",
		PublishedAt:        baseTime,
	}

	published, err := issuerregistry.Publish(store, issuer)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Round-tripped through Postgres and back: every field survives,
	// including the JSON-encoded pinned key and claim mapping.
	got, found, err := store.GetIssuer(published.Ref())
	if err != nil {
		t.Fatalf("GetIssuer: %v", err)
	}
	if !found {
		t.Fatal("GetIssuer did not find the just-published revision")
	}
	if got.Audience != issuer.Audience || len(got.JWKS.PinnedKeys) != 1 || got.JWKS.PinnedKeys[0].KeyID != "kid-1" {
		t.Fatalf("GetIssuer round-trip = %+v, missing expected fields", got)
	}
	if len(got.ClaimMappings) != 1 || got.ClaimMappings[0].Target != issuerregistry.PrincipalFieldRoles {
		t.Fatalf("GetIssuer round-trip claim mappings = %+v", got.ClaimMappings)
	}
	if got.ClockSkew != issuer.ClockSkew || got.MetadataStaleness != issuer.MetadataStaleness {
		t.Fatalf("GetIssuer round-trip skew/staleness = %v/%v, want %v/%v", got.ClockSkew, got.MetadataStaleness, issuer.ClockSkew, issuer.MetadataStaleness)
	}

	// DRAFT until activated.
	if _, err := issuerregistry.Lookup(store, tenant, issuerURL); !errors.Is(err, issuerregistry.ErrIssuerNotActive) {
		t.Fatalf("Lookup a DRAFT issuer error = %v, want ErrIssuerNotActive", err)
	}

	// Distinct-approver control holds against the real store too.
	if _, err := issuerregistry.Activate(store, published.Ref(), evidence(issuer.PublisherPrincipal, baseTime.Add(time.Minute))); !errors.Is(err, issuerregistry.ErrSameApprover) {
		t.Fatalf("Activate by the publisher error = %v, want ErrSameApprover", err)
	}
	if _, err := issuerregistry.Activate(store, published.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	active, err := issuerregistry.Lookup(store, tenant, issuerURL)
	if err != nil {
		t.Fatalf("Lookup after Activate: %v", err)
	}
	if active.Revision != 1 {
		t.Fatalf("Lookup after Activate = %+v, want revision 1", active)
	}

	if _, err := issuerregistry.Suspend(store, published.Ref(), evidence("bob-approver", baseTime.Add(2*time.Minute))); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if _, err := issuerregistry.Lookup(store, tenant, issuerURL); !errors.Is(err, issuerregistry.ErrIssuerSuspended) {
		t.Fatalf("Lookup a suspended issuer error = %v, want ErrIssuerSuspended", err)
	}

	if _, err := issuerregistry.Activate(store, published.Ref(), evidence("carol-approver", baseTime.Add(3*time.Minute))); err != nil {
		t.Fatalf("re-Activate: %v", err)
	}
	if _, err := issuerregistry.Retire(store, published.Ref(), evidence("carol-approver", baseTime.Add(4*time.Minute))); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if _, err := issuerregistry.Lookup(store, tenant, issuerURL); !errors.Is(err, issuerregistry.ErrIssuerRetired) {
		t.Fatalf("Lookup a retired issuer error = %v, want ErrIssuerRetired", err)
	}

	// Evidence on every state change, gap-free and in order: publish (->
	// DRAFT), activate, suspend, re-activate, retire.
	events, err := store.ListEvents(tenant, issuerURL)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	wantTo := []issuerregistry.Status{
		issuerregistry.StatusDraft, issuerregistry.StatusActive, issuerregistry.StatusSuspended,
		issuerregistry.StatusActive, issuerregistry.StatusRetired,
	}
	if len(events) != len(wantTo) {
		t.Fatalf("ListEvents returned %d events, want %d: %+v", len(events), len(wantTo), events)
	}
	for i, evt := range events {
		if evt.To != wantTo[i] {
			t.Fatalf("event %d To = %q, want %q", i, evt.To, wantTo[i])
		}
	}
}

func TestPGStoreRowLevelSecurityHidesOtherTenantsIssuer(t *testing.T) {
	db := pgtest.New(t)
	acmeID := insertTenant(t, db, "acme-corp")
	otherID := insertTenant(t, db, "other-corp")
	store := issuerregistry.NewPGStore(appConn(t, db))
	acmeTenant := values.TenantId(acmeID.String())
	otherTenant := values.TenantId(otherID.String())

	acme := issuerregistry.Issuer{
		Tenant: acmeTenant, IssuerURL: "https://login.acme.invalid/", Audience: "hcm-next-api",
		JWKS:               issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedKeys, PinnedKeys: []issuerregistry.PinnedKey{validPinnedKey(t, "kid-1")}},
		Algorithms:         []trustfederation.Algorithm{trustfederation.AlgRS256},
		ClockSkew:          0,
		MetadataStaleness:  time.Hour,
		Revision:           1,
		PublisherPrincipal: "alice",
		PublishedAt:        baseTime,
	}
	published, err := issuerregistry.Publish(store, acme)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := issuerregistry.Activate(store, published.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	// Row level security scopes every read to the caller's own tenant:
	// PGStore cannot distinguish "unknown" from "belongs to another
	// tenant" the way MemoryStore can (see doc.go) -- both report
	// ErrUnknownIssuer, never leaking that acme's issuer exists at all.
	if _, err := issuerregistry.Lookup(store, otherTenant, "https://login.acme.invalid/"); !errors.Is(err, issuerregistry.ErrUnknownIssuer) {
		t.Fatalf("Lookup from another tenant error = %v, want ErrUnknownIssuer", err)
	}
}

func TestPGStoreInvalidTenantIdentifierIsRefused(t *testing.T) {
	db := pgtest.New(t)
	store := issuerregistry.NewPGStore(appConn(t, db))
	if _, err := issuerregistry.Publish(store, issuerregistry.Issuer{
		Tenant: "not-a-uuid", IssuerURL: "https://login.invalid/", Audience: "a",
		JWKS:               issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedKeys, PinnedKeys: []issuerregistry.PinnedKey{validPinnedKey(t, "kid-1")}},
		Algorithms:         []trustfederation.Algorithm{trustfederation.AlgRS256},
		MetadataStaleness:  time.Hour,
		Revision:           1,
		PublisherPrincipal: "alice",
		PublishedAt:        baseTime,
	}); err == nil {
		t.Fatal("Publish with a non-uuid tenant identifier succeeded, want an error")
	}
}

func TestPGStoreUnprovisionedTenantUUIDIsRefused(t *testing.T) {
	db := pgtest.New(t)
	store := issuerregistry.NewPGStore(appConn(t, db))
	// A syntactically valid uuid that was never inserted into the tenant
	// table: refused by the foreign key migration 00038 declares, not by
	// this package's own validation.
	if _, err := issuerregistry.Publish(store, issuerregistry.Issuer{
		Tenant: "00000000-0000-4000-8000-000000000000", IssuerURL: "https://login.invalid/", Audience: "a",
		JWKS:               issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedKeys, PinnedKeys: []issuerregistry.PinnedKey{validPinnedKey(t, "kid-1")}},
		Algorithms:         []trustfederation.Algorithm{trustfederation.AlgRS256},
		MetadataStaleness:  time.Hour,
		Revision:           1,
		PublisherPrincipal: "alice",
		PublishedAt:        baseTime,
	}); err == nil {
		t.Fatal("Publish for a never-provisioned tenant uuid succeeded, want an error")
	}
}

func TestPGStoreLatestRevisionAndListEventsEmpty(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "acme-corp")
	store := issuerregistry.NewPGStore(appConn(t, db))
	tenant := values.TenantId(tenantID.String())

	if _, found, err := store.LatestRevision(tenant, "https://never.invalid/"); err != nil || found {
		t.Fatalf("LatestRevision = found=%v, err=%v, want not found", found, err)
	}
	events, err := store.ListEvents(tenant, "https://never.invalid/")
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("ListEvents = %+v, want empty", events)
	}
}
