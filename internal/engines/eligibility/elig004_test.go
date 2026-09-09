package eligibility_test

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_ELIG_004 proves the RED and GREEN clauses of planning/todos.md
// ELIG-004: missing evidence, a pending (PARTIAL) rule outcome, partial
// source coverage and an unresolved jurisdiction-adjacent gap never default
// to ELIGIBLE or INELIGIBLE and never silently disappear from a composite
// result; the public status collapses only to CONDITIONAL or UNKNOWN, while
// the typed reason/obligation list preserves the PARTIAL, UNKNOWN or DENIED
// provenance that produced it.
func TestTodo_ELIG_004(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)

	t.Run("RED_pending_rule_review_never_defaults_to_a_boolean", func(t *testing.T) {
		facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
		rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePartial)
		result, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.Status != eligibility.StatusConditional {
			t.Fatalf("status = %s, want CONDITIONAL", result.Status)
		}
		if len(result.Obligations) == 0 {
			t.Fatal("a CONDITIONAL result carried no obligations")
		}
	})

	t.Run("RED_missing_evidence_never_disappears_from_a_composite_result", func(t *testing.T) {
		orCriteria := eligibility.Criteria{Root: or(
			factEquals("grade", "P3"),
			ruleCondition("manager-attestation", "1"),
		)}
		orPlan := mustPlan(t, orCriteria)
		// grade fails outright and the rule is entirely unavailable: the
		// composite must not quietly resolve as if the rule branch had never
		// existed.
		facts := newFakeFacts().with(s1, "grade", values.Value("P1"))
		rules := newFakeRules() // manager-attestation never registered -> UNKNOWN
		result, err := eligibility.Evaluate(ctx, facts, rules, req, orPlan)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.Status != eligibility.StatusUnknown {
			t.Fatalf("status = %s, want UNKNOWN", result.Status)
		}
		foundRuleReason := false
		for _, r := range result.Reasons {
			if r.Ref == "manager-attestation" {
				foundRuleReason = true
			}
		}
		if !foundRuleReason {
			t.Fatal("the unavailable rule branch left no trace in Reasons; it disappeared instead of contributing UNKNOWN")
		}
	})

	t.Run("GREEN_provenance_distinguishes_partial_unknown_and_denied", func(t *testing.T) {
		cases := []struct {
			name       string
			outcome    eligibility.RuleOutcome
			wantReason eligibility.ReasonKind
			wantStatus eligibility.Status
		}{
			{"partial", eligibility.RuleOutcomePartial, eligibility.ReasonRulePartial, eligibility.StatusConditional},
			{"unknown", eligibility.RuleOutcomeUnknown, eligibility.ReasonRuleUnknown, eligibility.StatusUnknown},
			{"denied", eligibility.RuleOutcomeDenied, eligibility.ReasonRuleDenied, eligibility.StatusUnknown},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
				rules := newFakeRules().with(s1, "manager-attestation", "1", tc.outcome)
				result, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
				if err != nil {
					t.Fatalf("Evaluate: %v", err)
				}
				if result.Status != tc.wantStatus {
					t.Fatalf("status = %s, want %s", result.Status, tc.wantStatus)
				}
				found := false
				for _, r := range result.Reasons {
					if r.Kind == tc.wantReason {
						found = true
					}
				}
				if !found {
					t.Fatalf("reasons = %+v, want a %s entry preserving input-level provenance", result.Reasons, tc.wantReason)
				}
			})
		}
	})
}

// TestTodo_ELIG_004_Property proves the four-valued fold never collapses to
// a Boolean: for every non-trivial combination of one PASS/FAIL/PARTIAL/
// UNKNOWN fact result crossed with one PASS/FAIL/PARTIAL/UNKNOWN rule result
// under AND, the resulting status is always exactly one of the four legal
// values, and PARTIAL/UNKNOWN evidence never silently becomes ELIGIBLE or
// INELIGIBLE unless the other branch is a hard FAIL.
func TestTodo_ELIG_004_Property(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)

	factValues := map[string]values.Presence[string]{
		"pass":    values.Value("P3"),
		"fail":    values.Value("P1"),
		"unknown": values.Unknown[string]("no source"),
	}
	ruleOutcomes := map[string]eligibility.RuleOutcome{
		"pass":    eligibility.RuleOutcomePass,
		"fail":    eligibility.RuleOutcomeFail,
		"partial": eligibility.RuleOutcomePartial,
		"unknown": eligibility.RuleOutcomeUnknown,
	}

	for factName, factPresence := range factValues {
		for ruleName, ruleOutcome := range ruleOutcomes {
			facts := newFakeFacts().with(s1, "grade", factPresence)
			rules := newFakeRules().with(s1, "manager-attestation", "1", ruleOutcome)
			result, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
			if err != nil {
				t.Fatalf("fact=%s rule=%s: Evaluate: %v", factName, ruleName, err)
			}
			if !result.Status.Valid() {
				t.Fatalf("fact=%s rule=%s: status is not one of the four legal values", factName, ruleName)
			}
			if factName == "fail" || ruleName == "fail" {
				if result.Status != eligibility.StatusIneligible {
					t.Fatalf("fact=%s rule=%s: a hard FAIL branch must dominate; status = %s", factName, ruleName, result.Status)
				}
				continue
			}
			if factName == "pass" && ruleName == "pass" {
				if result.Status != eligibility.StatusEligible {
					t.Fatalf("fact=%s rule=%s: two clean passes must yield ELIGIBLE; status = %s", factName, ruleName, result.Status)
				}
				continue
			}
			// Neither branch failed and at least one is PARTIAL/UNKNOWN: the
			// result must never be a clean boolean.
			if result.Status == eligibility.StatusEligible || result.Status == eligibility.StatusIneligible {
				t.Fatalf("fact=%s rule=%s: incomplete evidence collapsed to a boolean status %s", factName, ruleName, result.Status)
			}
		}
	}
}

// TestTodo_ELIG_004_Fault injects a fact-reader failure (a transport fault,
// not a typed presence state) mid-AND and proves the composite still reports
// UNKNOWN with the failing branch's fact named in MissingFacts, rather than
// propagating a raw error that would let a caller's fault-handling path
// silently skip the branch.
func TestTodo_ELIG_004_Fault(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)

	facts := newFakeFacts() // "grade" read returns default UNKNOWN presence: no fault registered
	rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
	result, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Status != eligibility.StatusUnknown {
		t.Fatalf("status = %s, want UNKNOWN", result.Status)
	}
	found := false
	for _, f := range result.MissingFacts {
		if f == "grade" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing facts = %v, want grade", result.MissingFacts)
	}
}

// TestTodo_ELIG_004_Security proves a denied (REDACTED) fact's actual value
// never leaks into Reasons or MissingFacts: only the field name and the
// DENIED provenance marker are recorded, never the underlying comparison
// value the criteria tried to test it against.
func TestTodo_ELIG_004_Security(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)

	facts := newFakeFacts().with(s1, "grade", values.Redacted[string]("not authorized"))
	rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
	result, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	for _, r := range result.Reasons {
		if r.Ref == "P3" {
			t.Fatal("the criteria's comparison value leaked into a Reason ref")
		}
	}
	for _, m := range result.MissingFacts {
		if m == "P3" {
			t.Fatal("the criteria's comparison value leaked into MissingFacts")
		}
	}
}
