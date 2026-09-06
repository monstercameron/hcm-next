package productslice

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrUnknownReference is wrapped into a [Violation].Detail whenever a
// resolved field names a ref absent from its live registry.
var ErrUnknownReference = errors.New("productslice: unknown reference")

// Violation is one reason [ProductSliceDefinition.Validate] refuses a
// slice. Field always names the struct field the problem is in; Ref names
// the exact offending value when the problem is about one list entry (empty
// for a whole-field problem such as "no todos at all").
type Violation struct {
	Field  string
	Ref    string
	Detail string
}

// Error renders the violation as a single line naming the field and, when
// present, the exact ref that failed -- never a generic "invalid slice"
// message a reviewer would have to re-derive by hand.
func (v Violation) Error() string {
	if v.Ref == "" {
		return fmt.Sprintf("%s: %s", v.Field, v.Detail)
	}
	return fmt.Sprintf("%s %q: %s", v.Field, v.Ref, v.Detail)
}

// Valid reports whether violations is empty.
func Valid(violations []Violation) bool { return len(violations) == 0 }

// Validate resolves every reference field of d against reg -- the live
// [Registries] snapshot [LoadLiveRegistries] builds from the real
// registries planning/specs/default-product-slice-alignment.md names:
// business intents and features against
// definitions/governance/feature-intent-coverage.yaml (INTENT-010), pages
// against tools/uxqual/pagedef's published PageDefinitions, widgets against
// tools/uxqual/widgetreg's governed registry (WEB-005), capabilities
// against internal/capability's BOOTSTRAP registry (the same registry
// BIND-001's binding table binds to wire methods and Go handlers),
// packages against tools/policy/phaseonegate's live Phase 1 production
// closure (ARCH-GO-018), and todos against
// definitions/planning/todo-registry.json.
//
// This is the contract's "compiler": an unknown reference in any of the
// seven resolved lists, a missing slice id/version, an empty list, a
// duplicate ref within one list, or a missing jurisdiction/persona/exit
// criterion is refused by name -- the returned [Violation] identifies
// exactly which field and ref did not resolve. A d with zero violations is
// GREEN: every route/action it names has a business owner, a durable
// disposition and an admitted package, and every proof it cites actually
// exists.
func (d ProductSliceDefinition) Validate(reg Registries) []Violation {
	var violations []Violation

	if strings.TrimSpace(d.SliceID) == "" {
		violations = append(violations, Violation{Field: "slice_id", Detail: "slice id is required"})
	}
	if d.Version < 1 {
		violations = append(violations, Violation{Field: "version", Detail: "version must be >= 1"})
	}

	violations = append(violations, checkRefs("business_intents", d.BusinessIntents, reg.BusinessIntents)...)
	violations = append(violations, checkRefs("features", d.Features, reg.Features)...)
	violations = append(violations, checkRefs("pages", d.Pages, reg.Pages)...)
	violations = append(violations, checkRefs("widgets", d.Widgets, reg.Widgets)...)
	violations = append(violations, checkRefs("capabilities", d.Capabilities, reg.Capabilities)...)
	violations = append(violations, checkRefs("packages", d.Packages, reg.Packages)...)
	violations = append(violations, checkRefs("todos", d.Todos, reg.Todos)...)

	if len(d.Jurisdictions) == 0 {
		violations = append(violations, Violation{Field: "jurisdictions", Detail: "at least one jurisdiction is required"})
	}
	if len(d.Personas) == 0 {
		violations = append(violations, Violation{Field: "personas", Detail: "at least one persona is required"})
	}
	if len(d.ExitCriteria) == 0 {
		violations = append(violations, Violation{Field: "exit_criteria", Detail: "at least one exit criterion is required"})
	}

	return violations
}

// checkRefs proves one reference field: the field must be non-empty, every
// entry must be non-blank, no entry may repeat, and every entry must exist
// in known -- the live registry that field is resolved against. Nothing
// here special-cases which field is being checked, so a new resolved field
// costs one call, not a new code path.
func checkRefs(field string, refs []string, known map[string]bool) []Violation {
	var violations []Violation
	if len(refs) == 0 {
		violations = append(violations, Violation{Field: field, Detail: "at least one reference is required"})
		return violations
	}
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref) == "" {
			violations = append(violations, Violation{Field: field, Detail: "reference is empty"})
			continue
		}
		if seen[ref] {
			violations = append(violations, Violation{Field: field, Ref: ref, Detail: "duplicate reference"})
			continue
		}
		seen[ref] = true
		if !known[ref] {
			violations = append(violations, Violation{Field: field, Ref: ref, Detail: fmt.Sprintf("%s: not found in the live %s registry", ErrUnknownReference, field)})
		}
	}
	return violations
}

// ViolationStrings renders violations in a stable, deterministic order for
// diagnostics and test failure messages.
func ViolationStrings(violations []Violation) []string {
	out := make([]string, len(violations))
	for i, v := range violations {
		out[i] = v.Error()
	}
	sort.Strings(out)
	return out
}
