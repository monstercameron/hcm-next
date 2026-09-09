// POP-003: resolve a compiled population as-of an effective instant, known no
// later than a declared knowledge boundary. POP-008: preserve UNKNOWN and
// PARTIAL outcomes rather than defaulting an undetermined subject to excluded.
package population

import (
	"context"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Outcome is one subject's determined membership.
type Outcome uint8

// Outcomes.
const (
	OutcomeUnspecified Outcome = iota
	// OutcomeIncluded means every fact the criteria needed resolved to a value
	// and the predicate evaluated true.
	OutcomeIncluded
	// OutcomeExcluded means every fact the criteria needed resolved to a value
	// and the predicate evaluated false.
	OutcomeExcluded
	// OutcomeUnknown means at least one fact the criteria needed for a
	// deciding branch could not be determined. It is never coerced to
	// Included or Excluded.
	OutcomeUnknown
)

// String returns the wire token.
func (o Outcome) String() string {
	switch o {
	case OutcomeIncluded:
		return "INCLUDED"
	case OutcomeExcluded:
		return "EXCLUDED"
	case OutcomeUnknown:
		return "UNKNOWN"
	default:
		return "OUTCOME_UNSPECIFIED"
	}
}

// Completeness is the resolution's overall confidence in its result set.
type Completeness uint8

// Completeness states.
const (
	CompletenessUnspecified Completeness = iota
	// CompletenessComplete means every candidate subject reached a definite
	// Included or Excluded outcome.
	CompletenessComplete
	// CompletenessPartial means at least one subject is Unknown; the disclosed
	// membership set excludes it, per UnknownDisclosureExcludeAndReport, and
	// the caller can see exactly which subjects and why.
	CompletenessPartial
)

// String returns the wire token.
func (c Completeness) String() string {
	switch c {
	case CompletenessComplete:
		return "COMPLETE"
	case CompletenessPartial:
		return "PARTIAL"
	default:
		return "COMPLETENESS_UNSPECIFIED"
	}
}

// Member is one subject's resolved outcome, with the obligations that explain
// an Unknown outcome.
type Member struct {
	Subject     values.EntityRef
	Outcome     Outcome
	Obligations []Obligation
}

// Result is the full resolution of a compiled plan against a fact reader, as
// of one effective instant known no later than one knowledge boundary.
type Result struct {
	Plan             CompiledPlan
	AsOf             values.Instant
	KnownAt          values.KnownAt
	SourceWatermarks map[SubjectKind]values.Instant
	Members          []Member
	Completeness     Completeness
}

// Included returns the subjects with OutcomeIncluded, sorted by canonical
// entity-ref text so the result is deterministic regardless of FactReader
// enumeration order.
func (r Result) Included() []values.EntityRef {
	var out []values.EntityRef
	for _, m := range r.Members {
		if m.Outcome == OutcomeIncluded {
			out = append(out, m.Subject)
		}
	}
	sortRefs(out)
	return out
}

// Unknown returns the subjects with OutcomeUnknown, sorted deterministically.
func (r Result) Unknown() []Member {
	var out []Member
	for _, m := range r.Members {
		if m.Outcome == OutcomeUnknown {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Subject.String() < out[j].Subject.String() })
	return out
}

func sortRefs(refs []values.EntityRef) {
	sort.Slice(refs, func(i, j int) bool { return refs[i].String() < refs[j].String() })
}

// ternary is three-valued logic used to fold UNKNOWN through AND/OR/NOT
// without ever coercing it to true or false.
type ternary uint8

const (
	ternaryFalse ternary = iota
	ternaryTrue
	ternaryUnknown
)

// Resolve evaluates a compiled plan's criteria for every subject a FactReader
// enumerates for def's subject kind, as of asOf, known no later than knownAt.
//
// It is deterministic: identical plan, subject enumeration, fact values and
// time context produce identical members and completeness on every call.
func Resolve(ctx context.Context, reader FactReader, subject SubjectKind, plan CompiledPlan, asOf values.Instant, knownAt values.KnownAt) (Result, error) {
	if !subject.Valid() {
		return Result{}, fmt.Errorf("%w: %q", ErrDefinitionSubject, string(subject))
	}
	if err := asOf.Validate(); err != nil {
		return Result{}, fmt.Errorf("population: resolve as-of: %w", err)
	}
	if err := knownAt.Instant().Validate(); err != nil {
		return Result{}, fmt.Errorf("population: resolve known-at: %w", err)
	}

	subjects, err := reader.Subjects(ctx, subject, asOf)
	if err != nil {
		return Result{}, fmt.Errorf("%w: subjects: %v", ErrFactReader, err)
	}
	watermark, err := reader.Watermark(ctx, subject)
	if err != nil {
		return Result{}, fmt.Errorf("%w: watermark: %v", ErrFactReader, err)
	}
	stale := watermark.Before(knownAt.Instant())

	members := make([]Member, 0, len(subjects))
	completeness := CompletenessComplete
	for _, subj := range subjects {
		if err := subj.Validate(); err != nil {
			members = append(members, Member{
				Subject: subj, Outcome: OutcomeUnknown,
				Obligations: []Obligation{{Reason: ObligationIdentityUnresolved}},
			})
			completeness = CompletenessPartial
			continue
		}
		result, obligations, err := evaluatePredicate(ctx, reader, subj, plan, plan.Criteria.Root, asOf, knownAt, stale)
		if err != nil {
			return Result{}, fmt.Errorf("population: evaluate %s: %w", subj, err)
		}
		m := Member{Subject: subj, Obligations: obligations}
		switch result {
		case ternaryTrue:
			m.Outcome = OutcomeIncluded
		case ternaryFalse:
			m.Outcome = OutcomeExcluded
		default:
			m.Outcome = OutcomeUnknown
			completeness = CompletenessPartial
		}
		members = append(members, m)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Subject.String() < members[j].Subject.String() })

	return Result{
		Plan:             plan,
		AsOf:             asOf,
		KnownAt:          knownAt,
		SourceWatermarks: map[SubjectKind]values.Instant{subject: watermark},
		Members:          members,
		Completeness:     completeness,
	}, nil
}

