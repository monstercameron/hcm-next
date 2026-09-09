package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrInvalidCorrection     = errors.New("service: invalid correction")
	ErrCorrectionNotGoverned = errors.New("service: correction is not governed")
	ErrCorrectionNoChange    = errors.New("service: correction does not change the model")
	ErrCorrectionStale       = errors.New("service: correction current pin is stale")
	ErrCorrectionLineage     = errors.New("service: correction lineage is invalid")
)

type CorrectionIntentKind string

const (
	IntentReevaluate                 CorrectionIntentKind = "REEVALUATE"
	IntentGovernedCorrectionRequired CorrectionIntentKind = "GOVERNED_CORRECTION_REQUIRED"
)

type ServiceDependency struct {
	ID               string
	WorkerRef        string
	Dimension        SeniorityDimension
	Interval         values.EffectiveInterval
	RuleRelease      string
	OutcomeProtected bool
}

func (d ServiceDependency) Validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.WorkerRef) == "" || !d.Dimension.Valid() || strings.TrimSpace(d.RuleRelease) == "" {
		return fmt.Errorf("%w: dependency requires id, worker, dimension and rule release", ErrInvalidCorrection)
	}
	if err := d.Interval.Validate(); err != nil {
		return fmt.Errorf("%w: dependency interval: %v", ErrInvalidCorrection, err)
	}
	return nil
}

func (d ServiceDependency) Canonical() []byte {
	if d.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.service.ServiceDependency", evaluationSchemaVersion).String("id", d.ID).String("worker", d.WorkerRef).String("dimension", string(d.Dimension)).Value("interval", d.Interval).String("release", d.RuleRelease).Bool("outcome_protected", d.OutcomeProtected).Bytes()
	if err != nil {
		return nil
	}
	return b
}

type ServiceReevaluationIntent struct {
	Dependency      ServiceDependency
	ParentDigest    string
	SuccessorDigest string
	Kind            CorrectionIntentKind
}

func (i ServiceReevaluationIntent) Validate() error {
	if err := i.Dependency.Validate(); err != nil {
		return err
	}
	if i.ParentDigest == "" || i.SuccessorDigest == "" || i.ParentDigest == i.SuccessorDigest || (i.Kind != IntentReevaluate && i.Kind != IntentGovernedCorrectionRequired) {
		return fmt.Errorf("%w: reevaluation lineage is invalid", ErrInvalidCorrection)
	}
	return nil
}

func (i ServiceReevaluationIntent) Canonical() []byte {
	if i.Validate() != nil {
		return nil
	}
	b, err := canonicalbytes.New("hcmnext.domains.service.ServiceReevaluationIntent", evaluationSchemaVersion).Value("dependency", i.Dependency).String("parent", i.ParentDigest).String("successor", i.SuccessorDigest).String("kind", string(i.Kind)).Bytes()
	if err != nil {
		return nil
	}
	return b
}

type ServiceCorrectionRequest struct {
	Current                      ServiceModel
	Replacement                  ServiceModel
	AsOf                         AsOf
	WorkerRef                    string
	ExpectedCurrentDigest        string
	ExpectedPreviousResultDigest string
	Dependencies                 []ServiceDependency
}

type ServiceCorrectionPlan struct {
	Previous            ServiceModel
	Successor           ServiceModel
	PreviousResult      SenioritySnapshot
	SuccessorResult     SenioritySnapshot
	PreviousExplanation ServiceExplanation
	Impacts             []ServiceReevaluationIntent
	CanonicalDigest     string
	Draft               bool
}

