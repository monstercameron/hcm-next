// Package safety owns pure, append-only workplace-safety records. Operational,
// medical, claim, and regulatory facts are separate typed records and can be
// adapted to storage or workflow by callers without importing those concerns.
package safety

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

func Version() int { return schemaVersion }

var (
	ErrInvalidRevision      = errors.New("safety: invalid revision")
	ErrRefused              = errors.New("safety: operation refused")
	ErrIncidentRequired     = errors.New("safety: claim requires an incident reference")
	ErrRuleRequired         = errors.New("safety: reportability rule citation is required")
	ErrVerificationRequired = errors.New("safety: corrective action closure requires verification evidence")
)

// ValidationError is a typed field-level refusal.
type ValidationError struct{ Field, Reason string }

func (e *ValidationError) Error() string { return "safety: " + e.Field + ": " + e.Reason }
func (e *ValidationError) Unwrap() error { return ErrInvalidRevision }

// RefusalError represents a domain rule refusal rather than malformed input.
type RefusalError struct {
	Code, Field, Reason string
	Cause               error
}

func (e *RefusalError) Error() string {
	if e.Field == "" {
		return "safety: " + e.Code + ": " + e.Reason
	}
	return "safety: " + e.Code + ": " + e.Field + ": " + e.Reason
}
func (e *RefusalError) Unwrap() error        { return e.Cause }
func (e *RefusalError) Is(target error) bool { return target == ErrRefused || target == e.Cause }
func invalid(field, reason string) error     { return &ValidationError{Field: field, Reason: reason} }
func refused(code, field, reason string, cause error) error {
	return &RefusalError{Code: code, Field: field, Reason: reason, Cause: cause}
}

// SafetyCompartment prevents operational, medical, claims, and regulatory
// material from being represented as one unrestricted record.
type SafetyCompartment string

const (
	CompartmentOperational SafetyCompartment = "OPERATIONAL"
	CompartmentMedical     SafetyCompartment = "MEDICAL"
	CompartmentClaims      SafetyCompartment = "CLAIMS"
	CompartmentRegulatory  SafetyCompartment = "REGULATORY"
)

func (c SafetyCompartment) Valid() bool {
	return c == CompartmentOperational || c == CompartmentMedical || c == CompartmentClaims || c == CompartmentRegulatory
}

type IncidentKind string

const (
	IncidentInjury         IncidentKind = "INJURY"
	IncidentIllness        IncidentKind = "ILLNESS"
	IncidentNearMiss       IncidentKind = "NEAR_MISS"
	IncidentPropertyDamage IncidentKind = "PROPERTY_DAMAGE"
)

func (k IncidentKind) Valid() bool {
	return k == IncidentInjury || k == IncidentIllness || k == IncidentNearMiss || k == IncidentPropertyDamage
}

type IncidentStatus string

const (
	IncidentOpen   IncidentStatus = "OPEN"
	IncidentClosed IncidentStatus = "CLOSED"
)

func (s IncidentStatus) Valid() bool { return s == IncidentOpen || s == IncidentClosed }

func validateLineage(kind, id string, revision, parentRevision uint64, parentDigest string, caseRef string, compartment SafetyCompartment, digest string) error {
	if strings.TrimSpace(id) == "" {
		return invalid("id", kind+" id is required")
	}
	if revision == 0 {
		return invalid("revision", "revision must be non-zero")
	}
	if revision == 1 && (parentRevision != 0 || strings.TrimSpace(parentDigest) != "") {
		return invalid("parent_revision", "first revision cannot have a parent")
	}
	if revision > 1 {
		if parentRevision != revision-1 {
			return invalid("parent_revision", "successor must point to the immediately prior revision")
		}
		if strings.TrimSpace(parentDigest) == "" {
			return invalid("parent_digest", "successor requires the parent digest")
		}
	}
	if strings.TrimSpace(caseRef) == "" {
		return invalid("case_ref", "case reference is required")
	}
	if !compartment.Valid() {
		return invalid("compartment", "compartment is not declared")
	}
	_ = digest
	return nil
}
func validateDigest(expected, actual string) error {
	if expected != "" && expected != actual {
		return invalid("canonical_digest", "canonical digest mismatch")
	}
	return nil
}
func sortedRefs(refs []string) []string {
	out := append([]string(nil), refs...)
	sort.Strings(out)
	return out
}

