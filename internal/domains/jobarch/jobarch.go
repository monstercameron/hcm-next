// Package jobarch owns the immutable job-architecture vocabulary: families,
// levels, grades, profiles, their lineage, and position compatibility.
// Storage and publication workflows remain outside this pure package.
package jobarch

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/position"
	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/engines/payband"
)

const schemaVersion = 1

// Lifecycle is the closed lifecycle vocabulary for every architecture node.
type Lifecycle string

const (
	LifecycleDraft      Lifecycle = "DRAFT"
	LifecyclePublished  Lifecycle = "PUBLISHED"
	LifecycleRetired    Lifecycle = "RETIRED"
	LifecycleSuperseded Lifecycle = "SUPERSEDED"
)

func (l Lifecycle) Valid() bool {
	switch l {
	case LifecycleDraft, LifecyclePublished, LifecycleRetired, LifecycleSuperseded:
		return true
	default:
		return false
	}
}

func (l Lifecycle) String() string { return string(l) }

// RevisionLineage names the stable root and the immediately superseded
// revision. A missing lineage is valid only for an initial revision.
type RevisionLineage struct {
	RootID     string
	Supersedes string
}

var (
	ErrInvalidArchitecture               = errors.New("jobarch: invalid architecture")
	ErrInvalidRevision                   = errors.New("jobarch: invalid revision")
	ErrDuplicateIdentity                 = errors.New("jobarch: duplicate stable identity")
	ErrFamilyCycle                       = errors.New("jobarch: family hierarchy contains a cycle")
	ErrProfileReferencedByActivePosition = errors.New("jobarch: profile is referenced by an active position")
	ErrPositionPortFailed                = errors.New("jobarch: active-position port failed")
	ErrPayBandUnknown                    = errors.New("jobarch: pay-band catalog does not know the profile grade")
)

// FieldError identifies the exact architecture field that failed validation.
type FieldError struct {
	Field string
	Cause error
}

func (e *FieldError) Error() string { return fmt.Sprintf("jobarch: field %s: %v", e.Field, e.Cause) }
func (e *FieldError) Unwrap() error { return e.Cause }

func invalidField(field, message string) error {
	return &FieldError{Field: field, Cause: errors.New(message)}
}

func validateRevision(identity, revision, fieldPrefix string, lifecycle Lifecycle, from, to, knownFrom, knownTo time.Time, lineage RevisionLineage) error {
	if strings.TrimSpace(identity) == "" {
		return invalidField(fieldPrefix+".id", "is required")
	}
	if strings.TrimSpace(revision) == "" {
		return invalidField(fieldPrefix+".revision", "is required")
	}
	if !lifecycle.Valid() {
		return invalidField(fieldPrefix+".lifecycle", "is not declared")
	}
	if from.IsZero() {
		return invalidField(fieldPrefix+".effective_from", "is required")
	}
	if !to.IsZero() && !to.After(from) {
		return invalidField(fieldPrefix+".effective_to", "must be after effective_from")
	}
	if knownFrom.IsZero() {
		return invalidField(fieldPrefix+".known_from", "is required")
	}
	if !knownTo.IsZero() && !knownTo.After(knownFrom) {
		return invalidField(fieldPrefix+".known_to", "must be after known_from")
	}
	if lineage.Supersedes != "" && lineage.Supersedes == revision {
		return invalidField(fieldPrefix+".lineage.supersedes", "must name a different revision")
	}
	return nil
}

func archTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// JobFamilyRevision is one immutable family node. ParentID is empty only for
// a root family.
type JobFamilyRevision struct {
	ID, FamilyID, Revision     string
	Code, Name                 string
	ParentID                   string
	Lifecycle                  Lifecycle
	EffectiveFrom, EffectiveTo time.Time
	KnownFrom, KnownTo         time.Time
	Lineage                    RevisionLineage
}

type JobFamily = JobFamilyRevision

