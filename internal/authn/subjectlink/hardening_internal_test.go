package subjectlink

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/authn/issuerregistry"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	trustfederation "github.com/monstercameron/hcm-next/internal/trust/federation"
)

var hardAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func TestDecisionAndIdentityValidation_RejectsBoundaryAndApprovalFailures(t *testing.T) {
	t.Parallel()
	identity := WorkforceIdentityRef{Tenant: values.TenantId("tenant-a"), ID: "identity-1"}
	if err := identity.Validate(); err != nil {
		t.Fatalf("valid WorkforceIdentityRef rejected: %v", err)
	}
	for name, mutant := range map[string]WorkforceIdentityRef{
		"missing tenant": {ID: "identity-1"},
		"missing id":     {Tenant: values.TenantId("tenant-a")},
		"trimmed id":     {Tenant: values.TenantId("tenant-a"), ID: " identity-1"},
		"newline id":     {Tenant: values.TenantId("tenant-a"), ID: "identity\n1"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := mutant.Validate(); !errors.Is(err, ErrInvalidLink) {
				t.Fatalf("Validate() = %v, want ErrInvalidLink", err)
			}
		})
	}

	valid, err := NewDecision(RuleVerifiedEmail, "resolver", "", "authority", "evidence/1", hardAt)
	if err != nil || valid.Digest == "" || valid.At.Location() != time.UTC {
		t.Fatalf("NewDecision valid = %+v,%v", valid, err)
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid decision rejected: %v", err)
	}
	manual, err := NewDecision(RuleManualReview, "requester", "approver", "authority", "case/1", hardAt)
	if err != nil {
		t.Fatalf("valid manual decision rejected: %v", err)
	}
	if err := manual.Validate(); err != nil {
		t.Fatalf("valid manual decision validation: %v", err)
	}
	for name, args := range map[string]struct {
		rule                                 MatchingRule
		actor, approver, authority, evidence string
		at                                   time.Time
	}{
		"unknown rule":            {MatchingRule("UNKNOWN"), "actor", "", "authority", "evidence", hardAt},
		"missing actor":           {RuleVerifiedEmail, "", "", "authority", "evidence", hardAt},
		"missing evidence":        {RuleVerifiedEmail, "actor", "", "authority", "", hardAt},
		"zero time":               {RuleVerifiedEmail, "actor", "", "authority", "evidence", time.Time{}},
		"manual missing approver": {RuleManualReview, "actor", "", "authority", "evidence", hardAt},
		"manual self approval":    {RuleManualReview, "actor", "actor", "authority", "evidence", hardAt},
		"ordinary self approval":  {RuleVerifiedEmail, "actor", "actor", "authority", "evidence", hardAt},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewDecision(args.rule, args.actor, args.approver, args.authority, args.evidence, args.at); !errors.Is(err, ErrInvalidDecision) {
				t.Fatalf("NewDecision() = %v, want ErrInvalidDecision", err)
			}
		})
	}
	mutated := valid
	mutated.Digest = ""
	if err := mutated.Validate(); !errors.Is(err, ErrInvalidDecision) {
		t.Fatalf("empty decision digest = %v", err)
	}
	mutated = valid
	mutated.EvidenceRef = "changed"
	if err := mutated.Validate(); !errors.Is(err, ErrInvalidDecision) {
		t.Fatalf("tampered decision = %v", err)
	}
}

