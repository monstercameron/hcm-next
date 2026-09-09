// Ports Evaluate reads through. Eligibility never reads a database, a clock
// or an external service directly: every fact comes through FactReader and
// every rule outcome comes through RuleReader, both supplied by the caller.
package eligibility

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrPortFailed wraps every error a FactReader or RuleReader implementation
// returns.
var ErrPortFailed = errors.New("eligibility: read port failed")

// Fact is one field's bitemporal value for the subject under evaluation.
type Fact struct {
	Presence values.Presence[string]
}

// FactReader reads one subject's fact fields as of the request's effective
// interval.
type FactReader interface {
	ReadFact(ctx context.Context, subject values.EntityRef, field string, effective values.EffectiveInterval) (Fact, error)
}

// RuleOutcome is one rule's evaluated result, using the same PASS|FAIL|
// PARTIAL|UNKNOWN|NOT_APPLICABLE vocabulary as
// planning/data/models/rules-and-decisions.md's BusinessRuleEvaluation.output.
type RuleOutcome uint8

// Rule outcomes.
const (
	RuleOutcomeUnspecified RuleOutcome = iota
	RuleOutcomePass
	RuleOutcomeFail
	RuleOutcomePartial
	RuleOutcomeUnknown
	RuleOutcomeDenied
	RuleOutcomeNotApplicable
)

var ruleOutcomeWire = map[RuleOutcome]string{
	RuleOutcomePass:          "PASS",
	RuleOutcomeFail:          "FAIL",
	RuleOutcomePartial:       "PARTIAL",
	RuleOutcomeUnknown:       "UNKNOWN",
	RuleOutcomeDenied:        "DENIED",
	RuleOutcomeNotApplicable: "NOT_APPLICABLE",
}

// Valid reports whether o is a legal outcome.
func (o RuleOutcome) Valid() bool { return ruleOutcomeWire[o] != "" }

// String returns the wire token.
func (o RuleOutcome) String() string {
	if s, ok := ruleOutcomeWire[o]; ok {
		return s
	}
	return "RULE_OUTCOME_UNSPECIFIED"
}

// RuleEvaluation is one rule's outcome for the subject under evaluation.
type RuleEvaluation struct {
	Outcome RuleOutcome
}

// RuleReader evaluates one pinned rule version for subject as of the
// request's effective interval. RULE-001/RULE-003 own how a rule version is
// itself compiled and evaluated; this port only names the typed boundary
// eligibility reads across.
type RuleReader interface {
	EvaluateRule(ctx context.Context, subject values.EntityRef, ruleID, ruleVersion string, effective values.EffectiveInterval) (RuleEvaluation, error)
}
