package proofing

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestProofingVocabularyAndFieldError(t *testing.T) {
	if (FieldError{Field: "x", Problem: "bad"}).Error() != "proofing: x: bad" {
		t.Fatal("FieldError formatting changed")
	}
	for _, level := range []AssuranceLevel{AssuranceIAL1, AssuranceIAL2, AssuranceIAL3} {
		if !level.Valid() || level.rank() == 0 {
			t.Fatalf("assurance level %q invalid", level)
		}
	}
	if AssuranceLevel("IAL0").Valid() || AssuranceLevel("IAL0").rank() != 0 {
		t.Fatal("unknown assurance level accepted")
	}
	for _, kind := range []EvidenceKind{EvidenceGovernmentID, EvidencePassport, EvidenceDriversLicense, EvidenceNationalID, EvidenceSelfieLiveness, EvidenceKnowledgeCheck, EvidenceProviderAssertion, EvidenceWorkAuthorization} {
		if !kind.Valid() {
			t.Fatalf("evidence kind %q invalid", kind)
		}
	}
	if EvidenceKind("RAW").Valid() || EvidenceKind("RAW").Valid() {
		t.Fatal("unknown evidence kind accepted")
	}
	for _, outcome := range []ProofingOutcome{OutcomeVerified, OutcomeReviewRequired, OutcomeRejected, OutcomeExpired, OutcomeUnknown} {
		if !outcome.Valid() {
			t.Fatalf("outcome %q invalid", outcome)
		}
	}
	if ProofingOutcome("MAYBE").Valid() || digestString("sha256:") || digestString("md5:x") {
		t.Fatal("malformed proofing vocabulary/digest accepted")
	}
	if !digestString("sha256:short") {
		t.Fatal("non-empty sha256 digest should be accepted by the domain shape check")
	}
}

func TestEvidenceItem_ValidationCanonicalAndCopy(t *testing.T) {
	valid := proofingEvidence(t, AssuranceIAL2)
	if len(valid.Canonical()) == 0 {
		t.Fatal("valid evidence has no canonical form")
	}
	cases := []struct {
		name string
		edit func(*EvidenceItem)
	}{
		{"kind", func(e *EvidenceItem) { e.Kind = "UNKNOWN" }},
		{"evidence digest", func(e *EvidenceItem) { e.EvidenceDigest = "" }},
		{"provider digest", func(e *EvidenceItem) { e.ProviderObservationDigest = "not-a-digest" }},
		{"custody", func(e *EvidenceItem) { e.CustodyRef = " " }},
		{"assurance", func(e *EvidenceItem) { e.Supports = "IAL0" }},
		{"collected at", func(e *EvidenceItem) { e.CollectedAt = values.Instant{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := valid
			tc.edit(&bad)
			if err := bad.Validate(); !errors.Is(err, ErrInvalidEvidence) {
				t.Fatalf("error = %v, want ErrInvalidEvidence", err)
			}
			if got := bad.Canonical(); got != nil {
				t.Fatal("invalid evidence returned canonical bytes")
			}
		})
	}
	if _, err := NewEvidenceItem(EvidencePassport, "sha256:e", "", "custody://x", AssuranceIAL1, proofingNow); err != nil {
		t.Fatal(err)
	}
}

func recanonicalSession(s *ProofingSession) { s.CanonicalDigest = s.computedDigest() }

func TestProofingSession_ValidationOutcomeExpiryAndExplanation(t *testing.T) {
	session := proofingSession(t, AssuranceIAL2, AssuranceIAL2)
	input := append([]EvidenceItem(nil), session.Evidence...)
	if len(session.Canonical()) == 0 {
		t.Fatal("session canonical form is empty")
	}
	if got, err := NewIdentityProofSession(session.SessionID, session.Subject, session.Purpose, session.Target, input, session.VerifierPrincipal, session.ExpiresAt); err != nil || got.SessionID != session.SessionID {
		t.Fatalf("identity constructor = %+v, %v", got, err)
	}
	cases := []struct {
		name string
		edit func(*ProofingSession)
	}{
		{"session id", func(s *ProofingSession) { s.SessionID = "" }},
		{"subject", func(s *ProofingSession) { s.Subject = values.EntityRef{} }},
		{"purpose", func(s *ProofingSession) { s.Purpose = "" }},
		{"target", func(s *ProofingSession) { s.Target = "IAL0" }},
		{"verifier", func(s *ProofingSession) { s.VerifierPrincipal = "" }},
		{"outcome", func(s *ProofingSession) { s.Outcome = "MAYBE" }},
		{"expiry", func(s *ProofingSession) { s.ExpiresAt = values.Instant{} }},
		{"revision zero", func(s *ProofingSession) { s.Revision = 0 }},
		{"lineage", func(s *ProofingSession) { s.SupersedesRevision = s.Revision }},
		{"evidence empty", func(s *ProofingSession) { s.Evidence = nil }},
		{"duplicate evidence", func(s *ProofingSession) { s.Evidence = append(s.Evidence, s.Evidence[0]) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := session
			bad.Evidence = append([]EvidenceItem(nil), session.Evidence...)
			tc.edit(&bad)
			if err := bad.Validate(); !errors.Is(err, ErrInvalidSession) {
				t.Fatalf("error = %v, want ErrInvalidSession", err)
			}
		})
	}
	if _, err := session.OutcomeRevision("MAYBE"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("unknown outcome = %v", err)
	}
	if _, err := proofingSession(t, AssuranceIAL3, AssuranceIAL2).RecordOutcome(OutcomeVerified); !errors.Is(err, ErrUnsupportedAssurance) {
		t.Fatalf("unsupported assurance = %v", err)
	}
	next, err := session.OutcomeRevision(OutcomeRejected)
	if err != nil || next.Revision != 2 || next.SupersedesRevision != 1 || session.Outcome != OutcomeUnknown {
		t.Fatalf("outcome successor = %+v, %v", next, err)
	}
	if _, err := session.RecordOutcome(OutcomeReviewRequired); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Evaluate(values.Instant{}); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("invalid evaluation time = %v", err)
	}
	unchanged, err := session.Evaluate(values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)))
	if err != nil || unchanged.Revision != session.Revision {
		t.Fatalf("non-expired evaluation = %+v, %v", unchanged, err)
	}
	expired, err := session.Evaluate(session.ExpiresAt)
	if err != nil || expired.Outcome != OutcomeExpired || expired.Revision != 2 {
		t.Fatalf("expired evaluation = %+v, %v", expired, err)
	}
	already, err := expired.Evaluate(session.ExpiresAt)
	if err != nil || already.Revision != expired.Revision {
		t.Fatalf("already expired session changed = %+v, %v", already, err)
	}
	explanation, err := session.Explain()
	if err != nil || explanation.EvidenceCount != 1 || explanation.EvidenceCeiling != AssuranceIAL2 {
		t.Fatalf("explanation = %+v, %v", explanation, err)
	}
	if _, err := Explain(session); err != nil {
		t.Fatal(err)
	}
}

