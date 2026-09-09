package career

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/skill"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const careerSchemaVersion = 1

// Version reports the additive career vocabulary version.
func Version() int { return careerSchemaVersion }

// MobilityWillingness is a closed worker-authored preference, not a manager
// assessment or a model prediction.
type MobilityWillingness string

const (
	MobilityUnspecified   MobilityWillingness = "UNSPECIFIED"
	MobilityNotWilling    MobilityWillingness = "NOT_WILLING"
	MobilityWithinCountry MobilityWillingness = "WITHIN_COUNTRY"
	MobilityInternational MobilityWillingness = "INTERNATIONAL"
	MobilityAny           MobilityWillingness = "ANY"
)

func (m MobilityWillingness) Valid() bool {
	return m == MobilityNotWilling || m == MobilityWithinCountry || m == MobilityInternational || m == MobilityAny
}

// CareerPreferenceProfileRevision captures worker-owned mobility, role and
// timeframe choices. A new value supersedes an old one; it never mutates it.
type CareerPreferenceProfileRevision struct {
	PreferenceID    values.EntityRef
	Revision        values.RevisionToken
	Supersedes      values.RevisionToken
	Worker          values.EntityRef
	Mobility        MobilityWillingness
	TargetRoleRefs  []values.EntityRef
	Timeframe       values.EffectiveInterval
	Visibility      PreferenceVisibility
	CanonicalDigest string
}

type CareerPreferencesRevision = CareerPreferenceProfileRevision
type CareerPreferences = CareerPreferenceProfileRevision

func (p CareerPreferenceProfileRevision) Validate() error {
	if err := requireRef(p.PreferenceID, "preference", "career_preference"); err != nil {
		return err
	}
	if err := requireRevision(p.Revision, "preference"); err != nil {
		return err
	}
	if err := requireRef(p.Worker, "worker", "worker"); err != nil {
		return err
	}
	if p.PreferenceID.Tenant != p.Worker.Tenant {
		return fmt.Errorf("%w: preference and worker tenants differ", ErrInvalidReference)
	}
	if !p.Mobility.Valid() {
		return fmt.Errorf("%w: mobility is invalid", ErrInvalidRevision)
	}
	if len(p.TargetRoleRefs) == 0 {
		return fmt.Errorf("%w: target_role_refs is required", ErrInvalidRevision)
	}
	seen := map[string]struct{}{}
	for i, role := range p.TargetRoleRefs {
		if err := role.Validate(); err != nil {
			return fmt.Errorf("%w: target_role_refs[%d]: %v", ErrInvalidReference, i, err)
		}
		if role.Kind != values.Kind("career_target_role") && role.Kind != values.Kind("job_profile") {
			return fmt.Errorf("%w: target_role_refs[%d] kind %q is not a target role", ErrInvalidReference, i, role.Kind)
		}
		if role.Tenant != p.Worker.Tenant {
			return fmt.Errorf("%w: target_role_refs[%d] tenant differs", ErrInvalidReference, i)
		}
		if _, ok := seen[role.String()]; ok {
			return fmt.Errorf("%w: target_role_refs[%d] is duplicated", ErrInvalidRevision, i)
		}
		seen[role.String()] = struct{}{}
	}
	if err := p.Timeframe.Validate(); err != nil {
		return fmt.Errorf("%w: timeframe: %v", ErrInvalidRevision, err)
	}
	if p.Visibility != VisibilityWorkerOnly && p.Visibility != VisibilityWorkerAndAuthorized {
		return fmt.Errorf("%w: visibility is invalid", ErrInvalidRevision)
	}
	if p.Supersedes.IsSpecified() {
		if err := p.Supersedes.Validate(); err != nil {
			return fmt.Errorf("%w: supersedes: %v", ErrInvalidRevision, err)
		}
		if p.Supersedes.Equal(p.Revision) {
			return fmt.Errorf("%w: supersedes cannot equal revision", ErrInvalidRevision)
		}
	}
	if p.CanonicalDigest != "" && p.CanonicalDigest != p.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidRevision)
	}
	return nil
}
func (p CareerPreferenceProfileRevision) canonicalBody() []byte {
	copy := p
	copy.CanonicalDigest = ""
	if err := copy.Validate(); err != nil {
		return nil
	}
	roles := append([]values.EntityRef(nil), p.TargetRoleRefs...)
	sort.Slice(roles, func(i, j int) bool { return roles[i].String() < roles[j].String() })
	w := canonicalbytes.New("hcmnext.domains.career.CareerPreferenceProfileRevision", careerSchemaVersion).
		Value("preference_id", p.PreferenceID).Value("revision", p.Revision).Value("supersedes", p.Supersedes).Value("worker", p.Worker).
		String("mobility", string(p.Mobility)).Count("target_role_refs", len(roles))
	for _, role := range roles {
		w.Value("target_role_ref", role)
	}
	w.Value("timeframe", p.Timeframe).String("visibility", string(p.Visibility))
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
func (p CareerPreferenceProfileRevision) computedDigest() string {
	return canonicalbytes.Digest(p.canonicalBody())
}
func (p CareerPreferenceProfileRevision) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	return p.canonicalBody()
}
func (p CareerPreferenceProfileRevision) Digest() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	return p.computedDigest(), nil
}
func NewCareerPreference(p CareerPreferenceProfileRevision) (CareerPreferenceProfileRevision, error) {
	p.TargetRoleRefs = append([]values.EntityRef(nil), p.TargetRoleRefs...)
	p.CanonicalDigest = ""
	if err := p.Validate(); err != nil {
		return CareerPreferenceProfileRevision{}, err
	}
	p.CanonicalDigest = canonicalbytes.Digest(p.canonicalBody())
	return p, nil
}

