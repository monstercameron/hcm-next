// Package skill owns the canonical skill ontology and the evidence-backed
// proficiency vocabulary. It is deliberately pure: callers provide evidence
// through a read port and the package returns detached, immutable results.
package skill

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

const schemaVersion = 1

// Version reports this domain vocabulary version.
func Version() int { return schemaVersion }

var (
	ErrInvalidSkill       = errors.New("skill: invalid skill")
	ErrInvalidOntology    = errors.New("skill: invalid ontology")
	ErrInvalidEvidence    = errors.New("skill: invalid worker skill evidence")
	ErrInvalidEquivalence = errors.New("skill: invalid equivalence")
	ErrInvalidResolution  = errors.New("skill: invalid resolution")
	ErrReaderFailed       = errors.New("skill: evidence reader failed")
)

const (
	KindSkill  values.Kind = "skill"
	KindWorker values.Kind = "worker"
)

// ProficiencyLevel is a closed, ordered scale. The order is semantic and is
// used only within one ontology revision.
type ProficiencyLevel string

const (
	ProficiencyFoundational ProficiencyLevel = "FOUNDATIONAL"
	ProficiencyDeveloping   ProficiencyLevel = "DEVELOPING"
	ProficiencyProficient   ProficiencyLevel = "PROFICIENT"
	ProficiencyAdvanced     ProficiencyLevel = "ADVANCED"
	ProficiencyExpert       ProficiencyLevel = "EXPERT"

	// Friendly aliases retain one closed vocabulary.
	ProficiencyBeginner = ProficiencyFoundational
	ProficiencyWorking  = ProficiencyDeveloping
)

var defaultScale = []ProficiencyLevel{
	ProficiencyFoundational,
	ProficiencyDeveloping,
	ProficiencyProficient,
	ProficiencyAdvanced,
	ProficiencyExpert,
}

func DefaultProficiencyScale() []ProficiencyLevel {
	return append([]ProficiencyLevel(nil), defaultScale...)
}

func (p ProficiencyLevel) Valid() bool {
	for _, candidate := range defaultScale {
		if p == candidate {
			return true
		}
	}
	return false
}

func (p ProficiencyLevel) String() string { return string(p) }

func validateScale(scale []ProficiencyLevel) error {
	if len(scale) == 0 {
		return fmt.Errorf("%w: proficiency scale is required", ErrInvalidSkill)
	}
	seen := make(map[ProficiencyLevel]struct{}, len(scale))
	for i, level := range scale {
		if !level.Valid() {
			return fmt.Errorf("%w: proficiency scale[%d] %q is not declared", ErrInvalidSkill, i, level)
		}
		if _, ok := seen[level]; ok {
			return fmt.Errorf("%w: proficiency scale[%d] %q is duplicated", ErrInvalidSkill, i, level)
		}
		seen[level] = struct{}{}
	}
	return nil
}

func scaleOrDefault(scale []ProficiencyLevel) []ProficiencyLevel {
	if len(scale) == 0 {
		return defaultScale
	}
	return scale
}

func levelRank(scale []ProficiencyLevel, level ProficiencyLevel) int {
	for i, candidate := range scaleOrDefault(scale) {
		if candidate == level {
			return i + 1
		}
	}
	return 0
}

func levelFromRank(scale []ProficiencyLevel, rank int) ProficiencyLevel {
	scale = scaleOrDefault(scale)
	if rank < 1 || rank > len(scale) {
		return ""
	}
	return scale[rank-1]
}

// SkillDefinitionRevision is one immutable node in the ontology graph.
// Parents and aliases are canonical vocabulary facts; display names are not
// used as identity.
type SkillDefinitionRevision struct {
	SkillRef         values.EntityRef
	SkillID          values.EntityRef // additive spelling for callers using ID terminology
	Revision         values.RevisionToken
	Supersedes       values.RevisionToken
	Name             string
	ParentRefs       []values.EntityRef
	Aliases          []string
	ProficiencyScale []ProficiencyLevel
	CanonicalDigest  string
}

func (s SkillDefinitionRevision) ref() values.EntityRef {
	if s.SkillRef.Validate() == nil {
		return s.SkillRef
	}
	return s.SkillID
}

