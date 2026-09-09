// Package scopeexchange implements the execution-plan.md Scope-Exchange
// Rule (GOV-006): any requirement promoted into Gate A or B must identify
// its gate owner, staff/schedule impact, dependency, acceptance evidence
// and the equivalent scope removed or deferred, and record the resulting
// change to the critical path plus an approval digest. A requirement's
// long-term contract being `DEFINED` never makes a new subsystem mandatory
// on its own.
//
// REFACTOR note: a scope exchange reuses the delivery-manifest identity
// (GOV-001) for its requirement instead of re-declaring owner, estimate,
// dependency and acceptance-evidence fields under a second, free-form
// vocabulary.
package scopeexchange

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/manifest"
)

// gatedGates are the only gates the Scope-Exchange Rule applies to. Gate C
// and P0/other phases are unconstrained by this rule.
var gatedGates = map[string]bool{
	"GATE_A": true,
	"GATE_B": true,
}

// Exchange is one scope-exchange record for a requirement promoted into
// Gate A or B.
type Exchange struct {
	Requirement         manifest.Manifest
	ChangedCriticalPath string
	ApprovalDigest      string
}

// Violation names one missing element of an approved scope exchange.
type Violation struct {
	ID    string
	Field string
	Issue string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s: %s", v.ID, v.Field, v.Issue)
}

var manifestFieldToExchangeIssue = map[string]string{
	"owner":               "missing gate owner",
	"estimate":            "missing staff/schedule impact",
	"dependencies":        "missing dependency",
	"acceptance_evidence": "missing acceptance evidence",
	"displaced_scope":     "missing equivalent scope removed or deferred",
}

// Validate returns every Scope-Exchange Rule violation for ex.Requirement's
// Gate. Gates outside Gate A/B are not subject to this rule and always
// return nil. "gate" itself is not required here beyond selecting
// applicability: a manifest missing its own Gate field is still flagged by
// manifest.Validate, which callers should also run.
func Validate(ex Exchange) []Violation {
	if !gatedGates[ex.Requirement.Gate] {
		return nil
	}

	var violations []Violation
	id := ex.Requirement.ID

	for _, mv := range manifest.Validate(ex.Requirement) {
		if mv.Field == "gate" {
			// Already selected as GATE_A/GATE_B; irrelevant here.
			continue
		}
		issue, known := manifestFieldToExchangeIssue[mv.Field]
		if !known {
			issue = mv.Issue
		}
		violations = append(violations, Violation{ID: id, Field: mv.Field, Issue: issue})
	}

	if ex.ChangedCriticalPath == "" {
		violations = append(violations, Violation{ID: id, Field: "changed_critical_path", Issue: "approved exchange must record the changed critical path"})
	}
	if ex.ApprovalDigest == "" {
		violations = append(violations, Violation{ID: id, Field: "approval_digest", Issue: "approved exchange must record an approval digest"})
	}

	return violations
}
