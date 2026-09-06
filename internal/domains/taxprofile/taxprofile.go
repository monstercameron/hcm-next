// Package taxprofile owns the pure, append-only worker tax profile
// vocabulary. Registrations are governed references, not tax identifiers, and
// no caller can lower a profile's classification by omission or defaulting.
package taxprofile

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this package's stable contract version.
func Version() int { return schemaVersion }

var (
	ErrInvalidTaxProfile        = errors.New("taxprofile: invalid worker tax profile")
	ErrInvalidRegistration      = errors.New("taxprofile: invalid registration")
	ErrInvalidElection          = errors.New("taxprofile: invalid withholding election")
	ErrInvalidExemption         = errors.New("taxprofile: invalid exemption")
	ErrUnregisteredJurisdiction = errors.New("taxprofile: jurisdiction is not registered")
	ErrUnverifiedElection       = errors.New("taxprofile: election form or evidence is unverified")
	ErrRegistrationOverlap      = errors.New("taxprofile: registrations overlap")
)

// FilingStatus is a closed filing-status vocabulary.
type FilingStatus string

const (
	FilingSingle                    FilingStatus = "SINGLE"
	FilingMarriedJointly            FilingStatus = "MARRIED_FILING_JOINTLY"
	FilingMarriedSeparately         FilingStatus = "MARRIED_FILING_SEPARATELY"
	FilingHeadOfHousehold           FilingStatus = "HEAD_OF_HOUSEHOLD"
	FilingQualifyingSurvivingSpouse FilingStatus = "QUALIFYING_SURVIVING_SPOUSE"
)

func (s FilingStatus) Valid() bool {
	switch s {
	case FilingSingle, FilingMarriedJointly, FilingMarriedSeparately, FilingHeadOfHousehold, FilingQualifyingSurvivingSpouse:
		return true
	default:
		return false
	}
}

// TaxClassification is deliberately closed and has no permissive zero value.
type TaxClassification string

const (
	ClassificationResident     TaxClassification = "RESIDENT"
	ClassificationNonResident  TaxClassification = "NON_RESIDENT"
	ClassificationDualResident TaxClassification = "DUAL_RESIDENT"
	Resident                                     = ClassificationResident
	NonResident                                  = ClassificationNonResident
	DualResident                                 = ClassificationDualResident
)

func (c TaxClassification) Valid() bool {
	switch c {
	case ClassificationResident, ClassificationNonResident, ClassificationDualResident:
		return true
	default:
		return false
	}
}

// ElectionKind describes the governed shape of an election. The submitted
// form revision and evidence remain references and are never inferred.
type ElectionKind string

const (
	ElectionStandardWithholding ElectionKind = "STANDARD_WITHHOLDING"
	ElectionAdditionalAmount    ElectionKind = "ADDITIONAL_AMOUNT"
	ElectionExempt              ElectionKind = "EXEMPT"
	ElectionMultipleJobs        ElectionKind = "MULTIPLE_JOBS"
)

func (k ElectionKind) Valid() bool {
	switch k {
	case ElectionStandardWithholding, ElectionAdditionalAmount, ElectionExempt, ElectionMultipleJobs:
		return true
	default:
		return false
	}
}

// ExemptionKind is a closed vocabulary for exemption claims.
type ExemptionKind string

const (
	ExemptionFederal ExemptionKind = "FEDERAL"
	ExemptionState   ExemptionKind = "STATE"
	ExemptionLocal   ExemptionKind = "LOCAL"
	ExemptionSocial  ExemptionKind = "SOCIAL_INSURANCE"
)

func (k ExemptionKind) Valid() bool {
	switch k {
	case ExemptionFederal, ExemptionState, ExemptionLocal, ExemptionSocial:
		return true
	default:
		return false
	}
}

// TaxRegistrationRevision identifies a jurisdiction registration only by a
// governed reference. RawRegistrationID is a rejection probe and is never
// retained in a valid value or explanation.
type TaxRegistrationRevision struct {
	RegistrationIDRef string
	RegistrationID    string
	Jurisdiction      string
	AuthorityRef      string
	Revision          uint64
	ParentRevision    uint64
	ParentDigest      string
	Effective         values.EffectiveInterval
	KnownAt           values.Instant
	RawRegistrationID string
	CanonicalDigest   string
}

