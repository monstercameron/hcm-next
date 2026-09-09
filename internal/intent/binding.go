package intent

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

// CoveredEntities is the entity set the data model declares covered: the
// entities the drafted definitions actually require. A binding that names an
// entity outside this set is naming exploratory vocabulary, and the coverage
// checker says so rather than accepting it.
func CoveredEntities() []string {
	return []string{
		"Assignment",
		"ApprovalBinding",
		"BudgetReservation",
		"CompensationComponent",
		"CompensationPackage",
		"ConnectorOperation",
		"Employment",
		"ExecutionBinding",
		"IntentInstance",
		"Observation",
		"OrganizationRelationship",
		"OrganizationUnit",
		"Person",
		"Position",
		"PositionOccupancy",
		"ProposalRevision",
		"RepairPlan",
		"Worker",
	}
}

// Binding ties one definition to exact model behaviour. Every reference here is
// a stable id or schema path; a binding never names a prose label.
type Binding struct {
	Definition Ref

	// AggregateRoots are the covered entities the definition's subjects resolve
	// to.
	AggregateRoots []string

	// ReadProperties and WriteProperties are schema paths. A CHANGE_REQUEST
	// with no write property is either mis-declared or is a process whose
	// children carry the writes; the checker requires the child bindings then.
	ReadProperties  []string
	WriteProperties []string

	// ChildDefinitions are the child intents a composite definition binds.
	ChildDefinitions []Ref

	// DecisionRefs, EvidenceRefs and EffectRefs name the decisions the
	// definition makes, the evidence it must produce, and the effects it may
	// cause. A zero-effect definition declares an empty effect list explicitly
	// rather than leaving the field unset.
	DecisionRefs []string
	EvidenceRefs []string
	EffectRefs   []string

	// Transitions are the lifecycle transitions the definition uses, on each of
	// the five dimensions.
	Transitions map[lifecycle.Dimension][]lifecycle.TransitionRule

	// NegativeStatePolicyRef is the shared policy the definition references.
	NegativeStatePolicyRef string

	// ScenarioRefs are the conformance scenarios that exercise the definition.
	ScenarioRefs []string
}

// Gap is one missing binding element for one definition.
type Gap struct {
	Definition Ref
	Element    string
	Detail     string
}

func (g Gap) String() string {
	return fmt.Sprintf("%s: %s (%s)", g.Definition, g.Element, g.Detail)
}

// CoverageReport is the answer to "is every definition bound to exact model
// behaviour?".
//
// Total is the checked-in definition count and nothing else: there is no
// separate denominator anywhere, so coverage can never be diluted by a
// vocabulary list.
type CoverageReport struct {
	Total int
	Bound int
	Gaps  []Gap
}

// Summary renders "N/N BOUND" or the exact gap list.
func (r CoverageReport) Summary() string {
	if len(r.Gaps) == 0 {
		return fmt.Sprintf("%d/%d BOUND", r.Bound, r.Total)
	}
	lines := make([]string, 0, len(r.Gaps)+1)
	lines = append(lines, fmt.Sprintf("%d/%d BOUND", r.Bound, r.Total))
	for _, g := range r.Gaps {
		lines = append(lines, "  gap: "+g.String())
	}
	return strings.Join(lines, "\n")
}

// FullyBound reports whether every definition is bound.
func (r CoverageReport) FullyBound() bool { return len(r.Gaps) == 0 && r.Bound == r.Total }

// CheckCoverage reports whether every published definition binds to exact model
// behaviour: subjects and aggregate roots inside the covered entity set,
// property reads and writes, decisions, evidence, an explicit effect list, a
// lifecycle transition on each of the five dimensions, a negative-state policy
// and at least one conformance scenario.
func CheckCoverage(reg *Registry, bindings []Binding) CoverageReport {
	covered := map[string]bool{}
	for _, e := range CoveredEntities() {
		covered[e] = true
	}
	byRef := make(map[Ref]Binding, len(bindings))
	for _, b := range bindings {
		byRef[b.Definition] = b
	}

	report := CoverageReport{Total: reg.Len()}
	for _, def := range reg.Definitions() {
		b, ok := byRef[def.Ref]
		if !ok {
			report.Gaps = append(report.Gaps, Gap{
				Definition: def.Ref,
				Element:    "binding",
				Detail:     "no IntentEntityBinding exists",
			})
			continue
		}
		gaps := checkBinding(def, b, covered)
		if len(gaps) == 0 {
			report.Bound++
			continue
		}
		report.Gaps = append(report.Gaps, gaps...)
	}
	sort.Slice(report.Gaps, func(i, j int) bool {
		if report.Gaps[i].Definition.TypeID != report.Gaps[j].Definition.TypeID {
			return report.Gaps[i].Definition.TypeID < report.Gaps[j].Definition.TypeID
		}
		return report.Gaps[i].Element < report.Gaps[j].Element
	})
	return report
}

func checkBinding(def Definition, b Binding, covered map[string]bool) []Gap {
	var gaps []Gap
	add := func(element, detail string) {
		gaps = append(gaps, Gap{Definition: def.Ref, Element: element, Detail: detail})
	}

	if len(def.SubjectKinds) == 0 {
		add("subject_kinds", "definition names no subject kind")
	}
	if len(b.AggregateRoots) == 0 {
		add("aggregate_roots", "binding names no aggregate root")
	}
	for _, root := range b.AggregateRoots {
		if !covered[root] {
			add("aggregate_roots", "root "+root+" is outside the covered entity set")
		}
	}
	if len(b.ReadProperties) == 0 {
		add("read_properties", "binding names no property read")
	}
	if def.Family == FamilyChangeRequest && len(b.WriteProperties) == 0 && len(b.ChildDefinitions) == 0 {
		add("write_properties",
			"a CHANGE_REQUEST binds either its own writes or the child intents that carry them")
	}
	if def.Family != FamilyChangeRequest && len(b.WriteProperties) > 0 {
		add("write_properties", "a "+def.Family.String()+" may not bind a property write")
	}
	if len(b.DecisionRefs) == 0 {
		add("decisions", "binding names no decision")
	}
	if len(b.EvidenceRefs) == 0 {
		add("evidence", "binding names no evidence requirement")
	}
	if def.ZeroEffect() && len(b.EffectRefs) > 0 {
		add("effects", "a ZERO_EFFECT definition binds an effect")
	}
	if !def.ZeroEffect() && len(b.EffectRefs) == 0 {
		add("effects", "a definition with a non-zero effect class binds no effect")
	}
	for _, dim := range lifecycle.AllDimensions() {
		if len(b.Transitions[dim]) == 0 {
			add("transitions", "binding declares no transition on "+string(dim))
		}
	}
	if b.NegativeStatePolicyRef == "" {
		add("negative_state_policy", "binding references no negative-state policy")
	} else if b.NegativeStatePolicyRef != def.NegativeStatePolicyRef {
		add("negative_state_policy",
			"binding references "+b.NegativeStatePolicyRef+
				" but the definition references "+def.NegativeStatePolicyRef)
	}
	if len(b.ScenarioRefs) == 0 {
		add("scenarios", "binding names no conformance scenario")
	}
	for _, child := range b.ChildDefinitions {
		if err := child.Validate(); err != nil {
			add("child_definitions", "child reference "+child.String()+" is malformed")
		}
	}
	if def.PopulationScope != nil && !slices.ContainsFunc(b.AggregateRoots, func(r string) bool {
		return covered[r]
	}) {
		add("population_scope", "a population-scoped definition binds no covered aggregate root")
	}
	return gaps
}
