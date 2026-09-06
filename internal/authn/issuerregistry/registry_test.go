package issuerregistry_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
)

func evidence(actor string, at time.Time) issuerregistry.Evidence {
	return issuerregistry.Evidence{ActedBy: actor, Authority: "identity-admin", Reason: "test", At: at}
}

// TestTodo_AUTHN_001 is this todo's PRIMARY test: publish an issuer through
// to an active, lookup-able state with a distinct approver, then walk it
// through every lifecycle transition AUTHN-001's RED/GREEN clauses name --
// suspend, reactivate, retire -- verifying [Lookup] refuses the issuer at
// every state that is not ACTIVE, refuses an unknown issuer and a
// different tenant's issuer, and that every state change left evidence
// behind in [Store.ListEvents].
func TestTodo_AUTHN_001(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	issuer := validIssuer(t)

	published, err := issuerregistry.Publish(store, issuer)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	ref := published.Ref()

	// A freshly published revision is DRAFT, not ACTIVE: Lookup refuses it.
	if _, err := issuerregistry.Lookup(store, tenantAcme, issuerAcme); !errors.Is(err, issuerregistry.ErrIssuerNotActive) {
		t.Fatalf("Lookup on a DRAFT issuer error = %v, want ErrIssuerNotActive", err)
	}

	// The publisher cannot also be the activator: this is the
	// distinct-approver control AUTHN-001 requires by name.
	if _, err := issuerregistry.Activate(store, ref, evidence(issuer.PublisherPrincipal, baseTime.Add(time.Minute))); !errors.Is(err, issuerregistry.ErrSameApprover) {
		t.Fatalf("Activate by the publisher error = %v, want ErrSameApprover", err)
	}

	if _, err := issuerregistry.Activate(store, ref, evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	active, err := issuerregistry.Lookup(store, tenantAcme, issuerAcme)
	if err != nil {
		t.Fatalf("Lookup after Activate: %v", err)
	}
	if active.Revision != ref.Revision || active.PublisherPrincipal != issuer.PublisherPrincipal {
		t.Fatalf("Lookup after Activate = %+v, want revision %d from %q", active, ref.Revision, issuer.PublisherPrincipal)
	}

	// Refused: unknown issuer, and this tenant's own issuer looked up under
	// a different tenant.
	if _, err := issuerregistry.Lookup(store, tenantAcme, "https://never-registered.invalid/"); !errors.Is(err, issuerregistry.ErrUnknownIssuer) {
		t.Fatalf("Lookup unknown issuer error = %v, want ErrUnknownIssuer", err)
	}
	if _, err := issuerregistry.Lookup(store, tenantOther, issuerAcme); !errors.Is(err, issuerregistry.ErrWrongTenant) {
		t.Fatalf("Lookup wrong-tenant error = %v, want ErrWrongTenant", err)
	}

	// Suspend: Lookup refuses the suspended issuer.
	if _, err := issuerregistry.Suspend(store, ref, evidence("bob-approver", baseTime.Add(2*time.Minute))); err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if _, err := issuerregistry.Lookup(store, tenantAcme, issuerAcme); !errors.Is(err, issuerregistry.ErrIssuerSuspended) {
		t.Fatalf("Lookup on a SUSPENDED issuer error = %v, want ErrIssuerSuspended", err)
	}

	// Reactivate from SUSPENDED: allowed, and again subject to the
	// distinct-approver control.
	if _, err := issuerregistry.Activate(store, ref, evidence(issuer.PublisherPrincipal, baseTime.Add(3*time.Minute))); !errors.Is(err, issuerregistry.ErrSameApprover) {
		t.Fatalf("re-Activate by the publisher error = %v, want ErrSameApprover", err)
	}
	if _, err := issuerregistry.Activate(store, ref, evidence("carol-approver", baseTime.Add(3*time.Minute))); err != nil {
		t.Fatalf("re-Activate: %v", err)
	}
	if _, err := issuerregistry.Lookup(store, tenantAcme, issuerAcme); err != nil {
		t.Fatalf("Lookup after re-Activate: %v", err)
	}

	// Retire: terminal. Lookup refuses it and no further transition is
	// permitted out of RETIRED.
	if _, err := issuerregistry.Retire(store, ref, evidence("carol-approver", baseTime.Add(4*time.Minute))); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if _, err := issuerregistry.Lookup(store, tenantAcme, issuerAcme); !errors.Is(err, issuerregistry.ErrIssuerRetired) {
		t.Fatalf("Lookup on a RETIRED issuer error = %v, want ErrIssuerRetired", err)
	}
	if _, err := issuerregistry.Activate(store, ref, evidence("dave-approver", baseTime.Add(5*time.Minute))); !errors.Is(err, issuerregistry.ErrInvalidTransition) {
		t.Fatalf("Activate a RETIRED issuer error = %v, want ErrInvalidTransition", err)
	}

	// Evidence on every state change: publish (-> DRAFT), activate, suspend,
	// re-activate, retire -- five events, oldest first.
	events, err := store.ListEvents(tenantAcme, issuerAcme)
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
		if evt.ActedBy == "" {
			t.Fatalf("event %d has no ActedBy evidence", i)
		}
	}
}