func CorrectService(req ServiceCorrectionRequest) (ServiceCorrectionPlan, error) {
	if strings.TrimSpace(req.WorkerRef) == "" || strings.TrimSpace(req.ExpectedCurrentDigest) == "" || strings.TrimSpace(req.ExpectedPreviousResultDigest) == "" {
		return ServiceCorrectionPlan{}, ErrCorrectionNotGoverned
	}
	if err := req.Current.Validate(); err != nil {
		return ServiceCorrectionPlan{}, err
	}
	if err := req.Replacement.Validate(); err != nil {
		return ServiceCorrectionPlan{}, err
	}
	if err := req.AsOf.Validate(); err != nil {
		return ServiceCorrectionPlan{}, err
	}
	oldDigest, err := req.Current.Digest()
	if err != nil {
		return ServiceCorrectionPlan{}, err
	}
	if oldDigest != req.ExpectedCurrentDigest {
		return ServiceCorrectionPlan{}, ErrCorrectionStale
	}
	newDigest, err := req.Replacement.Digest()
	if err != nil {
		return ServiceCorrectionPlan{}, err
	}
	if oldDigest == newDigest {
		return ServiceCorrectionPlan{}, ErrCorrectionNoChange
	}
	if err := validateCorrectionLineage(req.Current, req.Replacement); err != nil {
		return ServiceCorrectionPlan{}, err
	}
	oldResult, err := req.Current.Compute(req.AsOf)
	if err != nil {
		return ServiceCorrectionPlan{}, err
	}
	if oldResult.CanonicalDigest != req.ExpectedPreviousResultDigest {
		return ServiceCorrectionPlan{}, ErrCorrectionStale
	}
	newResult, err := req.Replacement.Compute(req.AsOf)
	if err != nil {
		return ServiceCorrectionPlan{}, err
	}
	explanation, err := req.Current.Explain()
	if err != nil {
		return ServiceCorrectionPlan{}, err
	}
	impacts := make([]ServiceReevaluationIntent, 0, len(req.Dependencies))
	for _, dependency := range req.Dependencies {
		if err := dependency.Validate(); err != nil {
			return ServiceCorrectionPlan{}, err
		}
		ruleChanged := changedDimensionRule(req.Current, req.Replacement, dependency.Dimension)
		if dependency.WorkerRef != req.WorkerRef || (!changedDimension(oldResult, newResult, dependency.Dimension) && !ruleChanged) || !dependencyTouchesCorrection(dependency, req.Current, req.Replacement) {
			continue
		}
		kind := IntentReevaluate
		if dependency.OutcomeProtected {
			kind = IntentGovernedCorrectionRequired
		}
		impacts = append(impacts, ServiceReevaluationIntent{Dependency: dependency, ParentDigest: oldDigest, SuccessorDigest: newDigest, Kind: kind})
	}
	sort.Slice(impacts, func(i, j int) bool { return impacts[i].Dependency.ID < impacts[j].Dependency.ID })
	previous, successor := cloneServiceModel(req.Current), cloneServiceModel(req.Replacement)
	previous.CanonicalDigest, successor.CanonicalDigest = oldDigest, newDigest
	plan := ServiceCorrectionPlan{Previous: previous, Successor: successor, PreviousResult: oldResult, SuccessorResult: newResult, PreviousExplanation: explanation, Impacts: impacts, Draft: true}
	plan.CanonicalDigest = canonicalbytes.Digest(plan.canonical())
	if err := plan.Validate(); err != nil {
		return ServiceCorrectionPlan{}, err
	}
	return plan, nil
}

func cloneServiceModel(model ServiceModel) ServiceModel {
	cloned := model
	cloned.Periods = clonePeriods(model.Periods)
	cloned.Rules = cloneRules(model.Rules)
	cloned.PeerPopulation = make([]SeniorityPeer, len(model.PeerPopulation))
	for i, peer := range model.PeerPopulation {
		cloned.PeerPopulation[i] = SeniorityPeer{SubjectID: peer.SubjectID, Periods: clonePeriods(peer.Periods)}
	}
	return cloned
}

func changedDimension(a, b SenioritySnapshot, dimension SeniorityDimension) bool {
	var old, next *SeniorityMeasure
	for i := range a.Measures {
		if a.Measures[i].Dimension == dimension {
			old = &a.Measures[i]
		}
	}
	for i := range b.Measures {
		if b.Measures[i].Dimension == dimension {
			next = &b.Measures[i]
		}
	}
	if old == nil && next == nil {
		return false
	}
	if old == nil || next == nil {
		return true
	}
	return old.TotalCreditedDays != next.TotalCreditedDays || old.ContinuousDays != next.ContinuousDays || old.AdjustedDate.Compare(next.AdjustedDate) != 0 || old.Status != next.Status
}

func validateCorrectionLineage(current, replacement ServiceModel) error {
	nextByID := make(map[string]ServicePeriod, len(replacement.Periods))
	for _, period := range replacement.Periods {
		nextByID[period.periodID()] = period
	}
	for _, old := range current.Periods {
		next, ok := nextByID[old.periodID()]
		if !ok {
			return fmt.Errorf("%w: period %s was removed", ErrCorrectionLineage, old.periodID())
		}
		if string(old.Canonical()) == string(next.Canonical()) {
			continue
		}
		oldSequence, oldOK := old.Revision.Sequence()
		nextSequence, nextOK := next.Revision.Sequence()
		if !oldOK || !nextOK || old.Revision.Stream() != next.Revision.Stream() || nextSequence != oldSequence+1 || next.ParentDigest != canonicalbytes.Digest(old.Canonical()) {
			return fmt.Errorf("%w: period %s is not an exact successor", ErrCorrectionLineage, old.periodID())
		}
	}
	return nil
}

