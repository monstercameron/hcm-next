package jobarch

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// ReferenceKind identifies the governed catalog that owns a profile reference.
type ReferenceKind string

const (
	ReferenceClassification ReferenceKind = "CLASSIFICATION"
	ReferenceQualification  ReferenceKind = "QUALIFICATION"
	ReferenceSkill          ReferenceKind = "SKILL"
	ReferenceCredential     ReferenceKind = "CREDENTIAL"
	ReferenceGrade          ReferenceKind = "GRADE"
	ReferenceBand           ReferenceKind = "BAND"
)

var (
	ErrRequirementsIncomplete = errors.New("jobarch: profile requirements are incomplete")
	ErrReferenceUnresolved    = errors.New("jobarch: profile reference is unresolved")
	ErrReferenceOutOfInterval = errors.New("jobarch: profile reference is outside its effective interval")
	ErrReferenceAuthority     = errors.New("jobarch: profile reference authority does not match")
	ErrCompensationCurrency   = errors.New("jobarch: compensation currency does not match its band")
	ErrGradeRelationship      = errors.New("jobarch: compensation grade does not match profile grade")
	ErrRequirementsConflict   = errors.New("jobarch: profile requirement aliases conflict")
)

// VersionedReference is the only accepted identity for governed metadata.
// Display labels and free text are intentionally not part of this type.
type VersionedReference struct {
	Ref           string
	Revision      string
	Authority     string
	EffectiveFrom time.Time
	EffectiveTo   time.Time
}

type ClassificationReference = VersionedReference

func (r VersionedReference) empty() bool {
	return strings.TrimSpace(r.Ref) == "" && strings.TrimSpace(r.Revision) == "" && strings.TrimSpace(r.Authority) == "" && r.EffectiveFrom.IsZero() && r.EffectiveTo.IsZero()
}

func (r VersionedReference) Validate(kind ReferenceKind) error {
	if r.empty() {
		return nil
	}
	if strings.TrimSpace(r.Ref) == "" || strings.TrimSpace(r.Revision) == "" || strings.TrimSpace(r.Authority) == "" {
		return fmt.Errorf("%w: %s requires ref, revision and authority", ErrRequirementsIncomplete, kind)
	}
	if r.EffectiveFrom.IsZero() {
		return fmt.Errorf("%w: %s %s@%s requires effective_from", ErrRequirementsIncomplete, kind, r.Ref, r.Revision)
	}
	if !r.EffectiveTo.IsZero() && !r.EffectiveTo.After(r.EffectiveFrom) {
		return fmt.Errorf("%w: %s %s@%s has invalid effective interval", ErrRequirementsIncomplete, kind, r.Ref, r.Revision)
	}
	return nil
}

func (r VersionedReference) ActiveAt(at time.Time) bool {
	return !at.Before(r.EffectiveFrom) && (r.EffectiveTo.IsZero() || at.Before(r.EffectiveTo))
}

func (r VersionedReference) key() string { return r.Ref + "\x00" + r.Revision }

// QualificationReference points to a versioned Qualification requirement.
type QualificationReference struct {
	VersionedReference
	Ref           string
	Revision      string
	Authority     string
	EffectiveFrom time.Time
	EffectiveTo   time.Time
}

type QualificationRequirementReference = QualificationReference

// SkillRequirementReference points to a versioned skill and carries the
// minimum proficiency required by the job.
type SkillRequirementReference struct {
	VersionedReference
	Ref           string
	Revision      string
	Authority     string
	EffectiveFrom time.Time
	EffectiveTo   time.Time
	Proficiency   int
}

type SkillReference = SkillRequirementReference

// CredentialRequirementReference points to a versioned credential or license.
// Validity is mandatory because a license requirement without a validity
// window cannot be evaluated safely.
type CredentialRequirementReference struct {
	VersionedReference
	Ref           string
	Revision      string
	Authority     string
	EffectiveFrom time.Time
	EffectiveTo   time.Time
	Level         int
	ValidFrom     time.Time
	ValidTo       time.Time
}

type CredentialReference = CredentialRequirementReference

