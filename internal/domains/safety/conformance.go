package safety

// This file contains the conformance-only workflow facts for workers'
// compensation. It deliberately does not encode jurisdictional, legal, or
// medical rules: callers provide the applicable obligation and evidence refs.

import (
	"regexp"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

var isoCurrencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

var (
	ErrFreshObservationRequired = refused("SAFETY_FRESH_OBSERVATION", "observation_ref", "a fresh external observation is required", ErrRefused)
	ErrPendingObligation        = refused("SAFETY_PENDING_OBLIGATION", "obligations", "all obligations must be discharged before close", ErrRefused)
	ErrInvalidTransition        = refused("SAFETY_INVALID_TRANSITION", "status", "the requested lifecycle transition is not permitted", ErrRefused)
)

type FilingStatus string

const (
	FilingSubmitted FilingStatus = "SUBMITTED"
	FilingAccepted  FilingStatus = "ACCEPTED"
	FilingRejected  FilingStatus = "REJECTED"
	FilingAmended   FilingStatus = "AMENDED"
)

func (s FilingStatus) Valid() bool {
	return s == FilingSubmitted || s == FilingAccepted || s == FilingRejected || s == FilingAmended
}

// FilingRevision records an attempt or observation. A retry is a new revision,
// never a mutation of a timed-out submission.
type FilingRevision struct {
	ID, CaseRef, CompartmentRef string
	Revision, ParentRevision    uint64
	ParentDigest                string
	IncidentRef, AuthorityRef   string
	ProviderRef, SubmissionRef  string
	SignerRef, SignatureRef     string
	Status                      FilingStatus
	ObservationRef              string
	ObservedAt                  values.Instant
	Obligations                 []string
	CanonicalDigest             string
}

func (r FilingRevision) Validate() error {
	if err := validateLineage("filing", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.CaseRef, CompartmentRegulatory, r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.CompartmentRef) == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	for field, value := range map[string]string{"incident_ref": r.IncidentRef, "authority_ref": r.AuthorityRef, "provider_ref": r.ProviderRef, "submission_ref": r.SubmissionRef, "signer_ref": r.SignerRef, "signature_ref": r.SignatureRef} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if !r.Status.Valid() {
		return invalid("status", "filing status is not declared")
	}
	if r.Status != FilingSubmitted && strings.TrimSpace(r.ObservationRef) == "" {
		return ErrFreshObservationRequired
	}
	if (strings.TrimSpace(r.ObservationRef) == "") != (!r.ObservedAt.IsSet()) {
		return ErrFreshObservationRequired
	}
	if r.ObservedAt.IsSet() {
		if err := r.ObservedAt.Validate(); err != nil {
			return invalid("observed_at", err.Error())
		}
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r FilingRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.safety.FilingRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).String("incident_ref", r.IncidentRef).String("authority_ref", r.AuthorityRef).String("provider_ref", r.ProviderRef).String("submission_ref", r.SubmissionRef).String("signer_ref", r.SignerRef).String("signature_ref", r.SignatureRef).String("status", string(r.Status)).String("observation_ref", r.ObservationRef).Value("observed_at", r.ObservedAt)
	for _, o := range sortedRefs(r.Obligations) {
		w.String("obligation", o)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r FilingRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r FilingRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func NewFilingRevision(r FilingRevision) (FilingRevision, error) {
	r.Obligations = append([]string(nil), r.Obligations...)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return FilingRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}

// ResubmitFiling creates a distinct successor only after the prior attempt
// has a fresh rejection observation. Acceptance and timeout never authorize
// a duplicate submission.
func ResubmitFiling(previous FilingRevision, submissionRef, signatureRef, observationRef string, observedAt values.Instant) (FilingRevision, error) {
	if err := previous.Validate(); err != nil {
		return FilingRevision{}, err
	}
	if previous.Status != FilingRejected {
		return FilingRevision{}, ErrInvalidTransition
	}
	if strings.TrimSpace(previous.ObservationRef) == "" || strings.TrimSpace(observationRef) == "" || observationRef == previous.ObservationRef || !FreshObservation(previous.ObservedAt, observedAt) {
		return FilingRevision{}, ErrFreshObservationRequired
	}
	if strings.TrimSpace(submissionRef) == "" || submissionRef == previous.SubmissionRef || strings.TrimSpace(signatureRef) == "" || signatureRef == previous.SignatureRef {
		return FilingRevision{}, ErrInvalidTransition
	}
	return NewFilingRevision(FilingRevision{ID: previous.ID, CaseRef: previous.CaseRef, CompartmentRef: previous.CompartmentRef, Revision: previous.Revision + 1, ParentRevision: previous.Revision, ParentDigest: previous.CanonicalDigest, IncidentRef: previous.IncidentRef, AuthorityRef: previous.AuthorityRef, ProviderRef: previous.ProviderRef, SubmissionRef: submissionRef, SignerRef: previous.SignerRef, SignatureRef: signatureRef, Status: FilingSubmitted, ObservationRef: observationRef, ObservedAt: observedAt, Obligations: append([]string(nil), previous.Obligations...)})
}

type PaymentStatus string

const (
	PaymentObserved PaymentStatus = "OBSERVED"
	PaymentSettled  PaymentStatus = "SETTLED"
	PaymentReversed PaymentStatus = "REVERSED"
)

func (s PaymentStatus) Valid() bool {
	return s == PaymentObserved || s == PaymentSettled || s == PaymentReversed
}

// WorkersCompPaymentRevision stores exact minor units. Reversal is an
// append-only successor and never rewrites the original amount or ledger.
type WorkersCompPaymentRevision struct {
	ID, CaseRef, CompartmentRef string
	Revision, ParentRevision    uint64
	ParentDigest                string
	ClaimRef, WorkerRef         string
	AmountMinor                 int64
	Currency                    string
	Status                      PaymentStatus
	ObservationRef              string
	ReversalRef                 string
	CanonicalDigest             string
}

func (r WorkersCompPaymentRevision) Validate() error {
	if err := validateLineage("workers-comp payment", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.CaseRef, CompartmentClaims, r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.CompartmentRef) == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	for field, value := range map[string]string{"claim_ref": r.ClaimRef, "worker_ref": r.WorkerRef, "currency": r.Currency} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if r.AmountMinor <= 0 {
		return invalid("amount_minor", "amount must be positive and exact")
	}
	if !isoCurrencyCode.MatchString(r.Currency) {
		return invalid("currency", "currency must be a three-letter uppercase code")
	}
	if !r.Status.Valid() {
		return invalid("status", "payment status is not declared")
	}
	if strings.TrimSpace(r.ObservationRef) == "" {
		return ErrFreshObservationRequired
	}
	if r.Status == PaymentReversed && strings.TrimSpace(r.ReversalRef) == "" {
		return invalid("reversal_ref", "reversal requires an idempotency reference")
	}
	if r.Status == PaymentReversed && r.Revision == 1 {
		return ErrInvalidTransition
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r WorkersCompPaymentRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.safety.WorkersCompPaymentRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).String("claim_ref", r.ClaimRef).String("worker_ref", r.WorkerRef).Int("amount_minor", r.AmountMinor).String("currency", r.Currency).String("status", string(r.Status)).String("observation_ref", r.ObservationRef).String("reversal_ref", r.ReversalRef)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r WorkersCompPaymentRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r WorkersCompPaymentRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func NewWorkersCompPaymentRevision(r WorkersCompPaymentRevision) (WorkersCompPaymentRevision, error) {
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return WorkersCompPaymentRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}

// RestrictionClearanceRevision is an append-only medical observation. It does
// not alter or delete the WorkRestrictionRevision it references.
type RestrictionClearanceRevision struct {
	ID, CaseRef, CompartmentRef  string
	Revision, ParentRevision     uint64
	ParentDigest                 string
	RestrictionRef, WorkerRef    string
	EvidenceRef, ObservationRef  string
	AuthorityRef, EvidenceDigest string
	CanonicalDigest              string
}

func (r RestrictionClearanceRevision) Validate() error {
	if err := validateLineage("restriction clearance", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.CaseRef, CompartmentMedical, r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.CompartmentRef) == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	for field, value := range map[string]string{"restriction_ref": r.RestrictionRef, "worker_ref": r.WorkerRef, "evidence_ref": r.EvidenceRef, "observation_ref": r.ObservationRef, "authority_ref": r.AuthorityRef, "evidence_digest": r.EvidenceDigest} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r RestrictionClearanceRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.safety.RestrictionClearanceRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).String("restriction_ref", r.RestrictionRef).String("worker_ref", r.WorkerRef).String("evidence_ref", r.EvidenceRef).String("evidence_digest", r.EvidenceDigest).String("authority_ref", r.AuthorityRef).String("observation_ref", r.ObservationRef)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r RestrictionClearanceRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func NewRestrictionClearanceRevision(r RestrictionClearanceRevision) (RestrictionClearanceRevision, error) {
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return RestrictionClearanceRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}

type ReconciliationStatus string

const (
	ReconciliationOpen   ReconciliationStatus = "OPEN"
	ReconciliationClosed ReconciliationStatus = "CLOSED"
)

type SafetyReconciliationRevision struct {
	ID, CaseRef, CompartmentRef string
	Revision, ParentRevision    uint64
	ParentDigest                string
	IncidentRef                 string
	SourceRevisionDigest        string
	ObservationRef              string
	AmendedFilingRef            string
	RepairRef                   string
	Obligations                 []string
	ObservedAt                  values.Instant
	PriorObservedAt             values.Instant
	Status                      ReconciliationStatus
	CanonicalDigest             string
}

type Reconciliation = SafetyReconciliationRevision

func (r SafetyReconciliationRevision) Validate() error {
	if err := validateLineage("safety reconciliation", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.CaseRef, CompartmentRegulatory, r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.CompartmentRef) == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	if strings.TrimSpace(r.IncidentRef) == "" || strings.TrimSpace(r.SourceRevisionDigest) == "" {
		return invalid("incident_ref", "incident and source revision references are required")
	}
	if strings.TrimSpace(r.ObservationRef) == "" || !r.ObservedAt.IsSet() {
		return ErrFreshObservationRequired
	}
	if err := r.ObservedAt.Validate(); err != nil {
		return invalid("observed_at", err.Error())
	}
	if !r.Status.Valid() {
		return invalid("status", "reconciliation status is not declared")
	}
	if r.Status == ReconciliationClosed {
		if !FreshObservation(r.PriorObservedAt, r.ObservedAt) {
			return ErrFreshObservationRequired
		}
		if len(r.Obligations) != 0 {
			return ErrPendingObligation
		}
		if strings.TrimSpace(r.AmendedFilingRef) == "" && strings.TrimSpace(r.RepairRef) == "" {
			return invalid("repair_ref", "closure requires an amendment or repair reference")
		}
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (s ReconciliationStatus) Valid() bool {
	return s == ReconciliationOpen || s == ReconciliationClosed
}
func (r SafetyReconciliationRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.safety.SafetyReconciliationRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).String("incident_ref", r.IncidentRef).String("source_revision_digest", r.SourceRevisionDigest).String("observation_ref", r.ObservationRef).Value("observed_at", r.ObservedAt).Value("prior_observed_at", r.PriorObservedAt).String("amended_filing_ref", r.AmendedFilingRef).String("repair_ref", r.RepairRef).String("status", string(r.Status))
	for _, o := range sortedRefs(r.Obligations) {
		w.String("obligation", o)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r SafetyReconciliationRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func NewSafetyReconciliationRevision(r SafetyReconciliationRevision) (SafetyReconciliationRevision, error) {
	r.Obligations = append([]string(nil), r.Obligations...)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return SafetyReconciliationRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}

// SafetyCorrectionRevision records a retroactive correction and the resulting
// amendment/repair reference. The source revision remains immutable.
type SafetyCorrectionRevision struct {
	ID, CaseRef, CompartmentRef string
	Revision, ParentRevision    uint64
	ParentDigest                string
	IncidentRef                 string
	SourceRevisionDigest        string
	Reason, EvidenceRef         string
	AmendedFilingRef, RepairRef string
	CanonicalDigest             string
}

type CorrectionRevision = SafetyCorrectionRevision

func (r SafetyCorrectionRevision) Validate() error {
	if err := validateLineage("safety correction", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.CaseRef, CompartmentRegulatory, r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.CompartmentRef) == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	for field, value := range map[string]string{"incident_ref": r.IncidentRef, "source_revision_digest": r.SourceRevisionDigest, "reason": r.Reason, "evidence_ref": r.EvidenceRef} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if strings.TrimSpace(r.AmendedFilingRef) == "" && strings.TrimSpace(r.RepairRef) == "" {
		return invalid("amended_filing_ref", "correction requires an amendment or repair reference")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r SafetyCorrectionRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.safety.SafetyCorrectionRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).String("incident_ref", r.IncidentRef).String("source_revision_digest", r.SourceRevisionDigest).String("reason", r.Reason).String("evidence_ref", r.EvidenceRef).String("amended_filing_ref", r.AmendedFilingRef).String("repair_ref", r.RepairRef)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r SafetyCorrectionRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func NewSafetyCorrectionRevision(r SafetyCorrectionRevision) (SafetyCorrectionRevision, error) {
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return SafetyCorrectionRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewCorrectionRevision(r SafetyCorrectionRevision) (SafetyCorrectionRevision, error) {
	return NewSafetyCorrectionRevision(r)
}

// FreshObservation reports whether an observation is newer than the prior
// known time. It is intentionally time-input based and never consults a clock.
func FreshObservation(previous, observed values.Instant) bool {
	return previous.IsSet() && observed.IsSet() && observed.Time().After(previous.Time())
}
