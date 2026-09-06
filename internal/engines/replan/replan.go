// Package replan computes the smallest proposal invalidation set caused by a
// fresh snapshot. It is a pure engine: it does not mutate a proposal, dispatch
// an effect, or decide whether a successor revision should be created.
package replan

import (
	"errors"
	"fmt"
	"sort"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
	"github.com/monstercameron/hcm-next/internal/engines/fielddiff"
)

const schemaVersion = 1

// Errors returned for malformed declarations or diffs.
var (
	ErrInvalidDeclaration = errors.New("replan: invalid component declaration")
	ErrInvalidDiff        = errors.New("replan: invalid field diff")
)

// Classification declares whether drift in an input can change a component's
// material proposal content. Unspecified is intentionally retained as a
// non-material declaration: an unclassified dependency cannot be promoted to
// an invalidation by inference.
type Classification string

const (
	ClassificationUnspecified Classification = ""
	ClassificationMaterial    Classification = "MATERIAL"
	ClassificationInformative Classification = "INFORMATIVE"
)

// Materiality is an alias for callers that name the decision by its effect.
type Materiality = Classification

const (
	MaterialityUnspecified = ClassificationUnspecified
	MaterialityMaterial    = ClassificationMaterial
	MaterialityInformative = ClassificationInformative
)

// Dependency is one snapshot input consumed by a proposal component.
type Dependency struct {
	Input          string
	Classification Classification
}

// Component is a proposal component and its declared snapshot dependencies.
// Dependencies are a declaration, not a transitive graph: each component
// names the inputs that can materially change that component.
type Component struct {
	ID           string
	Dependencies []Dependency
}

// Declaration is the dependency declaration attached to a proposal revision.
type Declaration struct {
	ProposalRevisionID string
	Components         []Component
}

// FieldDiff is one result from comparing one baseline input with a fresh
// snapshot. An input absent from a declaration is preserved as unbound drift
// and never invalidates a component.
type FieldDiff struct {
	Input   string
	Outcome fielddiff.Outcome
}

// Drift is a descriptive alias for FieldDiff.
type Drift = FieldDiff

// Explanation identifies the exact declared input that invalidated a
// component. It contains no compared values.
type Explanation struct {
	ComponentID   string
	DriftedInputs []string
}

// Finding is the deterministic answer for one declared component.
type Finding struct {
	ComponentID   string
	Invalidated   bool
	DriftedInputs []string
	Explanation   Explanation
}

// Result is the minimal invalidation set and evidence for one replan check.
// Invalidated is sorted by component id; Findings contains every declared
// component, including unaffected ones.
type Result struct {
	ProposalRevisionID string
	Invalidated        []string
	Findings           []Finding
	UnboundDrift       []string
	Digest             string
}

