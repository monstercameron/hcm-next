package succession

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const NonGuaranteeNotice = "Succession slate is informational only; it is not a promotion, appointment, or employment guarantee."

const (
	actionCalibrate = "succession.calibrate"
	actionCorrect   = "succession.correct"
	actionSelect    = "succession.open-selection"
)

var (
	ErrInvalidCalibration = errors.New("succession: invalid calibration decision")
	ErrInvalidCorrection  = errors.New("succession: invalid succession correction")
	ErrInvalidSelection   = errors.New("succession: invalid vacancy selection intent")
	ErrGuaranteeSemantics = errors.New("succession: succession evidence cannot guarantee employment")
	ErrMissingConsent     = errors.New("succession: explicit processing consent is required")
	ErrUnauthorized       = errors.New("succession: authorization evidence was not verified")
)

// EvidenceVerifier is the authority boundary. References are resolved by the
// caller's authoritative service rather than trusted as bearer assertions.
type EvidenceVerifier interface {
	VerifyAuthority(ref, tenantID, actorID, action, resourceID string) error
	VerifyConsent(ref, tenantID, subjectID, purpose string) error
	VerifyPrivacy(ref, tenantID, privacyClass, action string) error
	VerifyCurrentReadiness(tenantID, subjectID string, revision uint64, digest string, asOf values.Instant) error
	VerifyCorrectionSource(tenantID, subjectID, decisionDigest, priorAssessmentDigest string) error
}

type Provenance struct {
	TenantID     string
	AuthorityRef string
	RecordedBy   string
	EvidenceRefs []string
	ConsentRef   string
	PrivacyClass string
}

func (p Provenance) validate(base error) error {
	for name, value := range map[string]string{"tenant_id": p.TenantID, "authority_ref": p.AuthorityRef, "recorded_by": p.RecordedBy, "privacy_class": p.PrivacyClass} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", base, name)
		}
	}
	if strings.TrimSpace(p.ConsentRef) == "" {
		return ErrMissingConsent
	}
	return validateRefs(p.EvidenceRefs, base, "evidence_refs")
}

func verify(v EvidenceVerifier, p Provenance, subject, purpose, action, resource string) error {
	if v == nil {
		return ErrUnauthorized
	}
	checks := []error{
		v.VerifyAuthority(p.AuthorityRef, p.TenantID, p.RecordedBy, action, resource),
		v.VerifyConsent(p.ConsentRef, p.TenantID, subject, purpose),
		v.VerifyPrivacy(p.EvidenceRefs[0], p.TenantID, p.PrivacyClass, action),
	}
	for _, err := range checks {
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUnauthorized, err)
		}
	}
	return nil
}

type CalibrationOutcome string

const (
	CalibrationAccepted CalibrationOutcome = "ACCEPTED"
	CalibrationReferred CalibrationOutcome = "REFERRED"
	CalibrationRejected CalibrationOutcome = "REJECTED"
)

func (o CalibrationOutcome) Valid() bool {
	return o == CalibrationAccepted || o == CalibrationReferred || o == CalibrationRejected
}

type CalibrationDecision struct {
	DecisionID            string
	SlateID               string
	SlateRevision         uint64
	CandidateID           string
	ReadinessRevision     uint64
	PriorAssessmentDigest string
	Outcome               CalibrationOutcome
	Rationale             string
	Provenance            Provenance
	EffectiveAt           values.Instant
	KnownAt               values.Instant
	CanonicalDigest       string
}

func (d CalibrationDecision) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.succession.CalibrationDecision", schemaVersion).
		String("tenant_id", d.Provenance.TenantID).String("decision_id", d.DecisionID).String("slate_id", d.SlateID).
		Int("slate_revision", int64(d.SlateRevision)).String("candidate_id", d.CandidateID).Int("readiness_revision", int64(d.ReadinessRevision)).
		String("prior_assessment_digest", d.PriorAssessmentDigest).String("outcome", string(d.Outcome)).String("rationale", d.Rationale).
		String("authority_ref", d.Provenance.AuthorityRef).String("recorded_by", d.Provenance.RecordedBy).
		SortedStrings("evidence_ref", sortedStrings(d.Provenance.EvidenceRefs)).String("consent_ref", d.Provenance.ConsentRef).
		String("privacy_class", d.Provenance.PrivacyClass).Value("effective_at", d.EffectiveAt).Value("known_at", d.KnownAt)
	b, _ := w.Bytes()
	return b
}

