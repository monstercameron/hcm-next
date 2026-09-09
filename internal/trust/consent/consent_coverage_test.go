package consent

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestConsent_NoticeAndPresentationValidation(t *testing.T) {
	if Version() != 1 {
		t.Fatalf("Version() = %d, want 1", Version())
	}
	n := fixtureNotice(t, BasisConsent)
	from, at, later := mustInstant(t, consentFrom), mustInstant(t, consentAt), mustInstant(t, consentLater)
	if !n.ActiveAt(from) || !n.ActiveAt(at) || n.ActiveAt(values.Instant{}) {
		t.Fatal("valid notice active window is incorrect")
	}
	bounded := n
	bounded.EffectiveTo = later
	if !bounded.ActiveAt(at) || bounded.ActiveAt(later) {
		t.Fatal("notice effective-to boundary is incorrect")
	}
	invalids := []struct {
		name   string
		mutate func(*Notice)
	}{
		{"id", func(v *Notice) { v.ID = "" }},
		{"version", func(v *Notice) { v.Version = " " }},
		{"controller", func(v *Notice) { v.Controller = "" }},
		{"purpose", func(v *Notice) { v.Purpose = " purpose" }},
		{"jurisdiction", func(v *Notice) { v.Jurisdiction = "" }},
		{"locale", func(v *Notice) { v.Locale = "en-US " }},
		{"legal context", func(v *Notice) { v.LegalContext = "" }},
		{"basis", func(v *Notice) { v.Basis = BasisUnspecified }},
		{"categories", func(v *Notice) { v.DataCategories = []string{"health", "health"} }},
		{"operations", func(v *Notice) { v.Operations = nil }},
		{"recipients", func(v *Notice) { v.RecipientClasses = []string{""} }},
		{"effective from", func(v *Notice) { v.EffectiveFrom = values.Instant{} }},
		{"effective to", func(v *Notice) { v.EffectiveTo = v.EffectiveFrom }},
	}
	for _, tc := range invalids {
		t.Run(tc.name, func(t *testing.T) {
			bad := n
			bad.DataCategories = append([]string(nil), n.DataCategories...)
			bad.Operations = append([]string(nil), n.Operations...)
			bad.RecipientClasses = append([]string(nil), n.RecipientClasses...)
			tc.mutate(&bad)
			if !errors.Is(bad.Validate(), ErrInvalidNotice) || bad.ActiveAt(at) {
				t.Fatalf("invalid notice Validate=%v ActiveAt=%v", bad.Validate(), bad.ActiveAt(at))
			}
		})
	}
	p, err := NewPresentation("presentation-1", "worker-1", n, "en-US", "portal", true, at, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateFor(n, "worker-1"); err != nil {
		t.Fatal(err)
	}
	presentationInvalids := []struct {
		name   string
		mutate func(*Presentation)
	}{
		{"id", func(v *Presentation) { v.ID = "" }},
		{"subject", func(v *Presentation) { v.Subject = "" }},
		{"locale", func(v *Presentation) { v.Locale = "" }},
		{"channel", func(v *Presentation) { v.Channel = "" }},
		{"inaccessible", func(v *Presentation) { v.Accessible = false }},
		{"notice binding", func(v *Presentation) { v.NoticeDigest = "tampered" }},
		{"presented time", func(v *Presentation) { v.PresentedAt = values.Instant{} }},
		{"ack time", func(v *Presentation) { v.AcknowledgedAt = values.Instant{} }},
		{"ack precedes presentation", func(v *Presentation) { v.AcknowledgedAt = from }},
	}
	for _, tc := range presentationInvalids {
		t.Run("presentation "+tc.name, func(t *testing.T) {
			bad := p
			tc.mutate(&bad)
			if !errors.Is(bad.ValidateFor(n, "worker-1"), ErrInvalidPresentation) {
				t.Fatalf("ValidateFor = %v", bad.ValidateFor(n, "worker-1"))
			}
		})
	}
	if _, err := NewPresentation("", "worker-1", n, "en-US", "portal", true, at, at); !errors.Is(err, ErrInvalidPresentation) {
		t.Fatalf("NewPresentation(invalid) = %v", err)
	}
	if err := p.ValidateFor(n, "worker-2"); !errors.Is(err, ErrInvalidPresentation) {
		t.Fatalf("subject mismatch = %v", err)
	}
	other := n
	other.Version = "v4"
	if err := p.ValidateFor(other, "worker-1"); !errors.Is(err, ErrInvalidPresentation) {
		t.Fatalf("notice version mismatch = %v", err)
	}
	if !strings.Contains(n.Digest(), "") || n.Digest() == other.Digest() {
		t.Fatal("notice digest did not identify notice content")
	}
}

func instantPlus(t *testing.T, instant values.Instant, delta int64) values.Instant {
	t.Helper()
	return values.NewInstant(instant.Time().Add(timeDuration(delta)))
}

func timeDuration(n int64) time.Duration { return time.Duration(n) }

func TestConsent_GrantRefuseWithdrawAndStatus(t *testing.T) {
	n, p, grant := fixtureGrant(t)
	if err := grant.Validate(); err != nil {
		t.Fatal(err)
	}
	if grant.StatusAt(mustInstant(t, consentAt)) != ConsentGranted || grant.StatusAt(mustInstant(t, consentLater)) != ConsentGranted {
		t.Fatal("granted consent status is incorrect")
	}
	if grant.StatusAt(mustInstant(t, consentFrom)) != ConsentInvalid || grant.StatusAt(values.Instant{}) != ConsentInvalid {
		t.Fatal("consent before grant or at zero was not invalid")
	}
	base := GrantRequest{ID: "consent-2", Subject: "worker-1", Controller: n.Controller, Purpose: n.Purpose, Scope: []string{n.Purpose}, Notice: n, Presentation: p, Affirmative: true, GrantedAt: mustInstant(t, consentAt)}
	if _, err := Grant(base); err != nil {
		t.Fatal(err)
	}
	invalid := []struct {
		name   string
		mutate func(*GrantRequest)
		want   error
	}{
		{"controller", func(v *GrantRequest) { v.Controller = "other" }, ErrInvalidConsent},
		{"not affirmative", func(v *GrantRequest) { v.Affirmative = false }, ErrInvalidConsent},
		{"duplicate scope", func(v *GrantRequest) { v.Scope = []string{"wellbeing", "wellbeing"} }, ErrInvalidConsent},
		{"invalid grant time", func(v *GrantRequest) { v.GrantedAt = values.Instant{} }, ErrInvalidConsent},
		{"expiry at grant", func(v *GrantRequest) { v.ExpiresAt = v.GrantedAt }, ErrInvalidConsent},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			bad := base
			bad.Scope = append([]string(nil), base.Scope...)
			tc.mutate(&bad)
			if _, err := Grant(bad); !errors.Is(err, tc.want) {
				t.Fatalf("Grant = %v, want %v", err, tc.want)
			}
		})
	}
	employment := n
	employment.Basis = BasisEmployment
	if _, err := Grant(GrantRequest{Notice: employment}); !errors.Is(err, ErrConsentBasis) {
		t.Fatalf("Grant(non-consent) = %v", err)
	}
	refused, err := Refuse(base, "declined")
	if err != nil || refused.State != ConsentRefused || refused.Reason != "declined" {
		t.Fatalf("Refuse = %+v, %v", refused, err)
	}
	if _, err := Refuse(base, " "); !errors.Is(err, ErrInvalidConsent) {
		t.Fatalf("Refuse(blank reason) = %v", err)
	}
	withdrawn, err := grant.Withdraw(mustInstant(t, consentLater), "no longer needed")
	if err != nil || withdrawn.State != ConsentWithdrawn || grant.State != ConsentGranted || withdrawn.StatusAt(mustInstant(t, consentLater)) != ConsentWithdrawn {
		t.Fatalf("Withdraw = %+v, %v; original=%+v", withdrawn, err, grant)
	}
	if _, err := withdrawn.Withdraw(mustInstant(t, consentLater), "again"); !errors.Is(err, ErrAlreadyWithdrawn) {
		t.Fatalf("second Withdraw = %v", err)
	}
	if _, err := refused.Withdraw(mustInstant(t, consentLater), "bad"); !errors.Is(err, ErrInvalidConsent) {
		t.Fatalf("Withdraw(refused) = %v", err)
	}
	for _, badAt := range []values.Instant{values.Instant{}, mustInstant(t, consentFrom)} {
		if _, err := grant.Withdraw(badAt, "reason"); !errors.Is(err, ErrInvalidConsent) {
			t.Errorf("Withdraw(%s) = %v", badAt, err)
		}
	}
	tampered := grant
	tampered.EvidenceID = "ev:consent:forged"
	if !errors.Is(tampered.Validate(), ErrInvalidConsent) {
		t.Fatal("tampered consent evidence was accepted")
	}
	unknown := grant
	unknown.State = ConsentState("FORGED")
	if !errors.Is(unknown.Validate(), ErrInvalidConsent) {
		t.Fatal("unknown consent state was accepted")
	}
	nonAffirmative := grant
	nonAffirmative.Affirmative = false
	if !errors.Is(nonAffirmative.Validate(), ErrInvalidConsent) {
		t.Fatal("non-affirmative grant was accepted")
	}
	expiring := grant
	expiring.ExpiresAt = mustInstant(t, consentLater)
	expiring.EvidenceID = expiring.evidenceID()
	if expiring.StatusAt(mustInstant(t, consentLater)) != ConsentExpired {
		t.Fatalf("expired consent status = %s", expiring.StatusAt(mustInstant(t, consentLater)))
	}
}