// NewCareerPreferenceRevision is the explicit revision-named constructor.
func NewCareerPreferenceRevision(p CareerPreferenceProfileRevision) (CareerPreferenceProfileRevision, error) {
	return NewCareerPreference(p)
}

// RoleSkillRequirement binds a target role to one minimum skill level.
type RoleSkillRequirement struct {
	SkillRef     values.EntityRef
	MinimumLevel int
}

func (r RoleSkillRequirement) Validate(tenant values.TenantId) error {
	if err := r.SkillRef.Validate(); err != nil {
		return fmt.Errorf("%w: skill_ref: %v", ErrInvalidReference, err)
	}
	if r.SkillRef.Kind != skill.KindSkill {
		return fmt.Errorf("%w: skill_ref kind %q is not skill", ErrInvalidReference, r.SkillRef.Kind)
	}
	if r.SkillRef.Tenant != tenant {
		return fmt.Errorf("%w: skill_ref tenant differs", ErrInvalidReference)
	}
	if r.MinimumLevel <= 0 {
		return fmt.Errorf("%w: minimum_level must be positive", ErrInvalidRevision)
	}
	return nil
}

// TargetRoleProfileRevision is a target role plus the pinned requirements used
// to compute readiness. The computation is descriptive and has no hiring or
// assignment authority.
type TargetRoleProfileRevision struct {
	TargetRoleID       values.EntityRef
	Revision           values.RevisionToken
	Supersedes         values.RevisionToken
	Worker             values.EntityRef
	JobProfile         values.EntityRef
	JobProfileRevision values.RevisionToken
	Requirements       []RoleSkillRequirement
	Visibility         PreferenceVisibility
	Effective          values.EffectiveInterval
	CanonicalDigest    string
}

