package location

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/governance/decision"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// ImpactKind identifies a downstream semantic consumer. The location package
// emits intents for these consumers; it never mutates their outcomes.
type ImpactKind string

const (
	ImpactLegal    ImpactKind = "LEGAL"
	ImpactTax      ImpactKind = "TAX"
	ImpactPayroll  ImpactKind = "PAYROLL"
	ImpactLeave    ImpactKind = "LEAVE"
	ImpactSchedule ImpactKind = "SCHEDULE"
)

func (k ImpactKind) Valid() bool {
	switch k {
	case ImpactLegal, ImpactTax, ImpactPayroll, ImpactLeave, ImpactSchedule:
		return true
	default:
		return false
	}
}

var (
	ErrInvalidCorrection     = errors.New("location: invalid correction")
	ErrCorrectionNotGoverned = errors.New("location: correction is not governed")
	ErrNoAffectedPeriod      = errors.New("location: correction has no affected period")
)

// DependentPeriod is an exact, already-known downstream dependency. The
// planner intersects it with the successor's effective interval and carries
// the surviving subject, period and rule release into a reevaluation intent.
type DependentPeriod struct {
	Kind        ImpactKind
	SubjectRef  string
	Period      values.EffectiveInterval
	RuleRelease string
}

// ReevaluationIntent is a bounded request for another domain to recompute its
// own outcome. It intentionally contains no command or downstream mutation.
type ReevaluationIntent struct {
	Kind                   ImpactKind
	SubjectRef             string
	Period                 values.EffectiveInterval
	RuleRelease            string
	LocationRevisionDigest string
}

func (i ReevaluationIntent) Validate() error {
	if !i.Kind.Valid() || strings.TrimSpace(i.SubjectRef) == "" || strings.TrimSpace(i.RuleRelease) == "" {
		return fmt.Errorf("%w: impact needs kind, subject and rule release", ErrInvalidCorrection)
	}
	return i.Period.Validate()
}

// WorkLocationCorrectionRequest contains a proposed successor and the
// governance decision that authorizes the correction proposal.
type WorkLocationCorrectionRequest struct {
	Current      WorkLocationRevision
	Replacement  WorkLocationRevision
	Authority    decision.Decision
	Dependencies []DependentPeriod
}

// CorrectionPlan preserves the old revision and returns the immutable
// successor plus exact downstream reevaluation intents.
type CorrectionPlan struct {
	Previous        WorkLocationRevision
	Successor       WorkLocationRevision
	DecisionDigest  string
	Impacts         []ReevaluationIntent
	CanonicalDigest string
}

