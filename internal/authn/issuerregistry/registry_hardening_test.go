package issuerregistry

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	trustfederation "github.com/monstercameron/human-capital-management-suite/internal/trust/federation"
)

type registryFailureStore struct {
	err            error
	latestRevision bool
	latestEvent    bool
	issuer         bool
	issuerFound    bool
	putIssuer      bool
	putEvent       bool
	latestState    StateEvent
	issuerValue    Issuer
}

func (s registryFailureStore) PutIssuer(Issuer) error {
	if s.putIssuer {
		return s.err
	}
	return nil
}
func (s registryFailureStore) GetIssuer(Ref) (Issuer, bool, error) {
	if s.issuer {
		return Issuer{}, false, s.err
	}
	return s.issuerValue, s.issuerFound, nil
}
func (s registryFailureStore) LatestRevision(values.TenantId, string) (uint32, bool, error) {
	if s.latestRevision {
		return 0, false, s.err
	}
	return 0, false, nil
}
func (s registryFailureStore) PutEvent(StateEvent) error {
	if s.putEvent {
		return s.err
	}
	return nil
}
func (s registryFailureStore) LatestEvent(values.TenantId, string) (StateEvent, bool, error) {
	if s.latestEvent {
		return StateEvent{}, false, s.err
	}
	return s.latestState, s.latestState != (StateEvent{}), nil
}
func (s registryFailureStore) ListEvents(values.TenantId, string) ([]StateEvent, error) {
	if s.err != nil {
		return nil, s.err
	}
	return nil, nil
}

func registryValidIssuer(t *testing.T) Issuer {
	t.Helper()
	return Issuer{
		Tenant:     tenantForRegistryTest,
		IssuerURL:  "https://issuer.registry.test/",
		Audience:   "audience",
		JWKS:       JWKSSource{Kind: JWKSSourcePinnedKeys, PinnedKeys: []PinnedKey{{KeyID: "kid", Algorithm: trustfederation.AlgRS256, PublicKeyDER: testIssuerDER(t)}}},
		Algorithms: []trustfederation.Algorithm{trustfederation.AlgRS256},
		ClockSkew:  time.Second, MetadataStaleness: time.Hour,
		Revision: 1, PublisherPrincipal: "publisher", PublishedAt: time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}
}

const tenantForRegistryTest = values.TenantId("registry-tenant")

func TestRegistryEntryPoints_RejectNilAndPropagateStoreErrors(t *testing.T) {
	issuer := registryValidIssuer(t)
	sentinel := errors.New("store failure")
	if _, err := Publish(nil, issuer); !errors.Is(err, ErrInvalidIssuer) {
		t.Fatalf("Publish(nil) = %v, want ErrInvalidIssuer", err)
	}
	if _, err := Activate(nil, issuer.Ref(), Evidence{ActedBy: "approver", At: issuer.PublishedAt}); !errors.Is(err, ErrInvalidIssuer) {
		t.Fatalf("Activate(nil) = %v, want ErrInvalidIssuer", err)
	}
	if _, err := Lookup(nil, issuer.Tenant, issuer.IssuerURL); !errors.Is(err, ErrInvalidIssuer) {
		t.Fatalf("Lookup(nil) = %v, want ErrInvalidIssuer", err)
	}

	for _, tt := range []struct {
		name  string
		store Store
		call  func(Store) error
	}{
		{name: "publish latest revision", store: registryFailureStore{err: sentinel, latestRevision: true}, call: func(s Store) error { _, err := Publish(s, issuer); return err }},
		{name: "publish put issuer", store: registryFailureStore{err: sentinel, putIssuer: true}, call: func(s Store) error { _, err := Publish(s, issuer); return err }},
		{name: "publish put event", store: registryFailureStore{err: sentinel, putEvent: true}, call: func(s Store) error { _, err := Publish(s, issuer); return err }},
		{name: "lookup latest event", store: registryFailureStore{err: sentinel, latestEvent: true}, call: func(s Store) error { _, err := Lookup(s, issuer.Tenant, issuer.IssuerURL); return err }},
		{name: "activate get issuer", store: registryFailureStore{err: sentinel, issuer: true}, call: func(s Store) error {
			_, err := Activate(s, issuer.Ref(), Evidence{ActedBy: "approver", At: issuer.PublishedAt})
			return err
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(tt.store); !errors.Is(err, sentinel) {
				t.Fatalf("error = %v, want errors.Is(..., sentinel)", err)
			}
		})
	}
}

func TestTransitionAndLookup_RejectCorruptOrUnsupportedState(t *testing.T) {
	issuer := registryValidIssuer(t)
	store := NewMemoryStore()
	if _, err := Publish(store, issuer); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := transition(store, issuer.Ref(), Evidence{ActedBy: "approver", At: issuer.PublishedAt}, Status("INVALID"), false); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("unsupported transition error = %v, want ErrInvalidTransition", err)
	}

	corrupt := registryFailureStore{latestState: StateEvent{Tenant: issuer.Tenant, IssuerURL: issuer.IssuerURL, Revision: issuer.Revision, To: Status("CORRUPT")}, issuerValue: issuer}
	if _, err := Lookup(corrupt, issuer.Tenant, issuer.IssuerURL); !errors.Is(err, ErrInvalidIssuer) {
		t.Fatalf("corrupt lookup error = %v, want ErrInvalidIssuer", err)
	}

	activeMissing := registryFailureStore{latestState: StateEvent{Tenant: issuer.Tenant, IssuerURL: issuer.IssuerURL, Revision: issuer.Revision, To: StatusActive}}
	if _, err := Lookup(activeMissing, issuer.Tenant, issuer.IssuerURL); !errors.Is(err, ErrUnknownRevision) {
		t.Fatalf("active missing revision error = %v, want ErrUnknownRevision", err)
	}
}

func TestMemoryStore_StoresCopiesAndOrdersTenantIndex(t *testing.T) {
	store := NewMemoryStore()
	issuer := registryValidIssuer(t)
	if err := store.PutIssuer(issuer); err != nil {
		t.Fatalf("PutIssuer: %v", err)
	}
	issuer.JWKS.PinnedKeys[0].PublicKeyDER[0] = 0
	got, found, err := store.GetIssuer(issuer.Ref())
	if err != nil || !found || got.JWKS.PinnedKeys[0].PublicKeyDER[0] == 0 {
		t.Fatalf("GetIssuer copy = found=%v err=%v key=%v, want stored original", found, err, got.JWKS.PinnedKeys[0].PublicKeyDER[0])
	}
	if err := store.PutIssuer(issuer); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("duplicate PutIssuer = %v, want ErrRevisionConflict", err)
	}
	store.PutIssuer(Issuer{Tenant: values.TenantId("another"), IssuerURL: issuer.IssuerURL, Revision: 2})
	tenantIndex := store.tenantsForIssuer(issuer.IssuerURL)
	if len(tenantIndex) != 2 || tenantIndex[0] != values.TenantId("another") || tenantIndex[1] != tenantForRegistryTest {
		t.Fatalf("tenantsForIssuer = %v, want sorted tenants", tenantIndex)
	}
	if events, err := store.ListEvents(tenantForRegistryTest, "missing"); err != nil || events == nil || len(events) != 0 {
		t.Fatalf("empty ListEvents = %#v, err=%v, want non-nil empty list", events, err)
	}
}
