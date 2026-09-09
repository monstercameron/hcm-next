package consent

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	consentFrom  = "2026-01-01T00:00:00Z"
	consentAt    = "2026-02-01T00:00:00Z"
	consentLater = "2026-03-01T00:00:00Z"
)

func mustInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	var instant values.Instant
	if err := instant.UnmarshalText([]byte(text)); err != nil {
		t.Fatalf("parse instant %q: %v", text, err)
	}
	return instant
}

func fixtureNotice(t *testing.T, basis LawfulBasis) Notice {
	t.Helper()
	n := Notice{
		ID: "notice-1", Version: "v3", Controller: "acme-hr", Purpose: "wellbeing",
		DataCategories: []string{"health", "contact"}, Operations: []string{"analyze"},
		RecipientClasses: []string{"wellbeing-provider"}, Jurisdiction: "US-NY", Locale: "en-US",
		LegalContext: "wellbeing programme policy 2026", Basis: basis,
		EffectiveFrom: mustInstant(t, consentFrom),
	}
	if err := n.Validate(); err != nil {
		t.Fatalf("fixture notice: %v", err)
	}
	return n
}

func fixtureGrant(t *testing.T) (Notice, Presentation, ConsentDecision) {
	t.Helper()
	n := fixtureNotice(t, BasisConsent)
	p, err := NewPresentation("presentation-1", "worker-1", n, "en-US", "portal", true, mustInstant(t, consentAt), mustInstant(t, consentAt))
	if err != nil {
		t.Fatalf("presentation: %v", err)
	}
	d, err := Grant(GrantRequest{
		ID: "consent-1", Subject: "worker-1", Controller: n.Controller, Purpose: n.Purpose,
		Scope: []string{"wellbeing"}, Notice: n, Presentation: p, Affirmative: true, GrantedAt: mustInstant(t, consentAt),
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}
	return n, p, d
}

func TestTodo_TRUST_024(t *testing.T) {
	n, p, grant := fixtureGrant(t)
	decision := Resolve(AuthorityRequest{
		Subject: "worker-1", Controller: n.Controller, Purpose: n.Purpose, Operation: "analyze",
		DataCategories: []string{"health"}, Notice: n, Presentation: &p, Consent: &grant, At: mustInstant(t, consentLater),
	})
	if !decision.AllowsUse() {
		t.Fatalf("valid affirmative consent was not allowed: %+v", decision)
	}
	if decision.EvidenceID == "" || !strings.Contains(decision.Explain(), "ALLOWED") {
		t.Fatalf("decision lacks safe evidence explanation: %+v", decision)
	}

	withdrawn, err := grant.Withdraw(mustInstant(t, consentLater), "worker revoked optional processing")
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	blocked := Resolve(AuthorityRequest{
		Subject: "worker-1", Controller: n.Controller, Purpose: n.Purpose, Operation: "analyze",
		Notice: n, Presentation: &p, Consent: &withdrawn, At: mustInstant(t, consentLater),
	})
	if blocked.Status != AuthorityRestricted || blocked.AllowsUse() {
		t.Fatalf("withdrawal did not fail closed: %+v", blocked)
	}
	wantObligations := []Obligation{ObligationDeleteDerived, ObligationNotifyConsumers, ObligationReevaluate, ObligationStopProcessing}
	if !equalObligations(blocked.Obligations, wantObligations) {
		t.Fatalf("withdrawal obligations = %v, want %v", blocked.Obligations, wantObligations)
	}

	binding, err := NewConsumerBinding("binding-1", "analytics", "worker-1", n.Purpose, n.Version, []string{"health"})
	if err != nil {
		t.Fatalf("binding: %v", err)
	}
	pending := Reconcile(blocked, []ConsumerBinding{binding}, nil)
	if pending.Complete || len(pending.Pending) != 1 {
		t.Fatalf("missing propagation acknowledgement was not visible: %+v", pending)
	}
	receipt, err := Acknowledge(binding, blocked, mustInstant(t, consentLater))
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	complete := Reconcile(blocked, []ConsumerBinding{binding}, []PropagationReceipt{receipt})
	if !complete.Complete || len(complete.Pending) != 0 {
		t.Fatalf("propagation did not reconcile: %+v", complete)
	}

	employmentNotice := fixtureNotice(t, BasisEmployment)
	employment := Resolve(AuthorityRequest{
		Subject: "worker-1", Controller: employmentNotice.Controller, Purpose: employmentNotice.Purpose,
		Operation: "analyze", Notice: employmentNotice, At: mustInstant(t, consentAt),
	})
	if !employment.AllowsUse() || employment.Basis != BasisEmployment {
		t.Fatalf("explicit employment basis was mislabeled or denied: %+v", employment)
	}
	if _, err := Grant(GrantRequest{ID: "wrong", Subject: "worker-1", Controller: employmentNotice.Controller, Purpose: employmentNotice.Purpose, Scope: []string{"retain"}, Notice: employmentNotice, Presentation: p, Affirmative: true, GrantedAt: mustInstant(t, consentAt)}); !errors.Is(err, ErrConsentBasis) {
		t.Fatalf("Grant(employment basis) error = %v, want ErrConsentBasis", err)
	}
}

func TestTodo_TRUST_024_Security(t *testing.T) {
	n, p, grant := fixtureGrant(t)
	badNotice := n
	badNotice.LegalContext = ""
	unknown := Resolve(AuthorityRequest{Subject: "worker-1", Controller: n.Controller, Purpose: n.Purpose, Operation: "analyze", Notice: badNotice, At: mustInstant(t, consentLater)})
	if unknown.Status != AuthorityUnknown || unknown.AllowsUse() {
		t.Fatalf("missing legal context did not fail closed: %+v", unknown)
	}

	tampered := grant
	tampered.Scope = []string{"wellbeing", "payroll"}
	if _, err := tampered.Withdraw(mustInstant(t, consentLater), "withdraw"); !errors.Is(err, ErrInvalidConsent) {
		t.Fatalf("tampered consent was accepted: %v", err)
	}
	wrongSubject := Resolve(AuthorityRequest{Subject: "worker-2", Controller: n.Controller, Purpose: n.Purpose, Operation: "analyze", Notice: n, Presentation: &p, Consent: &grant, At: mustInstant(t, consentLater)})
	if wrongSubject.AllowsUse() || wrongSubject.Code != CodePresentationInvalid {
		t.Fatalf("cross-subject evidence was accepted: %+v", wrongSubject)
	}

	restriction, err := ApplyRestriction("restriction-1", "worker-1", n.Controller, n.Purpose, "r1", "subject requested processing restriction", []string{"health"}, mustInstant(t, consentLater))
	if err != nil {
		t.Fatalf("restriction: %v", err)
	}
	restricted := Resolve(AuthorityRequest{Subject: "worker-1", Controller: n.Controller, Purpose: n.Purpose, Operation: "analyze", Notice: n, Presentation: &p, Consent: &grant, Restriction: &restriction, At: mustInstant(t, consentLater)})
	if restricted.Status != AuthorityRestricted || restricted.AllowsUse() || restricted.Code != CodeRestrictionActive {
		t.Fatalf("downstream use after restriction was accepted: %+v", restricted)
	}
	if _, err := Acknowledge(ConsumerBinding{}, restricted, mustInstant(t, consentLater)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid binding acknowledgement error = %v", err)
	}
}

func TestTodo_TRUST_024_Mutation(t *testing.T) {
	n := fixtureNotice(t, BasisConsent)
	baseDigest := n.Digest()
	mutants := []func(*Notice){
		func(v *Notice) { v.Version = "v4" }, func(v *Notice) { v.LegalContext = "different" },
		func(v *Notice) { v.Basis = BasisContract }, func(v *Notice) { v.DataCategories[0] = "salary" },
	}
	for _, mutate := range mutants {
		copyNotice := n
		copyNotice.DataCategories = append([]string(nil), n.DataCategories...)
		mutate(&copyNotice)
		if got := copyNotice.Digest(); got == baseDigest {
			t.Fatalf("notice mutation retained digest %q", got)
		}
	}
	_, _, grant := fixtureGrant(t)
	withdrawn, err := grant.Withdraw(mustInstant(t, consentLater), "no longer needed")
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if grant.StatusAt(mustInstant(t, consentLater)) != ConsentGranted || withdrawn.StatusAt(mustInstant(t, consentLater)) != ConsentWithdrawn {
		t.Fatalf("withdrawal mutated the original or failed boundary resolution")
	}
	if _, err := withdrawn.Withdraw(mustInstant(t, consentLater), "again"); !errors.Is(err, ErrAlreadyWithdrawn) {
		t.Fatalf("second withdrawal error = %v, want ErrAlreadyWithdrawn", err)
	}
	resolved, err := SubmitObjection("obj-1", "worker-1", "acme-hr", "wellbeing", "o2", "challenge", []string{"health"}, mustInstant(t, consentAt))
	if err != nil {
		t.Fatalf("objection: %v", err)
	}
	closed, err := resolved.Resolve("privacy-reviewer", "processing basis confirmed", mustInstant(t, consentLater))
	if err != nil || closed.ActiveAt(mustInstant(t, consentLater)) {
		t.Fatalf("resolved objection remained active: %v %+v", err, closed)
	}
}

func FuzzTodo_TRUST_024(f *testing.F) {
	f.Add("", "", "", "", "")
	f.Add("id", "subject", "purpose", "US", "context")
	f.Fuzz(func(t *testing.T, id, subject, purpose, jurisdiction, context string) {
		n := Notice{ID: id, Version: "v1", Controller: "controller", Purpose: purpose, DataCategories: []string{"category"}, Operations: []string{"read"}, RecipientClasses: []string{"internal"}, Jurisdiction: jurisdiction, Locale: "en-US", LegalContext: context, Basis: BasisConsent, EffectiveFrom: mustInstant(t, consentFrom)}
		decision := Resolve(AuthorityRequest{Subject: subject, Controller: "controller", Purpose: purpose, Operation: "read", Notice: n, At: mustInstant(t, consentAt)})
		if err := n.Validate(); err != nil && decision.AllowsUse() {
			t.Fatalf("invalid fuzz notice allowed processing: %+v", decision)
		}
	})
}

func equalObligations(a, b []Obligation) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