type RegistrationRevision = TaxRegistrationRevision
type TaxRegistration = TaxRegistrationRevision

func (r TaxRegistrationRevision) Validate() error {
	if strings.TrimSpace(r.RegistrationIDRef) == "" {
		return fmt.Errorf("%w: registration_id_ref is required", ErrInvalidRegistration)
	}
	if strings.TrimSpace(r.RawRegistrationID) != "" || strings.TrimSpace(r.RegistrationID) != "" {
		return fmt.Errorf("%w: registration_id: raw registration numbers are prohibited", ErrInvalidRegistration)
	}
	if strings.TrimSpace(r.Jurisdiction) == "" {
		return fmt.Errorf("%w: jurisdiction is required", ErrInvalidRegistration)
	}
	if strings.TrimSpace(r.AuthorityRef) == "" {
		return fmt.Errorf("%w: authority_ref is required", ErrInvalidRegistration)
	}
	if r.Revision == 0 {
		return fmt.Errorf("%w: revision is required", ErrInvalidRegistration)
	}
	if r.Revision == 1 && (r.ParentRevision != 0 || r.ParentDigest != "") {
		return fmt.Errorf("%w: parent_revision: first revision cannot have a parent", ErrInvalidRegistration)
	}
	if r.Revision > 1 && (r.ParentRevision == 0 || r.ParentRevision >= r.Revision || strings.TrimSpace(r.ParentDigest) == "") {
		return fmt.Errorf("%w: parent_digest: successor requires an earlier parent digest", ErrInvalidRegistration)
	}
	if err := r.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrInvalidRegistration, err)
	}
	if err := r.KnownAt.Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrInvalidRegistration, err)
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidRegistration)
	}
	return nil
}

func (r TaxRegistrationRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.taxprofile.TaxRegistrationRevision", schemaVersion).
		String("registration_id_ref", r.RegistrationIDRef).String("jurisdiction", r.Jurisdiction).
		String("authority_ref", r.AuthorityRef).Int("revision", int64(r.Revision)).Int("parent_revision", int64(r.ParentRevision)).
		String("parent_digest", r.ParentDigest).Value("effective", r.Effective).Value("known_at", r.KnownAt)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (r TaxRegistrationRevision) computedDigest() string { return canonicalbytes.Digest(r.body()) }
func (r TaxRegistrationRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

func NewTaxRegistrationRevision(r TaxRegistrationRevision) (TaxRegistrationRevision, error) {
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return TaxRegistrationRevision{}, err
	}
	r.CanonicalDigest = r.computedDigest()
	return r, nil
}

func NewRegistrationRevision(r TaxRegistrationRevision) (TaxRegistrationRevision, error) {
	return NewTaxRegistrationRevision(r)
}

// WithholdingElectionRevision is an immutable worker election. FormRevisionRef
// and EvidenceRef are mandatory governed references; a caller cannot submit a
// free-form or unverified election.
type WithholdingElectionRevision struct {
	ElectionID      string
	WorkerRef       string
	Jurisdiction    string
	Kind            ElectionKind
	FormRevisionRef string
	EvidenceRef     string
	Amount          values.Decimal
	Effective       values.EffectiveInterval
	KnownAt         values.Instant
	CanonicalDigest string
}

type ElectionRevision = WithholdingElectionRevision
type WithholdingElection = WithholdingElectionRevision

func (e WithholdingElectionRevision) Validate() error {
	for field, value := range map[string]string{"election_id": e.ElectionID, "worker_ref": e.WorkerRef, "jurisdiction": e.Jurisdiction, "form_revision_ref": e.FormRevisionRef, "evidence_ref": e.EvidenceRef} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidElection, field)
		}
	}
	if !e.Kind.Valid() {
		return fmt.Errorf("%w: kind %q is not declared", ErrInvalidElection, e.Kind)
	}
	if err := e.Amount.Validate(); err != nil && !e.Amount.IsZero() {
		return fmt.Errorf("%w: amount: %v", ErrInvalidElection, err)
	}
	if !e.Amount.IsZero() && e.Amount.Sign() < 0 {
		return fmt.Errorf("%w: amount cannot be negative", ErrInvalidElection)
	}
	if err := e.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrInvalidElection, err)
	}
	if err := e.KnownAt.Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrInvalidElection, err)
	}
	if e.CanonicalDigest != "" && e.CanonicalDigest != e.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidElection)
	}
	return nil
}

