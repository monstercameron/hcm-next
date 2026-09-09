package succession

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type successionVerifier struct {
	tenant, actor, subject string
	readinessRevision      uint64
	readinessDigest        string
	sourceSubject          string
	sourceDecisionDigest   string
	sourceAssessmentDigest string
	mu                     sync.Mutex
	calls                  []string
}

func (v *successionVerifier) record(kind, tenant, actor, action, resource string) error {
	if tenant != v.tenant || (actor != "" && actor != v.actor) {
		return errors.New("binding mismatch")
	}
	v.mu.Lock()
	v.calls = append(v.calls, fmt.Sprintf("%s:%s:%s", kind, action, resource))
	v.mu.Unlock()
	return nil
}
func (v *successionVerifier) VerifyAuthority(_ string, tenant, actor, action, resource string) error {
	return v.record("authority", tenant, actor, action, resource)
}
func (v *successionVerifier) VerifyConsent(_ string, tenant, subject, purpose string) error {
	if subject != v.subject {
		return errors.New("subject mismatch")
	}
	return v.record("consent", tenant, "", purpose, subject)
}
func (v *successionVerifier) VerifyPrivacy(_ string, tenant, class, action string) error {
	if class != "succession-restricted" {
		return errors.New("privacy mismatch")
	}
	return v.record("privacy", tenant, "", action, class)
}
func (v *successionVerifier) VerifyCurrentReadiness(tenant, subject string, revision uint64, digest string, _ values.Instant) error {
	if subject != v.subject || revision != v.readinessRevision || digest != v.readinessDigest {
		return errors.New("readiness is not current")
	}
	return v.record("readiness", tenant, "", fmt.Sprint(revision), digest)
}
func (v *successionVerifier) VerifyCorrectionSource(tenant, subject, decisionDigest, priorAssessmentDigest string) error {
	if subject != v.sourceSubject || decisionDigest != v.sourceDecisionDigest || priorAssessmentDigest != v.sourceAssessmentDigest {
		return errors.New("correction source tuple mismatch")
	}
	return v.record("correction-source", tenant, "", decisionDigest, priorAssessmentDigest)
}

func successionProvenance() Provenance {
	return Provenance{TenantID: "acme", AuthorityRef: "authority-1", RecordedBy: "reviewer-1", EvidenceRefs: []string{"privacy-1"}, ConsentRef: "consent-1", PrivacyClass: "succession-restricted"}
}
func verifier() *successionVerifier {
	return &successionVerifier{tenant: "acme", actor: "reviewer-1", subject: "worker-1", readinessRevision: 1, readinessDigest: "sha256:assessment-1", sourceSubject: "worker-1", sourceDecisionDigest: "sha256:147123d5d934dc4b3972b098e60a8c8cc6c5ba39925a0c9455069152a42f9bcd", sourceAssessmentDigest: "sha256:assessment-1"}
}
func calibrationInput() CalibrationDecision {
	return CalibrationDecision{DecisionID: "decision-1", SlateID: "slate-1", SlateRevision: 1, CandidateID: "worker-1", ReadinessRevision: 1, PriorAssessmentDigest: "sha256:assessment-1", Outcome: CalibrationAccepted, Rationale: "evidence reviewed", Provenance: successionProvenance(), EffectiveAt: successionInstant(), KnownAt: successionInstant()}
}
func selectionInput() VacancySelectionIntent {
	return VacancySelectionIntent{IntentID: "intent-1", CriticalRoleID: "role-1", SlateID: "slate-1", SlateRevision: 2, Purpose: "vacancy review", NonGuarantee: true, Provenance: successionProvenance(), EffectiveAt: successionInstant(), KnownAt: successionInstant()}
}
func successionCalibration(t *testing.T) CalibrationDecision {
	t.Helper()
	d, err := NewCalibrationDecision(calibrationInput(), verifier())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSuccessionConformanceNeverTurnsSlateIntoPromotionOrEmploymentPromise(t *testing.T) {
	s := successionSlate(t, DisclosureWithheld, nil, true)
	e, err := s.Explain()
	if err != nil || e.NonGuaranteeNotice != NonGuaranteeNotice {
		t.Fatalf("explanation=%+v err=%v", e, err)
	}
	i, err := NewVacancySelectionIntent(selectionInput(), "worker-1", verifier())
	if err != nil || !i.NonGuarantee {
		t.Fatalf("intent=%+v err=%v", i, err)
	}
}

func TestTodo_SUCCESSION_002_Property(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CalibrationDecision)
	}{
		{"missing tenant", func(d *CalibrationDecision) { d.Provenance.TenantID = "" }},
		{"missing subject", func(d *CalibrationDecision) { d.CandidateID = "" }},
		{"missing prior digest", func(d *CalibrationDecision) { d.PriorAssessmentDigest = "" }},
		{"invalid outcome", func(d *CalibrationDecision) { d.Outcome = "PROMOTED" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := calibrationInput()
			tt.mutate(&d)
			if _, err := NewCalibrationDecision(d, verifier()); err == nil {
				t.Fatal("invalid calibration accepted")
			}
		})
	}
}

func TestTodo_SUCCESSION_002_Golden(t *testing.T) {
	d, err := NewCalibrationDecision(calibrationInput(), verifier())
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:147123d5d934dc4b3972b098e60a8c8cc6c5ba39925a0c9455069152a42f9bcd"
	if d.CanonicalDigest != want {
		t.Fatalf("calibration digest=%q want fixed golden %q", d.CanonicalDigest, want)
	}
}