func (f JobFamilyRevision) Validate() error {
	if err := validateRevision(f.FamilyIDOrID(), f.Revision, "family", f.Lifecycle, f.EffectiveFrom, f.EffectiveTo, f.KnownFrom, f.KnownTo, f.Lineage); err != nil {
		return err
	}
	if f.Code == "" {
		return invalidField("family.code", "is required")
	}
	if f.Name == "" {
		return invalidField("family.name", "is required")
	}
	return nil
}

func (f JobFamilyRevision) FamilyIDOrID() string {
	if f.FamilyID != "" {
		return f.FamilyID
	}
	return f.ID
}

func (f JobFamilyRevision) Canonical() []byte {
	if f.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.jobarch.JobFamilyRevision", schemaVersion).
		String("id", f.ID).String("family_id", f.FamilyID).String("revision", f.Revision).
		String("code", f.Code).String("name", f.Name).String("parent_id", f.ParentID).
		String("lifecycle", f.Lifecycle.String()).String("effective_from", archTime(f.EffectiveFrom)).
		String("effective_to", archTime(f.EffectiveTo)).String("known_from", archTime(f.KnownFrom)).
		String("known_to", archTime(f.KnownTo)).String("lineage.root_id", f.Lineage.RootID).
		String("lineage.supersedes", f.Lineage.Supersedes).Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (f JobFamilyRevision) Digest() (string, error) {
	return digestOrError(f.Canonical(), f.Validate())
}

// JobLevelRevision is a level in one family's career framework.
type JobLevelRevision struct {
	ID, LevelID, Revision      string
	FamilyID, JobFamilyID      string
	Code, Title                string
	Rank                       int
	Lifecycle                  Lifecycle
	EffectiveFrom, EffectiveTo time.Time
	KnownFrom, KnownTo         time.Time
	Lineage                    RevisionLineage
}

type JobLevel = JobLevelRevision

func (l JobLevelRevision) LevelIDOrID() string {
	if l.LevelID != "" {
		return l.LevelID
	}
	return l.ID
}
func (l JobLevelRevision) FamilyIDOrFamily() string {
	if l.FamilyID != "" {
		return l.FamilyID
	}
	return l.JobFamilyID
}
func (l JobLevelRevision) Validate() error {
	if err := validateRevision(l.LevelIDOrID(), l.Revision, "level", l.Lifecycle, l.EffectiveFrom, l.EffectiveTo, l.KnownFrom, l.KnownTo, l.Lineage); err != nil {
		return err
	}
	if l.FamilyIDOrFamily() == "" {
		return invalidField("level.family_id", "is required")
	}
	if l.Code == "" {
		return invalidField("level.code", "is required")
	}
	if l.Title == "" {
		return invalidField("level.title", "is required")
	}
	if l.Rank <= 0 {
		return invalidField("level.rank", "must be positive")
	}
	return nil
}
func (l JobLevelRevision) Canonical() []byte {
	if l.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.jobarch.JobLevelRevision", schemaVersion).
		String("id", l.ID).String("level_id", l.LevelID).String("revision", l.Revision).
		String("family_id", l.FamilyIDOrFamily()).String("code", l.Code).String("title", l.Title).
		Int("rank", int64(l.Rank)).String("lifecycle", l.Lifecycle.String()).
		String("effective_from", archTime(l.EffectiveFrom)).String("effective_to", archTime(l.EffectiveTo)).
		String("known_from", archTime(l.KnownFrom)).String("known_to", archTime(l.KnownTo)).
		String("lineage.root_id", l.Lineage.RootID).String("lineage.supersedes", l.Lineage.Supersedes).Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (l JobLevelRevision) Digest() (string, error) { return digestOrError(l.Canonical(), l.Validate()) }

// JobGradeRevision is the architecture's grade reference. Its LevelID binds
// the compensation grade to one career level.
type JobGradeRevision struct {
	ID, GradeID, Revision      string
	LevelID, LevelRef          string
	Code, Name                 string
	Lifecycle                  Lifecycle
	EffectiveFrom, EffectiveTo time.Time
	KnownFrom, KnownTo         time.Time
	Lineage                    RevisionLineage
}

type JobGrade = JobGradeRevision

func (g JobGradeRevision) GradeIDOrID() string {
	if g.GradeID != "" {
		return g.GradeID
	}
	return g.ID
}
func (g JobGradeRevision) LevelIDOrRef() string {
	if g.LevelID != "" {
		return g.LevelID
	}
	return g.LevelRef
}
func (g JobGradeRevision) Validate() error {
	if err := validateRevision(g.GradeIDOrID(), g.Revision, "grade", g.Lifecycle, g.EffectiveFrom, g.EffectiveTo, g.KnownFrom, g.KnownTo, g.Lineage); err != nil {
		return err
	}
	if g.LevelIDOrRef() == "" {
		return invalidField("grade.level_id", "is required")
	}
	if g.Code == "" {
		return invalidField("grade.code", "is required")
	}
	if g.Name == "" {
		return invalidField("grade.name", "is required")
	}
	return nil
}
func (g JobGradeRevision) Canonical() []byte {
	if g.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.jobarch.JobGradeRevision", schemaVersion).
		String("id", g.ID).String("grade_id", g.GradeID).String("revision", g.Revision).
		String("level_id", g.LevelIDOrRef()).String("code", g.Code).String("name", g.Name).
		String("lifecycle", g.Lifecycle.String()).String("effective_from", archTime(g.EffectiveFrom)).
		String("effective_to", archTime(g.EffectiveTo)).String("known_from", archTime(g.KnownFrom)).
		String("known_to", archTime(g.KnownTo)).String("lineage.root_id", g.Lineage.RootID).
		String("lineage.supersedes", g.Lineage.Supersedes).Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (g JobGradeRevision) Digest() (string, error) { return digestOrError(g.Canonical(), g.Validate()) }

// JobProfileRevision is a versioned job meaning bound to exactly one family,
// level and grade. A published profile is never edited in place.
type JobProfileRevision struct {
	ID, ProfileID, Revision string
	FamilyID, FamilyRef     string
	LevelID, LevelRef       string
	GradeID, GradeRef       string
	JobCode, Title          string
	Description             string
	Requirements            JobProfileRequirements
	// Direct reference fields are compatibility spellings for callers that
	// build a profile without first assembling JobProfileRequirements. The
	// canonical form merges them with Requirements; neither is a second
	// authority.
	ClassificationRef          VersionedReference
	QualificationRefs          []QualificationReference
	SkillRefs                  []SkillRequirementReference
	CredentialRefs             []CredentialRequirementReference
	CompensationRef            CompensationReference
	Lifecycle                  Lifecycle
	EffectiveFrom, EffectiveTo time.Time
	KnownFrom, KnownTo         time.Time
	Lineage                    RevisionLineage
}

type JobProfile = JobProfileRevision

func (p JobProfileRevision) ProfileIDOrID() string {
	if p.ProfileID != "" {
		return p.ProfileID
	}
	return p.ID
}
func (p JobProfileRevision) FamilyIDOrRef() string {
	if p.FamilyID != "" {
		return p.FamilyID
	}
	return p.FamilyRef
}
func (p JobProfileRevision) LevelIDOrRef() string {
	if p.LevelID != "" {
		return p.LevelID
	}
	return p.LevelRef
}
func (p JobProfileRevision) GradeIDOrRef() string {
	if p.GradeID != "" {
		return p.GradeID
	}
	return p.GradeRef
}
func (p JobProfileRevision) Validate() error {
	if err := validateRevision(p.ProfileIDOrID(), p.Revision, "profile", p.Lifecycle, p.EffectiveFrom, p.EffectiveTo, p.KnownFrom, p.KnownTo, p.Lineage); err != nil {
		return err
	}
	if p.FamilyIDOrRef() == "" {
		return invalidField("profile.family_id", "is required")
	}
	if p.LevelIDOrRef() == "" {
		return invalidField("profile.level_id", "is required")
	}
	if p.GradeIDOrRef() == "" {
		return invalidField("profile.grade_id", "is required")
	}
	if p.JobCode == "" {
		return invalidField("profile.job_code", "is required")
	}
	if p.Title == "" {
		return invalidField("profile.title", "is required")
	}
	if p.requirementAliasesConflict() {
		return ErrRequirementsConflict
	}
	if err := p.requirements().Validate(); err != nil {
		return fmt.Errorf("%w: profile requirements: %w", ErrInvalidArchitecture, err)
	}
	return nil
}
func (p JobProfileRevision) ActiveAt(at, known time.Time) bool {
	return p.Lifecycle == LifecyclePublished && !at.Before(p.EffectiveFrom) && (p.EffectiveTo.IsZero() || at.Before(p.EffectiveTo)) && !known.Before(p.KnownFrom) && (p.KnownTo.IsZero() || known.Before(p.KnownTo))
}
func (p JobProfileRevision) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.jobarch.JobProfileRevision", schemaVersion).
		String("id", p.ID).String("profile_id", p.ProfileID).String("revision", p.Revision).
		String("family_id", p.FamilyIDOrRef()).String("level_id", p.LevelIDOrRef()).
		String("grade_id", p.GradeIDOrRef()).String("job_code", p.JobCode).String("title", p.Title).
		String("description", p.Description).String("lifecycle", p.Lifecycle.String()).
		String("effective_from", archTime(p.EffectiveFrom)).String("effective_to", archTime(p.EffectiveTo)).
		String("known_from", archTime(p.KnownFrom)).String("known_to", archTime(p.KnownTo)).
		String("lineage.root_id", p.Lineage.RootID).String("lineage.supersedes", p.Lineage.Supersedes).
		Field("requirements", p.requirements().Canonical()).Bytes()
	if err != nil {
		return nil
	}
	return b
}
func (p JobProfileRevision) Digest() (string, error) {
	return digestOrError(p.Canonical(), p.Validate())
}

func digestOrError(raw []byte, validation error) (string, error) {
	if raw == nil {
		return "", validation
	}
	return canonicalbytes.Digest(raw), nil
}

// ArchitectureRevision is one immutable graph snapshot.
type ArchitectureRevision struct {
	ID, Revision, SupersedesRevision string
	Families                         []JobFamilyRevision
	Levels                           []JobLevelRevision
	Grades                           []JobGradeRevision
	Profiles                         []JobProfileRevision
	CanonicalDigest                  string
}

type JobArchitecture = ArchitectureRevision

func cloneFamilies(in []JobFamilyRevision) []JobFamilyRevision {
	return append([]JobFamilyRevision(nil), in...)
}
func cloneLevels(in []JobLevelRevision) []JobLevelRevision {
	return append([]JobLevelRevision(nil), in...)
}
func cloneGrades(in []JobGradeRevision) []JobGradeRevision {
	return append([]JobGradeRevision(nil), in...)
}
func cloneProfiles(in []JobProfileRevision) []JobProfileRevision {
	out := append([]JobProfileRevision(nil), in...)
	for i := range out {
		out[i].Requirements = out[i].Requirements.clone()
		out[i].QualificationRefs = append([]QualificationReference(nil), out[i].QualificationRefs...)
		out[i].SkillRefs = append([]SkillRequirementReference(nil), out[i].SkillRefs...)
		out[i].CredentialRefs = append([]CredentialRequirementReference(nil), out[i].CredentialRefs...)
	}
	return out
}

func (a ArchitectureRevision) Validate() error {
	if a.ID == "" {
		return invalidField("architecture.id", "is required")
	}
	if a.Revision == "" {
		return invalidField("architecture.revision", "is required")
	}
	if a.SupersedesRevision == a.Revision && a.SupersedesRevision != "" {
		return invalidField("architecture.supersedes_revision", "must differ from revision")
	}
	familyIDs := make(map[string]struct{}, len(a.Families))
	for i, f := range a.Families {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("%w: families[%d]: %w", ErrInvalidArchitecture, i, err)
		}
		id := f.FamilyIDOrID()
		if _, ok := familyIDs[id]; ok {
			return fmt.Errorf("%w: family %s", ErrDuplicateIdentity, id)
		}
		familyIDs[id] = struct{}{}
	}
	if err := validateFamilyDAG(a.Families); err != nil {
		return err
	}
	levelIDs := make(map[string]struct{}, len(a.Levels))
	for i, l := range a.Levels {
		if err := l.Validate(); err != nil {
			return fmt.Errorf("%w: levels[%d]: %w", ErrInvalidArchitecture, i, err)
		}
		if _, ok := familyIDs[l.FamilyIDOrFamily()]; !ok {
			return invalidField("levels["+fmt.Sprint(i)+"].family_id", "does not reference a known family")
		}
		id := l.LevelIDOrID()
		if _, ok := levelIDs[id]; ok {
			return fmt.Errorf("%w: level %s", ErrDuplicateIdentity, id)
		}
		levelIDs[id] = struct{}{}
	}
	gradeIDs := make(map[string]struct{}, len(a.Grades))
	for i, g := range a.Grades {
		if err := g.Validate(); err != nil {
			return fmt.Errorf("%w: grades[%d]: %w", ErrInvalidArchitecture, i, err)
		}
		if _, ok := levelIDs[g.LevelIDOrRef()]; !ok {
			return invalidField("grades["+fmt.Sprint(i)+"].level_id", "does not reference a known level")
		}
		id := g.GradeIDOrID()
		if _, ok := gradeIDs[id]; ok {
			return fmt.Errorf("%w: grade %s", ErrDuplicateIdentity, id)
		}
		gradeIDs[id] = struct{}{}
	}
	profileIDs := make(map[string]struct{}, len(a.Profiles))
	for i, p := range a.Profiles {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("%w: profiles[%d]: %w", ErrInvalidArchitecture, i, err)
		}
		if _, ok := familyIDs[p.FamilyIDOrRef()]; !ok {
			return invalidField("profiles["+fmt.Sprint(i)+"].family_id", "does not reference a known family")
		}
		level, ok := findLevel(a.Levels, p.LevelIDOrRef())
		if !ok {
			return invalidField("profiles["+fmt.Sprint(i)+"].level_id", "does not reference a known level")
		}
		grade, ok := findGrade(a.Grades, p.GradeIDOrRef())
		if !ok {
			return invalidField("profiles["+fmt.Sprint(i)+"].grade_id", "does not reference a known grade")
		}
		if level.FamilyIDOrFamily() != p.FamilyIDOrRef() {
			return invalidField("profile.family_id", "does not match its level")
		}
		if grade.LevelIDOrRef() != p.LevelIDOrRef() {
			return invalidField("profile.grade_id", "grade does not belong to profile level")
		}
		id := p.ProfileIDOrID()
		if _, ok := profileIDs[id]; ok {
			return fmt.Errorf("%w: profile %s", ErrDuplicateIdentity, id)
		}
		profileIDs[id] = struct{}{}
	}
	if a.CanonicalDigest != "" && a.CanonicalDigest != a.computedDigest() {
		return fmt.Errorf("%w: canonical_digest does not match content", ErrInvalidRevision)
	}
	return nil
}

