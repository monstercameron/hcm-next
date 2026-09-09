package jobarch

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// PromotionPathKind states how two governed job profiles relate. The path is
// authoritative; a career preference or a matching title is not.
type PromotionPathKind string

const (
	PromotionPathUpward      PromotionPathKind = "UPWARD"
	PromotionPathLateral     PromotionPathKind = "LATERAL"
	PromotionPathCrossFamily PromotionPathKind = "CROSS_FAMILY"
)

func (k PromotionPathKind) Valid() bool {
	return k == PromotionPathUpward || k == PromotionPathLateral || k == PromotionPathCrossFamily
}

// ProfileRevisionRef pins the stable profile and the immutable revision that
// a transition starts from or reaches. Job codes are display/classification
// projections of these identities, not substitutes for them.
type ProfileRevisionRef struct {
	ProfileID string
	Revision  string
}

func (r ProfileRevisionRef) Validate(field string) error {
	if strings.TrimSpace(r.ProfileID) == "" {
		return invalidField(field+".profile_id", "is required")
	}
	if strings.TrimSpace(r.Revision) == "" {
		return invalidField(field+".revision", "is required")
	}
	return nil
}

// PromotionPathRevision is one immutable, effective-dated edge in a career
// ladder. CompensationPolicyRef names the existing versioned promotion rules;
// BenefitEligibilityRuleRefs request reevaluation under the benefits owner.
// The path never directly changes benefit elections.
type PromotionPathRevision struct {
	ID, PathID, Revision       string
	From, To                   ProfileRevisionRef
	Kind                       PromotionPathKind
	MinimumBaseIncrease        values.Percentage
	MaximumBaseIncrease        values.Percentage
	CompensationPolicyRef      VersionedReference
	BenefitEligibilityRuleRefs []VersionedReference
	Authority                  string
	Lifecycle                  Lifecycle
	EffectiveFrom, EffectiveTo time.Time
	KnownFrom, KnownTo         time.Time
	Lineage                    RevisionLineage
}

var (
	ErrInvalidPromotionPath    = errors.New("jobarch: invalid promotion path")
	ErrPromotionPathShape      = errors.New("jobarch: promotion path contradicts profile ladder")
	ErrBaseIncreaseOutsidePath = errors.New("jobarch: base increase is outside promotion path guardrails")
)

func (p PromotionPathRevision) PathIDOrID() string {
	if p.PathID != "" {
		return p.PathID
	}
	return p.ID
}

func (p PromotionPathRevision) Validate() error {
	if err := validateRevision(p.PathIDOrID(), p.Revision, "promotion_path", p.Lifecycle, p.EffectiveFrom, p.EffectiveTo, p.KnownFrom, p.KnownTo, p.Lineage); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPromotionPath, err)
	}
	if err := p.From.Validate("promotion_path.from"); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPromotionPath, err)
	}
	if err := p.To.Validate("promotion_path.to"); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidPromotionPath, err)
	}
	if p.From == p.To {
		return fmt.Errorf("%w: source and target profile revisions must differ", ErrInvalidPromotionPath)
	}
	if !p.Kind.Valid() {
		return fmt.Errorf("%w: kind is not declared", ErrInvalidPromotionPath)
	}
	if strings.TrimSpace(p.Authority) == "" {
		return fmt.Errorf("%w: authority is required", ErrInvalidPromotionPath)
	}
	if err := p.MinimumBaseIncrease.Validate(); err != nil {
		return fmt.Errorf("%w: minimum_base_increase: %w", ErrInvalidPromotionPath, err)
	}
	if err := p.MaximumBaseIncrease.Validate(); err != nil {
		return fmt.Errorf("%w: maximum_base_increase: %w", ErrInvalidPromotionPath, err)
	}
	zero := values.MustDecimal("0", 0, values.RoundingExactRequired)
	if p.MinimumBaseIncrease.Fraction().Cmp(zero) < 0 || p.MaximumBaseIncrease.Fraction().Cmp(zero) < 0 {
		return fmt.Errorf("%w: base increase guardrails cannot be negative", ErrInvalidPromotionPath)
	}
	if p.MinimumBaseIncrease.Fraction().Cmp(p.MaximumBaseIncrease.Fraction()) > 0 {
		return fmt.Errorf("%w: minimum base increase exceeds maximum", ErrInvalidPromotionPath)
	}
	if err := p.CompensationPolicyRef.Validate(ReferenceKind("PROMOTION_COMPENSATION_POLICY")); err != nil {
		return fmt.Errorf("%w: compensation_policy_ref: %w", ErrInvalidPromotionPath, err)
	}
	if p.CompensationPolicyRef.empty() {
		return fmt.Errorf("%w: compensation_policy_ref is required", ErrInvalidPromotionPath)
	}
	for i, ref := range p.BenefitEligibilityRuleRefs {
		if err := ref.Validate(ReferenceKind("BENEFIT_ELIGIBILITY_RULE")); err != nil {
			return fmt.Errorf("%w: benefit_eligibility_rule_refs[%d]: %w", ErrInvalidPromotionPath, i, err)
		}
	}
	return nil
}