func (d CalibrationDecision) Validate() error {
	for name, value := range map[string]string{"decision_id": d.DecisionID, "slate_id": d.SlateID, "candidate_id": d.CandidateID, "prior_assessment_digest": d.PriorAssessmentDigest, "rationale": d.Rationale} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidCalibration, name)
		}
	}
	if d.SlateRevision == 0 || d.ReadinessRevision == 0 || !d.Outcome.Valid() {
		return ErrInvalidCalibration
	}
	if err := d.Provenance.validate(ErrInvalidCalibration); err != nil {
		return err
	}
	if d.EffectiveAt.Validate() != nil || d.KnownAt.Validate() != nil {
		return ErrInvalidCalibration
	}
	if d.CanonicalDigest == "" || d.CanonicalDigest != canonicalbytes.Digest(d.canonical()) {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidCalibration)
	}
	return nil
}

func NewCalibrationDecision(d CalibrationDecision, v EvidenceVerifier) (CalibrationDecision, error) {
	d.Provenance.EvidenceRefs = append([]string(nil), d.Provenance.EvidenceRefs...)
	providedDigest := d.CanonicalDigest
	d.CanonicalDigest = canonicalbytes.Digest(d.canonical())
	if providedDigest != "" && providedDigest != d.CanonicalDigest {
		return CalibrationDecision{}, fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidCalibration)
	}
	if err := d.Validate(); err != nil {
		return CalibrationDecision{}, err
	}
	if err := verify(v, d.Provenance, d.CandidateID, actionCalibrate, actionCalibrate, d.SlateID); err != nil {
		return CalibrationDecision{}, err
	}
	if err := v.VerifyCurrentReadiness(d.Provenance.TenantID, d.CandidateID, d.ReadinessRevision, d.PriorAssessmentDigest, d.EffectiveAt); err != nil {
		return CalibrationDecision{}, fmt.Errorf("%w: stale readiness: %v", ErrUnauthorized, err)
	}
	return d, nil
}

type SuccessionCorrection struct {
	CorrectionID    string
	SubjectID       string
	DecisionDigest  string
	CorrectsDigest  string
	CorrectedDigest string
	Reason          string
	Provenance      Provenance
	EffectiveAt     values.Instant
	KnownAt         values.Instant
	CanonicalDigest string
}

func (c SuccessionCorrection) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.succession.SuccessionCorrection", schemaVersion).
		String("tenant_id", c.Provenance.TenantID).String("correction_id", c.CorrectionID).String("subject_id", c.SubjectID).String("decision_digest", c.DecisionDigest).
		String("corrects_digest", c.CorrectsDigest).String("corrected_digest", c.CorrectedDigest).String("reason", c.Reason).
		String("authority_ref", c.Provenance.AuthorityRef).String("recorded_by", c.Provenance.RecordedBy).
		SortedStrings("evidence_ref", sortedStrings(c.Provenance.EvidenceRefs)).String("consent_ref", c.Provenance.ConsentRef).
		String("privacy_class", c.Provenance.PrivacyClass).Value("effective_at", c.EffectiveAt).Value("known_at", c.KnownAt)
	b, _ := w.Bytes()
	return b
}

func (c SuccessionCorrection) Validate() error {
	for _, value := range []string{c.CorrectionID, c.SubjectID, c.DecisionDigest, c.CorrectsDigest, c.CorrectedDigest, c.Reason} {
		if strings.TrimSpace(value) == "" {
			return ErrInvalidCorrection
		}
	}
	if c.CorrectsDigest == c.CorrectedDigest {
		return ErrInvalidCorrection
	}
	if err := c.Provenance.validate(ErrInvalidCorrection); err != nil {
		return err
	}
	if c.EffectiveAt.Validate() != nil || c.KnownAt.Validate() != nil {
		return ErrInvalidCorrection
	}
	if c.CanonicalDigest == "" || c.CanonicalDigest != canonicalbytes.Digest(c.canonical()) {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidCorrection)
	}
	return nil
}

