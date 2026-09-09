package evolution

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// Verdict is the compatibility outcome [CompatibilityCheck] returns.
type Verdict string

// Verdict values.
const (
	VerdictCompatible   Verdict = "COMPATIBLE"
	VerdictIncompatible Verdict = "INCOMPATIBLE"
)

// ChangeCode identifies one difference between two versions of one intent
// definition. A [Change] and a [Violation] share the same vocabulary: each
// code names one kind of difference, and whether an occurrence of it is
// recorded as a change or a violation is exactly what decides the verdict.
type ChangeCode string

// Compatible change codes: recorded as [Change] and never affect the
// verdict.
const (
	ChangeOptionalInputAdded   ChangeCode = "optional_input_added"
	ChangeOptionalInputRemoved ChangeCode = "optional_input_removed"
	ChangeInputBecameOptional  ChangeCode = "input_became_optional"
	ChangeEffectClassLowered   ChangeCode = "effect_class_lowered"
	ChangeApprovalAdded        ChangeCode = "approval_requirement_added"
)

// Incompatible change codes: recorded as [Violation] and force the verdict
// to INCOMPATIBLE. These are the only five ways a successor version may be
// incompatible with its predecessor.
const (
	ChangeRequiredInputAdded         ChangeCode = "required_input_added"
	ChangeRequiredInputRemoved       ChangeCode = "required_input_removed"
	ChangeInputTypeChanged           ChangeCode = "input_type_changed"
	ChangeEffectClassRaised          ChangeCode = "effect_class_raised"
	ChangeApprovalRequirementRemoved ChangeCode = "approval_requirement_removed"
)

// Change is one compatible difference, recorded for evidence even though it
// does not affect the verdict.
type Change struct {
	Code   ChangeCode `json:"code"`
	Field  string     `json:"field"`
	Detail string     `json:"detail"`
}

// Violation is one incompatible difference. Field always names the exact
// schema path, "effect_class" or "approval_required": a caller is never
// left to infer which field broke a transition from a bare boolean.
type Violation struct {
	Code   ChangeCode `json:"code"`
	Field  string     `json:"field"`
	Detail string     `json:"detail"`
}

// Report is the deterministic outcome of comparing two versions of one
// intent definition. A report is COMPATIBLE exactly when Violations is
// empty.
type Report struct {
	Previous   intent.Ref  `json:"previous"`
	Current    intent.Ref  `json:"current"`
	Verdict    Verdict     `json:"verdict"`
	Changes    []Change    `json:"changes,omitempty"`
	Violations []Violation `json:"violations,omitempty"`
}

// OK reports whether the transition is COMPATIBLE.
func (r Report) OK() bool { return r.Verdict == VerdictCompatible }

