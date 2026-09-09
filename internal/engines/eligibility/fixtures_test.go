package eligibility_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const testTenant values.TenantId = "acme"

func subjectID(n int) string {
	return fmt.Sprintf("3f2504e0-4f89-41d3-9a0c-%012x", n)
}

func subject(n int) values.EntityRef {
	return values.EntityRef{Tenant: testTenant, Kind: "worker", Id: subjectID(n)}
}

// validRequest returns a fully-populated, publishable eligibility request for
// subject 1 against a fixture reward program.
func validRequest() eligibility.Request {
	return eligibility.Request{
		Subject: subject(1),
		SubjectMatter: eligibility.SubjectMatterRef{
			Kind: eligibility.SubjectMatterProgram, ID: "spot-bonus", Revision: "2026.1",
		},
		RequestedInterval: fixedInterval(),
		EffectiveInterval: fixedInterval(),
		Jurisdiction:      "US-FL",
		Snapshots: eligibility.Snapshots{
			PopulationSnapshotRef: "pop-snap-1",
			FactSnapshotRef:       "fact-snap-1",
			RuleSnapshotRef:       "rule-snap-1",
		},
		Purpose:   "reward_eligibility",
		Authority: "authz:role:rewards_admin",
	}
}

var fixedIntervalCache *values.EffectiveInterval

func fixedInterval() values.EffectiveInterval {
	if fixedIntervalCache != nil {
		return *fixedIntervalCache
	}
	start, _ := values.NewInstantFromUnix(1_000_000, 0)
	end, _ := values.NewInstantFromUnix(2_000_000, 0)
	iv, err := values.NewInstantInterval(start, end)
	if err != nil {
		panic(err)
	}
	fixedIntervalCache = &iv
	return iv
}

// testFactCatalog is the fixture fact catalog: grade and tenure are readable,
// ssn is present but not readable, standing in for a fact excluded from
// eligibility criteria entirely.
func testFactCatalog() eligibility.FactCatalog {
	return eligibility.FactCatalog{
		"grade":  {Type: eligibility.FieldTypeString, Readable: true},
		"tenure": {Type: eligibility.FieldTypeDecimal, Readable: true},
		"hired":  {Type: eligibility.FieldTypeDate, Readable: true},
		"ssn":    {Type: eligibility.FieldTypeString, Readable: false},
	}
}

// testRuleCatalog registers one pinned rule, "manager-attestation@1".
func testRuleCatalog() eligibility.RuleCatalog {
	return eligibility.RuleCatalog{
		"manager-attestation": {Version: "1"},
		"performance-rating":  {Version: "3"},
	}
}

func factEquals(field, value string) eligibility.Condition {
	return eligibility.Condition{Kind: eligibility.ConditionEquals, Field: field, Value: value}
}

func ruleCondition(id, version string) eligibility.Condition {
	return eligibility.Condition{Kind: eligibility.ConditionRule, RuleID: id, RuleVersion: version}
}

func and(children ...eligibility.Condition) eligibility.Condition {
	return eligibility.Condition{Kind: eligibility.ConditionAnd, Children: children}
}

func or(children ...eligibility.Condition) eligibility.Condition {
	return eligibility.Condition{Kind: eligibility.ConditionOr, Children: children}
}

// validCriteria: grade = P3 AND manager-attestation rule.
func validCriteria() eligibility.Criteria {
	return eligibility.Criteria{Root: and(
		factEquals("grade", "P3"),
		ruleCondition("manager-attestation", "1"),
	)}
}

func mustPlan(t testing.TB, criteria eligibility.Criteria) eligibility.CompiledPlan {
	t.Helper()
	plan, err := eligibility.Compile(criteria, testFactCatalog(), testRuleCatalog())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return plan
}

// fakeFacts is an in-memory FactReader fake keyed by subject|field.
type fakeFacts struct {
	values map[string]values.Presence[string]
	err    map[string]error
}

func newFakeFacts() *fakeFacts {
	return &fakeFacts{values: map[string]values.Presence[string]{}, err: map[string]error{}}
}

func (f *fakeFacts) with(subject values.EntityRef, field string, p values.Presence[string]) *fakeFacts {
	f.values[subject.String()+"|"+field] = p
	return f
}

func (f *fakeFacts) withErr(subject values.EntityRef, field string, err error) *fakeFacts {
	f.err[subject.String()+"|"+field] = err
	return f
}

func (f *fakeFacts) ReadFact(_ context.Context, subject values.EntityRef, field string, _ values.EffectiveInterval) (eligibility.Fact, error) {
	key := subject.String() + "|" + field
	if err, ok := f.err[key]; ok {
		return eligibility.Fact{}, err
	}
	if p, ok := f.values[key]; ok {
		return eligibility.Fact{Presence: p}, nil
	}
	return eligibility.Fact{Presence: values.Unknown[string]("no fact recorded")}, nil
}

// fakeRules is an in-memory RuleReader fake keyed by subject|ruleID@version.
type fakeRules struct {
	outcomes map[string]eligibility.RuleOutcome
	err      map[string]error
	calls    []string
}

func newFakeRules() *fakeRules {
	return &fakeRules{outcomes: map[string]eligibility.RuleOutcome{}, err: map[string]error{}}
}

func (r *fakeRules) with(subject values.EntityRef, ruleID, version string, outcome eligibility.RuleOutcome) *fakeRules {
	r.outcomes[subject.String()+"|"+ruleID+"@"+version] = outcome
	return r
}

func (r *fakeRules) withErr(subject values.EntityRef, ruleID, version string, err error) *fakeRules {
	r.err[subject.String()+"|"+ruleID+"@"+version] = err
	return r
}

func (r *fakeRules) EvaluateRule(_ context.Context, subject values.EntityRef, ruleID, version string, _ values.EffectiveInterval) (eligibility.RuleEvaluation, error) {
	key := subject.String() + "|" + ruleID + "@" + version
	r.calls = append(r.calls, key)
	if err, ok := r.err[key]; ok {
		return eligibility.RuleEvaluation{}, err
	}
	if o, ok := r.outcomes[key]; ok {
		return eligibility.RuleEvaluation{Outcome: o}, nil
	}
	return eligibility.RuleEvaluation{Outcome: eligibility.RuleOutcomeUnknown}, nil
}