func (s SkillDefinitionRevision) Validate() error {
	ref := s.ref()
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("%w: skill_ref: %v", ErrInvalidSkill, err)
	}
	if ref.Kind != KindSkill {
		return fmt.Errorf("%w: skill_ref kind %q is not skill", ErrInvalidSkill, ref.Kind)
	}
	if !s.Revision.IsSpecified() {
		return fmt.Errorf("%w: revision is required", ErrInvalidSkill)
	}
	if err := s.Revision.Validate(); err != nil {
		return fmt.Errorf("%w: revision: %v", ErrInvalidSkill, err)
	}
	if strings.TrimSpace(s.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidSkill)
	}
	if err := validateScale(scaleOrDefault(s.ProficiencyScale)); err != nil {
		return err
	}
	seenAliases := make(map[string]struct{}, len(s.Aliases))
	for i, alias := range s.Aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			return fmt.Errorf("%w: aliases[%d] is empty", ErrInvalidSkill, i)
		}
		if _, ok := seenAliases[alias]; ok {
			return fmt.Errorf("%w: aliases[%d] %q is duplicated", ErrInvalidSkill, i, alias)
		}
		seenAliases[alias] = struct{}{}
	}
	for i, parent := range s.ParentRefs {
		if err := parent.Validate(); err != nil {
			return fmt.Errorf("%w: parent_refs[%d]: %v", ErrInvalidSkill, i, err)
		}
		if parent.Kind != KindSkill {
			return fmt.Errorf("%w: parent_refs[%d] kind %q is not skill", ErrInvalidSkill, i, parent.Kind)
		}
		if parent.Tenant != ref.Tenant {
			return fmt.Errorf("%w: parent_refs[%d] tenant differs", ErrInvalidSkill, i)
		}
		if parent == ref {
			return fmt.Errorf("%w: parent_refs[%d] cannot reference the skill itself", ErrInvalidSkill, i)
		}
	}
	if s.Supersedes.IsSpecified() {
		if err := s.Supersedes.Validate(); err != nil {
			return fmt.Errorf("%w: supersedes: %v", ErrInvalidSkill, err)
		}
		if s.Supersedes.Equal(s.Revision) {
			return fmt.Errorf("%w: supersedes cannot equal revision", ErrInvalidSkill)
		}
	}
	if s.CanonicalDigest != "" && s.CanonicalDigest != s.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidSkill)
	}
	return nil
}