func dependencyTouchesCorrection(dependency ServiceDependency, current, replacement ServiceModel) bool {
	if changedDimensionRule(current, replacement, dependency.Dimension) {
		return true
	}
	oldByID := make(map[string]ServicePeriod, len(current.Periods))
	for _, period := range current.Periods {
		oldByID[period.periodID()] = period
	}
	for _, next := range replacement.Periods {
		old, existed := oldByID[next.periodID()]
		if existed && string(old.Canonical()) == string(next.Canonical()) {
			continue
		}
		if existed && containsDimension(old.dimensions(), dependency.Dimension) {
			overlaps, err := old.interval().Overlaps(dependency.Interval)
			if err == nil && overlaps {
				return true
			}
		}
		if containsDimension(next.dimensions(), dependency.Dimension) {
			overlaps, err := next.interval().Overlaps(dependency.Interval)
			if err == nil && overlaps {
				return true
			}
		}
	}
	return false
}

func changedDimensionRule(current, replacement ServiceModel, dimension SeniorityDimension) bool {
	var oldRule, nextRule *SeniorityRule
	for i := range current.Rules {
		if current.Rules[i].Dimension == dimension {
			oldRule = &current.Rules[i]
		}
	}
	for i := range replacement.Rules {
		if replacement.Rules[i].Dimension == dimension {
			nextRule = &replacement.Rules[i]
		}
	}
	if oldRule == nil || nextRule == nil {
		return oldRule != nextRule
	}
	return string(oldRule.Canonical()) != string(nextRule.Canonical())
}

func (p ServiceCorrectionPlan) canonical() []byte {
	w := canonicalbytes.New("hcmnext.domains.service.ServiceCorrectionPlan", evaluationSchemaVersion).String("previous", p.Previous.CanonicalDigest).String("successor", p.Successor.CanonicalDigest).Value("previous_result", p.PreviousResult).Value("successor_result", p.SuccessorResult).Bool("draft", p.Draft).Count("impact", len(p.Impacts))
	w.Int("previous_period_count", int64(p.PreviousExplanation.PeriodCount)).Int("previous_rule_count", int64(p.PreviousExplanation.RuleCount)).String("previous_explanation_digest", p.PreviousExplanation.CanonicalDigest).Count("previous_dimension", len(p.PreviousExplanation.Dimensions))
	for _, dimension := range p.PreviousExplanation.Dimensions {
		w.String("previous_dimension", string(dimension))
	}
	for _, impact := range p.Impacts {
		w.Value("impact", impact)
	}
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (p ServiceCorrectionPlan) Validate() error {
	if err := p.Previous.Validate(); err != nil {
		return err
	}
	if err := p.Successor.Validate(); err != nil {
		return err
	}
	if p.Previous.CanonicalDigest == p.Successor.CanonicalDigest || p.CanonicalDigest == "" || !p.Draft {
		return ErrInvalidCorrection
	}
	if err := p.PreviousResult.Validate(); err != nil {
		return err
	}
	if err := p.SuccessorResult.Validate(); err != nil {
		return err
	}
	explanation, err := p.Previous.Explain()
	if err != nil || explanation.PeriodCount != p.PreviousExplanation.PeriodCount || explanation.RuleCount != p.PreviousExplanation.RuleCount || explanation.CanonicalDigest != p.PreviousExplanation.CanonicalDigest || strings.Join(dimensionsAsStrings(explanation.Dimensions), ",") != strings.Join(dimensionsAsStrings(p.PreviousExplanation.Dimensions), ",") {
		return fmt.Errorf("%w: previous explanation pin mismatch", ErrInvalidCorrection)
	}
	seenImpacts := make(map[string]struct{}, len(p.Impacts))
	for _, impact := range p.Impacts {
		if err := impact.Validate(); err != nil {
			return err
		}
		if impact.ParentDigest != p.Previous.CanonicalDigest || impact.SuccessorDigest != p.Successor.CanonicalDigest {
			return fmt.Errorf("%w: impact lineage mismatch", ErrInvalidCorrection)
		}
		if _, exists := seenImpacts[impact.Dependency.ID]; exists {
			return fmt.Errorf("%w: duplicate impact %s", ErrInvalidCorrection, impact.Dependency.ID)
		}
		seenImpacts[impact.Dependency.ID] = struct{}{}
	}
	if p.CanonicalDigest != canonicalbytes.Digest(p.canonical()) {
		return fmt.Errorf("%w: canonical digest mismatch", ErrInvalidCorrection)
	}
	return nil
}

func dimensionsAsStrings(dimensions []SeniorityDimension) []string {
	out := make([]string, len(dimensions))
	for i, dimension := range dimensions {
		out[i] = string(dimension)
	}
	return out
}