// Compute returns the minimal invalidation set for declaration and diffs.
// Only a non-matching fielddiff outcome for a dependency declared MATERIAL
// invalidates its component. Unbound inputs and non-material dependencies are
// retained as evidence but do not invalidate anything.
func Compute(declaration Declaration, diffs []FieldDiff) (Result, error) {
	if err := validateDeclaration(declaration); err != nil {
		return Result{}, err
	}
	canonicalDiffs := append([]FieldDiff(nil), diffs...)
	sort.Slice(canonicalDiffs, func(i, j int) bool { return canonicalDiffs[i].Input < canonicalDiffs[j].Input })
	for _, diff := range canonicalDiffs {
		if diff.Input == "" {
			return Result{}, fmt.Errorf("%w: diff names no input", ErrInvalidDiff)
		}
		if err := diff.Outcome.Validate(); err != nil {
			return Result{}, fmt.Errorf("%w for %q: %v", ErrInvalidDiff, diff.Input, err)
		}
	}

	byInput := make(map[string]fielddiff.Outcome, len(canonicalDiffs))
	for _, diff := range canonicalDiffs {
		if prior, exists := byInput[diff.Input]; exists && prior != diff.Outcome {
			return Result{}, fmt.Errorf("%w: duplicate input %q has different outcomes", ErrInvalidDiff, diff.Input)
		}
		byInput[diff.Input] = diff.Outcome
	}
	result := Result{ProposalRevisionID: declaration.ProposalRevisionID}
	components := append([]Component(nil), declaration.Components...)
	sort.Slice(components, func(i, j int) bool { return components[i].ID < components[j].ID })
	for _, component := range components {
		var drifted []string
		for _, dependency := range component.Dependencies {
			outcome, driftedInput := byInput[dependency.Input]
			if !driftedInput || outcome.Relation == fielddiff.RelationMatch || dependency.Classification != ClassificationMaterial {
				continue
			}
			drifted = append(drifted, dependency.Input)
		}
		sort.Strings(drifted)
		finding := Finding{
			ComponentID:   component.ID,
			Invalidated:   len(drifted) > 0,
			DriftedInputs: append([]string(nil), drifted...),
		}
		if finding.Invalidated {
			finding.Explanation = Explanation{ComponentID: component.ID, DriftedInputs: append([]string(nil), drifted...)}
			result.Invalidated = append(result.Invalidated, component.ID)
		}
		result.Findings = append(result.Findings, finding)
	}
	for _, diff := range canonicalDiffs {
		if diff.Outcome.Relation == fielddiff.RelationMatch {
			continue
		}
		found := false
		for _, component := range components {
			for _, dependency := range component.Dependencies {
				if dependency.Input == diff.Input {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			result.UnboundDrift = append(result.UnboundDrift, diff.Input)
		}
	}
	sort.Strings(result.Invalidated)
	sort.Strings(result.UnboundDrift)
	result.Digest = resultDigest(declaration, canonicalDiffs, result)
	return result, nil
}

// Invalidate is a descriptive alias for Compute.
func Invalidate(declaration Declaration, diffs []FieldDiff) (Result, error) {
	return Compute(declaration, diffs)
}

// Explain returns one value-free explanation per invalidated component.
func (r Result) Explain() []Explanation {
	out := make([]Explanation, 0)
	for _, finding := range r.Findings {
		if finding.Invalidated {
			out = append(out, Explanation{ComponentID: finding.ComponentID, DriftedInputs: append([]string(nil), finding.DriftedInputs...)})
		}
	}
	return out
}

// ExplainResult is the functional form of Result.Explain.
func ExplainResult(r Result) []Explanation { return r.Explain() }

func validateDeclaration(declaration Declaration) error {
	seenComponents := make(map[string]struct{}, len(declaration.Components))
	for _, component := range declaration.Components {
		if component.ID == "" {
			return fmt.Errorf("%w: component names no id", ErrInvalidDeclaration)
		}
		if _, exists := seenComponents[component.ID]; exists {
			return fmt.Errorf("%w: duplicate component %q", ErrInvalidDeclaration, component.ID)
		}
		seenComponents[component.ID] = struct{}{}
		seenInputs := make(map[string]struct{}, len(component.Dependencies))
		for _, dependency := range component.Dependencies {
			if dependency.Input == "" {
				return fmt.Errorf("%w: component %q names no input", ErrInvalidDeclaration, component.ID)
			}
			if _, exists := seenInputs[dependency.Input]; exists {
				return fmt.Errorf("%w: component %q repeats input %q", ErrInvalidDeclaration, component.ID, dependency.Input)
			}
			seenInputs[dependency.Input] = struct{}{}
			if dependency.Classification != ClassificationUnspecified && dependency.Classification != ClassificationMaterial && dependency.Classification != ClassificationInformative {
				return fmt.Errorf("%w: component %q has unknown classification %q", ErrInvalidDeclaration, component.ID, dependency.Classification)
			}
		}
	}
	return nil
}

func resultDigest(declaration Declaration, diffs []FieldDiff, result Result) string {
	w := canonicalbytes.New("hcmnext.engines.replan.Result", schemaVersion).
		String("proposal_revision_id", declaration.ProposalRevisionID)
	components := append([]Component(nil), declaration.Components...)
	sort.Slice(components, func(i, j int) bool { return components[i].ID < components[j].ID })
	w.Count("components", len(components))
	for _, component := range components {
		n := canonicalbytes.New("hcmnext.engines.replan.Component", schemaVersion).String("id", component.ID)
		deps := append([]Dependency(nil), component.Dependencies...)
		sort.Slice(deps, func(i, j int) bool { return deps[i].Input < deps[j].Input })
		n.Count("dependencies", len(deps))
		for _, dependency := range deps {
			n.String("input", dependency.Input).String("classification", string(dependency.Classification))
		}
		w.Nested("component", n)
	}
	w.Count("diffs", len(diffs))
	for _, diff := range diffs {
		w.String("input", diff.Input).Nested("outcome", canonicalOutcome(diff.Outcome))
	}
	w.SortedStrings("invalidated", result.Invalidated).SortedStrings("unbound_drift", result.UnboundDrift)
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

func canonicalOutcome(outcome fielddiff.Outcome) *canonicalbytes.Writer {
	w := canonicalbytes.New("hcmnext.engines.replan.FieldDiff", schemaVersion)
	w.String("relation", outcome.Relation.String()).String("reason", outcome.Reason).Bool("ordered", outcome.Ordered)
	return w
}