func TestValidationHelpers_ClosedValuesAndDigestBoundaries(t *testing.T) {
	t.Parallel()
	for _, rule := range []MatchingRule{RuleVerifiedEmail, RuleHRProvidedExternal, RuleManualReview} {
		if !rule.valid() {
			t.Fatalf("valid rule %q rejected", rule)
		}
	}
	if MatchingRule("invalid").valid() {
		t.Fatalf("unknown matching rule accepted")
	}
	for _, state := range []Lifecycle{LifecycleLinked, LifecycleSuspended, LifecycleUnlinked} {
		if !state.valid() {
			t.Fatalf("valid lifecycle %q rejected", state)
		}
	}
	if Lifecycle("invalid").valid() {
		t.Fatalf("unknown lifecycle accepted")
	}
	if safeRequired(" value") != "" || safeRequired("value\x00") != "" || safeRequired("value\n") != "" || safeRequired(" value ") != "" || safeRequired("") != "" || safeRequired("value") != "value" {
		t.Fatalf("safeRequired did not enforce log-safe, untrimmed values")
	}
	ref := hardRef()
	for name, mutant := range map[string]issuerregistry.Ref{
		"missing tenant":   {IssuerURL: ref.IssuerURL, Revision: 1},
		"missing url":      {Tenant: ref.Tenant, Revision: 1},
		"missing revision": {Tenant: ref.Tenant, IssuerURL: ref.IssuerURL},
		"bad url":          {Tenant: ref.Tenant, IssuerURL: "not a url", Revision: 1},
	} {
		t.Run("issuer ref "+name, func(t *testing.T) {
			if err := validateIssuerRef(mutant); !errors.Is(err, ErrInvalidLink) {
				t.Fatalf("validateIssuerRef() = %v", err)
			}
		})
	}
	goodDigest := DigestSubject("subject")
	if err := validateSubjectDigest(goodDigest); err != nil || validateDigest(goodDigest) != nil {
		t.Fatalf("valid digest rejected: %v", err)
	}
	for _, bad := range []string{"", strings.ToUpper(goodDigest), goodDigest[:63], goodDigest[:63] + "g"} {
		if err := validateSubjectDigest(bad); !errors.Is(err, ErrSubjectDigest) {
			t.Fatalf("validateSubjectDigest(%q) = %v", bad, err)
		}
		if err := validateDigest(bad); !errors.Is(err, ErrInvalidLink) {
			t.Fatalf("validateDigest(%q) = %v", bad, err)
		}
	}
	if subjectKey(ref, goodDigest) == subjectKey(ref, DigestSubject("other")) || identityKey(ref, WorkforceIdentityRef{ID: "a"}) == identityKey(ref, WorkforceIdentityRef{ID: "b"}) {
		t.Fatalf("store keys do not distinguish their inputs")
	}
	if digestParts("ab", "c") == digestParts("a", "bc") || digestParts("x") != digestParts("x") {
		t.Fatalf("digestParts lacks length framing or determinism")
	}
	decision, err := NewDecision(RuleVerifiedEmail, "resolver", "", "authority", "evidence", hardAt)
	if err != nil {
		t.Fatal(err)
	}
	if digestDecision(decision) != decision.Digest {
		t.Fatalf("digestDecision does not match NewDecision digest")
	}
	if DigestSubjectForIssuer(ref, "subject") == DigestSubjectForIssuer(ref, "other") || DigestSubjectForIssuer(ref, "subject") == DigestSubject("subject") {
		t.Fatalf("issuer-namespaced digest is not input/namespace bound")
	}
	if !allowedTransition(LifecycleLinked, LifecycleSuspended) || !allowedTransition(LifecycleLinked, LifecycleUnlinked) || !allowedTransition(LifecycleSuspended, LifecycleLinked) || !allowedTransition(LifecycleSuspended, LifecycleUnlinked) {
		t.Fatalf("permitted lifecycle transition rejected")
	}
	for _, pair := range [][2]Lifecycle{{LifecycleLinked, LifecycleLinked}, {LifecycleSuspended, LifecycleSuspended}, {LifecycleUnlinked, LifecycleLinked}, {LifecycleUnlinked, LifecycleSuspended}, {LifecycleLinked, Lifecycle("invalid")}} {
		if allowedTransition(pair[0], pair[1]) {
			t.Fatalf("forbidden transition %q -> %q accepted", pair[0], pair[1])
		}
	}
}