func (s SkillDefinitionRevision) Canonical() []byte {
	if s.Validate() != nil {
		return nil
	}
	parents := append([]values.EntityRef(nil), s.ParentRefs...)
	aliases := append([]string(nil), s.Aliases...)
	sort.Slice(parents, func(i, j int) bool { return parents[i].String() < parents[j].String() })
	sort.Strings(aliases)
	w := canonicalbytes.New("hcmnext.domains.skill.SkillDefinitionRevision", schemaVersion).
		Value("skill_ref", s.ref()).Value("revision", s.Revision).Value("supersedes", s.Supersedes).
		String("name", s.Name).Count("parents", len(parents))
	for _, parent := range parents {
		w.Value("parent", parent)
	}
	w.Count("aliases", len(aliases))
	for _, alias := range aliases {
		w.String("alias", alias)
	}
	scale := scaleOrDefault(s.ProficiencyScale)
	w.Count("proficiency_scale", len(scale))
	for _, level := range scale {
		w.String("proficiency_level", string(level))
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (s SkillDefinitionRevision) computedDigest() string {
	return canonicalbytes.Digest(s.CanonicalWithoutDigest())
}

func (s SkillDefinitionRevision) CanonicalWithoutDigest() []byte {
	// Canonical is already independent of CanonicalDigest, so this method keeps
	// the digest construction explicit for callers inspecting the contract.
	return s.canonicalBody()
}

func (s SkillDefinitionRevision) canonicalBody() []byte {
	if err := s.validateWithoutDigest(); err != nil {
		return nil
	}
	parents := append([]values.EntityRef(nil), s.ParentRefs...)
	aliases := append([]string(nil), s.Aliases...)
	sort.Slice(parents, func(i, j int) bool { return parents[i].String() < parents[j].String() })
	sort.Strings(aliases)
	w := canonicalbytes.New("hcmnext.domains.skill.SkillDefinitionRevision", schemaVersion).
		Value("skill_ref", s.ref()).Value("revision", s.Revision).Value("supersedes", s.Supersedes).
		String("name", s.Name).Count("parents", len(parents))
	for _, parent := range parents {
		w.Value("parent", parent)
	}
	w.Count("aliases", len(aliases))
	for _, alias := range aliases {
		w.String("alias", alias)
	}
	scale := scaleOrDefault(s.ProficiencyScale)
	w.Count("proficiency_scale", len(scale))
	for _, level := range scale {
		w.String("proficiency_level", string(level))
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (s SkillDefinitionRevision) validateWithoutDigest() error {
	digest := s.CanonicalDigest
	s.CanonicalDigest = ""
	// Avoid recursion through Validate while checking the stored digest.
	if err := s.Validate(); err != nil && digest == "" {
		return err
	}
	return nil
}

// NewSkillDefinition validates and computes the canonical digest.
func NewSkillDefinition(s SkillDefinitionRevision) (SkillDefinitionRevision, error) {
	s.ParentRefs = append([]values.EntityRef(nil), s.ParentRefs...)
	s.Aliases = append([]string(nil), s.Aliases...)
	s.ProficiencyScale = append([]ProficiencyLevel(nil), s.ProficiencyScale...)
	s.CanonicalDigest = ""
	if err := s.Validate(); err != nil {
		return SkillDefinitionRevision{}, err
	}
	s.CanonicalDigest = canonicalbytes.Digest(s.canonicalBody())
	return s, nil
}

// SkillOntologyRevision is an immutable, cycle-free graph of skill nodes.
type SkillOntologyRevision struct {
	OntologyID      values.EntityRef
	Revision        values.RevisionToken
	Skills          []SkillDefinitionRevision
	CanonicalDigest string
}

// SkillOntology is a concise alias for the ontology aggregate.
type SkillOntology = SkillOntologyRevision

func (o SkillOntologyRevision) Validate() error {
	if err := o.OntologyID.Validate(); err != nil {
		return fmt.Errorf("%w: ontology_id: %v", ErrInvalidOntology, err)
	}
	if o.OntologyID.Kind != values.Kind("skill_ontology") {
		return fmt.Errorf("%w: ontology_id kind %q is not skill_ontology", ErrInvalidOntology, o.OntologyID.Kind)
	}
	if !o.Revision.IsSpecified() {
		return fmt.Errorf("%w: revision is required", ErrInvalidOntology)
	}
	if len(o.Skills) == 0 {
		return fmt.Errorf("%w: skills are required", ErrInvalidOntology)
	}
	byRef := make(map[string]SkillDefinitionRevision, len(o.Skills))
	for i, s := range o.Skills {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("%w: skills[%d]: %v", ErrInvalidOntology, i, err)
		}
		if s.ref().Tenant != o.OntologyID.Tenant {
			return fmt.Errorf("%w: skills[%d] tenant differs", ErrInvalidOntology, i)
		}
		key := s.ref().String()
		if _, ok := byRef[key]; ok {
			return fmt.Errorf("%w: skills[%d] duplicates %s", ErrInvalidOntology, i, key)
		}
		byRef[key] = s
	}
	state := make(map[string]uint8, len(byRef))
	var visit func(string) error
	visit = func(key string) error {
		switch state[key] {
		case 1:
			return fmt.Errorf("%w: parent graph cycle at %s", ErrInvalidOntology, key)
		case 2:
			return nil
		}
		state[key] = 1
		for _, parent := range byRef[key].ParentRefs {
			parentKey := parent.String()
			if _, ok := byRef[parentKey]; !ok {
				return fmt.Errorf("%w: parent_refs contains unknown skill %s", ErrInvalidOntology, parentKey)
			}
			if err := visit(parentKey); err != nil {
				return err
			}
		}
		state[key] = 2
		return nil
	}
	for key := range byRef {
		if err := visit(key); err != nil {
			return err
		}
	}
	if o.CanonicalDigest != "" && o.CanonicalDigest != o.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidOntology)
	}
	return nil
}

func (o SkillOntologyRevision) canonicalBody() []byte {
	if err := o.validateWithoutDigest(); err != nil {
		return nil
	}
	skills := append([]SkillDefinitionRevision(nil), o.Skills...)
	sort.Slice(skills, func(i, j int) bool { return skills[i].ref().String() < skills[j].ref().String() })
	w := canonicalbytes.New("hcmnext.domains.skill.SkillOntologyRevision", schemaVersion).
		Value("ontology_id", o.OntologyID).Value("revision", o.Revision).Count("skills", len(skills))
	for _, s := range skills {
		w.Field("skill", s.canonicalBody())
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
func (o SkillOntologyRevision) computedDigest() string {
	return canonicalbytes.Digest(o.canonicalBody())
}
func (o SkillOntologyRevision) Canonical() []byte {
	if o.Validate() != nil {
		return nil
	}
	return o.canonicalBody()
}
func (o SkillOntologyRevision) Digest() (string, error) {
	if err := o.Validate(); err != nil {
		return "", err
	}
	return o.computedDigest(), nil
}

func (o SkillOntologyRevision) validateWithoutDigest() error {
	digest := o.CanonicalDigest
	o.CanonicalDigest = ""
	if digest == "" {
		return o.Validate()
	}
	return o.Validate()
}

func NewSkillOntology(o SkillOntologyRevision) (SkillOntologyRevision, error) {
	o.Skills = append([]SkillDefinitionRevision(nil), o.Skills...)
	o.CanonicalDigest = ""
	if err := o.Validate(); err != nil {
		return SkillOntologyRevision{}, err
	}
	o.CanonicalDigest = canonicalbytes.Digest(o.canonicalBody())
	return o, nil
}

// EvidenceKind is a closed set. A self assertion can be retained but cannot
// become VERIFIED merely because its record says so.
type EvidenceKind string

const (
	EvidenceCredential EvidenceKind = "CREDENTIAL"
	EvidenceTraining   EvidenceKind = "TRAINING"
	EvidenceAssessment EvidenceKind = "ASSESSMENT"
	EvidenceSelfReport EvidenceKind = "SELF_REPORT"
)

func (k EvidenceKind) Valid() bool {
	return k == EvidenceCredential || k == EvidenceTraining || k == EvidenceAssessment || k == EvidenceSelfReport
}

type EvidenceStatus string

const (
	StatusVerified EvidenceStatus = "VERIFIED"
	StatusAsserted EvidenceStatus = "ASSERTED"
	StatusExpired  EvidenceStatus = "EXPIRED"
	StatusDisputed EvidenceStatus = "DISPUTED"
	StatusUnknown  EvidenceStatus = "UNKNOWN"
)

func (s EvidenceStatus) Valid() bool {
	return s == StatusVerified || s == StatusAsserted || s == StatusExpired || s == StatusDisputed || s == StatusUnknown
}

// WorkerSkillEvidence is an immutable, reference-only proficiency assertion.
// Effective's exclusive end is the expiry boundary.
type WorkerSkillEvidence struct {
	EvidenceID      values.EntityRef
	Worker          values.EntityRef
	SkillRef        values.EntityRef
	Level           int
	Proficiency     ProficiencyLevel
	EvidenceKind    EvidenceKind
	EvidenceRef     string
	Verified        bool
	Disputed        bool
	Effective       values.EffectiveInterval
	CanonicalDigest string
}

type ProficiencyAssertion = WorkerSkillEvidence

func (e WorkerSkillEvidence) Validate() error {
	if err := e.EvidenceID.Validate(); err != nil {
		return fmt.Errorf("%w: evidence_id: %v", ErrInvalidEvidence, err)
	}
	if e.EvidenceID.Kind != values.Kind("skill_evidence") {
		return fmt.Errorf("%w: evidence_id kind %q is not skill_evidence", ErrInvalidEvidence, e.EvidenceID.Kind)
	}
	if err := e.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: worker: %v", ErrInvalidEvidence, err)
	}
	if e.Worker.Kind != KindWorker {
		return fmt.Errorf("%w: worker kind %q is not worker", ErrInvalidEvidence, e.Worker.Kind)
	}
	if err := e.SkillRef.Validate(); err != nil {
		return fmt.Errorf("%w: skill_ref: %v", ErrInvalidEvidence, err)
	}
	if e.SkillRef.Kind != KindSkill {
		return fmt.Errorf("%w: skill_ref kind %q is not skill", ErrInvalidEvidence, e.SkillRef.Kind)
	}
	if e.Worker.Tenant != e.SkillRef.Tenant || e.Worker.Tenant != e.EvidenceID.Tenant {
		return fmt.Errorf("%w: worker, skill_ref and evidence_id tenants differ", ErrInvalidEvidence)
	}
	if e.Level <= 0 && e.Proficiency == "" {
		return fmt.Errorf("%w: level or proficiency is required", ErrInvalidEvidence)
	}
	if e.Level < 0 {
		return fmt.Errorf("%w: level must not be negative", ErrInvalidEvidence)
	}
	if e.Proficiency != "" && !e.Proficiency.Valid() {
		return fmt.Errorf("%w: proficiency %q is not declared", ErrInvalidEvidence, e.Proficiency)
	}
	if strings.TrimSpace(e.EvidenceRef) == "" {
		return fmt.Errorf("%w: evidence_ref is required", ErrInvalidEvidence)
	}
	if !e.EvidenceKind.Valid() {
		return fmt.Errorf("%w: evidence_kind %q is not declared", ErrInvalidEvidence, e.EvidenceKind)
	}
	if err := e.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrInvalidEvidence, err)
	}
	if e.Effective.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: effective must be LOCAL_DATE", ErrInvalidEvidence)
	}
	if e.CanonicalDigest != "" && e.CanonicalDigest != e.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidEvidence)
	}
	return nil
}