func (r TargetRoleProfileRevision) Validate() error {
	if err := requireRef(r.TargetRoleID, "target role", "career_target_role"); err != nil {
		return err
	}
	if err := requireRevision(r.Revision, "target role"); err != nil {
		return err
	}
	if err := requireRef(r.Worker, "worker", "worker"); err != nil {
		return err
	}
	if err := requireRef(r.JobProfile, "job profile", "job_profile"); err != nil {
		return err
	}
	if r.TargetRoleID.Tenant != r.Worker.Tenant || r.Worker.Tenant != r.JobProfile.Tenant {
		return fmt.Errorf("%w: target references have different tenants", ErrInvalidReference)
	}
	if !r.JobProfileRevision.IsSpecified() {
		return fmt.Errorf("%w: job_profile_revision is required", ErrInvalidRevision)
	}
	if len(r.Requirements) == 0 {
		return fmt.Errorf("%w: requirements is required", ErrInvalidRevision)
	}
	seen := map[string]struct{}{}
	for i, requirement := range r.Requirements {
		if err := requirement.Validate(r.Worker.Tenant); err != nil {
			return fmt.Errorf("%w: requirements[%d]: %v", ErrInvalidRevision, i, err)
		}
		key := requirement.SkillRef.String()
		if _, ok := seen[key]; ok {
			return fmt.Errorf("%w: requirements[%d] is duplicated", ErrInvalidRevision, i)
		}
		seen[key] = struct{}{}
	}
	if r.Visibility != VisibilityWorkerOnly && r.Visibility != VisibilityWorkerAndAuthorized {
		return fmt.Errorf("%w: visibility is invalid", ErrInvalidRevision)
	}
	if err := r.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrInvalidRevision, err)
	}
	if r.Supersedes.IsSpecified() {
		if err := r.Supersedes.Validate(); err != nil {
			return fmt.Errorf("%w: supersedes: %v", ErrInvalidRevision, err)
		}
		if r.Supersedes.Equal(r.Revision) {
			return fmt.Errorf("%w: supersedes cannot equal revision", ErrInvalidRevision)
		}
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidRevision)
	}
	return nil
}
func (r TargetRoleProfileRevision) canonicalBody() []byte {
	copy := r
	copy.CanonicalDigest = ""
	if copy.Validate() != nil {
		return nil
	}
	requirements := append([]RoleSkillRequirement(nil), r.Requirements...)
	sort.Slice(requirements, func(i, j int) bool { return requirements[i].SkillRef.String() < requirements[j].SkillRef.String() })
	w := canonicalbytes.New("hcmnext.domains.career.TargetRoleProfileRevision", careerSchemaVersion).Value("target_role_id", r.TargetRoleID).Value("revision", r.Revision).Value("supersedes", r.Supersedes).Value("worker", r.Worker).Value("job_profile", r.JobProfile).Value("job_profile_revision", r.JobProfileRevision).Count("requirements", len(requirements))
	for _, requirement := range requirements {
		w.Value("skill_ref", requirement.SkillRef).Int("minimum_level", int64(requirement.MinimumLevel))
	}
	w.String("visibility", string(r.Visibility)).Value("effective", r.Effective)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
func (r TargetRoleProfileRevision) computedDigest() string {
	return canonicalbytes.Digest(r.canonicalBody())
}
func (r TargetRoleProfileRevision) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.canonicalBody()
}
func NewTargetRoleProfile(r TargetRoleProfileRevision) (TargetRoleProfileRevision, error) {
	r.Requirements = append([]RoleSkillRequirement(nil), r.Requirements...)
	r.CanonicalDigest = ""
	if err := r.Validate(); err != nil {
		return TargetRoleProfileRevision{}, err
	}
	r.CanonicalDigest = canonicalbytes.Digest(r.canonicalBody())
	return r, nil
}

// NewTargetRoleProfileRevision is the explicit revision-named constructor.
func NewTargetRoleProfileRevision(r TargetRoleProfileRevision) (TargetRoleProfileRevision, error) {
	return NewTargetRoleProfile(r)
}

// ReadinessStatus keeps the semantic result separate from employment or
// promotion decisions.
type ReadinessStatus string

const (
	ReadinessReady    ReadinessStatus = "READY"
	ReadinessNeedsGap ReadinessStatus = "GAP"
	ReadinessUnknown  ReadinessStatus = "UNKNOWN"
)

type ReadinessGap struct {
	SkillRef       values.EntityRef
	RequiredLevel  int
	EffectiveLevel int
	Status         skill.EvidenceStatus
	Gap            int
}
type ReadinessResult struct {
	Worker             values.EntityRef
	TargetRole         values.EntityRef
	JobProfileRevision values.RevisionToken
	Status             ReadinessStatus
	Gaps               []ReadinessGap
	CanonicalDigest    string
}

type TargetRoleReadiness = ReadinessResult

