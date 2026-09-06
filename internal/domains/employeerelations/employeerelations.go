// Package employeerelations owns the evidence-bearing, append-only semantics
// for employee-relations matters. It deliberately contains no persistence,
// workflow, identity, or notification implementation.
package employeerelations

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

const schemaVersion = 1

// Version reports the vocabulary version for this package.
func Version() int { return schemaVersion }

var (
	ErrInvalidRevision      = errors.New("employeerelations: invalid revision")
	ErrRefused              = errors.New("employeerelations: operation refused")
	ErrInvestigatorConflict = errors.New("employeerelations: investigator conflicts with subject or reporter")
	ErrEvidenceRequired     = errors.New("employeerelations: evidence standard is required")
	ErrFindingRequired      = errors.New("employeerelations: discipline requires a finding reference")
	ErrDecisionRequired     = errors.New("employeerelations: grievance requires the contested decision reference")
	ErrReviewRequired       = errors.New("employeerelations: discipline requires legal and representation review")
	ErrStatementImmutable   = errors.New("employeerelations: interview statements are append-only")
)

// ValidationError identifies the field that caused a refusal.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string { return "employeerelations: " + e.Field + ": " + e.Reason }
func (e *ValidationError) Unwrap() error { return ErrInvalidRevision }

// RefusalError is a typed refusal for a domain rule, rather than a generic
// validation failure.
type RefusalError struct {
	Code   string
	Field  string
	Reason string
	Cause  error
}

func (e *RefusalError) Error() string {
	if e.Field == "" {
		return "employeerelations: " + e.Code + ": " + e.Reason
	}
	return "employeerelations: " + e.Code + ": " + e.Field + ": " + e.Reason
}
func (e *RefusalError) Unwrap() error { return e.Cause }
func (e *RefusalError) Is(target error) bool {
	return target == ErrRefused || target == e.Cause
}

func invalid(field, reason string) error { return &ValidationError{Field: field, Reason: reason} }
func refused(code, field, reason string, cause error) error {
	return &RefusalError{Code: code, Field: field, Reason: reason, Cause: cause}
}

// ParticipantRole is a closed vocabulary. A role grants no authorization by
// itself; authorization remains an outer policy concern.
type ParticipantRole string

const (
	RoleSubject        ParticipantRole = "SUBJECT"
	RoleReporter       ParticipantRole = "REPORTER"
	RoleInvestigator   ParticipantRole = "INVESTIGATOR"
	RoleInterviewer    ParticipantRole = "INTERVIEWER"
	RoleInterviewee    ParticipantRole = "INTERVIEWEE"
	RoleWitness        ParticipantRole = "WITNESS"
	RoleDecisionMaker  ParticipantRole = "DECISION_MAKER"
	RoleRepresentative ParticipantRole = "REPRESENTATIVE"
	RoleReviewer       ParticipantRole = "REVIEWER"
)

func (r ParticipantRole) Valid() bool {
	switch r {
	case RoleSubject, RoleReporter, RoleInvestigator, RoleInterviewer,
		RoleInterviewee, RoleWitness, RoleDecisionMaker, RoleRepresentative, RoleReviewer:
		return true
	default:
		return false
	}
}

// Participant identifies a participant by reference. The reference is
// intentionally never emitted by Explain.
type Participant struct {
	Role ParticipantRole
	Ref  string
}

func (p Participant) Validate() error {
	if !p.Role.Valid() {
		return invalid("participants.role", "role is not declared")
	}
	if strings.TrimSpace(p.Ref) == "" {
		return invalid("participants.ref", "participant reference is required")
	}
	return nil
}

func (p Participant) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.employeerelations.Participant", schemaVersion).
		String("role", string(p.Role)).String("ref", p.Ref)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

// Compartment is the mandatory case boundary shared by every ER record.
type Compartment struct {
	CaseRef        string
	CompartmentRef string
	Participants   []Participant
}