func TestConsent_PreferenceObjectionRestrictionLifecycles(t *testing.T) {
	from, to := mustInstant(t, consentAt), mustInstant(t, consentLater)
	pref, err := NewPreference("pref-1", "worker-1", "wellbeing", "v1", "portal", PreferenceOptOut, from, to)
	if err != nil || !pref.ActiveAt(from) || pref.ActiveAt(to) {
		t.Fatalf("preference = %+v, %v", pref, err)
	}
	if pref.ActiveAt(values.Instant{}) {
		t.Fatal("unset preference evaluation time was active")
	}
	if !errors.Is((func() error { p := pref; p.EvidenceID = "bad"; return p.Validate() })(), ErrInvalidPreference) {
		t.Fatal("tampered preference was accepted")
	}
	if _, err := NewPreference("p", "s", "p", "v", "src", PreferenceValue("UNKNOWN"), from, values.Instant{}); !errors.Is(err, ErrInvalidPreference) {
		t.Fatalf("invalid preference value = %v", err)
	}
	if _, err := NewPreference("p", "s", "p", "v", "src", PreferenceOptIn, to, from); !errors.Is(err, ErrInvalidPreference) {
		t.Fatalf("backward preference window = %v", err)
	}
	objection, err := SubmitObjection("obj-1", "worker-1", "acme-hr", "wellbeing", "v1", "challenge", []string{"health"}, from)
	if err != nil || !objection.ActiveAt(from) || !objection.ActiveAt(to) {
		t.Fatalf("objection = %+v, %v", objection, err)
	}
	resolved, err := objection.Resolve("reviewer", "confirmed", to)
	if err != nil || resolved.State != ObjectionResolved || !resolved.ActiveAt(from) || resolved.ActiveAt(to) {
		t.Fatalf("resolved objection = %+v, %v", resolved, err)
	}
	if _, err := resolved.Resolve("reviewer", "again", to); !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("second objection Resolve = %v", err)
	}
	if _, err := objection.Resolve("", "confirmed", to); !errors.Is(err, ErrInvalidObjection) {
		t.Fatalf("blank objection resolver = %v", err)
	}
	badObjection := objection
	badObjection.EvidenceID = "bad"
	if !errors.Is(badObjection.Validate(), ErrInvalidObjection) {
		t.Fatal("tampered objection was accepted")
	}
	restriction, err := ApplyRestriction("restriction-1", "worker-1", "acme-hr", "wellbeing", "v1", "stop", []string{"health"}, from)
	if err != nil || !restriction.ActiveAt(from) {
		t.Fatalf("restriction = %+v, %v", restriction, err)
	}
	released, err := restriction.Release("reviewer", to)
	if err != nil || released.ReleasedBy != "reviewer" || !released.ActiveAt(from) || released.ActiveAt(to) {
		t.Fatalf("released restriction = %+v, %v", released, err)
	}
	if _, err := released.Release("reviewer", to); !errors.Is(err, ErrAlreadyReleased) {
		t.Fatalf("second restriction Release = %v", err)
	}
	if _, err := restriction.Release("", to); !errors.Is(err, ErrInvalidRestriction) {
		t.Fatalf("blank restriction releaser = %v", err)
	}
	badRestriction := restriction
	badRestriction.EvidenceID = "bad"
	if !errors.Is(badRestriction.Validate(), ErrInvalidRestriction) {
		t.Fatal("tampered restriction was accepted")
	}
}