func (e WithholdingElectionRevision) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.taxprofile.WithholdingElectionRevision", schemaVersion).
		String("election_id", e.ElectionID).String("worker_ref", e.WorkerRef).String("jurisdiction", e.Jurisdiction).
		String("kind", string(e.Kind)).String("form_revision_ref", e.FormRevisionRef).String("evidence_ref", e.EvidenceRef).
		Optional("amount", !e.Amount.IsZero(), e.Amount).Value("effective", e.Effective).Value("known_at", e.KnownAt)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (e WithholdingElectionRevision) computedDigest() string { return canonicalbytes.Digest(e.body()) }
func (e WithholdingElectionRevision) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	return e.body()
}

func NewWithholdingElectionRevision(e WithholdingElectionRevision) (WithholdingElectionRevision, error) {
	e.CanonicalDigest = ""
	if err := e.Validate(); err != nil {
		return WithholdingElectionRevision{}, err
	}
	e.CanonicalDigest = e.computedDigest()
	return e, nil
}

func NewElectionRevision(e WithholdingElectionRevision) (WithholdingElectionRevision, error) {
	return NewWithholdingElectionRevision(e)
}

// TaxExemptionRevision is an immutable claim with explicit evidence and an
// expiry. An open-ended exemption is intentionally impossible.
type TaxExemptionRevision struct {
	ExemptionID     string
	WorkerRef       string
	Jurisdiction    string
	Kind            ExemptionKind
	EvidenceRefs    []string
	ExpiresAt       values.Instant
	Effective       values.EffectiveInterval
	KnownAt         values.Instant
	CanonicalDigest string
}

type ExemptionRevision = TaxExemptionRevision
type TaxExemption = TaxExemptionRevision

func (e TaxExemptionRevision) Validate() error {
	for field, value := range map[string]string{"exemption_id": e.ExemptionID, "worker_ref": e.WorkerRef, "jurisdiction": e.Jurisdiction} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalidExemption, field)
		}
	}
	if !e.Kind.Valid() {
		return fmt.Errorf("%w: kind %q is not declared", ErrInvalidExemption, e.Kind)
	}
	if len(e.EvidenceRefs) == 0 {
		return fmt.Errorf("%w: evidence_refs is required", ErrInvalidExemption)
	}
	seen := make(map[string]struct{}, len(e.EvidenceRefs))
	for _, ref := range e.EvidenceRefs {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("%w: evidence_refs contains an empty ref", ErrInvalidExemption)
		}
		if _, ok := seen[ref]; ok {
			return fmt.Errorf("%w: evidence_refs contains duplicate %q", ErrInvalidExemption, ref)
		}
		seen[ref] = struct{}{}
	}
	if err := e.ExpiresAt.Validate(); err != nil {
		return fmt.Errorf("%w: expires_at: %v", ErrInvalidExemption, err)
	}
	if err := e.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrInvalidExemption, err)
	}
	if end, ok := e.Effective.EndInstant(); ok && end.Compare(e.ExpiresAt) > 0 {
		return fmt.Errorf("%w: expires_at precedes effective end", ErrInvalidExemption)
	}
	if err := e.KnownAt.Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrInvalidExemption, err)
	}
	if e.CanonicalDigest != "" && e.CanonicalDigest != e.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidExemption)
	}
	return nil
}

func (e TaxExemptionRevision) body() []byte {
	refs := append([]string(nil), e.EvidenceRefs...)
	sort.Strings(refs)
	w := canonicalbytes.New("hcmnext.domains.taxprofile.TaxExemptionRevision", schemaVersion).
		String("exemption_id", e.ExemptionID).String("worker_ref", e.WorkerRef).String("jurisdiction", e.Jurisdiction).
		String("kind", string(e.Kind)).SortedStrings("evidence_ref", refs).Value("expires_at", e.ExpiresAt).
		Value("effective", e.Effective).Value("known_at", e.KnownAt)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (e TaxExemptionRevision) computedDigest() string { return canonicalbytes.Digest(e.body()) }
func (e TaxExemptionRevision) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	return e.body()
}