// IncidentRevision is the operational fact from which regulatory deadlines
// are derived. IncidentAt is always explicit; no package clock is consulted.
type IncidentRevision struct {
	ID, CaseRef, CompartmentRef string
	Revision, ParentRevision    uint64
	ParentDigest                string
	IncidentAt                  values.Instant
	Kind                        IncidentKind
	WorkerRef, ReporterRef      string
	LocationRef, Description    string
	Status                      IncidentStatus
	CanonicalDigest             string
}

type Incident = IncidentRevision

func (r IncidentRevision) Validate() error {
	if err := validateLineage("incident", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.CaseRef, CompartmentOperational, r.CanonicalDigest); err != nil {
		return err
	}
	if r.CompartmentRef == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	if !r.IncidentAt.IsSet() {
		return invalid("incident_at", "incident instant is required")
	}
	if err := r.IncidentAt.Validate(); err != nil {
		return invalid("incident_at", err.Error())
	}
	if !r.Kind.Valid() {
		return invalid("kind", "incident kind is not declared")
	}
	for field, value := range map[string]string{"worker_ref": r.WorkerRef, "reporter_ref": r.ReporterRef, "location_ref": r.LocationRef, "description": r.Description} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if !r.Status.Valid() {
		return invalid("status", "incident status is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r IncidentRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.safety.IncidentRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).Value("incident_at", r.IncidentAt).String("kind", string(r.Kind)).String("worker_ref", r.WorkerRef).String("reporter_ref", r.ReporterRef).String("location_ref", r.LocationRef).String("description", r.Description).String("status", string(r.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r IncidentRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r IncidentRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r IncidentRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r IncidentRevision) Explain() string {
	return fmt.Sprintf("incident %s in case %s, compartment %s, revision %d, kind %s, instant recorded, status %s, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.Kind, r.Status, r.CanonicalDigest)
}
func NewIncidentRevision(r IncidentRevision) (IncidentRevision, error) {
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return IncidentRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewIncident(r IncidentRevision) (IncidentRevision, error) { return NewIncidentRevision(r) }

type InjuryKind string

const (
	InjuryPhysical InjuryKind = "PHYSICAL"
	InjuryIllness  InjuryKind = "ILLNESS"
	InjuryExposure InjuryKind = "EXPOSURE"
)

func (k InjuryKind) Valid() bool {
	return k == InjuryPhysical || k == InjuryIllness || k == InjuryExposure
}

// InjuryRevision is medical-compartment metadata. It intentionally stores
// references rather than unrestricted medical narrative.
type InjuryRevision struct {
	ID, CaseRef, CompartmentRef string
	Revision, ParentRevision    uint64
	ParentDigest                string
	IncidentRef, WorkerRef      string
	Kind                        InjuryKind
	MedicalEvidenceRef          string
	Severity                    string
	CanonicalDigest             string
}

type Injury = InjuryRevision

func (r InjuryRevision) Validate() error {
	if err := validateLineage("injury", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.CaseRef, CompartmentMedical, r.CanonicalDigest); err != nil {
		return err
	}
	if r.CompartmentRef == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	for field, value := range map[string]string{"incident_ref": r.IncidentRef, "worker_ref": r.WorkerRef, "medical_evidence_ref": r.MedicalEvidenceRef, "severity": r.Severity} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if !r.Kind.Valid() {
		return invalid("kind", "injury kind is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r InjuryRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.safety.InjuryRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).String("incident_ref", r.IncidentRef).String("worker_ref", r.WorkerRef).String("kind", string(r.Kind)).String("medical_evidence_ref", r.MedicalEvidenceRef).String("severity", r.Severity)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r InjuryRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r InjuryRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r InjuryRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r InjuryRevision) Explain() string {
	return fmt.Sprintf("injury %s in case %s, medical compartment %s, revision %d, incident %s, severity recorded, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.IncidentRef, r.CanonicalDigest)
}
func NewInjuryRevision(r InjuryRevision) (InjuryRevision, error) {
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return InjuryRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewInjury(r InjuryRevision) (InjuryRevision, error) { return NewInjuryRevision(r) }

// ReportabilityClass is the closed OSHA-style outcome vocabulary.
type ReportabilityClass string

const (
	NotReportable          ReportabilityClass = "NOT_REPORTABLE"
	OSHARecordable         ReportabilityClass = "OSHA_RECORDABLE"
	OSHAReportableFatality ReportabilityClass = "OSHA_REPORTABLE_FATALITY"
	OSHAReportableSevere   ReportabilityClass = "OSHA_REPORTABLE_SEVERE"
)

func (c ReportabilityClass) Valid() bool {
	return c == NotReportable || c == OSHARecordable || c == OSHAReportableFatality || c == OSHAReportableSevere
}

type ReportabilityClock string

const (
	ClockNone               ReportabilityClock = "NONE"
	ClockRecordkeeping7Days ReportabilityClock = "RECORDKEEPING_7_DAYS"
	ClockFatality8Hours     ReportabilityClock = "FATALITY_8_HOURS"
	ClockSevere24Hours      ReportabilityClock = "SEVERE_24_HOURS"
)

// ClockRule is the declared clock table used by reportability calculations.
type ClockRule struct {
	Class    ReportabilityClass
	Clock    ReportabilityClock
	Duration time.Duration
	Citation string
}

var clockTable = []ClockRule{
	{Class: NotReportable, Clock: ClockNone, Duration: 0, Citation: "not applicable"},
	{Class: OSHARecordable, Clock: ClockRecordkeeping7Days, Duration: 7 * 24 * time.Hour, Citation: "29 CFR 1904.7"},
	{Class: OSHAReportableFatality, Clock: ClockFatality8Hours, Duration: 8 * time.Hour, Citation: "29 CFR 1904.39"},
	{Class: OSHAReportableSevere, Clock: ClockSevere24Hours, Duration: 24 * time.Hour, Citation: "29 CFR 1904.39"},
}

func ClockTable() []ClockRule { return append([]ClockRule(nil), clockTable...) }
func RuleFor(class ReportabilityClass) (ClockRule, bool) {
	for _, r := range clockTable {
		if r.Class == class {
			return r, true
		}
	}
	return ClockRule{}, false
}

// ReportabilityDeterminationRevision records both the result and the rule
// citation used to derive its deadline from IncidentAt.
type ReportabilityDeterminationRevision struct {
	ID, CaseRef, CompartmentRef string
	Revision, ParentRevision    uint64
	ParentDigest                string
	IncidentRef                 string
	IncidentAt                  values.Instant
	Class                       ReportabilityClass
	Clock                       ReportabilityClock
	RuleCitation                string
	Deadline                    values.Instant
	Rationale                   string
	CanonicalDigest             string
}
type ReportabilityDetermination = ReportabilityDeterminationRevision
type ReportabilityRevision = ReportabilityDeterminationRevision

func (r ReportabilityDeterminationRevision) Validate() error {
	if err := validateLineage("reportability determination", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.CaseRef, CompartmentRegulatory, r.CanonicalDigest); err != nil {
		return err
	}
	if r.CompartmentRef == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	if strings.TrimSpace(r.IncidentRef) == "" {
		return invalid("incident_ref", "incident reference is required")
	}
	if !r.IncidentAt.IsSet() {
		return invalid("incident_at", "incident instant is required")
	}
	if !r.Class.Valid() {
		return invalid("class", "reportability class is not declared")
	}
	rule, ok := RuleFor(r.Class)
	if !ok {
		return invalid("class", "no declared clock rule")
	}
	if r.Clock != rule.Clock {
		return invalid("clock", "clock does not match the declared class rule")
	}
	if strings.TrimSpace(r.RuleCitation) == "" {
		return refused("SAFETY_RULE_CITATION", "rule_citation", "reportability requires the cited rule", ErrRuleRequired)
	}
	if r.RuleCitation != rule.Citation {
		return invalid("rule_citation", "citation does not match the declared class rule")
	}
	if !r.Deadline.IsSet() {
		return invalid("deadline", "derived deadline is required")
	}
	if r.Deadline.Time().Before(r.IncidentAt.Time()) {
		return invalid("deadline", "deadline cannot precede incident instant")
	}
	if strings.TrimSpace(r.Rationale) == "" {
		return invalid("rationale", "rationale is required")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r ReportabilityDeterminationRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.safety.ReportabilityDeterminationRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).String("incident_ref", r.IncidentRef).Value("incident_at", r.IncidentAt).String("class", string(r.Class)).String("clock", string(r.Clock)).String("rule_citation", r.RuleCitation).Value("deadline", r.Deadline).String("rationale", r.Rationale)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r ReportabilityDeterminationRevision) computedDigest() string {
	return canonicalbytes.Digest(r.body())
}
func (r ReportabilityDeterminationRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r ReportabilityDeterminationRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r ReportabilityDeterminationRevision) Explain() string {
	return fmt.Sprintf("reportability %s in case %s, regulatory compartment %s, revision %d, class %s, clock %s, rule %s, deadline derived, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.Class, r.Clock, r.RuleCitation, r.CanonicalDigest)
}
func NewReportabilityDeterminationRevision(r ReportabilityDeterminationRevision) (ReportabilityDeterminationRevision, error) {
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return ReportabilityDeterminationRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewReportabilityRevision(r ReportabilityDeterminationRevision) (ReportabilityDeterminationRevision, error) {
	return NewReportabilityDeterminationRevision(r)
}
func DetermineReportability(incident IncidentRevision, class ReportabilityClass, id string) (ReportabilityDeterminationRevision, error) {
	if err := incident.Validate(); err != nil {
		return ReportabilityDeterminationRevision{}, err
	}
	rule, ok := RuleFor(class)
	if !ok {
		return ReportabilityDeterminationRevision{}, invalid("class", "no declared clock rule")
	}
	deadline := values.NewInstant(incident.IncidentAt.Time().Add(rule.Duration))
	return NewReportabilityDeterminationRevision(ReportabilityDeterminationRevision{ID: id, CaseRef: incident.CaseRef, CompartmentRef: "regulatory", Revision: 1, IncidentRef: incident.ID, IncidentAt: incident.IncidentAt, Class: class, Clock: rule.Clock, RuleCitation: rule.Citation, Deadline: deadline, Rationale: "derived from incident instant and declared clock table"})
}

type ClaimStatus string

const (
	ClaimDraft     ClaimStatus = "DRAFT"
	ClaimSubmitted ClaimStatus = "SUBMITTED"
	ClaimAccepted  ClaimStatus = "ACCEPTED"
	ClaimPaid      ClaimStatus = "PAID"
)

func (s ClaimStatus) Valid() bool {
	return s == ClaimDraft || s == ClaimSubmitted || s == ClaimAccepted || s == ClaimPaid
}

// ClaimRevision is a claims-compartment record and cannot exist without an
// incident reference.
type ClaimRevision struct {
	ID, CaseRef, CompartmentRef string
	Revision, ParentRevision    uint64
	ParentDigest                string
	IncidentRef, WorkerRef      string
	ClaimRef, AuthorityRef      string
	Status                      ClaimStatus
	CanonicalDigest             string
}

type Claim = ClaimRevision

func (r ClaimRevision) Validate() error {
	if err := validateLineage("claim", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.CaseRef, CompartmentClaims, r.CanonicalDigest); err != nil {
		return err
	}
	if r.CompartmentRef == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	if strings.TrimSpace(r.IncidentRef) == "" {
		return refused("SAFETY_CLAIM_INCIDENT", "incident_ref", "claim requires an incident reference", ErrIncidentRequired)
	}
	for field, value := range map[string]string{"worker_ref": r.WorkerRef, "claim_ref": r.ClaimRef, "authority_ref": r.AuthorityRef} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if !r.Status.Valid() {
		return invalid("status", "claim status is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r ClaimRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.safety.ClaimRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).String("incident_ref", r.IncidentRef).String("worker_ref", r.WorkerRef).String("claim_ref", r.ClaimRef).String("authority_ref", r.AuthorityRef).String("status", string(r.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r ClaimRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r ClaimRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r ClaimRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r ClaimRevision) Explain() string {
	return fmt.Sprintf("claim %s in case %s, claims compartment %s, revision %d, incident %s, status %s, authority bound, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.IncidentRef, r.Status, r.CanonicalDigest)
}
func NewClaimRevision(r ClaimRevision) (ClaimRevision, error) {
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return ClaimRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewClaim(r ClaimRevision) (ClaimRevision, error) { return NewClaimRevision(r) }

type RestrictionStatus string

const (
	RestrictionProposed RestrictionStatus = "PROPOSED"
	RestrictionActive   RestrictionStatus = "ACTIVE"
	RestrictionCleared  RestrictionStatus = "CLEARED"
)

func (s RestrictionStatus) Valid() bool {
	return s == RestrictionProposed || s == RestrictionActive || s == RestrictionCleared
}

type RestrictionKind string

const (
	RestrictionNoLift       RestrictionKind = "NO_LIFTING"
	RestrictionModifiedDuty RestrictionKind = "MODIFIED_DUTY"
	RestrictionNoExposure   RestrictionKind = "NO_EXPOSURE"
)

func (k RestrictionKind) Valid() bool {
	return k == RestrictionNoLift || k == RestrictionModifiedDuty || k == RestrictionNoExposure
}

// WorkRestrictionRevision contains a closed restriction vocabulary and a
// medical evidence reference, never unrestricted medical notes.
type WorkRestrictionRevision struct {
	ID, CaseRef, CompartmentRef string
	Revision, ParentRevision    uint64
	ParentDigest                string
	IncidentRef, WorkerRef      string
	Kind                        RestrictionKind
	MedicalEvidenceRef          string
	Status                      RestrictionStatus
	CanonicalDigest             string
}
type WorkRestriction = WorkRestrictionRevision
type RestrictionRevision = WorkRestrictionRevision

func (r WorkRestrictionRevision) Validate() error {
	if err := validateLineage("work restriction", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.CaseRef, CompartmentMedical, r.CanonicalDigest); err != nil {
		return err
	}
	if r.CompartmentRef == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	for field, value := range map[string]string{"incident_ref": r.IncidentRef, "worker_ref": r.WorkerRef, "medical_evidence_ref": r.MedicalEvidenceRef} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if !r.Kind.Valid() {
		return invalid("kind", "restriction kind is not declared")
	}
	if !r.Status.Valid() {
		return invalid("status", "restriction status is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r WorkRestrictionRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.safety.WorkRestrictionRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).String("incident_ref", r.IncidentRef).String("worker_ref", r.WorkerRef).String("kind", string(r.Kind)).String("medical_evidence_ref", r.MedicalEvidenceRef).String("status", string(r.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r WorkRestrictionRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r WorkRestrictionRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r WorkRestrictionRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r WorkRestrictionRevision) Explain() string {
	return fmt.Sprintf("work restriction %s in case %s, medical compartment %s, revision %d, incident %s, kind %s, status %s, medical detail protected, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.IncidentRef, r.Kind, r.Status, r.CanonicalDigest)
}
func NewWorkRestrictionRevision(r WorkRestrictionRevision) (WorkRestrictionRevision, error) {
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return WorkRestrictionRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewRestrictionRevision(r WorkRestrictionRevision) (WorkRestrictionRevision, error) {
	return NewWorkRestrictionRevision(r)
}

type CorrectiveActionStatus string

const (
	CorrectiveActionOpen     CorrectiveActionStatus = "OPEN"
	CorrectiveActionVerified CorrectiveActionStatus = "VERIFIED"
	CorrectiveActionClosed   CorrectiveActionStatus = "CLOSED"
)

func (s CorrectiveActionStatus) Valid() bool {
	return s == CorrectiveActionOpen || s == CorrectiveActionVerified || s == CorrectiveActionClosed
}

// CorrectiveActionRevision binds an owner, due rule, and verification evidence
// to an incident. CLOSED is impossible without fresh verification evidence.
type CorrectiveActionRevision struct {
	ID, CaseRef, CompartmentRef      string
	Revision, ParentRevision         uint64
	ParentDigest                     string
	IncidentRef, OwnerRef            string
	DueRule, VerificationEvidenceRef string
	Action                           string
	Status                           CorrectiveActionStatus
	CanonicalDigest                  string
}

type CorrectiveAction = CorrectiveActionRevision

func (r CorrectiveActionRevision) Validate() error {
	if err := validateLineage("corrective action", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.CaseRef, CompartmentOperational, r.CanonicalDigest); err != nil {
		return err
	}
	if r.CompartmentRef == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	for field, value := range map[string]string{"incident_ref": r.IncidentRef, "owner_ref": r.OwnerRef, "due_rule": r.DueRule, "action": r.Action} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if !r.Status.Valid() {
		return invalid("status", "corrective-action status is not declared")
	}
	if r.Status == CorrectiveActionClosed && strings.TrimSpace(r.VerificationEvidenceRef) == "" {
		return refused("SAFETY_CORRECTIVE_VERIFICATION", "verification_evidence_ref", "closure requires verification evidence", ErrVerificationRequired)
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r CorrectiveActionRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.safety.CorrectiveActionRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).String("incident_ref", r.IncidentRef).String("owner_ref", r.OwnerRef).String("due_rule", r.DueRule).String("verification_evidence_ref", r.VerificationEvidenceRef).String("action", r.Action).String("status", string(r.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r CorrectiveActionRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r CorrectiveActionRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r CorrectiveActionRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r CorrectiveActionRevision) Explain() string {
	return fmt.Sprintf("corrective action %s in case %s, operational compartment %s, revision %d, incident %s, owner assigned, status %s, verification state recorded, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.IncidentRef, r.Status, r.CanonicalDigest)
}
func NewCorrectiveActionRevision(r CorrectiveActionRevision) (CorrectiveActionRevision, error) {
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return CorrectiveActionRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewCorrectiveAction(r CorrectiveActionRevision) (CorrectiveActionRevision, error) {
	return NewCorrectiveActionRevision(r)
}

func Explain(record any) string {
	switch r := record.(type) {
	case IncidentRevision:
		return r.Explain()
	case InjuryRevision:
		return r.Explain()
	case ReportabilityDeterminationRevision:
		return r.Explain()
	case ClaimRevision:
		return r.Explain()
	case WorkRestrictionRevision:
		return r.Explain()
	case CorrectiveActionRevision:
		return r.Explain()
	default:
		return "safety: unsupported record"
	}
}

// Store is the in-memory semantic port supplied by this package.
type Store interface {
	SaveIncident(IncidentRevision) error
	GetIncident(string, uint64) (IncidentRevision, bool)
	SaveInjury(InjuryRevision) error
	GetInjury(string, uint64) (InjuryRevision, bool)
	SaveReportability(ReportabilityDeterminationRevision) error
	GetReportability(string, uint64) (ReportabilityDeterminationRevision, bool)
	SaveClaim(ClaimRevision) error
	GetClaim(string, uint64) (ClaimRevision, bool)
	SaveRestriction(WorkRestrictionRevision) error
	GetRestriction(string, uint64) (WorkRestrictionRevision, bool)
	SaveCorrectiveAction(CorrectiveActionRevision) error
	GetCorrectiveAction(string, uint64) (CorrectiveActionRevision, bool)
}

type MemoryStore struct {
	mu                                                                            sync.RWMutex
	incidents, injuries, reportabilities, claims, restrictions, correctiveActions map[string]map[uint64]any
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{incidents: map[string]map[uint64]any{}, injuries: map[string]map[uint64]any{}, reportabilities: map[string]map[uint64]any{}, claims: map[string]map[uint64]any{}, restrictions: map[string]map[uint64]any{}, correctiveActions: map[string]map[uint64]any{}}
}
func putRecord(m map[string]map[uint64]any, id string, rev uint64, value any, digest string) error {
	if m[id] == nil {
		m[id] = map[uint64]any{}
	}
	if old, ok := m[id][rev]; ok {
		if recordDigest(old) != digest {
			return refused("SAFETY_REVISION_CONFLICT", "canonical_digest", "revision key already contains another digest", ErrRefused)
		}
		return nil
	}
	m[id][rev] = value
	return nil
}
func recordDigest(v any) string {
	switch x := v.(type) {
	case IncidentRevision:
		return x.CanonicalDigest
	case InjuryRevision:
		return x.CanonicalDigest
	case ReportabilityDeterminationRevision:
		return x.CanonicalDigest
	case ClaimRevision:
		return x.CanonicalDigest
	case WorkRestrictionRevision:
		return x.CanonicalDigest
	case CorrectiveActionRevision:
		return x.CanonicalDigest
	default:
		return ""
	}
}
func (s *MemoryStore) SaveIncident(r IncidentRevision) error {
	r, err := NewIncidentRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.incidents, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetIncident(id string, rev uint64) (IncidentRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.incidents[id][rev]
	if !ok {
		return IncidentRevision{}, false
	}
	return x.(IncidentRevision), true
}
func (s *MemoryStore) SaveInjury(r InjuryRevision) error {
	r, err := NewInjuryRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.injuries, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetInjury(id string, rev uint64) (InjuryRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.injuries[id][rev]
	if !ok {
		return InjuryRevision{}, false
	}
	return x.(InjuryRevision), true
}
func (s *MemoryStore) SaveReportability(r ReportabilityDeterminationRevision) error {
	r, err := NewReportabilityDeterminationRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.reportabilities, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetReportability(id string, rev uint64) (ReportabilityDeterminationRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.reportabilities[id][rev]
	if !ok {
		return ReportabilityDeterminationRevision{}, false
	}
	return x.(ReportabilityDeterminationRevision), true
}
func (s *MemoryStore) SaveClaim(r ClaimRevision) error {
	r, err := NewClaimRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.claims, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetClaim(id string, rev uint64) (ClaimRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.claims[id][rev]
	if !ok {
		return ClaimRevision{}, false
	}
	return x.(ClaimRevision), true
}
func (s *MemoryStore) SaveRestriction(r WorkRestrictionRevision) error {
	r, err := NewWorkRestrictionRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.restrictions, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetRestriction(id string, rev uint64) (WorkRestrictionRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.restrictions[id][rev]
	if !ok {
		return WorkRestrictionRevision{}, false
	}
	return x.(WorkRestrictionRevision), true
}
func (s *MemoryStore) SaveCorrectiveAction(r CorrectiveActionRevision) error {
	r, err := NewCorrectiveActionRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.correctiveActions, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetCorrectiveAction(id string, rev uint64) (CorrectiveActionRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.correctiveActions[id][rev]
	if !ok {
		return CorrectiveActionRevision{}, false
	}
	return x.(CorrectiveActionRevision), true
}
