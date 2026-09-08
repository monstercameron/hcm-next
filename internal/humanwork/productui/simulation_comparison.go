// Package productui presents simulation comparisons. The
// view-as panel renders one server-projected simulation;
// this comparison presents two projections against each
// other without evaluating policy: localized outcomes with
// a change verdict, rule identifiers diffed into added,
// removed, and kept sets in projection order, and both
// version lines. Blank rule identifiers are ignored and
// duplicates collapse to first occurrence, mirroring the
// panel. The comparison carries the panel's no-authority
// notice so a delta never presents as permission.
package productui

import "strings"

// ComparedSimulation is the comparable projection of one
// simulated view: its subject, outcome, matched rule
// identifiers, and evaluated policy versions.
type ComparedSimulation struct {
	Subject  string
	Allowed  bool
	Rules    []string
	Versions []string
}

// SimulationComparison is the presented delta between two
// simulated views: localized outcomes with their change
// verdict, rule diffs, both version lines, and the
// no-authority notice.
type SimulationComparison struct {
	OutcomeBefore  string
	OutcomeAfter   string
	OutcomeChanged bool
	AddedRules     []string
	RemovedRules   []string
	KeptRules      []string
	VersionsBefore string
	VersionsAfter  string
	Notice         string
}

// CompareSimulations presents the delta from the before
// projection to the after projection. Projections are
// never mutated.
func CompareSimulations(locale LocaleContext, before, after ComparedSimulation) SimulationComparison {
	compared := SimulationComparison{
		OutcomeBefore:  simulationOutcome(locale, before.Allowed),
		OutcomeAfter:   simulationOutcome(locale, after.Allowed),
		OutcomeChanged: before.Allowed != after.Allowed,
		AddedRules:     []string{},
		RemovedRules:   []string{},
		KeptRules:      []string{},
		VersionsBefore: simulationVersions(before.Versions),
		VersionsAfter:  simulationVersions(after.Versions),
		Notice:         locale.Text("view_as.notice"),
	}
	beforeRules := simulationRuleSet(before.Rules)
	afterRules := simulationRuleSet(after.Rules)
	for _, rule := range simulationRuleList(after.Rules) {
		if !beforeRules[rule] {
			compared.AddedRules = append(compared.AddedRules, rule)
		}
	}
	for _, rule := range simulationRuleList(before.Rules) {
		if !afterRules[rule] {
			compared.RemovedRules = append(compared.RemovedRules, rule)
		} else {
			compared.KeptRules = append(compared.KeptRules, rule)
		}
	}
	return compared
}

// simulationOutcome renders one simulated outcome through
// the panel's catalog copy.
func simulationOutcome(locale LocaleContext, allowed bool) string {
	if allowed {
		return locale.Text("view_as.allowed")
	}
	return locale.Text("view_as.denied")
}

// simulationRuleList trims a projection's rule identifiers
// down to first occurrences, dropping blanks.
func simulationRuleList(rules []string) []string {
	seen := map[string]bool{}
	list := []string{}
	for _, rule := range rules {
		trimmed := strings.TrimSpace(rule)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		list = append(list, trimmed)
	}
	return list
}

// simulationRuleSet indexes one projection's rule
// identifiers for membership tests.
func simulationRuleSet(rules []string) map[string]bool {
	set := map[string]bool{}
	for _, rule := range simulationRuleList(rules) {
		set[rule] = true
	}
	return set
}

// simulationVersions joins one projection's versions the
// way the panel renders them.
func simulationVersions(versions []string) string {
	kept := []string{}
	for _, version := range versions {
		if trimmed := strings.TrimSpace(version); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return strings.Join(kept, ", ")
}