func NewTaxExemptionRevision(e TaxExemptionRevision) (TaxExemptionRevision, error) {
	e.EvidenceRefs = append([]string(nil), e.EvidenceRefs...)
	e.CanonicalDigest = ""
	if err := e.Validate(); err != nil {
		return TaxExemptionRevision{}, err
	}
	e.CanonicalDigest = e.computedDigest()
	return e, nil
}

func NewExemptionRevision(e TaxExemptionRevision) (TaxExemptionRevision, error) {
	return NewTaxExemptionRevision(e)
}

// WorkerTaxProfileRevision is an immutable effective-dated profile with
// explicit lineage. Jurisdictions used by the profile must have a matching
// registration whose effective interval overlaps the profile interval.
type WorkerTaxProfileRevision struct {
	WorkerRef                 string
	Revision                  uint64
	ParentRevision            uint64
	ParentDigest              string
	ResidenceJurisdictions    []string
	WorkJurisdictions         []string
	FilingStatus              FilingStatus
	Classification            TaxClassification
	ClassificationEvidenceRef string
	Registrations             []TaxRegistrationRevision
	Elections                 []WithholdingElectionRevision
	Exemptions                []TaxExemptionRevision
	Effective                 values.EffectiveInterval
	KnownAt                   values.Instant
	CanonicalDigest           string
}

type WorkerTaxProfile = WorkerTaxProfileRevision
type TaxProfileRevision = WorkerTaxProfileRevision
type TaxProfile = WorkerTaxProfileRevision

func uniqueJurisdictions(valuesIn []string) error {
	seen := make(map[string]struct{}, len(valuesIn))
	for _, jurisdiction := range valuesIn {
		if strings.TrimSpace(jurisdiction) == "" {
			return fmt.Errorf("%w: jurisdiction contains an empty value", ErrInvalidTaxProfile)
		}
		if _, ok := seen[jurisdiction]; ok {
			return fmt.Errorf("%w: duplicate jurisdiction %q", ErrInvalidTaxProfile, jurisdiction)
		}
		seen[jurisdiction] = struct{}{}
	}
	return nil
}

func registrationCovers(reg TaxRegistrationRevision, jurisdiction string, profile values.EffectiveInterval) bool {
	if reg.Jurisdiction != jurisdiction {
		return false
	}
	ok, err := reg.Effective.Overlaps(profile)
	return err == nil && ok
}