func TestTodo_SUCCESSION_002_Race(t *testing.T) {
	v := verifier()
	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for n := 0; n < workers; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := NewCalibrationDecision(calibrationInput(), v)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	v.mu.Lock()
	got := len(v.calls)
	v.mu.Unlock()
	if got != workers*4 {
		t.Fatalf("verified calls=%d want %d", got, workers*4)
	}
}

func TestTodo_SUCCESSION_002_Fault(t *testing.T) {
	i := selectionInput()
	i.NonGuarantee = false
	if _, err := NewVacancySelectionIntent(i, "worker-1", verifier()); !errors.Is(err, ErrGuaranteeSemantics) {
		t.Fatalf("err=%v", err)
	}
	if _, err := NewCalibrationDecision(calibrationInput(), nil); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("nil verifier err=%v", err)
	}
	d := calibrationInput()
	d.Provenance.ConsentRef = ""
	if _, err := NewCalibrationDecision(d, verifier()); !errors.Is(err, ErrMissingConsent) {
		t.Fatalf("missing consent err=%v", err)
	}
}

func TestTodo_SUCCESSION_002_Security(t *testing.T) {
	t.Run("tenant crossing", func(t *testing.T) {
		d := calibrationInput()
		d.Provenance.TenantID = "other"
		if _, err := NewCalibrationDecision(d, verifier()); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("forged consent subject", func(t *testing.T) {
		v := verifier()
		v.subject = "other-worker"
		if _, err := NewCalibrationDecision(calibrationInput(), v); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("forged privacy class", func(t *testing.T) {
		d := calibrationInput()
		d.Provenance.PrivacyClass = "public"
		if _, err := NewCalibrationDecision(d, verifier()); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("confidential rank", func(t *testing.T) {
		s := successionSlate(t, DisclosureWithheld, nil, true)
		if _, err := s.CandidatesFor("talent.read"); !errors.Is(err, ErrSlateMembershipWithheld) {
			t.Fatalf("err=%v", err)
		}
		explanation, err := s.Explain()
		if err != nil || explanation.CandidateCount != 0 {
			t.Fatalf("withheld explanation leaked candidate count: %+v err=%v", explanation, err)
		}
	})
	t.Run("stale readiness", func(t *testing.T) {
		v := verifier()
		v.readinessRevision = 2
		v.readinessDigest = "sha256:new-assessment"
		if _, err := NewCalibrationDecision(calibrationInput(), v); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("stale readiness err=%v", err)
		}
	})
	t.Run("correction subject replay", func(t *testing.T) {
		d := successionCalibration(t)
		c := SuccessionCorrection{CorrectionID: "correction-replay", SubjectID: "worker-1", DecisionDigest: d.CanonicalDigest, CorrectsDigest: d.PriorAssessmentDigest, CorrectedDigest: "sha256:corrected", Reason: "new evidence", Provenance: successionProvenance(), EffectiveAt: successionInstant(), KnownAt: successionInstant()}
		v := verifier()
		v.sourceSubject = "worker-2"
		if _, err := NewSuccessionCorrection(c, v); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("correction replay err=%v", err)
		}
	})
	t.Run("correction assessment replay", func(t *testing.T) {
		d := successionCalibration(t)
		c := SuccessionCorrection{CorrectionID: "correction-assessment-replay", SubjectID: "worker-1", DecisionDigest: d.CanonicalDigest, CorrectsDigest: "sha256:other-assessment", CorrectedDigest: "sha256:corrected", Reason: "new evidence", Provenance: successionProvenance(), EffectiveAt: successionInstant(), KnownAt: successionInstant()}
		if _, err := NewSuccessionCorrection(c, verifier()); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("assessment replay err=%v", err)
		}
	})
}

func TestTodo_SUCCESSION_002_Conformance(t *testing.T) {
	d := successionCalibration(t)
	c, err := NewSuccessionCorrection(SuccessionCorrection{CorrectionID: "correction-1", SubjectID: "worker-1", DecisionDigest: d.CanonicalDigest, CorrectsDigest: d.PriorAssessmentDigest, CorrectedDigest: "sha256:corrected", Reason: "new evidence", Provenance: successionProvenance(), EffectiveAt: successionInstant(), KnownAt: successionInstant()}, verifier())
	if err != nil || c.DecisionDigest != d.CanonicalDigest || c.CorrectsDigest != d.PriorAssessmentDigest {
		t.Fatalf("correction=%+v err=%v", c, err)
	}
}

func TestTodo_SUCCESSION_002_Mutation(t *testing.T) {
	d := successionCalibration(t)
	d.Rationale = "silently changed"
	if !errors.Is(d.Validate(), ErrInvalidCalibration) {
		t.Fatal("mutated decision validated")
	}
	input := calibrationInput()
	input.CanonicalDigest = "sha256:forged"
	if _, err := NewCalibrationDecision(input, verifier()); !errors.Is(err, ErrInvalidCalibration) {
		t.Fatalf("forged digest err=%v", err)
	}
	c := SuccessionCorrection{CorrectionID: "c", SubjectID: "worker-1", DecisionDigest: d.CanonicalDigest, CorrectsDigest: "same", CorrectedDigest: "same", Reason: "bad", Provenance: successionProvenance(), EffectiveAt: successionInstant(), KnownAt: successionInstant()}
	if _, err := NewSuccessionCorrection(c, verifier()); !errors.Is(err, ErrInvalidCorrection) {
		t.Fatalf("err=%v", err)
	}
}
