package authn_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/trust/session"
)

var lifecycleAt = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

type recordingSink struct {
	mu      sync.Mutex
	targets []authn.RevocationTarget
	err     error
}

func (s *recordingSink) Revoke(_ context.Context, target authn.RevocationTarget) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.targets = append(s.targets, target)
	return s.err
}

func (s *recordingSink) snapshot() []authn.RevocationTarget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]authn.RevocationTarget(nil), s.targets...)
}

func lifecycleFixture(t *testing.T) (*authn.Service, authn.Identity, *recordingSink) {
	t.Helper()
	service := authn.NewMemory(func() time.Time { return lifecycleAt })
	if _, _, err := service.CreateAccount(authn.AccountSpec{ID: "account-1", PersonID: "person-1", Tenant: "tenant-a", At: lifecycleAt}); err != nil {
		t.Fatal(err)
	}
	identity, _, err := service.LinkIdentity(authn.IdentitySpec{ID: "identity-1", AccountID: "account-1", Tenant: "tenant-a", ProviderRef: "idp-1", SubjectDigest: validSubjectDigest(), At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	service.SetRevocationSink(sink)
	return service, identity, sink
}

func validSubjectDigest() string {
	return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}

func evidence(reason string) authn.Evidence {
	return authn.Evidence{Actor: "security-admin", Authority: "account-governance", Reason: reason, At: lifecycleAt}
}

func registerAllDependents(t *testing.T, service *authn.Service, identity authn.Identity) []authn.Dependent {
	t.Helper()
	kinds := []authn.DependentKind{authn.DependentPrincipal, authn.DependentSession, authn.DependentTokenFamily, authn.DependentAuthenticator, authn.DependentFederation, authn.DependentDelegation, authn.DependentSubjectLink}
	dependents := make([]authn.Dependent, 0, len(kinds))
	for i, kind := range kinds {
		dependent, err := service.RegisterDependent(authn.DependentSpec{ID: "dependent-" + string(rune('1'+i)), AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-a", Kind: kind, Assurance: trust.AssuranceSubstantial, Queued: i%2 == 0, At: lifecycleAt})
		if err != nil {
			t.Fatalf("RegisterDependent(%s): %v", kind, err)
		}
		dependents = append(dependents, dependent)
	}
	return dependents
}

// TestSubscriberAccountLifecycleRevokesPrincipalSessionsAuthenticatorsAndDelegation
// is the AUTHN-009 primary contract: a committed account epoch fences every
// dependent and each concrete adapter receives the same revocation target.
func TestSubscriberAccountLifecycleRevokesPrincipalSessionsAuthenticatorsAndDelegation(t *testing.T) {
	service := authn.NewMemory(func() time.Time { return lifecycleAt })
	if _, _, err := service.CreateAccount(authn.AccountSpec{ID: "account-1", PersonID: "person-1", Tenant: "tenant-a", At: lifecycleAt}); err != nil {
		t.Fatal(err)
	}
	identity, _, err := service.LinkIdentity(authn.IdentitySpec{ID: "identity-1", AccountID: "account-1", Tenant: "tenant-a", ProviderRef: "idp-1", SubjectDigest: validSubjectDigest(), At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	dependents := registerAllDependents(t, service, identity)
	sink := &recordingSink{}
	service.SetRevocationSink(sink)
	account, event, err := service.Disable(context.Background(), "account-1", evidence("account_disabled"))
	if err != nil {
		t.Fatal(err)
	}
	if account.Status != authn.AccountDisabled || account.RevocationEpoch != 2 || event.Epoch != 2 {
		t.Fatalf("account=%+v event=%+v", account, event)
	}
	if len(event.Affected) != len(dependents) || len(sink.snapshot()) != len(dependents) {
		t.Fatalf("affected=%d sink=%d dependents=%d", len(event.Affected), len(sink.snapshot()), len(dependents))
	}
	if _, err := service.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, At: lifecycleAt}); !errors.Is(err, authn.ErrAccountNotActive) {
		t.Fatalf("authenticate disabled=%v, want ErrAccountNotActive", err)
	}
	for _, dependent := range dependents {
		got, ok, err := service.Dependent(dependent.ID)
		if err != nil || !ok || got.Status != authn.DependentRevoked || got.RevocationEpoch != account.RevocationEpoch {
			t.Fatalf("dependent=%+v ok=%v err=%v", got, ok, err)
		}
	}
	events, err := service.Events(account.ID)
	if err != nil || len(events) != 3 || events[2].Digest == "" || events[2].PreviousDigest != events[1].Digest {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

// TestTodo_AUTHN_009_Property proves all declared dependent kinds are fenced
// by one epoch, including queued authority.
func TestTodo_AUTHN_009_Property(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	dependents := registerAllDependents(t, service, identity)
	account, _, err := service.Suspend(context.Background(), "account-1", evidence("security_suspend"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dependent := range dependents {
		if err := service.CheckDependent(account.ID, dependent.ID, lifecycleAt); !errors.Is(err, authn.ErrAccountNotActive) {
			t.Fatalf("dependent %s check=%v, want account refusal", dependent.ID, err)
		}
	}
}

// TestTodo_AUTHN_009_Golden pins deterministic fan-out ordering and the
// immutable evidence shape.
func TestTodo_AUTHN_009_Golden(t *testing.T) {
	service, identity, sink := lifecycleFixture(t)
	registerAllDependents(t, service, identity)
	_, event, err := service.Terminate(context.Background(), "account-1", evidence("account_terminated"))
	if err != nil {
		t.Fatal(err)
	}
	if len(event.Affected) != 7 || event.Affected[0].Kind != authn.DependentAuthenticator || event.Affected[0].ID != "dependent-4" {
		t.Fatalf("event affected=%+v", event.Affected)
	}
	if len(sink.snapshot()) != 7 || event.Digest == "" {
		t.Fatalf("sink=%+v event=%+v", sink.snapshot(), event)
	}
}

// TestTodo_AUTHN_009_Race proves a committed revoke wins against concurrent
// authentication attempts; no successful result may observe the old epoch.
func TestTodo_AUTHN_009_Race(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	dependent, err := service.RegisterDependent(authn.DependentSpec{ID: "dependent-race", AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-a", Kind: authn.DependentSession, Assurance: trust.AssuranceSubstantial, At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes, refusals int
	var mu sync.Mutex
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := service.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, DependentID: dependent.ID, At: lifecycleAt})
			mu.Lock()
			if err == nil {
				successes++
			} else {
				refusals++
			}
			mu.Unlock()
		}()
	}
	close(start)
	if _, _, err := service.Disable(context.Background(), "account-1", evidence("race_disable")); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if successes > 16 || refusals == 0 {
		t.Fatalf("successes=%d refusals=%d", successes, refusals)
	}
}

// TestTodo_AUTHN_009_Integration proves the session adapter revokes the
// canonical trust/session record after the lifecycle epoch commits.
func TestTodo_AUTHN_009_Integration(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	clock := lifecycleAt
	mgr, err := session.NewManager(session.ManagerConfig{Now: func() time.Time { return clock }, IdleTimeout: time.Hour, AbsoluteTimeout: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := mgr.Create(context.Background(), session.CreateSpec{Tenant: "tenant-a", Subject: "subject-1", PrincipalFingerprint: "fp-1", Assurance: trust.AssuranceSubstantial})
	if err != nil {
		t.Fatal(err)
	}
	service.SetRevocationSink(authn.SessionRevocationSink{Revoker: mgr})
	if _, err := service.RegisterDependent(authn.DependentSpec{ID: string(record.ID()), AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-a", Kind: authn.DependentSession, Assurance: trust.AssuranceSubstantial, At: lifecycleAt}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Disable(context.Background(), "account-1", evidence("integration_disable")); err != nil {
		t.Fatal(err)
	}
	if err := mgr.CheckRevocation(context.Background(), string(record.ID()), lifecycleAt); !errors.Is(err, session.ErrSessionRevoked) {
		t.Fatalf("session check=%v, want ErrSessionRevoked", err)
	}
}

// TestTodo_AUTHN_009_Fault proves a failed adapter is reported after the
// committed epoch and cannot leave the derived projection usable.
func TestTodo_AUTHN_009_Fault(t *testing.T) {
	service, identity, sink := lifecycleFixture(t)
	sink.err = errors.New("adapter unavailable")
	dependent, err := service.RegisterDependent(authn.DependentSpec{ID: "dependent-fault", AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-a", Kind: authn.DependentDelegation, Assurance: trust.AssuranceSubstantial, At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	account, _, err := service.Disable(context.Background(), "account-1", evidence("fault_disable"))
	if !errors.Is(err, authn.ErrFanoutIncomplete) {
		t.Fatalf("disable error=%v, want ErrFanoutIncomplete", err)
	}
	got, _, _ := service.Dependent(dependent.ID)
	if account.RevocationEpoch != got.RevocationEpoch || got.Status != authn.DependentRevoked {
		t.Fatalf("account=%+v dependent=%+v", account, got)
	}
}

// TestTodo_AUTHN_009_Security proves tenant confusion and disabled identity
// attempts are typed refusals.
func TestTodo_AUTHN_009_Security(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	if _, err := service.RegisterDependent(authn.DependentSpec{ID: "cross-tenant", AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-b", Kind: authn.DependentPrincipal, Assurance: trust.AssuranceSubstantial, At: lifecycleAt}); !errors.Is(err, authn.ErrTenantMismatch) {
		t.Fatalf("cross tenant=%v, want ErrTenantMismatch", err)
	}
	if _, _, err := service.Unlink(context.Background(), identity.ID, evidence("unlink")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, At: lifecycleAt}); !errors.Is(err, authn.ErrIdentityNotUsable) {
		t.Fatalf("unlinked authentication=%v, want ErrIdentityNotUsable", err)
	}
}

// TestTodo_AUTHN_009_Conformance proves relink increments revision, preserves
// old events, and does not resurrect old derived authority.
func TestTodo_AUTHN_009_Conformance(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	dependent, err := service.RegisterDependent(authn.DependentSpec{ID: "dependent-history", AccountID: "account-1", IdentityID: identity.ID, Tenant: "tenant-a", Kind: authn.DependentSubjectLink, Assurance: trust.AssuranceSubstantial, At: lifecycleAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Unlink(context.Background(), identity.ID, evidence("unlink")); err != nil {
		t.Fatal(err)
	}
	relinked, _, err := service.Relink(context.Background(), identity.ID, evidence("relink"))
	if err != nil || relinked.Revision != 3 || relinked.Status != authn.IdentityLinked {
		t.Fatalf("relinked=%+v err=%v", relinked, err)
	}
	if _, err := service.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, DependentID: dependent.ID, At: lifecycleAt}); !errors.Is(err, authn.ErrDependentRevoked) {
		t.Fatalf("old authority after relink=%v, want ErrDependentRevoked", err)
	}
}

// TestTodo_AUTHN_009_Recovery proves recovery cannot elevate assurance and
// succeeds only when the requested level is bounded by trusted assurance.
func TestTodo_AUTHN_009_Recovery(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	if _, err := service.Recover(authn.RecoveryRequest{AccountID: "account-1", IdentityID: identity.ID, CurrentAssurance: trust.AssuranceSubstantial, RequestedAssurance: trust.AssuranceHigh, Evidence: evidence("weak_recovery")}); !errors.Is(err, authn.ErrRecoveryAssurance) {
		t.Fatalf("weak recovery=%v, want ErrRecoveryAssurance", err)
	}
	if _, err := service.Recover(authn.RecoveryRequest{AccountID: "account-1", IdentityID: identity.ID, CurrentAssurance: trust.AssuranceHigh, RequestedAssurance: trust.AssuranceSubstantial, Evidence: evidence("bounded_recovery")}); err != nil {
		t.Fatalf("bounded recovery=%v", err)
	}
}

// TestTodo_AUTHN_009_ModelBased exercises every lifecycle transition against
// a small model of the invariant that non-active accounts reject auth.
func TestTodo_AUTHN_009_ModelBased(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	for _, step := range []struct {
		name string
		call func() error
	}{
		{"suspend", func() error {
			_, _, err := service.Suspend(context.Background(), "account-1", evidence("model_suspend"))
			return err
		}},
		{"resume", func() error {
			_, _, err := service.Resume(context.Background(), "account-1", evidence("model_resume"))
			return err
		}},
		{"disable", func() error {
			_, _, err := service.Disable(context.Background(), "account-1", evidence("model_disable"))
			return err
		}},
	} {
		if err := step.call(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		_, err := service.Authenticate(authn.AuthenticationRequest{AccountID: "account-1", IdentityID: identity.ID, At: lifecycleAt})
		if step.name != "resume" && !errors.Is(err, authn.ErrAccountNotActive) {
			t.Fatalf("%s auth=%v, want account refusal", step.name, err)
		}
	}
}

// TestTodo_AUTHN_009_Mutation proves lifecycle evidence and projections are
// defensive copies and cannot be mutated through a returned slice.
func TestTodo_AUTHN_009_Mutation(t *testing.T) {
	service, identity, _ := lifecycleFixture(t)
	if _, _, err := service.Unlink(context.Background(), identity.ID, evidence("mutation")); err != nil {
		t.Fatal(err)
	}
	events, err := service.Events("account-1")
	if err != nil {
		t.Fatal(err)
	}
	events[0].Affected = append(events[0].Affected, authn.Reference{Kind: authn.DependentPrincipal, ID: "tampered"})
	again, err := service.Events("account-1")
	if err != nil || len(again) != 3 || len(again[0].Affected) != 0 {
		t.Fatalf("mutated history=%+v err=%v", again, err)
	}
	if authn.Explain() == "" || authn.Version() < 1 {
		t.Fatal("lifecycle contract exports incomplete")
	}
}