func consentAuthorityRequest(t *testing.T) (Notice, Presentation, ConsentDecision, AuthorityRequest) {
	t.Helper()
	n, p, d := fixtureGrant(t)
	return n, p, d, AuthorityRequest{Subject: "worker-1", Controller: n.Controller, Purpose: n.Purpose, Operation: "analyze", DataCategories: []string{"health"}, Notice: n, Presentation: &p, Consent: &d, At: mustInstant(t, consentLater)}
}

func TestConsent_ResolveAllSecurityDecisions(t *testing.T) {
	n, p, grant, base := consentAuthorityRequest(t)
	allowed := Resolve(base)
	if !allowed.AllowsUse() || allowed.Status != AuthorityAllowed || allowed.Code != CodeAllowed || allowed.EvidenceID == "" || !strings.Contains(allowed.Explain(), "ALLOWED") {
		t.Fatalf("allowed decision = %+v", allowed)
	}
	mutations := []struct {
		name   string
		mutate func(*AuthorityRequest)
		status AuthorityStatus
		code   AuthorityCode
	}{
		{"invalid request", func(v *AuthorityRequest) { v.Subject = "" }, AuthorityUnknown, CodeInvalidRequest},
		{"invalid notice", func(v *AuthorityRequest) { v.Notice.LegalContext = "" }, AuthorityUnknown, CodeInvalidNotice},
		{"controller mismatch", func(v *AuthorityRequest) { v.Controller = "other" }, AuthorityDenied, CodeInvalidNotice},
		{"inactive notice", func(v *AuthorityRequest) {
			v.Notice.EffectiveFrom = instantPlus(t, mustInstant(t, consentLater), timeSecond)
		}, AuthorityDenied, CodeNoticeInactive},
		{"operation absent", func(v *AuthorityRequest) { v.Operation = "delete" }, AuthorityDenied, CodeOperationNotDeclared},
		{"category absent", func(v *AuthorityRequest) { v.DataCategories = []string{"salary"} }, AuthorityDenied, CodeDataCategoryNotDeclared},
		{"presentation missing", func(v *AuthorityRequest) { v.Presentation = nil }, AuthorityDenied, CodeNoticeNotPresented},
		{"consent missing", func(v *AuthorityRequest) { v.Consent = nil }, AuthorityDenied, CodeConsentMissing},
		{"consent out of scope", func(v *AuthorityRequest) {
			c := grant
			c.Scope = []string{"other"}
			c.EvidenceID = c.evidenceID()
			v.Consent = &c
		}, AuthorityDenied, CodeConsentOutOfScope},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			request := base
			tc.mutate(&request)
			decision := Resolve(request)
			if decision.Status != tc.status || decision.Code != tc.code || decision.AllowsUse() {
				t.Fatalf("Resolve = %+v, want %s/%s deny-or-unknown", decision, tc.status, tc.code)
			}
		})
	}
	ackAfter := base
	ackAfter.Presentation = func() *Presentation {
		copy := p
		copy.AcknowledgedAt = instantPlus(t, mustInstant(t, consentLater), timeSecond)
		return &copy
	}()
	if decision := Resolve(ackAfter); decision.Code != CodeNoticeNotPresented {
		t.Fatalf("future acknowledgement decision = %+v", decision)
	}
	refused, err := Refuse(GrantRequest{ID: "refuse-1", Subject: "worker-1", Controller: n.Controller, Purpose: n.Purpose, Scope: []string{n.Purpose}, Notice: n, Presentation: p, Affirmative: true, GrantedAt: mustInstant(t, consentAt)}, "declined")
	if err != nil {
		t.Fatal(err)
	}
	refusedReq := base
	refusedReq.Consent = &refused
	if decision := Resolve(refusedReq); decision.Code != CodeConsentNotAffirmed {
		t.Fatalf("refused consent decision = %+v", decision)
	}
	expired := grant
	expired.ExpiresAt = instantPlus(t, mustInstant(t, consentAt), timeSecond)
	expired.EvidenceID = expired.evidenceID()
	expiredReq := base
	expiredReq.Consent = &expired
	if decision := Resolve(expiredReq); decision.Code != CodeConsentExpired {
		t.Fatalf("expired consent decision = %+v", decision)
	}
	withdrawn, err := grant.Withdraw(mustInstant(t, consentLater), "withdrawn")
	if err != nil {
		t.Fatal(err)
	}
	withdrawnReq := base
	withdrawnReq.Consent = &withdrawn
	if decision := Resolve(withdrawnReq); decision.Status != AuthorityRestricted || decision.Code != CodeConsentWithdrawn || decision.AllowsUse() {
		t.Fatalf("withdrawn consent decision = %+v", decision)
	}
	employment := n
	employment.Basis = BasisEmployment
	employmentReq := base
	employmentReq.Notice = employment
	employmentReq.Presentation, employmentReq.Consent = nil, nil
	if decision := Resolve(employmentReq); !decision.AllowsUse() {
		t.Fatalf("employment basis was not allowed: %+v", decision)
	}
	preference, err := NewPreference("pref", "worker-1", n.Purpose, "v1", "portal", PreferenceOptOut, mustInstant(t, consentAt), values.Instant{})
	if err != nil {
		t.Fatal(err)
	}
	preferenceReq := base
	preferenceReq.Preference = &preference
	if decision := Resolve(preferenceReq); decision.Code != CodePreferenceOptedOut || decision.Status != AuthorityRestricted {
		t.Fatalf("opt-out decision = %+v", decision)
	}
	objection, err := SubmitObjection("obj", "worker-1", n.Controller, n.Purpose, "v1", "challenge", []string{"health"}, mustInstant(t, consentAt))
	if err != nil {
		t.Fatal(err)
	}
	objectionReq := base
	objectionReq.Objection = &objection
	if decision := Resolve(objectionReq); decision.Code != CodeObjectionActive || decision.Status != AuthorityRestricted {
		t.Fatalf("objection decision = %+v", decision)
	}
	restriction, err := ApplyRestriction("r", "worker-1", n.Controller, n.Purpose, "v1", "stop", []string{"health"}, mustInstant(t, consentAt))
	if err != nil {
		t.Fatal(err)
	}
	restrictionReq := base
	restrictionReq.Restriction = &restriction
	if decision := Resolve(restrictionReq); decision.Code != CodeRestrictionActive || decision.Status != AuthorityRestricted {
		t.Fatalf("restriction decision = %+v", decision)
	}
}