func TestPublishNewRevisionResetsToDraft(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	issuer := validIssuer(t)
	published, err := issuerregistry.Publish(store, issuer)
	if err != nil {
		t.Fatalf("Publish v1: %v", err)
	}
	if _, err := issuerregistry.Activate(store, published.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
		t.Fatalf("Activate v1: %v", err)
	}
	if _, err := issuerregistry.Lookup(store, tenantAcme, issuerAcme); err != nil {
		t.Fatalf("Lookup after Activate v1: %v", err)
	}

	v2 := issuer
	v2.Revision = 2
	v2.PublishedAt = baseTime.Add(2 * time.Minute)
	if _, err := issuerregistry.Publish(store, v2); err != nil {
		t.Fatalf("Publish v2: %v", err)
	}
	// v2 starts DRAFT even though v1 was ACTIVE: new content is never live
	// until separately activated.
	if _, err := issuerregistry.Lookup(store, tenantAcme, issuerAcme); !errors.Is(err, issuerregistry.ErrIssuerNotActive) {
		t.Fatalf("Lookup after publishing v2 error = %v, want ErrIssuerNotActive", err)
	}
}

func TestPublishRejectsNonIncreasingRevision(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	issuer := validIssuer(t)
	if _, err := issuerregistry.Publish(store, issuer); err != nil {
		t.Fatalf("Publish v1: %v", err)
	}
	if _, err := issuerregistry.Publish(store, issuer); !errors.Is(err, issuerregistry.ErrRevisionConflict) {
		t.Fatalf("re-Publish same revision error = %v, want ErrRevisionConflict", err)
	}
	older := issuer
	older.Revision = 1
	older.PublishedAt = baseTime.Add(time.Hour)
	next := issuer
	next.Revision = 2
	next.PublishedAt = baseTime.Add(2 * time.Hour)
	if _, err := issuerregistry.Publish(store, next); err != nil {
		t.Fatalf("Publish v2: %v", err)
	}
	if _, err := issuerregistry.Publish(store, older); !errors.Is(err, issuerregistry.ErrRevisionConflict) {
		t.Fatalf("Publish an already-superseded revision number error = %v, want ErrRevisionConflict", err)
	}
}

func TestActivateRollsBackToOlderRevision(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	v1 := validIssuer(t)
	if _, err := issuerregistry.Publish(store, v1); err != nil {
		t.Fatalf("Publish v1: %v", err)
	}
	if _, err := issuerregistry.Activate(store, v1.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
		t.Fatalf("Activate v1: %v", err)
	}

	v2 := v1
	v2.Revision = 2
	v2.PublishedAt = baseTime.Add(2 * time.Minute)
	v2.JWKS.PinnedKeys[0].KeyID = "kid-2"
	if _, err := issuerregistry.Publish(store, v2); err != nil {
		t.Fatalf("Publish v2: %v", err)
	}
	if _, err := issuerregistry.Activate(store, v2.Ref(), evidence("carol-approver", baseTime.Add(3*time.Minute))); err != nil {
		t.Fatalf("Activate v2: %v", err)
	}
	active, err := issuerregistry.Lookup(store, tenantAcme, issuerAcme)
	if err != nil || active.Revision != 2 {
		t.Fatalf("Lookup = %+v, err=%v, want revision 2", active, err)
	}

	// Roll back to v1 by activating it again.
	if _, err := issuerregistry.Activate(store, v1.Ref(), evidence("dave-approver", baseTime.Add(4*time.Minute))); err != nil {
		t.Fatalf("Activate v1 (rollback): %v", err)
	}
	active, err = issuerregistry.Lookup(store, tenantAcme, issuerAcme)
	if err != nil || active.Revision != 1 {
		t.Fatalf("Lookup after rollback = %+v, err=%v, want revision 1", active, err)
	}
}