func (p CorrectionPlan) Validate() error {
	if err := p.Previous.Validate(); err != nil {
		return err
	}
	if err := p.Successor.Validate(); err != nil {
		return err
	}
	if p.Successor.ParentDigest != p.Previous.CanonicalDigest || p.Successor.ParentRevision != p.Previous.Revision {
		return fmt.Errorf("%w: successor lineage does not name previous revision", ErrInvalidCorrection)
	}
	if p.DecisionDigest == "" || p.CanonicalDigest == "" {
		return fmt.Errorf("%w: correction digests are required", ErrInvalidCorrection)
	}
	for _, impact := range p.Impacts {
		if err := impact.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// CorrectWorkLocation creates a governed successor and a bounded impact plan.
// It refuses denied/unknown governance, stale or invalid revisions, missing
// rule releases, and a correction with no affected dependency period.
func CorrectWorkLocation(req WorkLocationCorrectionRequest) (CorrectionPlan, error) {
	if err := validateAuthority(req.Authority); err != nil {
		return CorrectionPlan{}, err
	}
	if err := req.Current.Validate(); err != nil {
		return CorrectionPlan{}, err
	}
	successor, err := req.Current.Successor(req.Replacement)
	if err != nil {
		return CorrectionPlan{}, err
	}
	impacts, err := affectedIntents(successor.Effective, successor.CanonicalDigest, req.Dependencies)
	if err != nil {
		return CorrectionPlan{}, err
	}
	if len(impacts) == 0 {
		return CorrectionPlan{}, ErrNoAffectedPeriod
	}
	plan := CorrectionPlan{Previous: req.Current, Successor: successor, DecisionDigest: authorityDigest(req.Authority), Impacts: impacts}
	plan.CanonicalDigest = canonicalbytes.Digest(plan.canonical())
	return plan, nil
}

// PlanWorkLocationCorrection is the descriptive spelling used by workflow
// callers.
func PlanWorkLocationCorrection(req WorkLocationCorrectionRequest) (CorrectionPlan, error) {
	return CorrectWorkLocation(req)
}

func validateAuthority(authority decision.Decision) error {
	if authority.State != decision.Allow && authority.State != decision.AllowWithObligations {
		return fmt.Errorf("%w: governance state is %s", ErrCorrectionNotGoverned, authority.State)
	}
	if strings.TrimSpace(authority.ProposalRevisionDigest) == "" {
		return fmt.Errorf("%w: proposal revision digest is required", ErrCorrectionNotGoverned)
	}
	if authority.Digest == "" && authority.CanonicalDigest() == "" {
		return fmt.Errorf("%w: decision digest is required", ErrCorrectionNotGoverned)
	}
	return nil
}

func authorityDigest(authority decision.Decision) string {
	if authority.Digest != "" {
		return authority.Digest
	}
	return authority.CanonicalDigest()
}

func affectedIntents(successor values.EffectiveInterval, digest string, dependencies []DependentPeriod) ([]ReevaluationIntent, error) {
	if err := successor.Validate(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	intents := make([]ReevaluationIntent, 0, len(dependencies))
	for _, dependency := range dependencies {
		if err := dependency.Period.Validate(); err != nil {
			return nil, err
		}
		if !dependency.Kind.Valid() || strings.TrimSpace(dependency.SubjectRef) == "" || strings.TrimSpace(dependency.RuleRelease) == "" {
			return nil, fmt.Errorf("%w: dependency needs kind, subject and rule release", ErrInvalidCorrection)
		}
		overlaps, err := successor.Overlaps(dependency.Period)
		if err != nil {
			return nil, err
		}
		if !overlaps {
			continue
		}
		intent := ReevaluationIntent{Kind: dependency.Kind, SubjectRef: dependency.SubjectRef, Period: dependency.Period, RuleRelease: dependency.RuleRelease, LocationRevisionDigest: digest}
		key := string(intent.Kind) + "\x00" + intent.SubjectRef + "\x00" + intent.Period.String() + "\x00" + intent.RuleRelease
		if !seen[key] {
			seen[key] = true
			intents = append(intents, intent)
		}
	}
	sort.Slice(intents, func(i, j int) bool {
		ki := string(intents[i].Kind) + "\x00" + intents[i].SubjectRef + "\x00" + intents[i].Period.String() + "\x00" + intents[i].RuleRelease
		kj := string(intents[j].Kind) + "\x00" + intents[j].SubjectRef + "\x00" + intents[j].Period.String() + "\x00" + intents[j].RuleRelease
		return ki < kj
	})
	return intents, nil
}

func (p CorrectionPlan) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.location.CorrectionPlan", schemaVersion).
		String("previous_digest", p.Previous.CanonicalDigest).
		String("successor_digest", p.Successor.CanonicalDigest).
		Int("successor_revision", int64(p.Successor.Revision)).
		String("decision_digest", p.DecisionDigest).
		Count("impact", len(p.Impacts))
	for _, impact := range p.Impacts {
		w.String("impact_kind", string(impact.Kind)).String("subject_ref", impact.SubjectRef).String("rule_release", impact.RuleRelease).Value("period", impact.Period).String("location_revision_digest", impact.LocationRevisionDigest)
	}
	b, _ := w.Bytes()
	return b
}

// CorrectionExplanation is safe for logs and contains only counts and stable
// governance/revision identities.
type CorrectionExplanation struct {
	PreviousRevision  uint64
	SuccessorRevision uint64
	ImpactCount       int
	DecisionDigest    string
	Digest            string
}

func (p CorrectionPlan) Explain() (CorrectionExplanation, error) {
	if err := p.Validate(); err != nil {
		return CorrectionExplanation{}, err
	}
	return CorrectionExplanation{PreviousRevision: p.Previous.Revision, SuccessorRevision: p.Successor.Revision, ImpactCount: len(p.Impacts), DecisionDigest: p.DecisionDigest, Digest: p.CanonicalDigest}, nil
}

func ExplainCorrection(p CorrectionPlan) (CorrectionExplanation, error) { return p.Explain() }