const timeSecond = 1_000_000_000

func TestConsent_PropagationBindingsAndReconciliation(t *testing.T) {
	n, _, _, request := consentAuthorityRequest(t)
	authority := Resolve(request)
	binding, err := NewConsumerBinding("binding-1", "analytics", "worker-1", n.Purpose, n.Version, []string{"health"})
	if err != nil || !binding.Active || len(binding.DataCategories) != 1 {
		t.Fatalf("binding = %+v, %v", binding, err)
	}
	original := []string{"health"}
	binding, err = NewConsumerBinding("binding-1", "analytics", "worker-1", n.Purpose, n.Version, original)
	if err != nil {
		t.Fatal(err)
	}
	original[0] = "tampered"
	if binding.DataCategories[0] != "health" {
		t.Fatal("consumer binding aliased caller categories")
	}
	for _, tc := range []struct {
		name       string
		id         string
		categories []string
	}{
		{"blank id", "", []string{"health"}},
		{"duplicate category", "id", []string{"health", "health"}},
	} {
		if _, err := NewConsumerBinding(tc.id, "consumer", "subject", "purpose", "v1", tc.categories); !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("NewConsumerBinding(%s) = %v", tc.name, err)
		}
	}
	receipt, err := Acknowledge(binding, authority, mustInstant(t, consentLater))
	if err != nil || receipt.Status != ReceiptAcknowledged || receipt.EvidenceID == "" || receipt.AuthorityEvidenceID != authority.EvidenceID {
		t.Fatalf("receipt = %+v, %v", receipt, err)
	}
	if _, err := Acknowledge(ConsumerBinding{}, authority, mustInstant(t, consentLater)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid binding acknowledge = %v", err)
	}
	if _, err := Acknowledge(binding, AuthorityDecision{}, mustInstant(t, consentLater)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("evidence-less authority acknowledge = %v", err)
	}
	if _, err := Acknowledge(binding, authority, values.Instant{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid acknowledgement time = %v", err)
	}
	inactive := binding
	inactive.Active = false
	if _, err := Acknowledge(inactive, authority, mustInstant(t, consentLater)); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("inactive binding acknowledge = %v", err)
	}
	inactiveReceipt := receipt
	inactiveReceipt.EvidenceID = "bad"
	inactiveBinding := binding
	inactiveBinding.Active = false
	reconciled := Reconcile(authority, []ConsumerBinding{inactiveBinding, binding}, []PropagationReceipt{inactiveReceipt, receipt})
	if !reconciled.Complete || len(reconciled.Pending) != 0 || len(reconciled.Acknowledged) != 1 || reconciled.Acknowledged[0] != binding.ID || reconciled.EvidenceID == "" {
		t.Fatalf("reconciliation = %+v", reconciled)
	}
	second, err := NewConsumerBinding("binding-2", "reports", "worker-1", n.Purpose, n.Version, []string{"contact"})
	if err != nil {
		t.Fatal(err)
	}
	pending := Reconcile(authority, []ConsumerBinding{second, binding}, []PropagationReceipt{receipt})
	if pending.Complete || len(pending.Pending) != 1 || pending.Pending[0] != second.ID || len(pending.Acknowledged) != 1 {
		t.Fatalf("pending reconciliation = %+v", pending)
	}
	failed := receipt
	failed.Status = ReceiptFailed
	failed.EvidenceID = "ev:propagation:" + receiptDigest(failed)
	if result := Reconcile(authority, []ConsumerBinding{binding}, []PropagationReceipt{failed}); result.Complete {
		t.Fatal("failed receipt was treated as acknowledgement")
	}
}
