// Package todogovernance validates the planning backlog (planning/todos.md
// and its compiled registry at definitions/planning/todo-registry.json)
// against three governance rules:
//
//   - GOV-016: dependency edges must resolve, be free of prose, be acyclic,
//     and never point from an earlier declared phase to a later one.
//   - GOV-017: every todo must declare an explicit TDD contract (TEST,
//     TEST MATRIX with PRIMARY equal to TEST, RED, GREEN, REFACTOR) and a
//     checked-off todo must carry an Evidence line naming its TEST and a
//     `go test` result.
//   - GOV-025: every todo's INTENT CONTEXT must use a declared ROLE, a
//     declared SETS domain list, a DIRECT value that is either `none` or a
//     BusinessIntent name from the catalog, and a non-empty WHY.
//
// All three validators are pure functions over parsed data: no network, no
// mutation of planning/ or definitions/.
package todogovernance

import "fmt"

// Finding is one governance violation tied to a single todo.
type Finding struct {
	// TodoID is the offending todo's ID, or "" when the violation was
	// detected before an ID could be recovered (e.g. a structurally broken
	// block).
	TodoID string
	// Rule is the governance todo that owns this check: "GOV-016",
	// "GOV-017", or "GOV-025".
	Rule string
	// Code is a stable machine-readable violation code, e.g.
	// "UNRESOLVED_DEPENDENCY", "PHASE_INVERSION", "MISSING_EVIDENCE".
	Code string
	// Line is the todo's title line in the source markdown, when known.
	Line int
	// Detail is a secondary identifier that disambiguates multiple findings
	// of the same Code on the same todo, e.g. the specific dependency ID for
	// a GOV-016 edge finding, or the offending ROLE/DIRECT value for a
	// GOV-025 finding. Empty when the code is already a singleton per todo.
	Detail string
	// Reason is a human-readable explanation of the violation.
	Reason string
}

// String renders a finding in a stable "rule: id: code: reason (line N)"
// form used by the CLI and test failure output.
func (f Finding) String() string {
	id := f.TodoID
	if id == "" {
		id = "<unknown>"
	}
	if f.Line > 0 {
		return fmt.Sprintf("%s: %s: %s: %s (line %d)", f.Rule, id, f.Code, f.Reason, f.Line)
	}
	return fmt.Sprintf("%s: %s: %s: %s", f.Rule, id, f.Code, f.Reason)
}

// Key returns a stable identity for the finding suitable for allowlist
// comparison: it deliberately excludes Line and the free-text Reason since
// those may drift wording without changing the substance of the violation.
func (f Finding) Key() string {
	return f.Rule + "|" + f.TodoID + "|" + f.Code + "|" + f.Detail
}