// CompensationReference binds the job grade and pay band definitions used by
// a profile. Currency is explicit; no implicit FX conversion is allowed.
type CompensationReference struct {
	GradeRef      string
	GradeRevision string
	BandRef       string
	BandRevision  string
	Currency      string
	Authority     string
	EffectiveFrom time.Time
	EffectiveTo   time.Time
}

type GradeBandReference = CompensationReference
type ProfileRequirements = JobProfileRequirements

func (r CompensationReference) empty() bool {
	return r.GradeRef == "" && r.GradeRevision == "" && r.BandRef == "" && r.BandRevision == "" && r.Currency == "" && r.Authority == "" && r.EffectiveFrom.IsZero() && r.EffectiveTo.IsZero()
}

func (r CompensationReference) Validate() error {
	if r.empty() {
		return nil
	}
	if strings.TrimSpace(r.GradeRef) == "" || strings.TrimSpace(r.GradeRevision) == "" || strings.TrimSpace(r.BandRef) == "" || strings.TrimSpace(r.BandRevision) == "" || strings.TrimSpace(r.Currency) == "" || strings.TrimSpace(r.Authority) == "" {
		return fmt.Errorf("%w: compensation requires grade/band refs, revisions, currency and authority", ErrRequirementsIncomplete)
	}
	if r.EffectiveFrom.IsZero() {
		return fmt.Errorf("%w: compensation requires effective_from", ErrRequirementsIncomplete)
	}
	if !r.EffectiveTo.IsZero() && !r.EffectiveTo.After(r.EffectiveFrom) {
		return fmt.Errorf("%w: compensation has invalid effective interval", ErrRequirementsIncomplete)
	}
	return nil
}

// JobProfileRequirements is the governed metadata attached to a job profile.
// A zero value remains valid for legacy draft profiles; ValidateGoverned is
// used by publication and requires the complete set.
type JobProfileRequirements struct {
	Classification VersionedReference
	Qualifications []QualificationReference
	Skills         []SkillRequirementReference
	Credentials    []CredentialRequirementReference
	Compensation   CompensationReference

	// Ref-suffixed aliases make the wire intent explicit for callers. They are
	// merged into the canonical collections by normalize and are never stored
	// as separate authorities.
	ClassificationRef VersionedReference
	QualificationRefs []QualificationReference
	SkillRefs         []SkillRequirementReference
	CredentialRefs    []CredentialRequirementReference
	CompensationRef   CompensationReference
}

func (r JobProfileRequirements) clone() JobProfileRequirements {
	r.Qualifications = append([]QualificationReference(nil), r.Qualifications...)
	r.Skills = append([]SkillRequirementReference(nil), r.Skills...)
	r.Credentials = append([]CredentialRequirementReference(nil), r.Credentials...)
	r.QualificationRefs = append([]QualificationReference(nil), r.QualificationRefs...)
	r.SkillRefs = append([]SkillRequirementReference(nil), r.SkillRefs...)
	r.CredentialRefs = append([]CredentialRequirementReference(nil), r.CredentialRefs...)
	return r
}

func (r JobProfileRequirements) normalize() JobProfileRequirements {
	out := r.clone()
	if out.Classification.empty() {
		out.Classification = out.ClassificationRef
	} else if !out.ClassificationRef.empty() && out.Classification != out.ClassificationRef {
		out.ClassificationRef = VersionedReference{}
	}
	out.Qualifications = append(out.Qualifications, out.QualificationRefs...)
	out.Skills = append(out.Skills, out.SkillRefs...)
	out.Credentials = append(out.Credentials, out.CredentialRefs...)
	if out.Compensation.empty() {
		out.Compensation = out.CompensationRef
	} else if !out.CompensationRef.empty() && out.Compensation != out.CompensationRef {
		out.CompensationRef = CompensationReference{}
	}
	sort.Slice(out.Qualifications, func(i, j int) bool { return out.Qualifications[i].key() < out.Qualifications[j].key() })
	sort.Slice(out.Skills, func(i, j int) bool { return out.Skills[i].key() < out.Skills[j].key() })
	sort.Slice(out.Credentials, func(i, j int) bool { return out.Credentials[i].key() < out.Credentials[j].key() })
	return out
}