// CompatibilityCheck compares two versions of the same intent definition.
//
// Only a fixed, named set of differences make a transition INCOMPATIBLE: a
// required input added (including an existing optional input that became
// required), a required input removed, an input's declared kind changed,
// the effect class raised, or the approval requirement removed. Every other
// difference — most importantly a newly added optional input, or a
// previously required input that became optional — is COMPATIBLE and is
// recorded as a [Change] for evidence rather than silently dropped.
//
// previous and current must share an intent_type_id and current's version
// must be strictly greater than previous's, and both must already validate
// on their own terms; none of that is a compatibility question, so a
// violation of it returns an error rather than a report.
func CompatibilityCheck(previous, current intent.Definition) (Report, error) {
	if err := previous.Validate(); err != nil {
		return Report{}, fmt.Errorf("%w: previous %s: %v", ErrInvalidDefinition, previous.Ref, err)
	}
	if err := current.Validate(); err != nil {
		return Report{}, fmt.Errorf("%w: current %s: %v", ErrInvalidDefinition, current.Ref, err)
	}
	if previous.Ref.TypeID != current.Ref.TypeID {
		return Report{}, fmt.Errorf("%w: %s and %s", ErrDifferentIntentType, previous.Ref, current.Ref)
	}
	if current.Ref.Version <= previous.Ref.Version {
		return Report{}, fmt.Errorf("%w: %s does not advance %s", ErrVersionNotAdvancing, current.Ref, previous.Ref)
	}

	r := Report{Previous: previous.Ref, Current: current.Ref, Verdict: VerdictCompatible}

	prevInputs := indexInputs(previous.RequiredInputs)
	currInputs := indexInputs(current.RequiredInputs)

	for _, path := range sortedInputPaths(currInputs) {
		curr := currInputs[path]
		prev, existed := prevInputs[path]
		if !existed {
			if curr.Required {
				r.Violations = append(r.Violations, Violation{
					Code:  ChangeRequiredInputAdded,
					Field: path,
					Detail: fmt.Sprintf(
						"input %q was added as required; a caller bound to %s cannot satisfy it",
						path, previous.Ref),
				})
			} else {
				r.Changes = append(r.Changes, Change{
					Code:   ChangeOptionalInputAdded,
					Field:  path,
					Detail: fmt.Sprintf("input %q was added as optional", path),
				})
			}
			continue
		}
		if prev.Kind != curr.Kind {
			r.Violations = append(r.Violations, Violation{
				Code:  ChangeInputTypeChanged,
				Field: path,
				Detail: fmt.Sprintf("input %q changed kind from %s to %s",
					path, prev.Kind, curr.Kind),
			})
		}
		switch {
		case !prev.Required && curr.Required:
			r.Violations = append(r.Violations, Violation{
				Code:  ChangeRequiredInputAdded,
				Field: path,
				Detail: fmt.Sprintf(
					"input %q became required; a caller bound to %s cannot satisfy it",
					path, previous.Ref),
			})
		case prev.Required && !curr.Required:
			r.Changes = append(r.Changes, Change{
				Code:   ChangeInputBecameOptional,
				Field:  path,
				Detail: fmt.Sprintf("input %q became optional", path),
			})
		}
	}
	for _, path := range sortedInputPaths(prevInputs) {
		if _, exists := currInputs[path]; exists {
			continue
		}
		prev := prevInputs[path]
		if prev.Required {
			r.Violations = append(r.Violations, Violation{
				Code:   ChangeRequiredInputRemoved,
				Field:  path,
				Detail: fmt.Sprintf("required input %q was removed", path),
			})
		} else {
			r.Changes = append(r.Changes, Change{
				Code:   ChangeOptionalInputRemoved,
				Field:  path,
				Detail: fmt.Sprintf("optional input %q was removed", path),
			})
		}
	}

	if current.EffectClass > previous.EffectClass {
		r.Violations = append(r.Violations, Violation{
			Code:  ChangeEffectClassRaised,
			Field: "effect_class",
			Detail: fmt.Sprintf("effect class was raised from %s to %s",
				previous.EffectClass, current.EffectClass),
		})
	} else if current.EffectClass < previous.EffectClass {
		r.Changes = append(r.Changes, Change{
			Code:  ChangeEffectClassLowered,
			Field: "effect_class",
			Detail: fmt.Sprintf("effect class was lowered from %s to %s",
				previous.EffectClass, current.EffectClass),
		})
	}

	if previous.ApprovalRequired && !current.ApprovalRequired {
		r.Violations = append(r.Violations, Violation{
			Code:   ChangeApprovalRequirementRemoved,
			Field:  "approval_required",
			Detail: "the approval requirement was removed",
		})
	} else if !previous.ApprovalRequired && current.ApprovalRequired {
		r.Changes = append(r.Changes, Change{
			Code:   ChangeApprovalAdded,
			Field:  "approval_required",
			Detail: "the approval requirement was added",
		})
	}

	sort.Slice(r.Changes, func(i, j int) bool {
		return changeLess(r.Changes[i].Field, r.Changes[i].Code, r.Changes[i].Detail, r.Changes[j].Field, r.Changes[j].Code, r.Changes[j].Detail)
	})
	sort.Slice(r.Violations, func(i, j int) bool {
		return changeLess(r.Violations[i].Field, r.Violations[i].Code, r.Violations[i].Detail, r.Violations[j].Field, r.Violations[j].Code, r.Violations[j].Detail)
	})

	if len(r.Violations) > 0 {
		r.Verdict = VerdictIncompatible
	}
	return r, nil
}

func changeLess(fieldA string, codeA ChangeCode, detailA string, fieldB string, codeB ChangeCode, detailB string) bool {
	if fieldA != fieldB {
		return fieldA < fieldB
	}
	if codeA != codeB {
		return codeA < codeB
	}
	return detailA < detailB
}

func indexInputs(inputs []intent.RequiredInput) map[string]intent.RequiredInput {
	out := make(map[string]intent.RequiredInput, len(inputs))
	for _, in := range inputs {
		out[in.Path] = in
	}
	return out
}

func sortedInputPaths(m map[string]intent.RequiredInput) []string {
	paths := make([]string, 0, len(m))
	for p := range m {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}
