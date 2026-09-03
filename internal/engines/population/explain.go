// POP-007: explain one subject's inclusion or exclusion. The explanation
// walks the same compiled criteria Resolve would evaluate and reports each
// leaf's matched/failed/unknown status in the tree's own deterministic order,
// but it never names a field the caller's field-disclosure decision does not
// authorize, and it never computes or reveals anything about another subject.
package population

import (
	"context"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// CriterionStatus is one leaf predicate's contribution to a subject's result.
type CriterionStatus uint8

// Criterion statuses.
const (
	CriterionUnspecified CriterionStatus = iota
	CriterionMatched
	CriterionFailed
	CriterionUnknown
)

// String returns the wire token.
func (s CriterionStatus) String() string {
	switch s {
	case CriterionMatched:
		return "MATCHED"
	case CriterionFailed:
		return "FAILED"
	case CriterionUnknown:
		return "UNKNOWN"
	default:
		return "CRITERION_UNSPECIFIED"
	}
}

// CriterionExplanation is one leaf's disclosed (or redacted) contribution.
// Field is empty and Redacted is true whenever the field-disclosure decision
// does not authorize naming the field: the leaf's status still contributes to
// explaining the subject's own overall outcome, but which fact produced it is
// withheld.
type CriterionExplanation struct {
	Field      string
	Redacted   bool
	Status     CriterionStatus
	Obligation ObligationReason
}

// Explanation is the deterministic, side-effect-free account of one subject's
// resolved outcome.
type Explanation struct {
	Subject  values.EntityRef
	Outcome  Outcome
	Criteria []CriterionExplanation
	Versions PolicyVersions
	Digest   string
}

// Explain evaluates plan's criteria for subject exactly as Resolve would, and
// reports each leaf's contribution. disclosableFields names the fields this
// caller is authorized to see named in an explanation; a field not in that
// set is redacted rather than omitted, so the explanation's shape (one entry
// per leaf) never itself reveals how many restricted criteria exist.
func Explain(ctx context.Context, reader FactReader, plan CompiledPlan, subject values.EntityRef, asOf values.Instant, knownAt values.KnownAt, disclosableFields map[string]bool, versions PolicyVersions) (Explanation, error) {
	if err := subject.Validate(); err != nil {
		return Explanation{}, fmt.Errorf("population: explain subject: %w", err)
	}
	if err := versions.Validate(); err != nil {
		return Explanation{}, err
	}

	var criteria []CriterionExplanation
	result, _, err := evaluatePredicate(ctx, reader, subject, plan, plan.Criteria.Root, asOf, knownAt, false)
	if err != nil {
		return Explanation{}, err
	}
	if err := traceLeaves(ctx, reader, subject, plan, plan.Criteria.Root, asOf, knownAt, disclosableFields, &criteria); err != nil {
		return Explanation{}, err
	}

	outcome := OutcomeUnknown
	switch result {
	case ternaryTrue:
		outcome = OutcomeIncluded
	case ternaryFalse:
		outcome = OutcomeExcluded
	}

	explanation := Explanation{Subject: subject, Outcome: outcome, Criteria: criteria, Versions: versions}
	w := writerFor(explainSchema).
		Value("subject", subject).
		String("outcome", outcome.String()).
		Count("criteria", len(criteria))
	for _, c := range criteria {
		w.Bool("redacted", c.Redacted)
		w.String("field", c.Field)
		w.String("status", c.Status.String())
		w.String("obligation", c.Obligation.String())
	}
	digest, err := w.Digest()
	if err != nil {
		return Explanation{}, fmt.Errorf("population: explain digest: %w", err)
	}
	explanation.Digest = digest
	return explanation, nil
}

// traceLeaves walks p in the tree's own order, appending one CriterionExplanation
// per leaf.
func traceLeaves(ctx context.Context, reader FactReader, subject values.EntityRef, plan CompiledPlan, p Predicate, asOf values.Instant, knownAt values.KnownAt, disclosable map[string]bool, out *[]CriterionExplanation) error {
	if p.Kind.isComposite() {
		for _, child := range p.Children {
			if err := traceLeaves(ctx, reader, subject, plan, child, asOf, knownAt, disclosable, out); err != nil {
				return err
			}
		}
		return nil
	}
	result, obligations, err := evaluatePredicate(ctx, reader, subject, plan, p, asOf, knownAt, false)
	if err != nil {
		return err
	}
	entry := CriterionExplanation{Field: p.Field}
	switch result {
	case ternaryTrue:
		entry.Status = CriterionMatched
	case ternaryFalse:
		entry.Status = CriterionFailed
	default:
		entry.Status = CriterionUnknown
		if len(obligations) > 0 {
			entry.Obligation = obligations[0].Reason
		}
	}
	if !disclosable[p.Field] {
		entry.Redacted = true
		entry.Field = ""
	}
	*out = append(*out, entry)
	return nil
}