func (r JobProfileRequirements) Validate() error {
	if !r.Classification.empty() && !r.ClassificationRef.empty() && r.Classification != r.ClassificationRef {
		return ErrRequirementsConflict
	}
	if !r.Compensation.empty() && !r.CompensationRef.empty() && r.Compensation != r.CompensationRef {
		return ErrRequirementsConflict
	}
	r = r.normalize()
	if r.Classification.empty() && len(r.Qualifications) == 0 && len(r.Skills) == 0 && len(r.Credentials) == 0 && r.Compensation.empty() {
		return nil
	}
	if err := r.Classification.Validate(ReferenceClassification); err != nil {
		return err
	}
	seenQualification := make(map[string]struct{}, len(r.Qualifications))
	seenSkill := make(map[string]struct{}, len(r.Skills))
	seenCredential := make(map[string]struct{}, len(r.Credentials))
	for _, q := range r.Qualifications {
		if err := q.Validate(ReferenceQualification); err != nil {
			return err
		}
		if _, ok := seenQualification[q.key()]; ok {
			return fmt.Errorf("%w: duplicate qualification %s", ErrRequirementsIncomplete, q.key())
		}
		seenQualification[q.key()] = struct{}{}
	}
	for _, s := range r.Skills {
		if err := s.Validate(ReferenceSkill); err != nil {
			return err
		}
		if _, ok := seenSkill[s.key()]; ok {
			return fmt.Errorf("%w: duplicate skill %s", ErrRequirementsIncomplete, s.key())
		}
		seenSkill[s.key()] = struct{}{}
		if s.Proficiency <= 0 {
			return fmt.Errorf("%w: skill %s@%s requires positive proficiency", ErrRequirementsIncomplete, s.Ref, s.Revision)
		}
	}
	for _, c := range r.Credentials {
		if err := c.Validate(ReferenceCredential); err != nil {
			return err
		}
		if _, ok := seenCredential[c.key()]; ok {
			return fmt.Errorf("%w: duplicate credential %s", ErrRequirementsIncomplete, c.key())
		}
		seenCredential[c.key()] = struct{}{}
		if c.Level <= 0 || c.ValidFrom.IsZero() || (!c.ValidTo.IsZero() && !c.ValidTo.After(c.ValidFrom)) {
			return fmt.Errorf("%w: credential %s@%s requires positive level and valid validity interval", ErrRequirementsIncomplete, c.Ref, c.Revision)
		}
	}
	return r.Compensation.Validate()
}

// ValidateGoverned requires every authority-bound relationship needed for
// publication, then resolves each reference against the supplied catalog.
func (r JobProfileRequirements) ValidateGoverned(catalog ReferenceCatalog, profileFrom, profileTo, at time.Time) error {
	r = r.normalize()
	if r.Classification.empty() || len(r.Qualifications) == 0 || (len(r.Skills) == 0 && len(r.Credentials) == 0) || r.Compensation.empty() {
		return ErrRequirementsIncomplete
	}
	if err := r.Validate(); err != nil {
		return err
	}
	refs := []struct {
		kind ReferenceKind
		ref  VersionedReference
	}{
		{ReferenceClassification, r.Classification},
	}
	for _, q := range r.Qualifications {
		refs = append(refs, struct {
			kind ReferenceKind
			ref  VersionedReference
		}{ReferenceQualification, q.reference()})
	}
	for _, s := range r.Skills {
		refs = append(refs, struct {
			kind ReferenceKind
			ref  VersionedReference
		}{ReferenceSkill, s.reference()})
	}
	for _, c := range r.Credentials {
		refs = append(refs, struct {
			kind ReferenceKind
			ref  VersionedReference
		}{ReferenceCredential, c.reference()})
	}
	refs = append(refs,
		struct {
			kind ReferenceKind
			ref  VersionedReference
		}{ReferenceGrade, VersionedReference{Ref: r.Compensation.GradeRef, Revision: r.Compensation.GradeRevision, Authority: r.Compensation.Authority, EffectiveFrom: r.Compensation.EffectiveFrom, EffectiveTo: r.Compensation.EffectiveTo}},
		struct {
			kind ReferenceKind
			ref  VersionedReference
		}{ReferenceBand, VersionedReference{Ref: r.Compensation.BandRef, Revision: r.Compensation.BandRevision, Authority: r.Compensation.Authority, EffectiveFrom: r.Compensation.EffectiveFrom, EffectiveTo: r.Compensation.EffectiveTo}},
	)
	for _, item := range refs {
		record, ok := catalog.Resolve(item.kind, item.ref.Ref, item.ref.Revision)
		if !ok {
			return fmt.Errorf("%w: %s %s@%s", ErrReferenceUnresolved, item.kind, item.ref.Ref, item.ref.Revision)
		}
		if record.Authority != item.ref.Authority {
			return fmt.Errorf("%w: %s %s@%s", ErrReferenceAuthority, item.kind, item.ref.Ref, item.ref.Revision)
		}
		if !record.Covers(profileFrom, profileTo, at) {
			return fmt.Errorf("%w: %s %s@%s", ErrReferenceOutOfInterval, item.kind, item.ref.Ref, item.ref.Revision)
		}
		if item.kind == ReferenceBand && record.Currency != r.Compensation.Currency {
			return fmt.Errorf("%w: band=%s profile=%s", ErrCompensationCurrency, record.Currency, r.Compensation.Currency)
		}
	}
	return nil
}