// ValidateAgainst proves that the edge agrees with the pinned architecture.
func (p PromotionPathRevision) ValidateAgainst(a ArchitectureRevision) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := a.Validate(); err != nil {
		return err
	}
	from, ok := findProfileRevision(a.Profiles, p.From)
	if !ok {
		return fmt.Errorf("%w: source profile %s@%s not found", ErrPromotionPathShape, p.From.ProfileID, p.From.Revision)
	}
	to, ok := findProfileRevision(a.Profiles, p.To)
	if !ok {
		return fmt.Errorf("%w: target profile %s@%s not found", ErrPromotionPathShape, p.To.ProfileID, p.To.Revision)
	}
	fromLevel, _ := findLevel(a.Levels, from.LevelIDOrRef())
	toLevel, _ := findLevel(a.Levels, to.LevelIDOrRef())
	sameFamily := from.FamilyIDOrRef() == to.FamilyIDOrRef()
	switch p.Kind {
	case PromotionPathUpward:
		if !sameFamily || toLevel.Rank <= fromLevel.Rank {
			return fmt.Errorf("%w: upward paths require a higher-ranked profile in the same family", ErrPromotionPathShape)
		}
	case PromotionPathLateral:
		if !sameFamily || toLevel.Rank != fromLevel.Rank {
			return fmt.Errorf("%w: lateral paths require equal-ranked profiles in the same family", ErrPromotionPathShape)
		}
	case PromotionPathCrossFamily:
		if sameFamily {
			return fmt.Errorf("%w: cross-family paths require different families", ErrPromotionPathShape)
		}
	}
	return nil
}

func findProfileRevision(profiles []JobProfileRevision, ref ProfileRevisionRef) (JobProfileRevision, bool) {
	for _, profile := range profiles {
		if profile.ProfileIDOrID() == ref.ProfileID && profile.Revision == ref.Revision {
			return profile, true
		}
	}
	return JobProfileRevision{}, false
}

// AllowsBaseIncrease applies the path's exact fractional guardrails. For
// example 0.0750 means 7.5 percent; float rounding is never involved.
func (p PromotionPathRevision) AllowsBaseIncrease(increase values.Percentage) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := increase.Validate(); err != nil {
		return err
	}
	if increase.Fraction().Cmp(p.MinimumBaseIncrease.Fraction()) < 0 ||
		increase.Fraction().Cmp(p.MaximumBaseIncrease.Fraction()) > 0 {
		return ErrBaseIncreaseOutsidePath
	}
	return nil
}

func (p PromotionPathRevision) Canonical() []byte {
	if p.Validate() != nil {
		return nil
	}
	w := canonicalbytes.New("hcmnext.domains.jobarch.PromotionPathRevision", 1).
		String("id", p.ID).String("path_id", p.PathID).String("revision", p.Revision).
		String("from.profile_id", p.From.ProfileID).String("from.revision", p.From.Revision).
		String("to.profile_id", p.To.ProfileID).String("to.revision", p.To.Revision).
		String("kind", string(p.Kind)).String("minimum_base_increase", p.MinimumBaseIncrease.String()).
		String("maximum_base_increase", p.MaximumBaseIncrease.String()).
		String("compensation_policy_ref", referenceKey(p.CompensationPolicyRef)).
		String("authority", p.Authority).String("lifecycle", p.Lifecycle.String()).
		String("effective_from", archTime(p.EffectiveFrom)).String("effective_to", archTime(p.EffectiveTo)).
		String("known_from", archTime(p.KnownFrom)).String("known_to", archTime(p.KnownTo)).
		String("lineage.root_id", p.Lineage.RootID).String("lineage.supersedes", p.Lineage.Supersedes)
	for _, ref := range p.BenefitEligibilityRuleRefs {
		w.String("benefit_eligibility_rule_ref", referenceKey(ref))
	}
	raw, err := w.Bytes()
	if err != nil {
		return nil
	}
	return raw
}

func (p PromotionPathRevision) Digest() (string, error) {
	return digestOrError(p.Canonical(), p.Validate())
}