func TestLinkAndEventValidation_RejectTampering(t *testing.T) {
	t.Parallel()
	link, event := hardValidLink(t)
	if err := link.Validate(); err != nil {
		t.Fatalf("valid link rejected: %v", err)
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("valid event rejected: %v", err)
	}
	linkCases := map[string]func(*Link){
		"empty id":              func(l *Link) { l.ID = "" },
		"bad digest":            func(l *Link) { l.Digest = "not-a-digest" },
		"zero revision":         func(l *Link) { l.Revision = 0 },
		"wrong lifecycle":       func(l *Link) { l.Lifecycle = Lifecycle("invalid") },
		"cross tenant identity": func(l *Link) { l.WorkforceIdentityRef.Tenant = values.TenantId("tenant-b") },
		"missing decision":      func(l *Link) { l.DecisionDigest = "" },
	}
	for name, mutate := range linkCases {
		t.Run("link "+name, func(t *testing.T) {
			mutant := link
			mutate(&mutant)
			if err := mutant.Validate(); !errors.Is(err, ErrInvalidLink) {
				t.Fatalf("Link.Validate() = %v", err)
			}
		})
	}
	eventCases := map[string]func(*LifecycleEvent){
		"empty id":        func(e *LifecycleEvent) { e.LinkID = "" },
		"bad from":        func(e *LifecycleEvent) { e.From = Lifecycle("invalid") },
		"same states":     func(e *LifecycleEvent) { e.From = e.To },
		"zero time":       func(e *LifecycleEvent) { e.At = time.Time{} },
		"cross tenant":    func(e *LifecycleEvent) { e.WorkforceIdentityRef.Tenant = values.TenantId("tenant-b") },
		"empty actor":     func(e *LifecycleEvent) { e.ActedBy = " " },
		"tampered digest": func(e *LifecycleEvent) { e.Digest = "tampered" },
	}
	for name, mutate := range eventCases {
		t.Run("event "+name, func(t *testing.T) {
			mutant := event
			mutate(&mutant)
			if err := mutant.Validate(); !errors.Is(err, ErrInvalidEvent) {
				t.Fatalf("LifecycleEvent.Validate() = %v", err)
			}
		})
	}
	if Explain() == "" || strings.Contains(Explain(), link.SubjectDigest) || strings.Contains(link.Explain(), link.SubjectDigest) {
		t.Fatalf("Explain leaked subject material")
	}
	if got := (LookupResult{Status: LookupWithheld}).Explain(); got != "subject link lookup=WITHHELD" {
		t.Fatalf("LookupResult.Explain() = %q", got)
	}
}

func TestMemoryStore_AtomicIndexesAndAppendOnlyEvents(t *testing.T) {
	t.Parallel()
	link, event := hardValidLink(t)
	store := NewMemoryStore()
	if err := store.Create(link, event); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, found, err := store.Get(link.ID)
	if err != nil || !found || got != link {
		t.Fatalf("Get() = %+v,%v,%v", got, found, err)
	}
	if _, found, err := store.Get("missing"); err != nil || found {
		t.Fatalf("missing Get() = found=%v err=%v", found, err)
	}
	if foundLink, found, err := store.Find(link.IssuerRef, link.SubjectDigest); err != nil || !found || foundLink.ID != link.ID {
		t.Fatalf("Find() = %+v,%v,%v", foundLink, found, err)
	}
	if _, found, err := store.Find(link.IssuerRef, DigestSubject("missing")); err != nil || found {
		t.Fatalf("missing Find() = found=%v err=%v", found, err)
	}
	if err := store.Create(link, event); !errors.Is(err, ErrLinkExists) {
		t.Fatalf("duplicate id = %v", err)
	}
	sameSubject := link
	sameSubject.ID = "link-subject-duplicate"
	sameSubjectEvent := eventForLink(t, sameSubject)
	sameSubject.Digest = sameSubjectEvent.Digest
	if err := store.Create(sameSubject, sameSubjectEvent); !errors.Is(err, ErrDuplicateSubject) {
		t.Fatalf("duplicate subject = %v", err)
	}
	sameIdentity := link
	sameIdentity.ID = "link-identity-duplicate"
	sameIdentity.SubjectDigest = DigestSubject("other")
	sameIdentityEvent := eventForLink(t, sameIdentity)
	sameIdentity.Digest = sameIdentityEvent.Digest
	if err := store.Create(sameIdentity, sameIdentityEvent); !errors.Is(err, ErrDuplicateIdentity) {
		t.Fatalf("duplicate identity = %v", err)
	}

	suspended := link
	suspended.Lifecycle = LifecycleSuspended
	suspended.Revision++
	suspendedEvent := newEvent(link, LifecycleEvidence{ActedBy: "security", Reason: "pause", At: hardAt.Add(time.Hour)}, link.Digest, LifecycleSuspended)
	suspended.Digest = suspendedEvent.Digest
	if err := store.Transition(suspended, suspendedEvent); err != nil {
		t.Fatalf("Transition suspend: %v", err)
	}
	if freshErr := store.Create(sameSubject, sameSubjectEvent); freshErr != nil {
		t.Fatalf("suspended link should release active subject index: %v", freshErr)
	}
	stale := suspended
	stale.Revision++
	if err := store.Transition(stale, suspendedEvent); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("stale transition = %v", err)
	}
	events, err := store.Events(link.ID)
	if err != nil || len(events) != 2 {
		t.Fatalf("Events() = %d,%v", len(events), err)
	}
	events[0].Reason = "mutated copy"
	again, err := store.Events(link.ID)
	if err != nil || again[0].Reason == "mutated copy" || again[1].PreviousDigest != again[0].Digest {
		t.Fatalf("Events() did not preserve immutable chain: %+v,%v", again, err)
	}
	if _, err := store.Events("missing"); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("missing Events() = %v", err)
	}

	var nilStore *MemoryStore
	if err := nilStore.Create(link, event); !errors.Is(err, ErrStore) {
		t.Fatalf("nil Create = %v", err)
	}
	if _, _, err := nilStore.Get("x"); !errors.Is(err, ErrStore) {
		t.Fatalf("nil Get = %v", err)
	}
	if _, _, err := nilStore.Find(link.IssuerRef, link.SubjectDigest); !errors.Is(err, ErrStore) {
		t.Fatalf("nil Find = %v", err)
	}
	if err := nilStore.Transition(link, event); !errors.Is(err, ErrStore) {
		t.Fatalf("nil Transition = %v", err)
	}
	if _, err := nilStore.Events("x"); !errors.Is(err, ErrStore) {
		t.Fatalf("nil Events = %v", err)
	}
}

