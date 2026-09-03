// Package depthvocab enforces the implementation-depth vocabulary defined
// in execution-plan.md's Scope Rules (GOV-005): only IMPLEMENT, MINIMAL
// CONTRACT, DESIGN / CONFORMANCE ONLY and OUT OF PHASE may ever be used as
// a depth/staffing declaration. Artifact-maturity words (DRAFT_CONTRACT,
// CONTRACTED, PUBLISHED, DEPRECATED, RETIRED) and coverage words (DEFINED,
// PARTIAL, IMPLIED, MISSING, DEFERRED) describe different axes entirely,
// and legacy alternate wordings (`Phase 1 slice`, `CONTRACT ONLY`,
// `CONFORMANCE ONLY`, `Deferred`, `OUT`, ...) are translation aids for
// other documents, not values a depth declaration may use directly.
//
// REFACTOR note: this normalization must stay the single source of truth
// consumed by both planning and release checks; do not fork the vocabulary
// list into a second copy.
package depthvocab

import (
	"fmt"
	"regexp"
	"strings"
)

// CanonicalDepths is the exhaustive, ordered set of valid depth values.
var CanonicalDepths = []string{
	"IMPLEMENT",
	"MINIMAL CONTRACT",
	"DESIGN / CONFORMANCE ONLY",
	"OUT OF PHASE",
}

// crossAxisWords are maturity or coverage-axis words (and known legacy
// alternate wordings for depth) that must never appear as the literal
// value of a depth declaration, because doing so silently promotes a
// maturity/coverage classification into staffing authority.
var crossAxisWords = []string{
	// Coverage axis (specs/platform-capability-coverage-matrix.md).
	"DEFINED", "PARTIAL", "IMPLIED", "MISSING", "DEFERRED",
	// Artifact-maturity axis.
	"DRAFT_CONTRACT", "CONTRACTED", "PUBLISHED", "DEPRECATED", "RETIRED",
	// Legacy alternate wordings for depth (execution-plan.md alternate
	// wording table) - translation aids only, never literal values.
	"PHASE 1 SLICE", "GATE_A_IMPLEMENT", "GATE_B_IMPLEMENT",
	"PHASE 1 GATE", "CONTRACT ONLY", "MINIMAL CONTRACT ONLY",
	"CONFORMANCE ONLY", "DESIGN_CONFORMANCE_ONLY", "LONG-TERM ARCHITECTURE",
	"OUT",
}

var canonicalSet = func() map[string]bool {
	m := make(map[string]bool, len(CanonicalDepths))
	for _, d := range CanonicalDepths {
		m[strings.ToUpper(d)] = true
	}
	return m
}()

var crossAxisSet = func() map[string]bool {
	m := make(map[string]bool, len(crossAxisWords))
	for _, w := range crossAxisWords {
		m[strings.ToUpper(w)] = true
	}
	return m
}()

// declarationRe matches a bolded depth-declaration field marker in the
// planning-document convention "- **FIELD:** value", e.g.
// "- **Depth:** DEFINED" or "- **Implementation Depth:** OUT OF PHASE".
// It deliberately does not match "depth" used loosely in prose (e.g.
// "P1B at reduced depth: ...") - only the bolded field-marker form used
// throughout planning/todos.md counts as a declaration.
var declarationRe = regexp.MustCompile(`(?i)\*\*(implementation[- ]depth|depth|staffing)\s*:\*\*\s*([^.\n]+)`)

// Violation names one non-canonical depth declaration found in a document.
type Violation struct {
	Line  int
	Value string
	Issue string
}

func (v Violation) String() string {
	return fmt.Sprintf("line %d: %q: %s", v.Line, v.Value, v.Issue)
}

// CheckDepthDeclarations scans text for depth-declaration lines and
// rejects any whose value is not exactly one of CanonicalDepths.
// Cross-axis words and legacy alternate wordings are called out by name
// (silent promotion); anything else unrecognized is a generic rejection.
func CheckDepthDeclarations(text string) []Violation {
	var violations []Violation

	for i, line := range strings.Split(text, "\n") {
		m := declarationRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		value := strings.TrimSpace(strings.Trim(m[2], "*`"))
		value = strings.TrimSuffix(value, ".")
		upper := strings.ToUpper(value)

		if canonicalSet[upper] {
			continue
		}

		lineNum := i + 1
		if crossAxisSet[upper] {
			violations = append(violations, Violation{
				Line:  lineNum,
				Value: value,
				Issue: "silent promotion: an artifact-maturity/coverage word or legacy alternate wording used as a depth declaration",
			})
			continue
		}

		violations = append(violations, Violation{
			Line:  lineNum,
			Value: value,
			Issue: "not one of the four canonical implementation-depth values",
		})
	}

	return violations
}