func (r ReadinessResult) Validate() error {
	if err := r.Worker.Validate(); err != nil {
		return fmt.Errorf("%w: readiness worker: %v", ErrInvalidRevision, err)
	}
	if err := r.TargetRole.Validate(); err != nil {
		return fmt.Errorf("%w: readiness target_role: %v", ErrInvalidRevision, err)
	}
	if !r.JobProfileRevision.IsSpecified() {
		return fmt.Errorf("%w: readiness job_profile_revision is required", ErrInvalidRevision)
	}
	if err := r.JobProfileRevision.Validate(); err != nil {
		return fmt.Errorf("%w: readiness job_profile_revision: %v", ErrInvalidRevision, err)
	}
	if !r.Status.Valid() {
		return fmt.Errorf("%w: readiness status is invalid", ErrInvalidRevision)
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return fmt.Errorf("%w: readiness canonical_digest mismatch", ErrInvalidRevision)
	}
	return nil
}
func (s ReadinessStatus) Valid() bool {
	return s == ReadinessReady || s == ReadinessNeedsGap || s == ReadinessUnknown
}
func (r ReadinessResult) canonicalBody() []byte {
	copy := r
	copy.CanonicalDigest = ""
	if copy.Validate() != nil {
		return nil
	}
	gaps := append([]ReadinessGap(nil), r.Gaps...)
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].SkillRef.String() < gaps[j].SkillRef.String() })
	w := canonicalbytes.New("hcmnext.domains.career.ReadinessResult", careerSchemaVersion).Value("worker", r.Worker).Value("target_role", r.TargetRole).Value("job_profile_revision", r.JobProfileRevision).String("status", string(r.Status)).Count("gaps", len(gaps))
	for _, gap := range gaps {
		w.Value("skill_ref", gap.SkillRef).Int("required_level", int64(gap.RequiredLevel)).Int("effective_level", int64(gap.EffectiveLevel)).String("evidence_status", string(gap.Status)).Int("gap", int64(gap.Gap))
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
func (r ReadinessResult) computedDigest() string { return canonicalbytes.Digest(r.canonicalBody()) }
func (r ReadinessResult) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.canonicalBody()
}

// SkillResolverPort keeps career dependent on the skill read contract rather
// than on a concrete resolver implementation.
type SkillResolverPort interface {
	Resolve(context.Context, skill.ResolveRequest) (skill.Resolution, error)
}
type ProficiencyResolver = SkillResolverPort

func ComputeReadinessGaps(ctx context.Context, resolver SkillResolverPort, role TargetRoleProfileRevision, asOf values.LocalDate) (ReadinessResult, error) {
	if err := role.Validate(); err != nil {
		return ReadinessResult{}, err
	}
	if resolver == nil {
		return ReadinessResult{}, fmt.Errorf("%w: skill resolver is required", ErrInvalidRevision)
	}
	refs := make([]values.EntityRef, 0, len(role.Requirements))
	for _, requirement := range role.Requirements {
		refs = append(refs, requirement.SkillRef)
	}
	resolved, err := resolver.Resolve(ctx, skill.ResolveRequest{Worker: role.Worker, AsOf: asOf, SkillRefs: refs, Ontology: skill.SkillOntologyRevision{}})
	if err != nil {
		// The resolver normally needs the ontology pinned by its composition root.
		// A port implementation may accept the request and fill that context;
		// otherwise the error is returned without guessing readiness.
		return ReadinessResult{}, err
	}
	bySkill := make(map[string]skill.ProficiencyResult, len(resolved.Proficiencies))
	for _, item := range resolved.Proficiencies {
		bySkill[item.SkillRef.String()] = item
	}
	out := ReadinessResult{Worker: role.Worker, TargetRole: role.TargetRoleID, JobProfileRevision: role.JobProfileRevision, Status: ReadinessReady}
	for _, requirement := range role.Requirements {
		item, ok := bySkill[requirement.SkillRef.String()]
		gap := requirement.MinimumLevel
		status := skill.StatusUnknown
		effective := 0
		if ok {
			effective, status = item.Level, item.Status
			gap = requirement.MinimumLevel - effective
			if gap < 0 {
				gap = 0
			}
		}
		entry := ReadinessGap{SkillRef: requirement.SkillRef, RequiredLevel: requirement.MinimumLevel, EffectiveLevel: effective, Status: status, Gap: gap}
		out.Gaps = append(out.Gaps, entry)
		if !ok || status != skill.StatusVerified || gap > 0 {
			if status == skill.StatusUnknown {
				out.Status = ReadinessUnknown
			} else if out.Status != ReadinessUnknown {
				out.Status = ReadinessNeedsGap
			}
		}
	}
	out.CanonicalDigest = out.computedDigest()
	return out, nil
}

