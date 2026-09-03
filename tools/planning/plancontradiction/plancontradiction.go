// Package plancontradiction detects planning documents that contradict the
// master-plan hierarchy (GOV-014): plan.md controls architectural
// invariants and long-term direction, execution-plan.md controls near-term
// Phase 1 sequencing and scope, and no other document may silently
// override either. Three concrete contradiction shapes are detected,
// matching execution-plan.md's Phase 1 Outcome (one Promotion +
// Compensation Change workflow), Explicit Non-Goals (messaging/omnichannel
// is never a Phase 1 mandatory capability), and plan.md 7.5 (the four
// other lifecycle reference workflows are architecture conformance tests,
// never product scope):
//
//   - a later spec claiming Phase 1 implements a reference workflow other
//     than Promotion + Compensation Change;
//   - a later spec claiming conditional/policy-governed messaging is
//     already implemented rather than a bounded Phase 1 capability or a
//     later-phase plane;
//   - a later spec claiming all reference workflows are product scope
//     rather than architecture conformance tests.
//
// REFACTOR note: suppressions must retain owner, rationale and expiry
// (mirroring definitions/planning/known-defects.yaml), never a bare
// allow-list entry.
package plancontradiction

import (
	"fmt"
	"regexp"
	"strings"
)

// Violation names one detected contradiction.
type Violation struct {
	Line int
	Rule string
	Text string
}

func (v Violation) String() string {
	return fmt.Sprintf("line %d: %s: %q", v.Line, v.Rule, v.Text)
}

var (
	// Rule A: execution-plan.md's Phase 1 Outcome is exactly one
	// Promotion + Compensation Change workflow; the other four lifecycle
	// reference workflows (plan.md 7.5 / 18) are conformance tests only.
	phaseOneBroadenedRe = regexp.MustCompile(`(?i)\bphase 1\b[^.]{0,100}\b(implements?|will implement|includes?|ships?|delivers?)\b[^.]{0,100}\b(Hire and Onboard|Cross-Company Transfer|Leave and Return|Termination and Offboarding|Payroll Correction)\b`)

	// Rule B: messaging/notification delivery is bounded/governed, not a
	// blanket Phase 1 capability (Explicit Non-Goals: "omnichannel/inbound
	// conversations, or bulk communications"; plan.md 18: the Messaging
	// and Notification Plane is a later capability built on governed
	// intent, not an already-shipped conditional trigger system).
	messagingImplementedRe = regexp.MustCompile(`(?i)\b(conditional messaging|messaging and notification plane)\b[^.]{0,80}\b(is implemented|has shipped|is production.?ready|is complete)\b`)

	// Rule C: plan.md 7.5 - the five lifecycle workflows and payroll-
	// correction stress test "remain architecture conformance tests, not
	// a Phase 1 feature roadmap."
	allWorkflowsProductScopeRe = regexp.MustCompile(`(?i)\ball (five )?reference workflows?\b[^.]{0,80}\b(product scope|are in scope|ship (in|for) phase 1|are phase 1)\b`)
)

// specFieldPrefixes are todo fields (RED/GREEN) that describe a checker's
// expected detections rather than assert planning scope themselves - e.g.
// GOV-014's own RED field names "treats all reference workflows as product
// scope" as the exact contradiction its test must catch. Lines starting
// with one of these (after trimming leading "- ") are not scanned.
var specFieldPrefixes = []string{"**RED:**", "**GREEN:**"}

func isSpecDescriptionLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "-")
	trimmed = strings.TrimSpace(trimmed)
	for _, p := range specFieldPrefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
}

// CheckPlanningContradictions scans text line by line and returns every
// contradiction found.
func CheckPlanningContradictions(text string) []Violation {
	var violations []Violation

	for i, line := range strings.Split(text, "\n") {
		lineNum := i + 1
		if isSpecDescriptionLine(line) {
			continue
		}

		if m := phaseOneBroadenedRe.FindString(line); m != "" {
			violations = append(violations, Violation{Line: lineNum, Rule: "later spec broadens Phase 1 beyond Promotion + Compensation Change", Text: m})
		}
		if m := messagingImplementedRe.FindString(line); m != "" {
			violations = append(violations, Violation{Line: lineNum, Rule: "conditional messaging marked implemented", Text: m})
		}
		if m := allWorkflowsProductScopeRe.FindString(line); m != "" {
			violations = append(violations, Violation{Line: lineNum, Rule: "all reference workflows treated as product scope", Text: m})
		}
	}

	return violations
}
