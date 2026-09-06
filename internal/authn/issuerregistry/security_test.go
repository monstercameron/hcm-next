package issuerregistry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
	trustfederation "github.com/monstercameron/hcm-next/internal/trust/federation"
)

// TestTodo_AUTHN_001_Security is this todo's SECURITY matrix test. It
// exercises every refusal that protects the boundary between tenants and
// between governed and ungoverned issuer material: an issuer of another
// tenant is never handed back, a suspended or retired issuer is never
// treated as live, an unpinned verification-material reference is never
// accepted at publish time, and the maker/checker separation between
// publishing and activating an issuer cannot be bypassed by the same
// principal acting twice. Together these are the controls a compromised or
// careless caller could otherwise use to authenticate as -- or impersonate
// -- the wrong tenant.
func TestTodo_AUTHN_001_Security(t *testing.T) {
	store := issuerregistry.NewMemoryStore()

	t.Run("cross_tenant_issuer_never_resolves", func(t *testing.T) {
		acme := validIssuer(t)
		published, err := issuerregistry.Publish(store, acme)
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
		if _, err := issuerregistry.Activate(store, published.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
			t.Fatalf("Activate: %v", err)
		}

		// A different tenant asking about the exact same issuer URL is
		// refused, not silently handed acme's material.
		if _, err := issuerregistry.Lookup(store, tenantOther, issuerAcme); !errors.Is(err, issuerregistry.ErrWrongTenant) {
			t.Fatalf("Lookup(otherTenant, acmeIssuer) error = %v, want ErrWrongTenant", err)
		}
		resolver, err := issuerregistry.NewResolver(store, nil)
		if err != nil {
			t.Fatalf("NewResolver: %v", err)
		}
		if _, err := issuerregistry.NewTenantResolver(store, tenantOther, nil).ResolveIssuerKeys(context.Background(), issuerAcme); err == nil {
			t.Fatal("a different tenant's resolver resolved acme's keys, want a refusal")
		}
		// The multi-tenant resolver still finds it correctly for the right
		// tenant -- proving the refusal above is tenant-scoping, not a
		// general breakage.
		if _, err := resolver.ResolveIssuerKeys(context.Background(), issuerAcme); err != nil {
			t.Fatalf("multi-tenant resolver for the correct tenant: %v", err)
		}
	})

	t.Run("suspended_issuer_never_resolves", func(t *testing.T) {
		other := validIssuer(t)
		other.Tenant = tenantOther
		other.IssuerURL = "https://login.other-corp.invalid/"
		published, err := issuerregistry.Publish(store, other)
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
		if _, err := issuerregistry.Activate(store, published.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
			t.Fatalf("Activate: %v", err)
		}
		if _, err := issuerregistry.Suspend(store, published.Ref(), evidence("bob-approver", baseTime.Add(2*time.Minute))); err != nil {
			t.Fatalf("Suspend: %v", err)
		}
		if _, err := issuerregistry.Lookup(store, tenantOther, other.IssuerURL); !errors.Is(err, issuerregistry.ErrIssuerSuspended) {
			t.Fatalf("Lookup a suspended issuer error = %v, want ErrIssuerSuspended", err)
		}
		resolver := issuerregistry.NewTenantResolver(store, tenantOther, nil)
		if _, err := resolver.ResolveIssuerKeys(context.Background(), other.IssuerURL); !errors.Is(err, issuerregistry.ErrIssuerSuspended) {
			t.Fatalf("ResolveIssuerKeys for a suspended issuer error = %v, want ErrIssuerSuspended", err)
		}
	})

	t.Run("retired_issuer_never_resolves_and_cannot_be_revived", func(t *testing.T) {
		i := validIssuer(t)
		i.Tenant = "retire-tenant"
		i.IssuerURL = "https://login.retired.invalid/"
		published, err := issuerregistry.Publish(store, i)
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
		if _, err := issuerregistry.Retire(store, published.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
			t.Fatalf("Retire: %v", err)
		}
		if _, err := issuerregistry.Lookup(store, i.Tenant, i.IssuerURL); !errors.Is(err, issuerregistry.ErrIssuerRetired) {
			t.Fatalf("Lookup a retired issuer error = %v, want ErrIssuerRetired", err)
		}
		for _, attempt := range []func() (issuerregistry.StateEvent, error){
			func() (issuerregistry.StateEvent, error) {
				return issuerregistry.Activate(store, published.Ref(), evidence("carol-approver", baseTime.Add(2*time.Minute)))
			},
			func() (issuerregistry.StateEvent, error) {
				return issuerregistry.Suspend(store, published.Ref(), evidence("carol-approver", baseTime.Add(2*time.Minute)))
			},
		} {
			if _, err := attempt(); !errors.Is(err, issuerregistry.ErrInvalidTransition) {
				t.Fatalf("reviving a retired issuer error = %v, want ErrInvalidTransition", err)
			}
		}
	})

	t.Run("unpinned_jwks_source_never_publishes", func(t *testing.T) {
		i := validIssuer(t)
		i.Tenant = "unpinned-tenant"
		i.IssuerURL = "https://login.unpinned.invalid/"
		i.JWKS = issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedKeys} // no keys: not pinned
		if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrJWKSNotPinned) {
			t.Fatalf("Publish an unpinned issuer error = %v, want ErrJWKSNotPinned", err)
		}
		// It never became lookup-able.
		if _, err := issuerregistry.Lookup(store, i.Tenant, i.IssuerURL); !errors.Is(err, issuerregistry.ErrUnknownIssuer) {
			t.Fatalf("Lookup a never-published issuer error = %v, want ErrUnknownIssuer", err)
		}
	})

	t.Run("algorithm_confusion_never_publishes", func(t *testing.T) {
		i := validIssuer(t)
		i.Tenant = "alg-confusion-tenant"
		i.IssuerURL = "https://login.alg-confusion.invalid/"
		i.Algorithms = []trustfederation.Algorithm{"none"}
		if _, err := issuerregistry.Publish(store, i); !errors.Is(err, issuerregistry.ErrAlgorithmNotAllowed) {
			t.Fatalf("Publish issuer declaring \"none\" error = %v, want ErrAlgorithmNotAllowed", err)
		}
	})

	t.Run("maker_checker_separation_cannot_be_bypassed", func(t *testing.T) {
		i := validIssuer(t)
		i.Tenant = "maker-checker-tenant"
		i.IssuerURL = "https://login.maker-checker.invalid/"
		published, err := issuerregistry.Publish(store, i)
		if err != nil {
			t.Fatalf("Publish: %v", err)
		}
		if _, err := issuerregistry.Activate(store, published.Ref(), evidence(i.PublisherPrincipal, baseTime.Add(time.Minute))); !errors.Is(err, issuerregistry.ErrSameApprover) {
			t.Fatalf("Activate by the publisher error = %v, want ErrSameApprover", err)
		}
		if _, err := issuerregistry.Lookup(store, i.Tenant, i.IssuerURL); !errors.Is(err, issuerregistry.ErrIssuerNotActive) {
			t.Fatalf("Lookup after a rejected self-activation error = %v, want ErrIssuerNotActive", err)
		}
	})
}