// Evaluate is CompiledPlan's half of the engine's Compile/Evaluate contract
// pair (ARCH-GO-009): it runs Resolve for this compiled plan. Resolve
// remains the primary, documented entry point - "resolve population
// membership" is this package's own domain verb, not "evaluate" - and
// Evaluate is an additive, symmetric method placed on CompiledPlan so a
// caller already holding a compiled plan does not have to pass it back
// into a free function to run it.
func (plan CompiledPlan) Evaluate(ctx context.Context, reader FactReader, subject SubjectKind, asOf values.Instant, knownAt values.KnownAt) (Result, error) {
	return Resolve(ctx, reader, subject, plan, asOf, knownAt)
}

// evaluatePredicate folds a predicate tree to a ternary result, collecting the
// obligations behind every Unknown leaf it touched on the winning path.
func evaluatePredicate(ctx context.Context, reader FactReader, subject values.EntityRef, plan CompiledPlan, p Predicate, asOf values.Instant, knownAt values.KnownAt, sourceStale bool) (ternary, []Obligation, error) {
	switch p.Kind {
	case PredicateAnd:
		var obligations []Obligation
		result := ternaryTrue
		for _, child := range p.Children {
			cr, obs, err := evaluatePredicate(ctx, reader, subject, plan, child, asOf, knownAt, sourceStale)
			if err != nil {
				return ternaryUnknown, nil, err
			}
			obligations = append(obligations, obs...)
			if cr == ternaryFalse {
				return ternaryFalse, obligations, nil
			}
			if cr == ternaryUnknown {
				result = ternaryUnknown
			}
		}
		return result, obligations, nil
	case PredicateOr:
		var obligations []Obligation
		result := ternaryFalse
		for _, child := range p.Children {
			cr, obs, err := evaluatePredicate(ctx, reader, subject, plan, child, asOf, knownAt, sourceStale)
			if err != nil {
				return ternaryUnknown, nil, err
			}
			obligations = append(obligations, obs...)
			if cr == ternaryTrue {
				return ternaryTrue, obligations, nil
			}
			if cr == ternaryUnknown {
				result = ternaryUnknown
			}
		}
		return result, obligations, nil
	case PredicateNot:
		cr, obs, err := evaluatePredicate(ctx, reader, subject, plan, p.Children[0], asOf, knownAt, sourceStale)
		if err != nil {
			return ternaryUnknown, nil, err
		}
		switch cr {
		case ternaryTrue:
			return ternaryFalse, obs, nil
		case ternaryFalse:
			return ternaryTrue, obs, nil
		default:
			return ternaryUnknown, obs, nil
		}
	default:
		return evaluateLeafFact(ctx, reader, subject, plan, p, asOf, knownAt, sourceStale)
	}
}

func evaluateLeafFact(ctx context.Context, reader FactReader, subject values.EntityRef, plan CompiledPlan, p Predicate, asOf values.Instant, knownAt values.KnownAt, sourceStale bool) (ternary, []Obligation, error) {
	desc, ok := plan.Fields[p.Field]
	if !ok {
		return ternaryUnknown, nil, fmt.Errorf("%w: %q was not part of the compiled plan", ErrUnresolvedField, p.Field)
	}
	fact, err := reader.ReadFact(ctx, subject, p.Field, asOf, knownAt)
	if err != nil {
		return ternaryUnknown, []Obligation{{Reason: ObligationSourceUnavailable, Field: p.Field}}, nil
	}
	if err := fact.Presence.Validate(); err != nil {
		return ternaryUnknown, nil, fmt.Errorf("%w: field %q: %v", ErrFactReader, p.Field, err)
	}

	// A fact the source claims to know only after the requested knowledge
	// boundary is a future-known correction: it must not change this
	// resolution. It is discarded rather than trusted, regardless of what
	// value it carries.
	if fact.Presence.IsValue() && fact.KnownAt.Instant().After(knownAt.Instant()) {
		return ternaryUnknown, []Obligation{{Reason: ObligationFutureKnowledge, Field: p.Field}}, nil
	}

	switch fact.Presence.State() {
	case values.PresenceValue:
		text, _ := fact.Presence.Get()
		match, err := evaluateLeaf(p, desc, text)
		if err != nil {
			return ternaryUnknown, nil, err
		}
		if match {
			return ternaryTrue, nil, nil
		}
		return ternaryFalse, nil, nil
	case values.PresenceUnavailable:
		return ternaryUnknown, []Obligation{{Reason: ObligationSourceUnavailable, Field: p.Field}}, nil
	case values.PresenceRedacted:
		return ternaryUnknown, []Obligation{{Reason: ObligationFactRedacted, Field: p.Field}}, nil
	case values.PresenceUnknown, values.PresenceAbsent, values.PresenceNull, values.PresenceNotApplicable:
		if sourceStale {
			return ternaryUnknown, []Obligation{{Reason: ObligationStaleWatermark, Field: p.Field}}, nil
		}
		return ternaryUnknown, []Obligation{{Reason: ObligationFactUnknown, Field: p.Field}}, nil
	default:
		return ternaryUnknown, nil, fmt.Errorf("%w: field %q has presence state %d", ErrFactReader, p.Field, fact.Presence.State())
	}
}