func TestWorkAuthorizationEvidence_ValidationAndSuccessor(t *testing.T) {
	base := proofingAuthorization(t)
	if len(base.Canonical()) == 0 {
		t.Fatal("valid authorization has no canonical form")
	}
	if !DocumentPassport.Valid() || !DocumentNationalIdentity.Valid() || !DocumentResidencePermit.Valid() || !DocumentWorkPermit.Valid() || !DocumentEmploymentCertificate.Valid() || DocumentClass("OTHER").Valid() {
		t.Fatal("document vocabulary mismatch")
	}
	if !VerificationManualReview.Valid() || !VerificationGovernmentSource.Valid() || !VerificationEmployerReview.Valid() || !VerificationProviderCheck.Valid() || VerificationMethod("OTHER").Valid() {
		t.Fatal("verification vocabulary mismatch")
	}
	cases := []struct {
		name string
		edit func(*WorkAuthorizationEvidence)
	}{
		{"id", func(e *WorkAuthorizationEvidence) { e.EvidenceID = "" }},
		{"subject", func(e *WorkAuthorizationEvidence) { e.Subject = values.EntityRef{} }},
		{"revision", func(e *WorkAuthorizationEvidence) { e.Revision = 0 }},
		{"lineage", func(e *WorkAuthorizationEvidence) { e.SupersedesRevision = e.Revision }},
		{"class", func(e *WorkAuthorizationEvidence) { e.DocumentClass = "OTHER" }},
		{"method", func(e *WorkAuthorizationEvidence) { e.VerificationMethod = "OTHER" }},
		{"scope", func(e *WorkAuthorizationEvidence) { e.Jurisdiction = "" }},
		{"from", func(e *WorkAuthorizationEvidence) { e.ValidFrom = values.LocalDate{} }},
		{"until", func(e *WorkAuthorizationEvidence) { e.ValidUntil = values.LocalDate{} }},
		{"inverted", func(e *WorkAuthorizationEvidence) { e.ValidUntil = e.ValidFrom }},
		{"due before", func(e *WorkAuthorizationEvidence) { e.ReverificationDue = e.ValidFrom }},
		{"digest", func(e *WorkAuthorizationEvidence) { e.EvidenceDigest = "bad" }},
		{"source", func(e *WorkAuthorizationEvidence) { e.SourceRef = " " }},
		{"canonical", func(e *WorkAuthorizationEvidence) { e.CanonicalDigest = "tampered" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := base
			tc.edit(&bad)
			if err := bad.Validate(); !errors.Is(err, ErrInvalidAuthorization) {
				t.Fatalf("error = %v, want ErrInvalidAuthorization", err)
			}
		})
	}
	next := base
	next.Revision = 2
	next.SupersedesRevision = 1
	next.Category = "VOLUNTEER"
	child, err := base.Successor(next)
	if err != nil || child.CanonicalDigest == base.CanonicalDigest {
		t.Fatalf("successor = %+v, %v", child, err)
	}
	for _, bad := range []WorkAuthorizationEvidence{func() WorkAuthorizationEvidence { x := next; x.EvidenceID = "other"; return x }(), func() WorkAuthorizationEvidence { x := next; x.Subject = values.EntityRef{}; return x }(), func() WorkAuthorizationEvidence { x := next; x.Revision = 3; return x }()} {
		if _, err := base.Successor(bad); err == nil {
			t.Fatalf("invalid successor %+v accepted", bad)
		}
	}
	if _, err := NewWorkAuthorizationEvidence(base); err != nil {
		t.Fatal(err)
	}
}