func validateFamilyDAG(families []JobFamilyRevision) error {
	parents := make(map[string]string, len(families))
	for _, f := range families {
		parents[f.FamilyIDOrID()] = f.ParentID
	}
	for id := range parents {
		seen := map[string]bool{id: true}
		for parent := parents[id]; parent != ""; parent = parents[parent] {
			if seen[parent] {
				return fmt.Errorf("%w: family %s", ErrFamilyCycle, parent)
			}
			seen[parent] = true
			if _, ok := parents[parent]; !ok {
				return invalidField("family.parent_id", "does not reference a known family")
			}
		}
	}
	return nil
}

func findLevel(levels []JobLevelRevision, id string) (JobLevelRevision, bool) {
	for _, l := range levels {
		if l.LevelIDOrID() == id {
			return l, true
		}
	}
	return JobLevelRevision{}, false
}
func findGrade(grades []JobGradeRevision, id string) (JobGradeRevision, bool) {
	for _, g := range grades {
		if g.GradeIDOrID() == id {
			return g, true
		}
	}
	return JobGradeRevision{}, false
}

func (a ArchitectureRevision) body() []byte {
	b := canonicalbytes.New("hcmnext.domains.jobarch.ArchitectureRevision", schemaVersion).
		String("id", a.ID).String("revision", a.Revision).String("supersedes_revision", a.SupersedesRevision).
		Count("families", len(a.Families))
	for _, f := range a.Families {
		b.Field("family", f.Canonical())
	}
	b.Count("levels", len(a.Levels))
	for _, l := range a.Levels {
		b.Field("level", l.Canonical())
	}
	b.Count("grades", len(a.Grades))
	for _, g := range a.Grades {
		b.Field("grade", g.Canonical())
	}
	b.Count("profiles", len(a.Profiles))
	for _, p := range a.Profiles {
		b.Field("profile", p.Canonical())
	}
	raw, err := b.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
func (a ArchitectureRevision) computedDigest() string { return canonicalbytes.Digest(a.body()) }
func (a ArchitectureRevision) Canonical() []byte {
	if a.Validate() != nil {
		return nil
	}
	return a.body()
}
func (a ArchitectureRevision) Digest() (string, error) {
	return digestOrError(a.Canonical(), a.Validate())
}

// NewArchitectureRevision copies and validates a graph, then assigns its
// canonical digest. The input slices are never retained by the result.
func NewArchitectureRevision(a ArchitectureRevision) (ArchitectureRevision, error) {
	a.Families, a.Levels, a.Grades, a.Profiles = cloneFamilies(a.Families), cloneLevels(a.Levels), cloneGrades(a.Grades), cloneProfiles(a.Profiles)
	a.CanonicalDigest = ""
	if err := a.Validate(); err != nil {
		return ArchitectureRevision{}, err
	}
	a.CanonicalDigest = a.computedDigest()
	return a, nil
}
func NewArchitecture(a ArchitectureRevision) (ArchitectureRevision, error) {
	return NewArchitectureRevision(a)
}

// Revise returns a new graph revision and rejects a same-version meaning edit.
func (a ArchitectureRevision) Revise(next ArchitectureRevision) (ArchitectureRevision, error) {
	if err := a.Validate(); err != nil {
		return ArchitectureRevision{}, err
	}
	if next.ID != a.ID {
		return ArchitectureRevision{}, invalidField("architecture.id", "cannot change across lineage")
	}
	if next.Revision == "" || next.Revision == a.Revision {
		return ArchitectureRevision{}, invalidField("architecture.revision", "must be a new revision")
	}
	if next.SupersedesRevision != a.Revision {
		return ArchitectureRevision{}, invalidField("architecture.supersedes_revision", "must name the current revision")
	}
	for _, old := range a.Profiles {
		for _, newer := range next.Profiles {
			if old.ProfileIDOrID() == newer.ProfileIDOrID() && old.Revision == newer.Revision && string(old.Canonical()) != string(newer.Canonical()) {
				return ArchitectureRevision{}, fmt.Errorf("%w: profile %s revision %s", ErrInvalidRevision, old.ProfileIDOrID(), old.Revision)
			}
		}
	}
	return NewArchitectureRevision(next)
}

// PositionReference is the position-side reference needed for profile
// compatibility. It is deliberately smaller than a position aggregate and
// is normally supplied by a PositionFacts adapter.
type PositionReference struct {
	ID, ProfileID, JobCode, GradeCode, PayZone string
	Lifecycle                                  position.Lifecycle
}

func (p PositionReference) Active() bool {
	switch p.Lifecycle {
	case position.LifecycleOpen, position.LifecycleReserved, position.LifecyclePartiallyFilled, position.LifecycleFilled, position.LifecycleVacant:
		return true
	default:
		return false
	}
}

// ActivePositionReader is the port used before a profile can be retired.
type ActivePositionReader interface {
	ActivePositionsReferencingProfile(context.Context, string) ([]PositionReference, error)
}

type CompatibilityOutcome string

const (
	Compatible   CompatibilityOutcome = "COMPATIBLE"
	Incompatible CompatibilityOutcome = "INCOMPATIBLE"
	Unknown      CompatibilityOutcome = "UNKNOWN"
)

type CompatibilityDiagnostic struct{ Code, Detail string }

type CompatibilityResult struct {
	Outcome                                           CompatibilityOutcome
	PositionID, ProfileID, ProfileRevision, GradeCode string
	BandID, BandVersion                               string
	Diagnostics                                       []CompatibilityDiagnostic
}

func (r CompatibilityResult) Explain() string {
	return fmt.Sprintf("job architecture compatibility %s: position=%s profile=%s revision=%s grade=%s band=%s@%s diagnostics=%d", r.Outcome, r.PositionID, r.ProfileID, r.ProfileRevision, r.GradeCode, r.BandID, r.BandVersion, len(r.Diagnostics))
}

// CheckCompatibility permits a position only when its profile is active and
// its grade is present in the pay-band catalog for the position's scope.
func CheckCompatibility(a ArchitectureRevision, p PositionReference, bands payband.Catalog, at time.Time) (CompatibilityResult, error) {
	if err := a.Validate(); err != nil {
		return CompatibilityResult{Outcome: Unknown}, err
	}
	if p.ID == "" || p.ProfileID == "" || p.JobCode == "" || p.GradeCode == "" || p.PayZone == "" {
		return CompatibilityResult{Outcome: Unknown}, invalidField("position", "id, profile_id, job_code, grade_code and pay_zone are required")
	}
	profile, ok := findProfile(a.Profiles, p.ProfileID)
	if !ok {
		return CompatibilityResult{Outcome: Incompatible, PositionID: p.ID, ProfileID: p.ProfileID, Diagnostics: []CompatibilityDiagnostic{{Code: "PROFILE_NOT_FOUND", Detail: "position profile is not in this architecture"}}}, nil
	}
	res := CompatibilityResult{Outcome: Incompatible, PositionID: p.ID, ProfileID: profile.ProfileIDOrID(), ProfileRevision: profile.Revision, GradeCode: p.GradeCode}
	if !profile.ActiveAt(at, at) {
		res.Diagnostics = append(res.Diagnostics, CompatibilityDiagnostic{Code: "PROFILE_NOT_ACTIVE", Detail: "profile is not published and effective at the requested coordinate"})
		return res, nil
	}
	grade, ok := findGrade(a.Grades, profile.GradeIDOrRef())
	if !ok || grade.Code != p.GradeCode {
		res.Diagnostics = append(res.Diagnostics, CompatibilityDiagnostic{Code: "GRADE_MISMATCH", Detail: "position grade does not match the profile grade"})
		return res, nil
	}
	band, ok := bands.Lookup(payband.Scope{JobCode: p.JobCode, Grade: p.GradeCode, PayZone: p.PayZone})
	if !ok {
		res.Diagnostics = append(res.Diagnostics, CompatibilityDiagnostic{Code: "GRADE_NOT_IN_PAYBAND_CATALOG", Detail: ErrPayBandUnknown.Error()})
		return res, nil
	}
	res.Outcome, res.BandID, res.BandVersion = Compatible, band.ID, band.Version
	return res, nil
}

func findProfile(profiles []JobProfileRevision, id string) (JobProfileRevision, bool) {
	for _, p := range profiles {
		if p.ProfileIDOrID() == id {
			return p, true
		}
	}
	return JobProfileRevision{}, false
}
func CheckPositionCompatibility(a ArchitectureRevision, p PositionReference, bands payband.Catalog, at time.Time) (CompatibilityResult, error) {
	return CheckCompatibility(a, p, bands, at)
}

// RetireProfile returns an immutable successor. An active position reference
// blocks the transition, and a reader failure is never treated as no usage.
func (a ArchitectureRevision) RetireProfile(ctx context.Context, profileID string, reader ActivePositionReader) (ArchitectureRevision, error) {
	if err := a.Validate(); err != nil {
		return ArchitectureRevision{}, err
	}
	if reader == nil {
		return ArchitectureRevision{}, invalidField("active_position_port", "is required")
	}
	profileIndex := -1
	for i, p := range a.Profiles {
		if p.ProfileIDOrID() == profileID {
			profileIndex = i
			break
		}
	}
	if profileIndex < 0 {
		return ArchitectureRevision{}, invalidField("profile_id", "does not reference a known profile")
	}
	refs, err := reader.ActivePositionsReferencingProfile(ctx, profileID)
	if err != nil {
		return ArchitectureRevision{}, fmt.Errorf("%w: %w", ErrPositionPortFailed, err)
	}
	for _, ref := range refs {
		// The port contract already asks for active references. Treat an
		// unspecified lifecycle as active as well: an incomplete answer must
		// not become a silent permission to retire the profile.
		if ref.Active() || ref.Lifecycle == "" {
			return ArchitectureRevision{}, fmt.Errorf("%w: %s", ErrProfileReferencedByActivePosition, ref.ID)
		}
	}
	next := a
	next.Revision = nextRevision(a.Revision)
	next.SupersedesRevision = a.Revision
	next.Profiles = cloneProfiles(a.Profiles)
	next.Profiles[profileIndex].Lifecycle = LifecycleRetired
	next.Profiles[profileIndex].Lineage = RevisionLineage{RootID: profileID, Supersedes: a.Profiles[profileIndex].Revision}
	next.Profiles[profileIndex].Revision = a.Profiles[profileIndex].Revision + ".next"
	return a.Revise(next)
}

func nextRevision(current string) string { return current + ".next" }

func (a ArchitectureRevision) Explain() string {
	governed := 0
	classifications, qualifications, skills, credentials := 0, 0, 0, 0
	for _, profile := range a.Profiles {
		r := profile.requirements()
		if !r.Classification.empty() || len(r.Qualifications) != 0 || len(r.Skills) != 0 || len(r.Credentials) != 0 || !r.Compensation.empty() {
			governed++
		}
		if !r.Classification.empty() {
			classifications++
		}
		qualifications += len(r.Qualifications)
		skills += len(r.Skills)
		credentials += len(r.Credentials)
	}
	return fmt.Sprintf("job architecture %s revision=%s families=%d levels=%d grades=%d profiles=%d requirements=%d classifications=%d qualifications=%d skills=%d credentials=%d digest=%s", a.ID, a.Revision, len(a.Families), len(a.Levels), len(a.Grades), len(a.Profiles), governed, classifications, qualifications, skills, credentials, a.CanonicalDigest)
}

// Explain is the package-level audit explanation entry point.
func Explain(a ArchitectureRevision) string { return a.Explain() }

// Version reports this domain contract's canonical schema version.
func Version() int { return schemaVersion }

// SortedProfileIDs is a small deterministic projection for audit consumers.
func (a ArchitectureRevision) SortedProfileIDs() []string {
	out := make([]string, 0, len(a.Profiles))
	for _, p := range a.Profiles {
		out = append(out, p.ProfileIDOrID())
	}
	sort.Strings(out)
	return out
}