func (p WorkerTaxProfileRevision) Validate() error {
	if strings.TrimSpace(p.WorkerRef) == "" {
		return fmt.Errorf("%w: worker_ref is required", ErrInvalidTaxProfile)
	}
	if p.Revision == 0 {
		return fmt.Errorf("%w: revision is required", ErrInvalidTaxProfile)
	}
	if p.Revision == 1 && (p.ParentRevision != 0 || p.ParentDigest != "") {
		return fmt.Errorf("%w: parent_revision: first revision cannot have a parent", ErrInvalidTaxProfile)
	}
	if p.Revision > 1 && (p.ParentRevision == 0 || p.ParentRevision >= p.Revision || strings.TrimSpace(p.ParentDigest) == "") {
		return fmt.Errorf("%w: parent_digest: successor requires an earlier parent digest", ErrInvalidTaxProfile)
	}
	if len(p.ResidenceJurisdictions) == 0 {
		return fmt.Errorf("%w: residence_jurisdictions is required", ErrInvalidTaxProfile)
	}
	if len(p.WorkJurisdictions) == 0 {
		return fmt.Errorf("%w: work_jurisdictions is required", ErrInvalidTaxProfile)
	}
	if err := uniqueJurisdictions(p.ResidenceJurisdictions); err != nil {
		return err
	}
	if err := uniqueJurisdictions(p.WorkJurisdictions); err != nil {
		return err
	}
	if !p.FilingStatus.Valid() {
		return fmt.Errorf("%w: filing_status %q is not declared", ErrInvalidTaxProfile, p.FilingStatus)
	}
	if !p.Classification.Valid() {
		return fmt.Errorf("%w: classification %q is not declared", ErrInvalidTaxProfile, p.Classification)
	}
	if strings.TrimSpace(p.ClassificationEvidenceRef) == "" {
		return fmt.Errorf("%w: classification_evidence_ref is required", ErrInvalidTaxProfile)
	}
	if err := p.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrInvalidTaxProfile, err)
	}
	if err := p.KnownAt.Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrInvalidTaxProfile, err)
	}
	for _, registration := range p.Registrations {
		if err := registration.Validate(); err != nil {
			return err
		}
	}
	for i, left := range p.Registrations {
		for j := i + 1; j < len(p.Registrations); j++ {
			right := p.Registrations[j]
			if left.Jurisdiction == right.Jurisdiction {
				overlap, _ := left.Effective.Overlaps(right.Effective)
				if overlap {
					return fmt.Errorf("%w: jurisdiction: %s", ErrRegistrationOverlap, left.Jurisdiction)
				}
			}
		}
	}
	for _, jurisdiction := range append(append([]string(nil), p.ResidenceJurisdictions...), p.WorkJurisdictions...) {
		covered := false
		for _, registration := range p.Registrations {
			if registrationCovers(registration, jurisdiction, p.Effective) {
				covered = true
				break
			}
		}
		if !covered {
			return fmt.Errorf("%w: %s", ErrUnregisteredJurisdiction, jurisdiction)
		}
	}
	for _, election := range p.Elections {
		if err := election.Validate(); err != nil {
			return err
		}
		if election.WorkerRef != p.WorkerRef {
			return fmt.Errorf("%w: worker_ref does not match profile", ErrInvalidTaxProfile)
		}
		if !p.registeredFor(election.Jurisdiction, election.Effective) {
			return fmt.Errorf("%w: %s", ErrUnregisteredJurisdiction, election.Jurisdiction)
		}
	}
	for _, exemption := range p.Exemptions {
		if err := exemption.Validate(); err != nil {
			return err
		}
		if exemption.WorkerRef != p.WorkerRef {
			return fmt.Errorf("%w: worker_ref does not match profile", ErrInvalidTaxProfile)
		}
		if !p.registeredFor(exemption.Jurisdiction, exemption.Effective) {
			return fmt.Errorf("%w: %s", ErrUnregisteredJurisdiction, exemption.Jurisdiction)
		}
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidTaxProfile)
	}
	return nil
}

func (p WorkerTaxProfileRevision) registeredFor(jurisdiction string, interval values.EffectiveInterval) bool {
	for _, registration := range p.Registrations {
		if registrationCovers(registration, jurisdiction, interval) {
			return true
		}
	}
	return false
}