func TestMemoryStore_RejectsMalformedCommandsAndReactivationConflicts(t *testing.T) {
	t.Parallel()
	link, event := hardValidLink(t)
	zero := &MemoryStore{}
	if err := zero.Create(link, event); err != nil {
		t.Fatalf("zero-value store Create() = %v", err)
	}
	badRevision := link
	badRevision.Revision = 2
	if err := zero.Create(badRevision, event); !errors.Is(err, ErrInvalidLink) {
		t.Fatalf("bad create revision = %v", err)
	}
	badEvent := event
	badEvent.Reason = "tampered"
	if err := zero.Create(Link{ID: "bad-event", IssuerRef: link.IssuerRef, SubjectDigest: DigestSubject("another"), WorkforceIdentityRef: WorkforceIdentityRef{Tenant: link.IssuerRef.Tenant, ID: "identity-2"}, Rule: link.Rule, DecisionDigest: link.DecisionDigest, Lifecycle: LifecycleLinked, Revision: 1, CreatedAt: hardAt, Digest: badEvent.Digest}, badEvent); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("bad create event = %v", err)
	}
	updated := link
	updated.Digest = ""
	if err := zero.Transition(updated, event); !errors.Is(err, ErrInvalidLink) {
		t.Fatalf("invalid updated link = %v", err)
	}
	badTransitionEvent := event
	badTransitionEvent.Reason = "tampered"
	if err := zero.Transition(link, badTransitionEvent); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invalid transition event = %v", err)
	}
	unknown := link
	unknown.ID = "missing"
	unknownEvent := eventForLink(t, unknown)
	unknown.Digest = unknownEvent.Digest
	if err := zero.Transition(unknown, unknownEvent); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("unknown transition = %v", err)
	}
	mismatch := link
	mismatch.SubjectDigest = DigestSubject("changed")
	mismatchEvent := newEvent(mismatch, LifecycleEvidence{ActedBy: "security", At: hardAt}, link.Digest, LifecycleSuspended)
	mismatch.Lifecycle = LifecycleSuspended
	mismatch.Revision++
	mismatch.Digest = mismatchEvent.Digest
	if err := zero.Transition(mismatch, mismatchEvent); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("cross-record transition = %v", err)
	}

	// An active link for the same subject blocks reactivation after a
	// suspension, while the ordinary active duplicate checks are still
	// enforced at creation time.
	subjectStore := NewMemoryStore()
	original, originalEvent := hardValidLink(t)
	if err := subjectStore.Create(original, originalEvent); err != nil {
		t.Fatal(err)
	}
	suspended := original
	suspendEvent := newEvent(original, LifecycleEvidence{ActedBy: "security", At: hardAt.Add(time.Hour)}, original.Digest, LifecycleSuspended)
	suspended.Lifecycle = LifecycleSuspended
	suspended.Revision++
	suspended.Digest = suspendEvent.Digest
	if err := subjectStore.Transition(suspended, suspendEvent); err != nil {
		t.Fatal(err)
	}
	alt := original
	alt.ID = "active-alt"
	alt.WorkforceIdentityRef.ID = "identity-alt"
	altEvent := eventForLink(t, alt)
	alt.Digest = altEvent.Digest
	if err := subjectStore.Create(alt, altEvent); err != nil {
		t.Fatal(err)
	}
	resume := suspended
	resume.Lifecycle = LifecycleLinked
	resume.Revision++
	resumeEvent := newEvent(suspended, LifecycleEvidence{ActedBy: "security", At: hardAt.Add(2 * time.Hour)}, suspended.Digest, LifecycleLinked)
	resume.Digest = resumeEvent.Digest
	if err := subjectStore.Transition(resume, resumeEvent); !errors.Is(err, ErrDuplicateSubject) {
		t.Fatalf("duplicate subject on resume = %v", err)
	}

	identityStore := NewMemoryStore()
	original, originalEvent = hardValidLink(t)
	if err := identityStore.Create(original, originalEvent); err != nil {
		t.Fatal(err)
	}
	suspended = original
	suspendEvent = newEvent(original, LifecycleEvidence{ActedBy: "security", At: hardAt.Add(time.Hour)}, original.Digest, LifecycleSuspended)
	suspended.Lifecycle = LifecycleSuspended
	suspended.Revision++
	suspended.Digest = suspendEvent.Digest
	if err := identityStore.Transition(suspended, suspendEvent); err != nil {
		t.Fatal(err)
	}
	alt = original
	alt.ID = "active-alt-identity"
	alt.SubjectDigest = DigestSubject("different-subject")
	altEvent = eventForLink(t, alt)
	alt.Digest = altEvent.Digest
	if err := identityStore.Create(alt, altEvent); err != nil {
		t.Fatal(err)
	}
	resume = suspended
	resume.Lifecycle = LifecycleLinked
	resume.Revision++
	resumeEvent = newEvent(suspended, LifecycleEvidence{ActedBy: "security", At: hardAt.Add(2 * time.Hour)}, suspended.Digest, LifecycleLinked)
	resume.Digest = resumeEvent.Digest
	if err := identityStore.Transition(resume, resumeEvent); !errors.Is(err, ErrDuplicateIdentity) {
		t.Fatalf("duplicate identity on resume = %v", err)
	}
}