// ComputeReadiness is a concise alias for ComputeReadinessGaps.
func ComputeReadiness(ctx context.Context, resolver SkillResolverPort, role TargetRoleProfileRevision, asOf values.LocalDate) (ReadinessResult, error) {
	return ComputeReadinessGaps(ctx, resolver, role, asOf)
}

// ObjectiveState is a closed lifecycle for development objectives.
type ObjectiveState string

const (
	ObjectivePlanned    ObjectiveState = "PLANNED"
	ObjectiveInProgress ObjectiveState = "IN_PROGRESS"
	ObjectiveComplete   ObjectiveState = "COMPLETE"
	ObjectiveCancelled  ObjectiveState = "CANCELLED"
)

func (s ObjectiveState) Valid() bool {
	return s == ObjectivePlanned || s == ObjectiveInProgress || s == ObjectiveComplete || s == ObjectiveCancelled
}

// DevelopmentObjectiveProfileRevision is an append-only objective revision.
// Completion is valid only with at least one evidence reference.
type DevelopmentObjectiveProfileRevision struct {
	ObjectiveID            values.EntityRef
	Revision               values.RevisionToken
	Supersedes             values.RevisionToken
	Worker                 values.EntityRef
	TargetRole             values.EntityRef
	SkillRefs              []values.EntityRef
	Description            string
	Owner                  values.EntityRef
	State                  ObjectiveState
	CompletionEvidenceRefs []string
	Visibility             PreferenceVisibility
	Effective              values.EffectiveInterval
	CanonicalDigest        string
}
type ObjectiveRevision = DevelopmentObjectiveProfileRevision