func NewSuccessionCorrection(c SuccessionCorrection, v EvidenceVerifier) (SuccessionCorrection, error) {
	c.Provenance.EvidenceRefs = append([]string(nil), c.Provenance.EvidenceRefs...)
	providedDigest := c.CanonicalDigest
	c.CanonicalDigest = canonicalbytes.Digest(c.canonical())
	if providedDigest != "" && providedDigest != c.CanonicalDigest {
		return SuccessionCorrection{}, fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidCorrection)
	}
	if err := c.Validate(); err != nil {
		return SuccessionCorrection{}, err
	}
	if err := verify(v, c.Provenance, c.SubjectID, actionCorrect, actionCorrect, c.DecisionDigest); err != nil {
		return SuccessionCorrection{}, err
	}
	if err := v.VerifyCorrectionSource(c.Provenance.TenantID, c.SubjectID, c.DecisionDigest, c.CorrectsDigest); err != nil {
		return SuccessionCorrection{}, fmt.Errorf("%w: correction source: %v", ErrUnauthorized, err)
	}
	return c, nil
}

// VacancySelectionIntent requests a separately governed workflow. It has no
// candidate, outcome, promotion, appointment, or write instruction.
type VacancySelectionIntent struct {
	IntentID        string
	CriticalRoleID  string
	SlateID         string
	SlateRevision   uint64
	Purpose         string
	NonGuarantee    bool
	Provenance      Provenance
	EffectiveAt     values.Instant
	KnownAt         values.Instant
	CanonicalDigest string
}

func (i VacancySelectionIntent) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.succession.VacancySelectionIntent", schemaVersion).
		String("tenant_id", i.Provenance.TenantID).String("intent_id", i.IntentID).String("critical_role_id", i.CriticalRoleID).
		String("slate_id", i.SlateID).Int("slate_revision", int64(i.SlateRevision)).String("purpose", i.Purpose).Bool("non_guarantee", i.NonGuarantee).
		String("authority_ref", i.Provenance.AuthorityRef).String("recorded_by", i.Provenance.RecordedBy).
		SortedStrings("evidence_ref", sortedStrings(i.Provenance.EvidenceRefs)).String("consent_ref", i.Provenance.ConsentRef).
		String("privacy_class", i.Provenance.PrivacyClass).Value("effective_at", i.EffectiveAt).Value("known_at", i.KnownAt)
	b, _ := w.Bytes()
	return b
}

func (i VacancySelectionIntent) Validate() error {
	for _, value := range []string{i.IntentID, i.CriticalRoleID, i.SlateID, i.Purpose} {
		if strings.TrimSpace(value) == "" {
			return ErrInvalidSelection
		}
	}
	if i.SlateRevision == 0 || !i.NonGuarantee {
		return ErrGuaranteeSemantics
	}
	if err := i.Provenance.validate(ErrInvalidSelection); err != nil {
		return err
	}
	if i.EffectiveAt.Validate() != nil || i.KnownAt.Validate() != nil {
		return ErrInvalidSelection
	}
	if i.CanonicalDigest == "" || i.CanonicalDigest != canonicalbytes.Digest(i.canonical()) {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidSelection)
	}
	return nil
}

func NewVacancySelectionIntent(i VacancySelectionIntent, subject string, v EvidenceVerifier) (VacancySelectionIntent, error) {
	i.Provenance.EvidenceRefs = append([]string(nil), i.Provenance.EvidenceRefs...)
	providedDigest := i.CanonicalDigest
	i.CanonicalDigest = canonicalbytes.Digest(i.canonical())
	if providedDigest != "" && providedDigest != i.CanonicalDigest {
		return VacancySelectionIntent{}, fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidSelection)
	}
	if err := i.Validate(); err != nil {
		return VacancySelectionIntent{}, err
	}
	if err := verify(v, i.Provenance, subject, i.Purpose, actionSelect, i.SlateID); err != nil {
		return VacancySelectionIntent{}, err
	}
	return i, nil
}