func (e WorkerSkillEvidence) effectiveLevel(scale []ProficiencyLevel) (int, error) {
	if e.Level > 0 && e.Proficiency != "" && levelRank(scale, e.Proficiency) != e.Level {
		return 0, fmt.Errorf("%w: level and proficiency disagree", ErrInvalidEvidence)
	}
	if e.Level > 0 {
		if e.Level > len(scaleOrDefault(scale)) {
			return 0, fmt.Errorf("%w: level %d is outside scale", ErrInvalidEvidence, e.Level)
		}
		return e.Level, nil
	}
	rank := levelRank(scale, e.Proficiency)
	if rank == 0 {
		return 0, fmt.Errorf("%w: proficiency is outside scale", ErrInvalidEvidence)
	}
	return rank, nil
}

func (e WorkerSkillEvidence) canonicalBody() []byte {
	if err := e.validateWithoutDigest(); err != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.skill.WorkerSkillEvidence", schemaVersion).
		Value("evidence_id", e.EvidenceID).Value("worker", e.Worker).Value("skill_ref", e.SkillRef).
		Int("level", int64(e.Level)).String("proficiency", string(e.Proficiency)).String("evidence_kind", string(e.EvidenceKind)).
		String("evidence_ref", e.EvidenceRef).Bool("verified", e.Verified).Bool("disputed", e.Disputed).Value("effective", e.Effective)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
func (e WorkerSkillEvidence) validateWithoutDigest() error {
	e.CanonicalDigest = ""
	return e.Validate()
}
func (e WorkerSkillEvidence) computedDigest() string { return canonicalbytes.Digest(e.canonicalBody()) }
func (e WorkerSkillEvidence) Canonical() []byte {
	if e.Validate() != nil {
		return nil
	}
	return e.canonicalBody()
}
func NewWorkerSkillEvidence(e WorkerSkillEvidence) (WorkerSkillEvidence, error) {
	e.CanonicalDigest = ""
	if err := e.Validate(); err != nil {
		return WorkerSkillEvidence{}, err
	}
	e.CanonicalDigest = canonicalbytes.Digest(e.canonicalBody())
	return e, nil
}

// EquivalenceRule is a reviewed, directional mapping. It never permits an
// unverified assertion to become verified and cycles are rejected by the
// ontology-level validator.
type EquivalenceRule struct {
	RuleID          values.EntityRef
	Revision        values.RevisionToken
	SourceSkill     values.EntityRef
	TargetSkill     values.EntityRef
	SourceLevel     int
	TargetLevel     int
	Approved        bool
	EvidenceRef     string
	Effective       values.EffectiveInterval
	Supersedes      values.RevisionToken
	CanonicalDigest string
}

func (r EquivalenceRule) Validate() error {
	if err := r.RuleID.Validate(); err != nil {
		return fmt.Errorf("%w: rule_id: %v", ErrInvalidEquivalence, err)
	}
	if r.RuleID.Kind != values.Kind("skill_equivalence") {
		return fmt.Errorf("%w: rule_id kind %q is not skill_equivalence", ErrInvalidEquivalence, r.RuleID.Kind)
	}
	if !r.Revision.IsSpecified() {
		return fmt.Errorf("%w: revision is required", ErrInvalidEquivalence)
	}
	if err := r.Revision.Validate(); err != nil {
		return fmt.Errorf("%w: revision: %v", ErrInvalidEquivalence, err)
	}
	for name, ref := range map[string]values.EntityRef{"source_skill": r.SourceSkill, "target_skill": r.TargetSkill} {
		if err := ref.Validate(); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidEquivalence, name, err)
		}
		if ref.Kind != KindSkill {
			return fmt.Errorf("%w: %s kind %q is not skill", ErrInvalidEquivalence, name, ref.Kind)
		}
		if ref.Tenant != r.RuleID.Tenant {
			return fmt.Errorf("%w: %s tenant differs", ErrInvalidEquivalence, name)
		}
	}
	if r.SourceSkill == r.TargetSkill {
		return fmt.Errorf("%w: source_skill and target_skill cannot be equal", ErrInvalidEquivalence)
	}
	if r.SourceLevel <= 0 || r.TargetLevel <= 0 {
		return fmt.Errorf("%w: source_level and target_level must be positive", ErrInvalidEquivalence)
	}
	if !r.Approved {
		return fmt.Errorf("%w: approved is required", ErrInvalidEquivalence)
	}
	if strings.TrimSpace(r.EvidenceRef) == "" {
		return fmt.Errorf("%w: evidence_ref is required", ErrInvalidEquivalence)
	}
	if err := r.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrInvalidEquivalence, err)
	}
	if r.Effective.Kind() != values.IntervalKindLocalDate {
		return fmt.Errorf("%w: effective must be LOCAL_DATE", ErrInvalidEquivalence)
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidEquivalence)
	}
	return nil
}
func (r EquivalenceRule) canonicalBody() []byte {
	if err := r.validateWithoutDigest(); err != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.skill.EquivalenceRule", schemaVersion).
		Value("rule_id", r.RuleID).Value("revision", r.Revision).Value("source_skill", r.SourceSkill).Value("target_skill", r.TargetSkill).
		Int("source_level", int64(r.SourceLevel)).Int("target_level", int64(r.TargetLevel)).Bool("approved", r.Approved).
		String("evidence_ref", r.EvidenceRef).Value("effective", r.Effective).Value("supersedes", r.Supersedes)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
func (r EquivalenceRule) validateWithoutDigest() error { r.CanonicalDigest = ""; return r.Validate() }
func (r EquivalenceRule) computedDigest() string       { return canonicalbytes.Digest(r.canonicalBody()) }
func (r EquivalenceRule) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.canonicalBody()
}
func NewEquivalenceRule(r EquivalenceRule) (EquivalenceRule, error) {
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return EquivalenceRule{}, err
	}
	r.CanonicalDigest = canonicalbytes.Digest(r.canonicalBody())
	return r, nil
}