func (o DevelopmentObjectiveProfileRevision) Validate() error {
	if err := requireRef(o.ObjectiveID, "objective", "development_objective"); err != nil {
		return err
	}
	if err := requireRevision(o.Revision, "objective"); err != nil {
		return err
	}
	if err := requireRef(o.Worker, "worker", "worker"); err != nil {
		return err
	}
	if err := requireRef(o.TargetRole, "target role", "career_target_role"); err != nil {
		return err
	}
	if err := requireRef(o.Owner, "owner", "career_owner"); err != nil {
		return err
	}
	if o.ObjectiveID.Tenant != o.Worker.Tenant || o.Worker.Tenant != o.TargetRole.Tenant || o.Worker.Tenant != o.Owner.Tenant {
		return fmt.Errorf("%w: objective references have different tenants", ErrInvalidReference)
	}
	if strings.TrimSpace(o.Description) == "" {
		return fmt.Errorf("%w: description is required", ErrInvalidRevision)
	}
	if len(o.SkillRefs) == 0 {
		return fmt.Errorf("%w: skill_refs is required", ErrInvalidRevision)
	}
	for i, ref := range o.SkillRefs {
		if err := requireRef(ref, fmt.Sprintf("skill_refs[%d]", i), "skill"); err != nil {
			return err
		}
		if ref.Tenant != o.Worker.Tenant {
			return fmt.Errorf("%w: skill_refs[%d] tenant differs", ErrInvalidReference, i)
		}
	}
	if !o.State.Valid() {
		return fmt.Errorf("%w: state is invalid", ErrInvalidRevision)
	}
	if o.State == ObjectiveComplete && len(o.CompletionEvidenceRefs) == 0 {
		return fmt.Errorf("%w: completion_evidence_refs is required when complete", ErrInvalidRevision)
	}
	for i, ref := range o.CompletionEvidenceRefs {
		if strings.TrimSpace(ref) == "" {
			return fmt.Errorf("%w: completion_evidence_refs[%d] is empty", ErrInvalidRevision, i)
		}
	}
	if o.Visibility != VisibilityWorkerOnly && o.Visibility != VisibilityWorkerAndAuthorized {
		return fmt.Errorf("%w: visibility is invalid", ErrInvalidRevision)
	}
	if err := o.Effective.Validate(); err != nil {
		return fmt.Errorf("%w: effective: %v", ErrInvalidRevision, err)
	}
	if o.Supersedes.IsSpecified() {
		if err := o.Supersedes.Validate(); err != nil {
			return fmt.Errorf("%w: supersedes: %v", ErrInvalidRevision, err)
		}
		if o.Supersedes.Equal(o.Revision) {
			return fmt.Errorf("%w: supersedes cannot equal revision", ErrInvalidRevision)
		}
	}
	if o.CanonicalDigest != "" && o.CanonicalDigest != o.computedDigest() {
		return fmt.Errorf("%w: canonical_digest mismatch", ErrInvalidRevision)
	}
	return nil
}
func (o DevelopmentObjectiveProfileRevision) canonicalBody() []byte {
	copy := o
	copy.CanonicalDigest = ""
	if copy.Validate() != nil {
		return nil
	}
	skills := append([]values.EntityRef(nil), o.SkillRefs...)
	sort.Slice(skills, func(i, j int) bool { return skills[i].String() < skills[j].String() })
	evidence := append([]string(nil), o.CompletionEvidenceRefs...)
	sort.Strings(evidence)
	w := canonicalbytes.New("hcmnext.domains.career.DevelopmentObjectiveProfileRevision", careerSchemaVersion).Value("objective_id", o.ObjectiveID).Value("revision", o.Revision).Value("supersedes", o.Supersedes).Value("worker", o.Worker).Value("target_role", o.TargetRole).Count("skills", len(skills))
	for _, ref := range skills {
		w.Value("skill_ref", ref)
	}
	w.String("description", o.Description).Value("owner", o.Owner).String("state", string(o.State)).Count("completion_evidence_refs", len(evidence))
	for _, ref := range evidence {
		w.String("completion_evidence_ref", ref)
	}
	w.String("visibility", string(o.Visibility)).Value("effective", o.Effective)
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}
func (o DevelopmentObjectiveProfileRevision) computedDigest() string {
	return canonicalbytes.Digest(o.canonicalBody())
}
func (o DevelopmentObjectiveProfileRevision) Canonical() []byte {
	if o.Validate() != nil {
		return nil
	}
	return o.canonicalBody()
}
func NewDevelopmentObjective(o DevelopmentObjectiveProfileRevision) (DevelopmentObjectiveProfileRevision, error) {
	o.SkillRefs = append([]values.EntityRef(nil), o.SkillRefs...)
	o.CompletionEvidenceRefs = append([]string(nil), o.CompletionEvidenceRefs...)
	o.CanonicalDigest = ""
	if err := o.Validate(); err != nil {
		return DevelopmentObjectiveProfileRevision{}, err
	}
	o.CanonicalDigest = canonicalbytes.Digest(o.canonicalBody())
	return o, nil
}

// NewDevelopmentObjectiveRevision is the explicit revision-named constructor.
func NewDevelopmentObjectiveRevision(o DevelopmentObjectiveProfileRevision) (DevelopmentObjectiveProfileRevision, error) {
	return NewDevelopmentObjective(o)
}

// Explain is aggregate-only; it never echoes mobility choices, descriptions,
// rationale, or evidence references.
func Explain(v any) string {
	switch x := v.(type) {
	case CareerPreferenceProfileRevision:
		return fmt.Sprintf("career preference revision %s: worker-owned choices withheld, digest %s", x.Revision, x.CanonicalDigest)
	case TargetRoleProfileRevision:
		return fmt.Sprintf("career target role revision %s: %d readiness requirements, digest %s", x.Revision, len(x.Requirements), x.CanonicalDigest)
	case ReadinessResult:
		return fmt.Sprintf("career readiness: %s with %d gaps, digest %s", x.Status, len(x.Gaps), x.CanonicalDigest)
	case DevelopmentObjectiveProfileRevision:
		return fmt.Sprintf("development objective revision %s: %s, completion evidence withheld, digest %s", x.Revision, x.State, x.CanonicalDigest)
	default:
		return "career explanation: unsupported value"
	}
}