func (p WorkerTaxProfileRevision) body() []byte {
	residence := append([]string(nil), p.ResidenceJurisdictions...)
	work := append([]string(nil), p.WorkJurisdictions...)
	sort.Strings(residence)
	sort.Strings(work)
	registrations := append([]TaxRegistrationRevision(nil), p.Registrations...)
	sort.Slice(registrations, func(i, j int) bool { return registrations[i].Jurisdiction < registrations[j].Jurisdiction })
	elections := append([]WithholdingElectionRevision(nil), p.Elections...)
	sort.Slice(elections, func(i, j int) bool { return elections[i].ElectionID < elections[j].ElectionID })
	exemptions := append([]TaxExemptionRevision(nil), p.Exemptions...)
	sort.Slice(exemptions, func(i, j int) bool { return exemptions[i].ExemptionID < exemptions[j].ExemptionID })
	w := canonicalbytes.New("hcmnext.domains.taxprofile.WorkerTaxProfileRevision", schemaVersion).
		String("worker_ref", p.WorkerRef).Int("revision", int64(p.Revision)).Int("parent_revision", int64(p.ParentRevision)).String("parent_digest", p.ParentDigest).
		SortedStrings("residence_jurisdiction", residence).SortedStrings("work_jurisdiction", work).
		String("filing_status", string(p.FilingStatus)).String("classification", string(p.Classification)).String("classification_evidence_ref", p.ClassificationEvidenceRef).
		Value("effective", p.Effective).Value("known_at", p.KnownAt).Count("registrations", len(registrations)).Count("elections", len(elections)).Count("exemptions", len(exemptions))
	for _, registration := range registrations {
		w.Value("registration", registration)
	}
	for _, election := range elections {
		w.Value("election", election)
	}
	for _, exemption := range exemptions {
		w.Value("exemption", exemption)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (p WorkerTaxProfileRevision) computedDigest() string { return canonicalbytes.Digest(p.body()) }
func (p WorkerTaxProfileRevision) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.body()
}

// NewWorkerTaxProfileRevision validates, detaches, and digests a profile.
func NewWorkerTaxProfileRevision(p WorkerTaxProfileRevision) (WorkerTaxProfileRevision, error) {
	p.ResidenceJurisdictions = append([]string(nil), p.ResidenceJurisdictions...)
	p.WorkJurisdictions = append([]string(nil), p.WorkJurisdictions...)
	p.Registrations = append([]TaxRegistrationRevision(nil), p.Registrations...)
	p.Elections = append([]WithholdingElectionRevision(nil), p.Elections...)
	p.Exemptions = append([]TaxExemptionRevision(nil), p.Exemptions...)
	p.CanonicalDigest = ""
	if err := p.Validate(); err != nil {
		return WorkerTaxProfileRevision{}, err
	}
	p.CanonicalDigest = p.computedDigest()
	return p, nil
}

// Fork appends a successor revision while preserving the parent's digest and
// worker identity. The successor is validated independently.
func (p WorkerTaxProfileRevision) Fork(next WorkerTaxProfileRevision) (WorkerTaxProfileRevision, error) {
	if err := p.Validate(); err != nil {
		return WorkerTaxProfileRevision{}, err
	}
	next.WorkerRef, next.Revision, next.ParentRevision, next.ParentDigest = p.WorkerRef, p.Revision+1, p.Revision, p.CanonicalDigest
	return NewWorkerTaxProfileRevision(next)
}

// TaxProfileExplanation is safe for audit display: it reports no registration
// reference or number and no election evidence value.
type TaxProfileExplanation struct {
	WorkerRef         string
	Revision          uint64
	ResidenceCount    int
	WorkCount         int
	FilingStatus      FilingStatus
	Classification    TaxClassification
	RegistrationCount int
	ElectionCount     int
	ExemptionCount    int
	Digest            string
}

func (p WorkerTaxProfileRevision) Explain() (TaxProfileExplanation, error) {
	if err := p.Validate(); err != nil {
		return TaxProfileExplanation{}, err
	}
	return TaxProfileExplanation{WorkerRef: p.WorkerRef, Revision: p.Revision, ResidenceCount: len(p.ResidenceJurisdictions), WorkCount: len(p.WorkJurisdictions), FilingStatus: p.FilingStatus, Classification: p.Classification, RegistrationCount: len(p.Registrations), ElectionCount: len(p.Elections), ExemptionCount: len(p.Exemptions), Digest: p.CanonicalDigest}, nil
}

func Explain(p WorkerTaxProfileRevision) (TaxProfileExplanation, error) { return p.Explain() }

// RegistrationPort is the small in-memory boundary adapters may replace with
// an external authority. The domain itself only needs immutable registrations.
type RegistrationPort interface {
	Registrations() []TaxRegistrationRevision
}

// MemoryRegistrationPort is a detached, read-only registration fake for pure
// tests and callers composing a profile snapshot.
type MemoryRegistrationPort struct{ values []TaxRegistrationRevision }

func NewMemoryRegistrationPort(registrations []TaxRegistrationRevision) (*MemoryRegistrationPort, error) {
	copyOf := append([]TaxRegistrationRevision(nil), registrations...)
	for _, registration := range copyOf {
		if err := registration.Validate(); err != nil {
			return nil, err
		}
	}
	return &MemoryRegistrationPort{values: copyOf}, nil
}

func (p *MemoryRegistrationPort) Registrations() []TaxRegistrationRevision {
	if p == nil {
		return nil
	}
	return append([]TaxRegistrationRevision(nil), p.values...)
}