func TestWorkAuthorizationResult_ValidationExplainAndVocabulary(t *testing.T) {
	auth := proofingAuthorization(t)
	base, err := NewWorkAuthorizationResult(WorkAuthorizationResult{ResultID: "result-1", Subject: auth.Subject, EvidenceRevisionDigest: auth.CanonicalDigest, Jurisdiction: auth.Jurisdiction, Category: auth.Category, EffectiveFrom: auth.ValidFrom, EffectiveUntil: auth.ValidUntil, Decision: DecisionAuthorized})
	if err != nil || len(base.Canonical()) == 0 {
		t.Fatalf("result = %+v, %v", base, err)
	}
	for _, decision := range []AuthorizationDecision{DecisionAuthorized, DecisionReview, DecisionRejected, DecisionExpired, DecisionUnknown} {
		if !decision.Valid() {
			t.Fatalf("decision %q invalid", decision)
		}
	}
	if AuthorizationDecision("OTHER").Valid() {
		t.Fatal("unknown decision accepted")
	}
	cases := []struct {
		name string
		edit func(*WorkAuthorizationResult)
	}{
		{"id", func(r *WorkAuthorizationResult) { r.ResultID = "" }},
		{"subject", func(r *WorkAuthorizationResult) { r.Subject = values.EntityRef{} }},
		{"digest", func(r *WorkAuthorizationResult) { r.EvidenceRevisionDigest = "bad" }},
		{"scope", func(r *WorkAuthorizationResult) { r.Category = "" }},
		{"from", func(r *WorkAuthorizationResult) { r.EffectiveFrom = values.LocalDate{} }},
		{"until", func(r *WorkAuthorizationResult) { r.EffectiveUntil = values.LocalDate{} }},
		{"inverted", func(r *WorkAuthorizationResult) { r.EffectiveUntil = r.EffectiveFrom }},
		{"decision", func(r *WorkAuthorizationResult) { r.Decision = "OTHER" }},
		{"canonical", func(r *WorkAuthorizationResult) { r.CanonicalDigest = "tampered" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := base
			tc.edit(&bad)
			if err := bad.Validate(); !errors.Is(err, ErrInvalidResult) {
				t.Fatalf("error = %v, want ErrInvalidResult", err)
			}
		})
	}
	if explanation, err := base.Explain(); err != nil || explanation.Digest != base.CanonicalDigest {
		t.Fatalf("explanation = %+v, %v", explanation, err)
	}
	if _, err := ExplainWorkAuthorization(base); err != nil {
		t.Fatal(err)
	}
	invalid := base
	invalid.CanonicalDigest = ""
	if _, err := invalid.Explain(); err == nil || !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("invalid explanation error = %v", err)
	}
}

func TestInMemoryStores_RevisionLineageAndCopies(t *testing.T) {
	sessionStore := NewInMemorySessionStore()
	session := proofingSession(t, AssuranceIAL1, AssuranceIAL1)
	if err := sessionStore.Put(session); err != nil {
		t.Fatal(err)
	}
	if err := sessionStore.Put(session); !errors.Is(err, ErrRevisionLineage) {
		t.Fatalf("duplicate session = %v", err)
	}
	if _, ok := sessionStore.Get(session.SessionID); !ok {
		t.Fatal("session not found")
	}
	history, err := sessionStore.History(session.SessionID)
	if err != nil || len(history) != 1 {
		t.Fatalf("session history = %#v, %v", history, err)
	}
	history[0].Evidence[0].EvidenceDigest = "tampered"
	got, _ := sessionStore.Get(session.SessionID)
	if got.Evidence[0].EvidenceDigest == "tampered" {
		t.Fatal("session history was not detached")
	}
	if _, err := sessionStore.History("missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing session history = %v", err)
	}
	var nilSessionStore *InMemorySessionStore
	if err := nilSessionStore.Put(session); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("nil session store = %v", err)
	}

	authStore := NewInMemoryAuthorizationStore()
	auth := proofingAuthorization(t)
	if err := authStore.Put(auth); err != nil {
		t.Fatal(err)
	}
	if err := authStore.Put(auth); !errors.Is(err, ErrRevisionLineage) {
		t.Fatalf("duplicate authorization = %v", err)
	}
	if got, ok := authStore.Get(auth.EvidenceID); !ok || got.CanonicalDigest != auth.CanonicalDigest {
		t.Fatalf("authorization get = %+v, %v", got, ok)
	}
	if _, ok := authStore.Get("missing"); ok {
		t.Fatal("missing authorization found")
	}
	var nilAuthStore *InMemoryAuthorizationStore
	if err := nilAuthStore.Put(auth); !errors.Is(err, ErrInvalidAuthorization) {
		t.Fatalf("nil authorization store = %v", err)
	}
}