func validateEquivalences(ontology SkillOntologyRevision, rules []EquivalenceRule) error {
	known := make(map[string]struct{}, len(ontology.Skills))
	for _, s := range ontology.Skills {
		known[s.ref().String()] = struct{}{}
	}
	graph := make(map[string][]string)
	for i, r := range rules {
		if err := r.Validate(); err != nil {
			return fmt.Errorf("%w: rules[%d]: %v", ErrInvalidOntology, i, err)
		}
		if _, ok := known[r.SourceSkill.String()]; !ok {
			return fmt.Errorf("%w: rules[%d] source_skill is not in ontology", ErrInvalidOntology, i)
		}
		if _, ok := known[r.TargetSkill.String()]; !ok {
			return fmt.Errorf("%w: rules[%d] target_skill is not in ontology", ErrInvalidOntology, i)
		}
		graph[r.SourceSkill.String()] = append(graph[r.SourceSkill.String()], r.TargetSkill.String())
	}
	state := map[string]uint8{}
	var visit func(string) error
	visit = func(node string) error {
		if state[node] == 1 {
			return fmt.Errorf("%w: equivalence graph cycle at %s", ErrInvalidOntology, node)
		}
		if state[node] == 2 {
			return nil
		}
		state[node] = 1
		for _, next := range graph[node] {
			if err := visit(next); err != nil {
				return err
			}
		}
		state[node] = 2
		return nil
	}
	for node := range graph {
		if err := visit(node); err != nil {
			return err
		}
	}
	return nil
}

// Explanation is intentionally aggregate-only and does not echo evidence
// references, aliases, or other caller-supplied sensitive strings.
type Explanation struct {
	Kind         string
	StatusCounts map[EvidenceStatus]int
	Digest       string
}

// Explain returns a stable, audit-safe summary for any public skill value.
func Explain(v any) string {
	switch x := v.(type) {
	case SkillOntologyRevision:
		return fmt.Sprintf("skill ontology: %d definitions, digest %s", len(x.Skills), x.CanonicalDigest)
	case WorkerSkillEvidence:
		return "worker skill evidence: reference-only assertion; evidence content withheld"
	case Resolution:
		return fmt.Sprintf("skill resolution: %d proficiency outcomes, digest %s", len(x.Proficiencies), x.CanonicalDigest)
	default:
		return "skill explanation: unsupported value"
	}
}
