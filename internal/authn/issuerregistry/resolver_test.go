package issuerregistry_test

import (
	"context"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/trust/bundle"
	trustfederation "github.com/monstercameron/hcm-next/internal/trust/federation"
)

// publishAndActivate is a resolver-test helper: publish issuer and activate
// it under a distinct approver, failing the test on any error.
func publishAndActivate(t *testing.T, store issuerregistry.Store, issuer issuerregistry.Issuer) {
	t.Helper()
	published, err := issuerregistry.Publish(store, issuer)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := issuerregistry.Activate(store, published.Ref(), evidence("bob-approver", issuer.PublishedAt.Add(time.Minute))); err != nil {
		t.Fatalf("Activate: %v", err)
	}
}

func TestResolverResolvesPinnedKeys(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	publishAndActivate(t, store, validIssuer(t))

	resolver, err := issuerregistry.NewResolver(store, nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	keys, err := resolver.ResolveIssuerKeys(context.Background(), issuerAcme)
	if err != nil {
		t.Fatalf("ResolveIssuerKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].ID != "kid-1" || keys[0].Algorithm != trustfederation.AlgRS256 {
		t.Fatalf("ResolveIssuerKeys = %+v, want one RS256 key kid-1", keys)
	}
	if keys[0].Public == nil {
		t.Fatal("ResolveIssuerKeys returned a key with no public key material")
	}
}

func TestResolverPropagatesLookupRefusal(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	// Published but never activated: still DRAFT.
	if _, err := issuerregistry.Publish(store, validIssuer(t)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	resolver, err := issuerregistry.NewResolver(store, nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	if _, err := resolver.ResolveIssuerKeys(context.Background(), issuerAcme); !errors.Is(err, issuerregistry.ErrIssuerNotActive) {
		t.Fatalf("ResolveIssuerKeys error = %v, want ErrIssuerNotActive", err)
	}
}

func TestResolverUnknownIssuer(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	resolver, err := issuerregistry.NewResolver(store, nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	if _, err := resolver.ResolveIssuerKeys(context.Background(), "https://nobody.invalid/"); !errors.Is(err, issuerregistry.ErrUnknownIssuer) {
		t.Fatalf("ResolveIssuerKeys error = %v, want ErrUnknownIssuer", err)
	}
}

func TestNewTenantResolverResolvesPinnedKeys(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	publishAndActivate(t, store, validIssuer(t))

	resolver := issuerregistry.NewTenantResolver(store, tenantAcme, nil)
	keys, err := resolver.ResolveIssuerKeys(context.Background(), issuerAcme)
	if err != nil {
		t.Fatalf("ResolveIssuerKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("ResolveIssuerKeys = %+v, want exactly one key", keys)
	}

	// A tenant-scoped resolver never sees another tenant's issuer, even one
	// with the identical URL.
	if _, err := issuerregistry.NewTenantResolver(store, tenantOther, nil).ResolveIssuerKeys(context.Background(), issuerAcme); err == nil {
		t.Fatal("tenant-scoped resolver for a different tenant resolved keys, want a refusal")
	}
}

func TestNewResolverRejectsStoreWithoutCrossTenantIndex(t *testing.T) {
	t.Parallel()
	if _, err := issuerregistry.NewResolver(fakeStoreNoCrossTenant{}, nil); err == nil {
		t.Fatal("NewResolver accepted a store with no cross-tenant index, want an error")
	}
}

// fakeStoreNoCrossTenant is a minimal [issuerregistry.Store] that does not
// implement the unexported cross-tenant index [issuerregistry.MemoryStore]
// provides -- standing in for [issuerregistry.PGStore]'s own deliberate
// lack of one without requiring a live database in this test.
type fakeStoreNoCrossTenant struct{}

func (fakeStoreNoCrossTenant) PutIssuer(issuerregistry.Issuer) error { return nil }
func (fakeStoreNoCrossTenant) GetIssuer(issuerregistry.Ref) (issuerregistry.Issuer, bool, error) {
	return issuerregistry.Issuer{}, false, nil
}
func (fakeStoreNoCrossTenant) LatestRevision(values.TenantId, string) (uint32, bool, error) {
	return 0, false, nil
}
func (fakeStoreNoCrossTenant) PutEvent(issuerregistry.StateEvent) error { return nil }
func (fakeStoreNoCrossTenant) LatestEvent(values.TenantId, string) (issuerregistry.StateEvent, bool, error) {
	return issuerregistry.StateEvent{}, false, nil
}
func (fakeStoreNoCrossTenant) ListEvents(values.TenantId, string) ([]issuerregistry.StateEvent, error) {
	return nil, nil
}

func TestResolverBundleSourcePath(t *testing.T) {
	t.Parallel()
	notBefore := baseTime.Add(-time.Hour)
	notAfter := baseTime.Add(24 * time.Hour)
	der, _ := testSelfSignedCA(t, 1, notBefore, notAfter)
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}
	pinned := bundle.PinnedCert{Digest: bundle.DigestOf(cert.Raw), Cert: cert}
	b, err := bundle.New(1, "issuerregistry-test", []bundle.PinnedCert{pinned}, nil, notBefore, notAfter)
	if err != nil {
		t.Fatalf("bundle.New: %v", err)
	}

	store := issuerregistry.NewMemoryStore()
	issuer := validIssuer(t)
	issuer.JWKS = issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedBundle, BundleRef: "acme-ca", BundleVersion: 1}
	publishAndActivate(t, store, issuer)

	resolver, err := issuerregistry.NewResolver(store, fakeBundleSource{ref: "acme-ca", version: 1, bundle: b})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	keys, err := resolver.ResolveIssuerKeys(context.Background(), issuerAcme)
	if err != nil {
		t.Fatalf("ResolveIssuerKeys: %v", err)
	}
	if len(keys) != 1 || keys[0].Algorithm != trustfederation.AlgRS256 {
		t.Fatalf("ResolveIssuerKeys = %+v, want one RS256 key from the pinned bundle", keys)
	}
	if string(keys[0].ID) != string(pinned.Digest) {
		t.Fatalf("ResolveIssuerKeys key id = %q, want the pinned cert digest %q", keys[0].ID, pinned.Digest)
	}
}

func TestResolverBundleSourceMissingConfiguredSource(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	issuer := validIssuer(t)
	issuer.JWKS = issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedBundle, BundleRef: "acme-ca", BundleVersion: 1}
	publishAndActivate(t, store, issuer)

	resolver, err := issuerregistry.NewResolver(store, nil)
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	if _, err := resolver.ResolveIssuerKeys(context.Background(), issuerAcme); err == nil {
		t.Fatal("ResolveIssuerKeys with no bundle source configured succeeded, want an error")
	}
}

func TestResolverBundleSourceUnresolvedReference(t *testing.T) {
	t.Parallel()
	store := issuerregistry.NewMemoryStore()
	issuer := validIssuer(t)
	issuer.JWKS = issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedBundle, BundleRef: "acme-ca", BundleVersion: 1}
	publishAndActivate(t, store, issuer)

	resolver, err := issuerregistry.NewResolver(store, fakeBundleSource{})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	if _, err := resolver.ResolveIssuerKeys(context.Background(), issuerAcme); err == nil {
		t.Fatal("ResolveIssuerKeys with an unresolvable bundle reference succeeded, want an error")
	}
}

// fakeBundleSource is a minimal, deterministic [issuerregistry.BundleSource]
// test double.
type fakeBundleSource struct {
	ref     string
	version int
	bundle  *bundle.Bundle
}

func (f fakeBundleSource) LookupBundle(ref string, version int) (*bundle.Bundle, bool) {
	if f.bundle == nil || ref != f.ref || version != f.version {
		return nil, false
	}
	return f.bundle, true
}