func TestServiceCommands_EnforceScopeLifecycleAndPackageForms(t *testing.T) {
	t.Parallel()
	service, ref := hardActiveService(t)
	d, err := NewDecision(RuleVerifiedEmail, "resolver", "", "authority", "evidence/service", hardAt)
	if err != nil {
		t.Fatal(err)
	}
	req := CreateRequest{LinkID: "service-link", IssuerRef: ref, SubjectDigest: DigestSubject("service-subject"), WorkforceIdentityRef: WorkforceIdentityRef{Tenant: ref.Tenant, ID: "identity-service"}, Decision: d}
	link, event, err := Create(service.Store, service.Issuers, req)
	if err != nil || link.Lifecycle != LifecycleLinked || event.Digest != link.Digest {
		t.Fatalf("package Create() = %+v,%+v,%v", link, event, err)
	}
	if found, err := Lookup(service.Store, LookupRequest{IssuerRef: ref, SubjectDigest: req.SubjectDigest, Scope: LookupScope{Tenant: ref.Tenant, Purpose: "identity-governance", Granted: true}}); err != nil || found.Status != LookupFound || found.Link.ID != link.ID {
		t.Fatalf("package Lookup() = %+v,%v", found, err)
	}
	for name, request := range map[string]CreateRequest{
		"missing link id":    reqWith(req, func(r *CreateRequest) { r.LinkID = " " }),
		"bad issuer ref":     reqWith(req, func(r *CreateRequest) { r.IssuerRef.Revision = 0 }),
		"bad subject digest": reqWith(req, func(r *CreateRequest) { r.SubjectDigest = "bad" }),
		"cross tenant":       reqWith(req, func(r *CreateRequest) { r.WorkforceIdentityRef.Tenant = values.TenantId("tenant-b") }),
		"bad decision":       reqWith(req, func(r *CreateRequest) { r.Decision.Digest = "bad" }),
	} {
		t.Run("create "+name, func(t *testing.T) {
			if _, _, err := service.Create(request); err == nil {
				t.Fatalf("Create() accepted invalid request")
			}
		})
	}
	if got, err := service.Lookup(LookupRequest{IssuerRef: ref, SubjectDigest: req.SubjectDigest}); err != nil || got.Status != LookupWithheld || got.Link.ID != "" {
		t.Fatalf("unscoped present Lookup() = %+v,%v", got, err)
	}
	for i, scope := range []LookupScope{{Granted: true, Tenant: values.TenantId("tenant-b"), Purpose: "identity-governance"}, {Granted: true, Tenant: ref.Tenant, Purpose: ""}} {
		t.Run("scope "+strconv.Itoa(i), func(t *testing.T) {
			if _, err := service.Lookup(LookupRequest{IssuerRef: ref, SubjectDigest: req.SubjectDigest, Scope: scope}); !errors.Is(err, ErrScopeRequired) {
				t.Fatalf("Lookup() = %v", err)
			}
		})
	}
	if got, err := service.Lookup(LookupRequest{IssuerRef: ref, SubjectDigest: DigestSubject("absent"), Scope: LookupScope{Tenant: ref.Tenant, Purpose: "identity-governance", Granted: true}}); err != nil || got.Status != LookupNotFound {
		t.Fatalf("authorized absent Lookup() = %+v,%v", got, err)
	}
	if _, _, err := service.Suspend("missing", LifecycleEvidence{ActedBy: "security", At: hardAt}); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("missing Suspend() = %v", err)
	}
	if _, _, err := service.Suspend(link.ID, LifecycleEvidence{}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("invalid evidence Suspend() = %v", err)
	}
	suspended, _, err := service.Suspend(link.ID, LifecycleEvidence{ActedBy: "security", At: hardAt.Add(time.Hour)})
	if err != nil || suspended.Lifecycle != LifecycleSuspended {
		t.Fatalf("Suspend() = %+v,%v", suspended, err)
	}
	if _, _, err := service.Suspend(link.ID, LifecycleEvidence{ActedBy: "security", At: hardAt.Add(2 * time.Hour)}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second Suspend() = %v", err)
	}
	resumed, _, err := service.Resume(link.ID, LifecycleEvidence{ActedBy: "security", At: hardAt.Add(2 * time.Hour)})
	if err != nil || resumed.Lifecycle != LifecycleLinked {
		t.Fatalf("Resume() = %+v,%v", resumed, err)
	}
	if _, _, err := service.Unlink(link.ID, LifecycleEvidence{ActedBy: "security", At: hardAt.Add(3 * time.Hour)}); err != nil {
		t.Fatalf("Unlink() = %v", err)
	}
	if _, _, err := service.Resume(link.ID, LifecycleEvidence{ActedBy: "security", At: hardAt.Add(4 * time.Hour)}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Resume after unlink = %v", err)
	}
	var nilService *Service
	if _, _, err := nilService.Create(req); !errors.Is(err, ErrStore) {
		t.Fatalf("nil Service.Create = %v", err)
	}
	if _, err := nilService.Lookup(LookupRequest{}); !errors.Is(err, ErrStore) {
		t.Fatalf("nil Service.Lookup = %v", err)
	}
	if _, _, err := nilService.Suspend("x", LifecycleEvidence{}); !errors.Is(err, ErrStore) {
		t.Fatalf("nil Service.Suspend = %v", err)
	}
}