func (r JobProfileRequirements) Canonical() []byte {
	r = r.normalize()
	if r.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.jobarch.JobProfileRequirements", 1).
		String("classification", referenceKey(r.Classification))
	for _, q := range r.Qualifications {
		w.String("qualification", q.key())
	}
	for _, s := range r.Skills {
		w.String("skill", s.key()+fmt.Sprint("\x00", s.Proficiency))
	}
	for _, c := range r.Credentials {
		w.String("credential", c.key()+fmt.Sprint("\x00", c.Level, "\x00", c.ValidFrom.UTC().Format(time.RFC3339Nano), "\x00", c.ValidTo.UTC().Format(time.RFC3339Nano)))
	}
	w.String("compensation", compensationKey(r.Compensation))
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func referenceKey(r VersionedReference) string {
	return r.Ref + "@" + r.Revision + ":" + r.Authority + ":" + r.EffectiveFrom.UTC().Format(time.RFC3339Nano) + ":" + r.EffectiveTo.UTC().Format(time.RFC3339Nano)
}
func compensationKey(r CompensationReference) string {
	return strings.Join([]string{r.GradeRef, r.GradeRevision, r.BandRef, r.BandRevision, r.Currency, r.Authority, r.EffectiveFrom.UTC().Format(time.RFC3339Nano), r.EffectiveTo.UTC().Format(time.RFC3339Nano)}, "\x00")
}
func mergeReference(explicit VersionedReference, ref, revision, authority string, from, to time.Time) VersionedReference {
	if ref != "" {
		explicit.Ref = ref
	}
	if revision != "" {
		explicit.Revision = revision
	}
	if authority != "" {
		explicit.Authority = authority
	}
	if !from.IsZero() {
		explicit.EffectiveFrom = from
	}
	if !to.IsZero() {
		explicit.EffectiveTo = to
	}
	return explicit
}

func (r QualificationReference) reference() VersionedReference {
	return mergeReference(r.VersionedReference, r.Ref, r.Revision, r.Authority, r.EffectiveFrom, r.EffectiveTo)
}
func (r QualificationReference) Validate(kind ReferenceKind) error {
	return r.reference().Validate(kind)
}
func (r QualificationReference) key() string { return r.reference().key() }
func (r SkillRequirementReference) reference() VersionedReference {
	return mergeReference(r.VersionedReference, r.Ref, r.Revision, r.Authority, r.EffectiveFrom, r.EffectiveTo)
}
func (r SkillRequirementReference) Validate(kind ReferenceKind) error {
	return r.reference().Validate(kind)
}
func (r SkillRequirementReference) key() string { return r.reference().key() }
func (r CredentialRequirementReference) reference() VersionedReference {
	return mergeReference(r.VersionedReference, r.Ref, r.Revision, r.Authority, r.EffectiveFrom, r.EffectiveTo)
}
func (r CredentialRequirementReference) Validate(kind ReferenceKind) error {
	return r.reference().Validate(kind)
}
func (r CredentialRequirementReference) key() string { return r.reference().key() }

func (p JobProfileRevision) requirements() JobProfileRequirements {
	r := p.Requirements.clone()
	if r.Classification.empty() {
		r.Classification = p.ClassificationRef
	}
	r.QualificationRefs = append(r.QualificationRefs, p.QualificationRefs...)
	r.SkillRefs = append(r.SkillRefs, p.SkillRefs...)
	r.CredentialRefs = append(r.CredentialRefs, p.CredentialRefs...)
	if r.Compensation.empty() {
		r.Compensation = p.CompensationRef
	}
	return r.normalize()
}

func (p JobProfileRevision) requirementAliasesConflict() bool {
	return (!p.Requirements.Classification.empty() && !p.ClassificationRef.empty() && p.Requirements.Classification != p.ClassificationRef) ||
		(!p.Requirements.Compensation.empty() && !p.CompensationRef.empty() && p.Requirements.Compensation != p.CompensationRef)
}

// ReferenceRecord is one catalog fact against which a profile reference is
// resolved. Currency is populated for grade/band records when applicable.
type ReferenceRecord struct {
	Kind          ReferenceKind
	Ref           string
	Revision      string
	Authority     string
	EffectiveFrom time.Time
	EffectiveTo   time.Time
	Currency      string
}

func (r ReferenceRecord) Covers(from, to, at time.Time) bool {
	if !r.EffectiveFrom.IsZero() && from.Before(r.EffectiveFrom) {
		return false
	}
	if !r.EffectiveTo.IsZero() && (!to.IsZero() && to.After(r.EffectiveTo)) {
		return false
	}
	return at.IsZero() || (r.EffectiveFrom.IsZero() || !at.Before(r.EffectiveFrom)) && (r.EffectiveTo.IsZero() || at.Before(r.EffectiveTo))
}

// ReferenceCatalog is a detached, immutable-on-use snapshot of authority
// records. Publication callers should construct a new catalog for each
// governance snapshot rather than mutate one concurrently.
type ReferenceCatalog struct{ Records []ReferenceRecord }

func (c ReferenceCatalog) Resolve(kind ReferenceKind, ref, revision string) (ReferenceRecord, bool) {
	for _, r := range c.Records {
		if r.Kind == kind && r.Ref == ref && r.Revision == revision {
			return r, true
		}
	}
	return ReferenceRecord{}, false
}

func (c ReferenceCatalog) CanonicalDigest() string {
	records := append([]ReferenceRecord(nil), c.Records...)
	sort.Slice(records, func(i, j int) bool {
		return string(records[i].Kind)+records[i].Ref+records[i].Revision < string(records[j].Kind)+records[j].Ref+records[j].Revision
	})
	w := canonicalbytes.New("hcmnext.domains.jobarch.ReferenceCatalog", 1)
	for _, r := range records {
		w.String("record", string(r.Kind)+"\x00"+r.Ref+"\x00"+r.Revision+"\x00"+r.Authority+"\x00"+r.Currency+"\x00"+r.EffectiveFrom.UTC().Format(time.RFC3339Nano)+"\x00"+r.EffectiveTo.UTC().Format(time.RFC3339Nano))
	}
	b, err := w.Bytes()
	if err != nil {
		return ""
	}
	return canonicalbytes.Digest(b)
}

func (p JobProfileRevision) ValidateGoverned(catalog ReferenceCatalog, at time.Time) error {
	requirements := p.requirements()
	if requirements.Compensation.GradeRef != p.GradeIDOrRef() {
		return fmt.Errorf("%w: profile=%s compensation=%s", ErrGradeRelationship, p.ProfileIDOrID(), requirements.Compensation.GradeRef)
	}
	return requirements.ValidateGoverned(catalog, p.EffectiveFrom, p.EffectiveTo, at)
}