func (c Compartment) Validate() error {
	if strings.TrimSpace(c.CaseRef) == "" {
		return invalid("case_ref", "case reference is required")
	}
	if strings.TrimSpace(c.CompartmentRef) == "" {
		return invalid("compartment_ref", "compartment reference is required")
	}
	if len(c.Participants) == 0 {
		return invalid("participants", "at least one participant is required")
	}
	seen := map[string]struct{}{}
	for i, p := range c.Participants {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("%w: participants[%d]", err, i)
		}
		key := string(p.Role) + "\x00" + p.Ref
		if _, ok := seen[key]; ok {
			return invalid(fmt.Sprintf("participants[%d]", i), "duplicate participant")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func cloneParticipants(in []Participant) []Participant { return append([]Participant(nil), in...) }

func (c Compartment) Canonical() []byte {
	if c.Validate() != nil {
		return nil
	}
	participants := cloneParticipants(c.Participants)
	sort.Slice(participants, func(i, j int) bool {
		if participants[i].Role != participants[j].Role {
			return participants[i].Role < participants[j].Role
		}
		return participants[i].Ref < participants[j].Ref
	})
	w := canonicalbytes.New("hcmnext.domains.employeerelations.Compartment", schemaVersion).
		String("case_ref", c.CaseRef).String("compartment_ref", c.CompartmentRef).
		Count("participants", len(participants))
	for _, p := range participants {
		w.Value("participant", p)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func validateLineage(kind, id string, revision, parentRevision uint64, parentDigest string, compartment Compartment, digest string) error {
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
	if err := compartment.Validate(); err != nil {
		return err
	}
	if digest != "" {
		return nil
	}
	return nil
}

func validateDigest(expected, actual string) error {
	if expected != "" && expected != actual {
		return invalid("canonical_digest", "canonical digest mismatch")
	}
	return nil
}

type AllegationStatus string

const (
	AllegationDraft         AllegationStatus = "DRAFT"
	AllegationOpen          AllegationStatus = "OPEN"
	AllegationUnderReview   AllegationStatus = "UNDER_REVIEW"
	AllegationDispositioned AllegationStatus = "DISPOSITIONED"
)

func (s AllegationStatus) Valid() bool {
	return s == AllegationDraft || s == AllegationOpen || s == AllegationUnderReview || s == AllegationDispositioned
}

// AllegationRevision records the protected allegation intake and its lineage.
type AllegationRevision struct {
	ID, CaseRef, CompartmentRef string
	Revision, ParentRevision    uint64
	ParentDigest                string
	Participants                []Participant
	ReporterRef, SubjectRef     string
	Summary                     string
	Status                      AllegationStatus
	RetaliationSafeguard        bool
	CanonicalDigest             string
}

// Allegation is the concise vocabulary spelling for AllegationRevision.
type Allegation = AllegationRevision

func (r AllegationRevision) compartment() Compartment {
	return Compartment{CaseRef: r.CaseRef, CompartmentRef: r.CompartmentRef, Participants: r.Participants}
}
func (r AllegationRevision) Validate() error {
	if err := validateLineage("allegation", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.compartment(), r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.ReporterRef) == "" {
		return invalid("reporter_ref", "reporter reference is required")
	}
	if strings.TrimSpace(r.SubjectRef) == "" {
		return invalid("subject_ref", "subject reference is required")
	}
	if strings.TrimSpace(r.Summary) == "" {
		return invalid("summary", "allegation summary is required")
	}
	if !r.Status.Valid() {
		return invalid("status", "allegation status is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r AllegationRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.employeerelations.AllegationRevision", schemaVersion).
		String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).
		Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).
		Value("compartment", r.compartment()).String("reporter_ref", r.ReporterRef).String("subject_ref", r.SubjectRef).
		String("summary", r.Summary).String("status", string(r.Status)).Bool("retaliation_safeguard", r.RetaliationSafeguard)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r AllegationRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r AllegationRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r AllegationRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r AllegationRevision) Explain() string {
	return fmt.Sprintf("allegation %s in case %s, compartment %s, revision %d, status %s, retaliation safeguard %t, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.Status, r.RetaliationSafeguard, r.CanonicalDigest)
}
func NewAllegationRevision(r AllegationRevision) (AllegationRevision, error) {
	r.Participants = cloneParticipants(r.Participants)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return AllegationRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewAllegation(r AllegationRevision) (AllegationRevision, error) { return NewAllegationRevision(r) }

type InvestigationStatus string

const (
	InvestigationPlanned InvestigationStatus = "PLANNED"
	InvestigationActive  InvestigationStatus = "ACTIVE"
	InvestigationClosed  InvestigationStatus = "CLOSED"
)

func (s InvestigationStatus) Valid() bool {
	return s == InvestigationPlanned || s == InvestigationActive || s == InvestigationClosed
}

// InvestigationRevision binds an investigation to an allegation and a
// purpose-scoped compartment.
type InvestigationRevision struct {
	ID, CaseRef, CompartmentRef                  string
	Revision, ParentRevision                     uint64
	ParentDigest                                 string
	Participants                                 []Participant
	AllegationRef, InvestigatorRef, AuthorityRef string
	Purpose, Scope                               string
	Status                                       InvestigationStatus
	CanonicalDigest                              string
}

type Investigation = InvestigationRevision

func (r InvestigationRevision) compartment() Compartment {
	return Compartment{r.CaseRef, r.CompartmentRef, r.Participants}
}
func (r InvestigationRevision) Validate() error {
	if err := validateLineage("investigation", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.compartment(), r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.AllegationRef) == "" {
		return invalid("allegation_ref", "allegation reference is required")
	}
	if strings.TrimSpace(r.InvestigatorRef) == "" {
		return invalid("investigator_ref", "investigator reference is required")
	}
	if strings.TrimSpace(r.AuthorityRef) == "" {
		return refused("ER_INVESTIGATION_AUTHORITY", "authority_ref", "investigation authority is required", ErrRefused)
	}
	if strings.TrimSpace(r.Purpose) == "" {
		return invalid("purpose", "investigation purpose is required")
	}
	if strings.TrimSpace(r.Scope) == "" {
		return invalid("scope", "investigation scope is required")
	}
	if !r.Status.Valid() {
		return invalid("status", "investigation status is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r InvestigationRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.employeerelations.InvestigationRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).Value("compartment", r.compartment()).String("allegation_ref", r.AllegationRef).String("investigator_ref", r.InvestigatorRef).String("authority_ref", r.AuthorityRef).String("purpose", r.Purpose).String("scope", r.Scope).String("status", string(r.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r InvestigationRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r InvestigationRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r InvestigationRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r InvestigationRevision) Explain() string {
	return fmt.Sprintf("investigation %s in case %s, compartment %s, revision %d, allegation %s, investigator role recorded, status %s, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.AllegationRef, r.Status, r.CanonicalDigest)
}
func NewInvestigationRevision(r InvestigationRevision) (InvestigationRevision, error) {
	r.Participants = cloneParticipants(r.Participants)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return InvestigationRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewInvestigation(r InvestigationRevision) (InvestigationRevision, error) {
	return NewInvestigationRevision(r)
}

type InterviewStatus string

const (
	InterviewScheduled InterviewStatus = "SCHEDULED"
	InterviewTaken     InterviewStatus = "TAKEN"
	InterviewSealed    InterviewStatus = "SEALED"
)

func (s InterviewStatus) Valid() bool {
	return s == InterviewScheduled || s == InterviewTaken || s == InterviewSealed
}

// InterviewRevision stores a statement only in the immutable revision. A
// correction is represented by a successor revision and never by editing the
// prior statement in place.
type InterviewRevision struct {
	ID, CaseRef, CompartmentRef                      string
	Revision, ParentRevision                         uint64
	ParentDigest                                     string
	Participants                                     []Participant
	InvestigationRef, InterviewerRef, IntervieweeRef string
	Statement                                        string
	Status                                           InterviewStatus
	CanonicalDigest                                  string
}

type Interview = InterviewRevision

func (r InterviewRevision) compartment() Compartment {
	return Compartment{r.CaseRef, r.CompartmentRef, r.Participants}
}
func (r InterviewRevision) Validate() error {
	if err := validateLineage("interview", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.compartment(), r.CanonicalDigest); err != nil {
		return err
	}
	for field, value := range map[string]string{"investigation_ref": r.InvestigationRef, "interviewer_ref": r.InterviewerRef, "interviewee_ref": r.IntervieweeRef, "statement": r.Statement} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if !r.Status.Valid() {
		return invalid("status", "interview status is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r InterviewRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.employeerelations.InterviewRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).Value("compartment", r.compartment()).String("investigation_ref", r.InvestigationRef).String("interviewer_ref", r.InterviewerRef).String("interviewee_ref", r.IntervieweeRef).String("statement", r.Statement).String("status", string(r.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r InterviewRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r InterviewRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r InterviewRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r InterviewRevision) Explain() string {
	return fmt.Sprintf("interview %s in case %s, compartment %s, revision %d, investigation %s, status %s, statement sealed, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.InvestigationRef, r.Status, r.CanonicalDigest)
}
func NewInterviewRevision(r InterviewRevision) (InterviewRevision, error) {
	r.Participants = cloneParticipants(r.Participants)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return InterviewRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewInterview(r InterviewRevision) (InterviewRevision, error) { return NewInterviewRevision(r) }

type EvidenceStandard string

const (
	EvidenceInsufficient       EvidenceStandard = "INSUFFICIENT"
	EvidenceMoreLikelyThanNot  EvidenceStandard = "MORE_LIKELY_THAN_NOT"
	EvidenceClearAndConvincing EvidenceStandard = "CLEAR_AND_CONVINCING"
)

func (s EvidenceStandard) Valid() bool {
	return s == EvidenceInsufficient || s == EvidenceMoreLikelyThanNot || s == EvidenceClearAndConvincing
}

type FindingDisposition string

const (
	FindingUnsubstantiated FindingDisposition = "UNSUBSTANTIATED"
	FindingSubstantiated   FindingDisposition = "SUBSTANTIATED"
	FindingInconclusive    FindingDisposition = "INCONCLUSIVE"
)

func (d FindingDisposition) Valid() bool {
	return d == FindingUnsubstantiated || d == FindingSubstantiated || d == FindingInconclusive
}

// FindingRevision records the investigator's conclusion. The investigator
// must be distinct from both subject and reporter.
type FindingRevision struct {
	ID, CaseRef, CompartmentRef                                string
	Revision, ParentRevision                                   uint64
	ParentDigest                                               string
	Participants                                               []Participant
	InvestigationRef, InvestigatorRef, SubjectRef, ReporterRef string
	EvidenceStandard                                           EvidenceStandard
	EvidenceRefs                                               []string
	Disposition                                                FindingDisposition
	Rationale                                                  string
	CanonicalDigest                                            string
}

type Finding = FindingRevision

func (r FindingRevision) compartment() Compartment {
	return Compartment{r.CaseRef, r.CompartmentRef, r.Participants}
}
func (r FindingRevision) Validate() error {
	if err := validateLineage("finding", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.compartment(), r.CanonicalDigest); err != nil {
		return err
	}
	for field, value := range map[string]string{"investigation_ref": r.InvestigationRef, "investigator_ref": r.InvestigatorRef, "subject_ref": r.SubjectRef, "reporter_ref": r.ReporterRef, "rationale": r.Rationale} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if r.InvestigatorRef == r.SubjectRef || r.InvestigatorRef == r.ReporterRef {
		return refused("ER_FINDING_RECUSAL", "investigator_ref", "investigator must be distinct from subject and reporter", ErrInvestigatorConflict)
	}
	if !r.EvidenceStandard.Valid() || r.EvidenceStandard == EvidenceInsufficient {
		return refused("ER_FINDING_EVIDENCE", "evidence_standard", "finding requires a declared sufficient evidence standard", ErrEvidenceRequired)
	}
	if len(r.EvidenceRefs) == 0 {
		return refused("ER_FINDING_EVIDENCE", "evidence_refs", "finding requires at least one evidence reference", ErrEvidenceRequired)
	}
	if !r.Disposition.Valid() {
		return invalid("disposition", "finding disposition is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r FindingRevision) body() []byte {
	refs := append([]string(nil), r.EvidenceRefs...)
	sort.Strings(refs)
	w := canonicalbytes.New("hcmnext.domains.employeerelations.FindingRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).Value("compartment", r.compartment()).String("investigation_ref", r.InvestigationRef).String("investigator_ref", r.InvestigatorRef).String("subject_ref", r.SubjectRef).String("reporter_ref", r.ReporterRef).String("evidence_standard", string(r.EvidenceStandard)).SortedStrings("evidence_ref", refs).String("disposition", string(r.Disposition)).String("rationale", r.Rationale)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r FindingRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r FindingRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r FindingRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r FindingRevision) Explain() string {
	return fmt.Sprintf("finding %s in case %s, compartment %s, revision %d, investigation %s, disposition %s, evidence standard %s, reporter identity protected, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.InvestigationRef, r.Disposition, r.EvidenceStandard, r.CanonicalDigest)
}
func NewFindingRevision(r FindingRevision) (FindingRevision, error) {
	r.Participants = cloneParticipants(r.Participants)
	r.EvidenceRefs = append([]string(nil), r.EvidenceRefs...)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return FindingRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewFinding(r FindingRevision) (FindingRevision, error) { return NewFindingRevision(r) }

type DisciplineStatus string

const (
	DisciplineProposed DisciplineStatus = "PROPOSED"
	DisciplineApproved DisciplineStatus = "APPROVED"
	DisciplineIssued   DisciplineStatus = "ISSUED"
)

func (s DisciplineStatus) Valid() bool {
	return s == DisciplineProposed || s == DisciplineApproved || s == DisciplineIssued
}

// DisciplineRevision is a decision that cannot bypass a finding or its two
// review gates.
type DisciplineRevision struct {
	ID, CaseRef, CompartmentRef                     string
	Revision, ParentRevision                        uint64
	ParentDigest                                    string
	Participants                                    []Participant
	FindingRef, SubjectRef                          string
	Action, LegalReviewRef, RepresentationReviewRef string
	Status                                          DisciplineStatus
	CanonicalDigest                                 string
}

type Discipline = DisciplineRevision

func (r DisciplineRevision) compartment() Compartment {
	return Compartment{r.CaseRef, r.CompartmentRef, r.Participants}
}
func (r DisciplineRevision) Validate() error {
	if err := validateLineage("discipline", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.compartment(), r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.FindingRef) == "" {
		return refused("ER_DISCIPLINE_FINDING", "finding_ref", "discipline requires a finding reference", ErrFindingRequired)
	}
	for field, value := range map[string]string{"subject_ref": r.SubjectRef, "action": r.Action, "legal_review_ref": r.LegalReviewRef, "representation_review_ref": r.RepresentationReviewRef} {
		if strings.TrimSpace(value) == "" {
			return refused("ER_DISCIPLINE_REVIEW", field, "required before discipline can proceed", ErrReviewRequired)
		}
	}
	if !r.Status.Valid() {
		return invalid("status", "discipline status is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r DisciplineRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.employeerelations.DisciplineRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).Value("compartment", r.compartment()).String("finding_ref", r.FindingRef).String("subject_ref", r.SubjectRef).String("action", r.Action).String("legal_review_ref", r.LegalReviewRef).String("representation_review_ref", r.RepresentationReviewRef).String("status", string(r.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r DisciplineRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r DisciplineRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r DisciplineRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r DisciplineRevision) Explain() string {
	return fmt.Sprintf("discipline %s in case %s, compartment %s, revision %d, finding %s, status %s, review gates recorded, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.FindingRef, r.Status, r.CanonicalDigest)
}
func NewDisciplineRevision(r DisciplineRevision) (DisciplineRevision, error) {
	r.Participants = cloneParticipants(r.Participants)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return DisciplineRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewDiscipline(r DisciplineRevision) (DisciplineRevision, error) { return NewDisciplineRevision(r) }

type GrievanceStatus string

const (
	GrievanceOpen     GrievanceStatus = "OPEN"
	GrievanceReviewed GrievanceStatus = "REVIEWED"
	GrievanceResolved GrievanceStatus = "RESOLVED"
)

func (s GrievanceStatus) Valid() bool {
	return s == GrievanceOpen || s == GrievanceReviewed || s == GrievanceResolved
}

// GrievanceRevision always points at the decision it contests; it cannot
// replace that decision or erase its chronology.
type GrievanceRevision struct {
	ID, CaseRef, CompartmentRef string
	Revision, ParentRevision    uint64
	ParentDigest                string
	Participants                []Participant
	DecisionRef, GrievantRef    string
	Grounds, Outcome            string
	Status                      GrievanceStatus
	CanonicalDigest             string
}

type Grievance = GrievanceRevision

func (r GrievanceRevision) compartment() Compartment {
	return Compartment{r.CaseRef, r.CompartmentRef, r.Participants}
}
func (r GrievanceRevision) Validate() error {
	if err := validateLineage("grievance", r.ID, r.Revision, r.ParentRevision, r.ParentDigest, r.compartment(), r.CanonicalDigest); err != nil {
		return err
	}
	if strings.TrimSpace(r.DecisionRef) == "" {
		return refused("ER_GRIEVANCE_DECISION", "decision_ref", "grievance must link to the decision it contests", ErrDecisionRequired)
	}
	for field, value := range map[string]string{"grievant_ref": r.GrievantRef, "grounds": r.Grounds} {
		if strings.TrimSpace(value) == "" {
			return invalid(field, "value is required")
		}
	}
	if !r.Status.Valid() {
		return invalid("status", "grievance status is not declared")
	}
	return validateDigest(r.CanonicalDigest, r.computedDigest())
}
func (r GrievanceRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.employeerelations.GrievanceRevision", schemaVersion).String("id", r.ID).String("case_ref", r.CaseRef).String("compartment_ref", r.CompartmentRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).String("parent_digest", r.ParentDigest).Value("compartment", r.compartment()).String("decision_ref", r.DecisionRef).String("grievant_ref", r.GrievantRef).String("grounds", r.Grounds).String("outcome", r.Outcome).String("status", string(r.Status))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r GrievanceRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r GrievanceRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}
func (r GrievanceRevision) Digest() (string, error) {
	if err := r.Validate(); err != nil {
		return "", err
	}
	return r.computedDigest(), nil
}
func (r GrievanceRevision) Explain() string {
	return fmt.Sprintf("grievance %s in case %s, compartment %s, revision %d, contests decision %s, status %s, digest %s", r.ID, r.CaseRef, r.CompartmentRef, r.Revision, r.DecisionRef, r.Status, r.CanonicalDigest)
}
func NewGrievanceRevision(r GrievanceRevision) (GrievanceRevision, error) {
	r.Participants = cloneParticipants(r.Participants)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return GrievanceRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}
func NewGrievance(r GrievanceRevision) (GrievanceRevision, error) { return NewGrievanceRevision(r) }

// Explain is the common audit-safe explanation entry point. It intentionally
// accepts only the six domain record types and never renders protected values.
func Explain(record any) string {
	switch r := record.(type) {
	case AllegationRevision:
		return r.Explain()
	case InvestigationRevision:
		return r.Explain()
	case InterviewRevision:
		return r.Explain()
	case FindingRevision:
		return r.Explain()
	case DisciplineRevision:
		return r.Explain()
	case GrievanceRevision:
		return r.Explain()
	default:
		return "employeerelations: unsupported record"
	}
}

// Store is the package-local semantic port. Adapters may persist it elsewhere;
// this package supplies MemoryStore as a deterministic fake for conformance.
type Store interface {
	SaveAllegation(AllegationRevision) error
	GetAllegation(string, uint64) (AllegationRevision, bool)
	SaveInvestigation(InvestigationRevision) error
	GetInvestigation(string, uint64) (InvestigationRevision, bool)
	SaveInterview(InterviewRevision) error
	GetInterview(string, uint64) (InterviewRevision, bool)
	SaveFinding(FindingRevision) error
	GetFinding(string, uint64) (FindingRevision, bool)
	SaveDiscipline(DisciplineRevision) error
	GetDiscipline(string, uint64) (DisciplineRevision, bool)
	SaveGrievance(GrievanceRevision) error
	GetGrievance(string, uint64) (GrievanceRevision, bool)
}

// MemoryStore is an append-only fake. A repeated identical revision is
// idempotent; a repeated key with a different digest is refused.
type MemoryStore struct {
	mu                                                                         sync.RWMutex
	allegations, investigations, interviews, findings, disciplines, grievances map[string]map[uint64]any
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{allegations: map[string]map[uint64]any{}, investigations: map[string]map[uint64]any{}, interviews: map[string]map[uint64]any{}, findings: map[string]map[uint64]any{}, disciplines: map[string]map[uint64]any{}, grievances: map[string]map[uint64]any{}}
}
func putRecord(m map[string]map[uint64]any, id string, rev uint64, value any, digest string) error {
	if m[id] == nil {
		m[id] = map[uint64]any{}
	}
	if old, ok := m[id][rev]; ok {
		if oldDigest := recordDigest(old); oldDigest != digest {
			return refused("ER_REVISION_CONFLICT", "canonical_digest", "revision key already contains another digest", ErrRefused)
		}
		return nil
	}
	m[id][rev] = value
	return nil
}
func recordDigest(v any) string {
	switch x := v.(type) {
	case AllegationRevision:
		return x.CanonicalDigest
	case InvestigationRevision:
		return x.CanonicalDigest
	case InterviewRevision:
		return x.CanonicalDigest
	case FindingRevision:
		return x.CanonicalDigest
	case DisciplineRevision:
		return x.CanonicalDigest
	case GrievanceRevision:
		return x.CanonicalDigest
	default:
		return ""
	}
}
func (s *MemoryStore) SaveAllegation(r AllegationRevision) error {
	r, err := NewAllegationRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.allegations, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetAllegation(id string, rev uint64) (AllegationRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.allegations[id][rev]
	if !ok {
		return AllegationRevision{}, false
	}
	return x.(AllegationRevision), true
}
func (s *MemoryStore) SaveInvestigation(r InvestigationRevision) error {
	r, err := NewInvestigationRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.investigations, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetInvestigation(id string, rev uint64) (InvestigationRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.investigations[id][rev]
	if !ok {
		return InvestigationRevision{}, false
	}
	return x.(InvestigationRevision), true
}
func (s *MemoryStore) SaveInterview(r InterviewRevision) error {
	r, err := NewInterviewRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.interviews, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetInterview(id string, rev uint64) (InterviewRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.interviews[id][rev]
	if !ok {
		return InterviewRevision{}, false
	}
	return x.(InterviewRevision), true
}
func (s *MemoryStore) SaveFinding(r FindingRevision) error {
	r, err := NewFindingRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.findings, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetFinding(id string, rev uint64) (FindingRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.findings[id][rev]
	if !ok {
		return FindingRevision{}, false
	}
	return x.(FindingRevision), true
}
func (s *MemoryStore) SaveDiscipline(r DisciplineRevision) error {
	r, err := NewDisciplineRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.disciplines, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetDiscipline(id string, rev uint64) (DisciplineRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.disciplines[id][rev]
	if !ok {
		return DisciplineRevision{}, false
	}
	return x.(DisciplineRevision), true
}
func (s *MemoryStore) SaveGrievance(r GrievanceRevision) error {
	r, err := NewGrievanceRevision(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return putRecord(s.grievances, r.ID, r.Revision, r, r.CanonicalDigest)
}
func (s *MemoryStore) GetGrievance(id string, rev uint64) (GrievanceRevision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.grievances[id][rev]
	if !ok {
		return GrievanceRevision{}, false
	}
	return x.(GrievanceRevision), true
}