func TestService_PropagatesIssuerAndStoreFailures(t *testing.T) {
	t.Parallel()
	service, ref := hardActiveService(t)
	d, err := NewDecision(RuleVerifiedEmail, "resolver", "", "authority", "evidence/failure", hardAt)
	if err != nil {
		t.Fatal(err)
	}
	request := CreateRequest{LinkID: "failure-link", IssuerRef: ref, SubjectDigest: DigestSubject("failure-subject"), WorkforceIdentityRef: WorkforceIdentityRef{Tenant: ref.Tenant, ID: "failure-identity"}, Decision: d}
	request.IssuerRef.Revision = 2
	if _, _, err := service.Create(request); !errors.Is(err, ErrIssuerRevision) {
		t.Fatalf("revision mismatch Create() = %v", err)
	}
	if _, err := issuerregistry.Retire(service.Issuers, ref, issuerregistry.Evidence{ActedBy: "retirer", At: hardAt.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	request.IssuerRef.Revision = 1
	if _, _, err := service.Create(request); !errors.Is(err, ErrIssuerNotActive) {
		t.Fatalf("retired issuer Create() = %v", err)
	}
	failed := &Service{Store: errorStore{err: errors.New("store unavailable")}}
	if _, err := failed.Lookup(LookupRequest{IssuerRef: ref, SubjectDigest: DigestSubject("subject"), Scope: LookupScope{Tenant: ref.Tenant, Purpose: "governance", Granted: true}}); err == nil || !strings.Contains(err.Error(), "store unavailable") {
		t.Fatalf("store failure Lookup() = %v", err)
	}
	if _, _, err := failed.Suspend("link", LifecycleEvidence{ActedBy: "security", At: hardAt}); err == nil || !strings.Contains(err.Error(), "store unavailable") {
		t.Fatalf("store failure Suspend() = %v", err)
	}
	if _, _, err := Create(nil, nil, request); !errors.Is(err, ErrStore) {
		t.Fatalf("nil package Create() = %v", err)
	}
}

type errorStore struct{ err error }

func (s errorStore) Create(Link, LifecycleEvent) error                   { return s.err }
func (s errorStore) Get(string) (Link, bool, error)                      { return Link{}, false, s.err }
func (s errorStore) Find(issuerregistry.Ref, string) (Link, bool, error) { return Link{}, false, s.err }
func (s errorStore) Transition(Link, LifecycleEvent) error               { return s.err }
func (s errorStore) Events(string) ([]LifecycleEvent, error)             { return nil, s.err }

var _ Store = errorStore{}

func reqWith(req CreateRequest, mutate func(*CreateRequest)) CreateRequest {
	out := req
	mutate(&out)
	return out
}

func hardRef() issuerregistry.Ref {
	return issuerregistry.Ref{Tenant: values.TenantId("tenant-a"), IssuerURL: "https://issuer.example/", Revision: 1}
}

func hardValidLink(t *testing.T) (Link, LifecycleEvent) {
	t.Helper()
	ref := hardRef()
	d, err := NewDecision(RuleVerifiedEmail, "resolver", "", "authority", "evidence/link", hardAt)
	if err != nil {
		t.Fatal(err)
	}
	link := Link{ID: "link-1", IssuerRef: ref, SubjectDigest: DigestSubject("subject"), WorkforceIdentityRef: WorkforceIdentityRef{Tenant: ref.Tenant, ID: "identity-1"}, Rule: d.Rule, DecisionDigest: d.Digest, Lifecycle: LifecycleLinked, Revision: 1, CreatedAt: hardAt}
	event := eventForLink(t, link)
	link.Digest = event.Digest
	return link, event
}

func eventForLink(t *testing.T, link Link) LifecycleEvent {
	t.Helper()
	event := newEvent(link, LifecycleEvidence{ActedBy: "resolver", Authority: "authority", Reason: "link created", At: hardAt}, "", LifecycleLinked)
	event.From = ""
	event.Digest = digestEvent(event)
	return event
}

func hardActiveService(t *testing.T) (*Service, issuerregistry.Ref) {
	t.Helper()
	tenant := values.TenantId("tenant-a")
	ref := issuerregistry.Ref{Tenant: tenant, IssuerURL: "https://issuer.example/", Revision: 1}
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	issuers := issuerregistry.NewMemoryStore()
	issuer, err := issuerregistry.Publish(issuers, issuerregistry.Issuer{Tenant: tenant, IssuerURL: ref.IssuerURL, Audience: "hcm-next", JWKS: issuerregistry.JWKSSource{Kind: issuerregistry.JWKSSourcePinnedKeys, PinnedKeys: []issuerregistry.PinnedKey{{KeyID: "kid-1", Algorithm: trustfederation.AlgEdDSA, PublicKeyDER: der}}}, Algorithms: []trustfederation.Algorithm{trustfederation.AlgEdDSA}, MetadataStaleness: time.Hour, Revision: 1, PublisherPrincipal: "publisher", PublishedAt: hardAt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issuerregistry.Activate(issuers, issuer.Ref(), issuerregistry.Evidence{ActedBy: "approver", At: hardAt}); err != nil {
		t.Fatal(err)
	}
	return New(NewMemoryStore(), issuers), ref
}