func TestSuspendRejectsFromNonActive(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	issuer := validIssuer(t)
	published, _ := issuerregistry.Publish(store, issuer)
	if _, err := issuerregistry.Suspend(store, published.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); !errors.Is(err, issuerregistry.ErrInvalidTransition) {
		t.Fatalf("Suspend a DRAFT issuer error = %v, want ErrInvalidTransition", err)
	}
}

func TestRetirePermittedFromDraft(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	issuer := validIssuer(t)
	published, _ := issuerregistry.Publish(store, issuer)
	if _, err := issuerregistry.Retire(store, published.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
		t.Fatalf("Retire a DRAFT issuer: %v", err)
	}
}

func TestRetireRejectsWhenAlreadyRetired(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	issuer := validIssuer(t)
	published, _ := issuerregistry.Publish(store, issuer)
	if _, err := issuerregistry.Retire(store, published.Ref(), evidence("bob-approver", baseTime.Add(time.Minute))); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if _, err := issuerregistry.Retire(store, published.Ref(), evidence("carol-approver", baseTime.Add(2*time.Minute))); !errors.Is(err, issuerregistry.ErrInvalidTransition) {
		t.Fatalf("re-Retire error = %v, want ErrInvalidTransition", err)
	}
}

func TestActivateRejectsUnknownRevision(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	ref := issuerregistry.Ref{Tenant: tenantAcme, IssuerURL: issuerAcme, Revision: 99}
	if _, err := issuerregistry.Activate(store, ref, evidence("bob-approver", baseTime)); !errors.Is(err, issuerregistry.ErrUnknownRevision) {
		t.Fatalf("Activate unknown revision error = %v, want ErrUnknownRevision", err)
	}
}

func TestEvidenceRequiresActorAndTime(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	issuer := validIssuer(t)
	published, _ := issuerregistry.Publish(store, issuer)

	if _, err := issuerregistry.Activate(store, published.Ref(), issuerregistry.Evidence{At: baseTime}); !errors.Is(err, issuerregistry.ErrMissingEvidence) {
		t.Fatalf("Activate with no ActedBy error = %v, want ErrMissingEvidence", err)
	}
	if _, err := issuerregistry.Activate(store, published.Ref(), issuerregistry.Evidence{ActedBy: "bob-approver"}); !errors.Is(err, issuerregistry.ErrMissingEvidence) {
		t.Fatalf("Activate with no At error = %v, want ErrMissingEvidence", err)
	}
}

func TestLookupUnknownIssuerNoTenantEverRegistered(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	if _, err := issuerregistry.Lookup(store, tenantAcme, "https://nobody.invalid/"); !errors.Is(err, issuerregistry.ErrUnknownIssuer) {
		t.Fatalf("Lookup error = %v, want ErrUnknownIssuer", err)
	}
}

func TestMemoryStoreLatestRevisionNotFound(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	if _, found, err := store.LatestRevision(tenantAcme, issuerAcme); err != nil || found {
		t.Fatalf("LatestRevision = found=%v, err=%v, want not found", found, err)
	}
}

func TestMemoryStoreGetIssuerNotFound(t *testing.T) {
	store := issuerregistry.NewMemoryStore()
	if _, found, err := store.GetIssuer(issuerregistry.Ref{Tenant: tenantAcme, IssuerURL: issuerAcme, Revision: 1}); err != nil || found {
		t.Fatalf("GetIssuer = found=%v, err=%v, want not found", found, err)
	}
}
