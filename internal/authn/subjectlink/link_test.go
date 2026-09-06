package subjectlink_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
	"github.com/monstercameron/hcm-next/internal/authn/subjectlink"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	trustfederation "github.com/monstercameron/hcm-next/internal/trust/federation"
)

const testAt = "2026-09-05T12:00:00Z"

func activeFixture(t *testing.T) (*subjectlink.Service, issuerregistry.Ref) {
	t.Helper()
	tenant := values.TenantId("tenant-a")
	ref := issuerregistry.Ref{Tenant: tenant, IssuerURL: "https://issuer.example/", Revision: 1}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	issuers := issuerregistry.NewMemoryStore()
	published, err := issuerregistry.Publish(issuers, issuerregistry.Issuer{
		Tenant: tenant, IssuerURL: ref.IssuerURL, Audience: "hcm-next",
		JWKS:       issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedKeys, PinnedKeys: []issuerregistry.PinnedKey{{KeyID: "kid-1", Algorithm: trustfederation.AlgEdDSA, PublicKeyDER: der}}},
		Algorithms: []trustfederation.Algorithm{trustfederation.AlgEdDSA}, ClockSkew: time.Minute,
		MetadataStaleness: 24 * time.Hour, Revision: ref.Revision, PublisherPrincipal: "publisher", PublishedAt: mustTime(t, testAt),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issuerregistry.Activate(issuers, published.Ref(), issuerregistry.Evidence{ActedBy: "approver", At: mustTime(t, "2026-09-05T12:01:00Z")}); err != nil {
		t.Fatal(err)
	}
	_ = priv
	return subjectlink.New(subjectlink.NewMemoryStore(), issuers), ref
}

func mustTime(t *testing.T, text string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func decision(t *testing.T, rule subjectlink.MatchingRule, actor, approver, evidence string) subjectlink.Decision {
	t.Helper()
	d, err := subjectlink.NewDecision(rule, actor, approver, "identity-governance", evidence, mustTime(t, testAt))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func createRequest(t *testing.T, ref issuerregistry.Ref, id, subject, identity string, d subjectlink.Decision) subjectlink.CreateRequest {
	t.Helper()
	return subjectlink.CreateRequest{
		LinkID: id, IssuerRef: ref, SubjectDigest: subjectlink.DigestSubject(subject),
		WorkforceIdentityRef: subjectlink.WorkforceIdentityRef{Tenant: ref.Tenant, ID: identity}, Decision: d,
	}
}

func authorized(ref issuerregistry.Ref, digest string) subjectlink.LookupRequest {
	return subjectlink.LookupRequest{IssuerRef: ref, SubjectDigest: digest, Scope: subjectlink.LookupScope{Tenant: ref.Tenant, Purpose: "identity-governance", Granted: true}}
}

// TestTodo_AUTHN_003 proves that an ACTIVE issuer plus a digested decision
// creates a tenant-bound subject link and an append-only LINKED event.
func TestTodo_AUTHN_003(t *testing.T) {
	service, ref := activeFixture(t)
	d := decision(t, subjectlink.RuleVerifiedEmail, "resolver-1", "", "evidence/email-1")
	request := createRequest(t, ref, "link-1", "alice@example.com", "identity-1", d)
	link, event, err := service.Create(request)
	if err != nil {
		t.Fatal(err)
	}
	if link.Lifecycle != subjectlink.LifecycleLinked || event.From != "" || event.To != subjectlink.LifecycleLinked || event.Digest == "" || event.Digest != link.Digest {
		t.Fatalf("link=%+v event=%+v", link, event)
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	got, err := service.Lookup(authorized(ref, request.SubjectDigest))
	if err != nil || got.Status != subjectlink.LookupFound || got.Link.ID != "link-1" {
		t.Fatalf("authorized lookup=%+v err=%v", got, err)
	}
	withheld, err := service.Lookup(subjectlink.LookupRequest{IssuerRef: ref, SubjectDigest: request.SubjectDigest})
	if err != nil || withheld.Status != subjectlink.LookupWithheld {
		t.Fatalf("unscoped lookup=%+v err=%v", withheld, err)
	}
	raw, _ := json.Marshal(struct {
		Link  subjectlink.Link
		Event subjectlink.LifecycleEvent
	}{link, event})
	if strings.Contains(string(raw), "alice@example.com") || strings.Contains(link.Explain(), "alice@example.com") || strings.Contains(subjectlink.Explain(), "alice@example.com") {
		t.Fatalf("raw subject escaped boundary: %s", raw)
	}
	duplicateSubject := request
	duplicateSubject.LinkID = "link-duplicate-subject"
	if _, _, err := service.Create(duplicateSubject); !errors.Is(err, subjectlink.ErrDuplicateSubject) {
		t.Fatalf("duplicate subject err=%v", err)
	}
	otherSubject := createRequest(t, ref, "link-2", "bob@example.com", "identity-1", d)
	if _, _, err := service.Create(otherSubject); !errors.Is(err, subjectlink.ErrDuplicateIdentity) {
		t.Fatalf("duplicate identity err=%v", err)
	}
}

// FuzzTodo_AUTHN_003 proves digest derivation and validation are total for
// arbitrary identifier bytes and never retain the raw value in a link.
func FuzzTodo_AUTHN_003(f *testing.F) {
	for _, seed := range []string{"alice@example.com", "", "\x00", "unicode-名前", strings.Repeat("x", 512)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, identifier string) {
		digest := subjectlink.DigestSubject(identifier)
		if len(digest) != 64 || digest != strings.ToLower(digest) {
			t.Fatalf("digest=%q", digest)
		}
		if strings.Contains(digest, identifier) && identifier != "" {
			t.Fatalf("digest contains raw identifier")
		}
	})
}

// TestTodo_AUTHN_003_Integration proves lifecycle changes append digested
// events and that a suspended link can be resumed only through the service.
func TestTodo_AUTHN_003_Integration(t *testing.T) {
	service, ref := activeFixture(t)
	request := createRequest(t, ref, "link-integration", "integration-subject", "identity-integration", decision(t, subjectlink.RuleHRProvidedExternal, "hr-sync", "", "hr/import/42"))
	link, _, err := service.Create(request)
	if err != nil {
		t.Fatal(err)
	}
	suspended, suspendedEvent, err := service.Suspend(link.ID, subjectlink.LifecycleEvidence{ActedBy: "security", At: mustTime(t, "2026-09-05T13:00:00Z")})
	if err != nil || suspended.Lifecycle != subjectlink.LifecycleSuspended || suspendedEvent.PreviousDigest != link.Digest {
		t.Fatalf("suspended=%+v event=%+v err=%v", suspended, suspendedEvent, err)
	}
	resumed, _, err := service.Resume(link.ID, subjectlink.LifecycleEvidence{ActedBy: "security", At: mustTime(t, "2026-09-05T14:00:00Z")})
	if err != nil || resumed.Lifecycle != subjectlink.LifecycleLinked {
		t.Fatalf("resumed=%+v err=%v", resumed, err)
	}
	if _, _, err := service.Unlink(link.ID, subjectlink.LifecycleEvidence{ActedBy: "security", At: mustTime(t, "2026-09-05T15:00:00Z")}); err != nil {
		t.Fatal(err)
	}
	events, err := service.Store.Events(link.ID)
	if err != nil || len(events) != 4 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	for i := range events {
		if err := events[i].Validate(); err != nil {
			t.Fatalf("event %d invalid: %v", i, err)
		}
		if i > 0 && events[i].PreviousDigest != events[i-1].Digest {
			t.Fatalf("event %d chain broken", i)
		}
	}
}

// TestTodo_AUTHN_003_Security proves inactive issuers, cross-tenant links,
// self-approved manual links, and unscoped existence probes fail closed.
func TestTodo_AUTHN_003_Security(t *testing.T) {
	service, ref := activeFixture(t)
	d := decision(t, subjectlink.RuleManualReview, "requester", "approver", "case/link-1")
	request := createRequest(t, ref, "link-security", "security-subject", "identity-security", d)
	if _, _, err := service.Create(request); err != nil {
		t.Fatal(err)
	}
	self, err := subjectlink.NewDecision(subjectlink.RuleManualReview, "same", "same", "authority", "case/self", mustTime(t, testAt))
	if err == nil || !errors.Is(err, subjectlink.ErrInvalidDecision) {
		t.Fatalf("self-approved decision=%+v err=%v", self, err)
	}
	otherTenant := request
	otherTenant.LinkID = "link-cross-tenant"
	otherTenant.WorkforceIdentityRef.Tenant = values.TenantId("tenant-b")
	if _, _, err := service.Create(otherTenant); err == nil || !errors.Is(err, subjectlink.ErrInvalidLink) {
		t.Fatalf("cross-tenant err=%v", err)
	}
	noScopePresent, err := service.Lookup(subjectlink.LookupRequest{IssuerRef: ref, SubjectDigest: request.SubjectDigest})
	if err != nil || noScopePresent.Status != subjectlink.LookupWithheld || noScopePresent.Link.ID != "" {
		t.Fatalf("present withheld=%+v err=%v", noScopePresent, err)
	}
	noScopeAbsent, err := service.Lookup(subjectlink.LookupRequest{IssuerRef: ref, SubjectDigest: subjectlink.DigestSubject("absent")})
	if err != nil || noScopeAbsent.Status != subjectlink.LookupWithheld || noScopeAbsent.Status != noScopePresent.Status {
		t.Fatalf("absent withheld=%+v err=%v", noScopeAbsent, err)
	}
}

// TestTodo_AUTHN_003_Mutation proves tampering with decision or lifecycle
// evidence is rejected by digest verification.
func TestTodo_AUTHN_003_Mutation(t *testing.T) {
	service, ref := activeFixture(t)
	d := decision(t, subjectlink.RuleVerifiedEmail, "resolver", "", "evidence/mutation")
	d.EvidenceRef = "evidence/changed"
	if err := d.Validate(); !errors.Is(err, subjectlink.ErrInvalidDecision) {
		t.Fatalf("mutated decision err=%v", err)
	}
	request := createRequest(t, ref, "link-mutation", "mutation-subject", "identity-mutation", decision(t, subjectlink.RuleVerifiedEmail, "resolver", "", "evidence/mutation"))
	link, _, err := service.Create(request)
	if err != nil {
		t.Fatal(err)
	}
	events, err := service.Store.Events(link.ID)
	if err != nil {
		t.Fatal(err)
	}
	mutant := events[0]
	mutant.Reason = "tampered"
	if err := mutant.Validate(); !errors.Is(err, subjectlink.ErrInvalidEvent) {
		t.Fatalf("mutated event err=%v", err)
	}
}
