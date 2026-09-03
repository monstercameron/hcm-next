// ELIG-005: explain an eligibility decision safely. The explanation walks the
// same compiled criteria Evaluate folds, in the tree's own order, and reports
// each leaf's contribution - but a fact or rule the caller's disclosure
// decision does not authorize naming is redacted rather than shown. Because
// FactReader and RuleReader only ever take the one subject named in the
// request, an explanation can never compare that subject against, or
// disclose evidence about, anyone else.
package eligibility

import (
	"context"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// ConditionStatus is one leaf's contribution to the explained result.
type ConditionStatus uint8

// Condition statuses.
const (
	ConditionStatusUnspecified ConditionStatus = iota
	ConditionStatusPassed
	ConditionStatusFailed
	ConditionStatusPartial
	ConditionStatusUnknown
	ConditionStatusNotApplicable
)

// String returns the wire token.
func (s ConditionStatus) String() string {
	switch s {
	case ConditionStatusPassed:
		return "PASSED"
	case ConditionStatusFailed:
		return "FAILED"
	case ConditionStatusPartial:
		return "PARTIAL"
	case ConditionStatusUnknown:
		return "UNKNOWN"
	case ConditionStatusNotApplicable:
		return "NOT_APPLICABLE"
	default:
		return "CONDITION_STATUS_UNSPECIFIED"
	}
}

func statusFromLeaf(s leafState) ConditionStatus {
	switch s {
	case leafPass:
		return ConditionStatusPassed
	case leafFail:
		return ConditionStatusFailed
	case leafPartial:
		return ConditionStatusPartial
	case leafNotApplicable:
		return ConditionStatusNotApplicable
	default:
		return ConditionStatusUnknown
	}
}

// ConditionExplanation is one leaf's disclosed (or redacted) contribution.
// Exactly one of Field or RuleID is set for a disclosed leaf; both are empty
// when Redacted is true.
type ConditionExplanation struct {
	Field    string
	RuleID   string
	Redacted bool
	Status   ConditionStatus
}

// Explanation is the deterministic, side-effect-free account of one
// request's result.
type Explanation struct {
	Subject               string
	Status                Status
	Conditions            []ConditionExplanation
	Authority             string
	ProgramVersion        string
	PopulationSnapshotRef string
	FactSnapshotRef       string
	RuleSnapshotRef       string
	EffectiveInterval     values.EffectiveInterval
	KnownAt               values.KnownAt
	Digest                string
}

// Explain evaluates plan's criteria for req exactly as Evaluate would, and
// reports each leaf's contribution in the criteria tree's own deterministic
// order. disclosableFacts and disclosableRules name the fields and rule IDs
// this caller is authorized to see named; anything else is redacted rather
// than omitted, so the explanation's shape never itself reveals how many
// restricted leaves exist.
func Explain(ctx context.Context, facts FactReader, rules RuleReader, req Request, plan CompiledPlan, disclosableFacts, disclosableRules map[string]bool) (Explanation, error) {
	if err := req.Validate(); err != nil {
		return Explanation{}, err
	}

	trace := &evalTrace{}
	rootState, err := evaluateCondition(ctx, facts, rules, req, plan, plan.Criteria.Root, trace)
	if err != nil {
		return Explanation{}, err
	}

	var conditions []ConditionExplanation
	if err := traceConditions(ctx, facts, rules, req, plan, plan.Criteria.Root, disclosableFacts, disclosableRules, &conditions); err != nil {
		return Explanation{}, err
	}

	explanation := Explanation{
		Subject:               req.Subject.String(),
		Status:                statusFor(rootState),
		Conditions:            conditions,
		Authority:             req.Authority,
		ProgramVersion:        req.SubjectMatter.Revision,
		PopulationSnapshotRef: req.Snapshots.PopulationSnapshotRef,
		FactSnapshotRef:       req.Snapshots.FactSnapshotRef,
		RuleSnapshotRef:       req.Snapshots.RuleSnapshotRef,
		EffectiveInterval:     req.EffectiveInterval,
		KnownAt:               req.KnownAt,
	}

	w := canonicalbytes.New("hcmnext.engines.eligibility.Explanation", schemaVersion).
		String("subject", explanation.Subject).
		String("status", explanation.Status.String()).
		String("authority", explanation.Authority).
		String("program_version", explanation.ProgramVersion).
		String("population_snapshot", explanation.PopulationSnapshotRef).
		String("fact_snapshot", explanation.FactSnapshotRef).
		String("rule_snapshot", explanation.RuleSnapshotRef).
		Value("effective_interval", explanation.EffectiveInterval).
		Count("conditions", len(conditions))
	if explanation.KnownAt.Instant().Validate() == nil {
		w.Value("known_at", explanation.KnownAt.Instant())
	}
	for _, c := range conditions {
		w.Bool("redacted", c.Redacted)
		w.String("field", c.Field)
		w.String("rule_id", c.RuleID)
		w.String("status", c.Status.String())
	}
	digest, err := w.Digest()
	if err != nil {
		return Explanation{}, fmt.Errorf("eligibility: explain digest: %w", err)
	}
	explanation.Digest = digest
	return explanation, nil
}

// traceConditions walks c in the tree's own order, appending one
// ConditionExplanation per leaf (composites contribute no entry of their
// own).
func traceConditions(ctx context.Context, facts FactReader, rules RuleReader, req Request, plan CompiledPlan, c Condition, disclosableFacts, disclosableRules map[string]bool, out *[]ConditionExplanation) error {
	if c.Kind.isComposite() {
		for _, child := range c.Children {
			if err := traceConditions(ctx, facts, rules, req, plan, child, disclosableFacts, disclosableRules, out); err != nil {
				return err
			}
		}
		return nil
	}

	// Re-evaluate this single leaf in isolation so its own status is reported
	// independent of how its siblings folded; a fresh trace discards any
	// obligations/evidence this call would otherwise duplicate into the
	// caller's real trace.
	leafTrace := &evalTrace{}
	state, err := evaluateCondition(ctx, facts, rules, req, plan, c, leafTrace)
	if err != nil {
		return err
	}

	entry := ConditionExplanation{Status: statusFromLeaf(state)}
	if c.Kind.isFactLeaf() {
		entry.Field = c.Field
		if !disclosableFacts[c.Field] {
			entry.Redacted = true
			entry.Field = ""
		}
	} else {
		entry.RuleID = c.RuleID
		if !disclosableRules[c.RuleID] {
			entry.Redacted = true
			entry.RuleID = ""
		}
	}
	*out = append(*out, entry)
	return nil
}
