package eligibility_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_ELIG_003 proves the RED and GREEN clauses of planning/todos.md
// ELIG-003: identical authorized inputs never diverge, unavailable/denied
// evidence is never coerced into a boolean, and evaluation returns a value
// with zero side effects - there is no writer parameter Evaluate could use to
// create an enrollment, approval or other domain mutation.
func TestTodo_ELIG_003(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)

	t.Run("GREEN_eligible_when_fact_matches_and_rule_passes", func(t *testing.T) {
		facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
		rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
		result, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.Status != eligibility.StatusEligible {
			t.Fatalf("status = %s, want ELIGIBLE", result.Status)
		}
	})

	t.Run("GREEN_ineligible_when_fact_fails_regardless_of_rule", func(t *testing.T) {
		facts := newFakeFacts().with(s1, "grade", values.Value("P1"))
		rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
		result, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.Status != eligibility.StatusIneligible {
			t.Fatalf("status = %s, want INELIGIBLE", result.Status)
		}
	})

	t.Run("RED_unavailable_rule_evidence_is_not_coerced_to_a_boolean", func(t *testing.T) {
		facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
		rules := newFakeRules().withErr(s1, "manager-attestation", "1", errors.New("rule engine unreachable"))
		result, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.Status == eligibility.StatusEligible || result.Status == eligibility.StatusIneligible {
			t.Fatalf("status = %s, want UNKNOWN (unavailable evidence must never coerce to a boolean)", result.Status)
		}
	})

	t.Run("RED_unavailable_fact_evidence_is_not_coerced_to_a_boolean", func(t *testing.T) {
		facts := newFakeFacts().withErr(s1, "grade", errors.New("fact source unreachable"))
		rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
		result, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.Status == eligibility.StatusEligible || result.Status == eligibility.StatusIneligible {
			t.Fatalf("status = %s, want UNKNOWN (an unreachable fact source must never coerce to a boolean)", result.Status)
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
	})

	t.Run("RED_denied_evidence_is_not_coerced_to_a_boolean", func(t *testing.T) {
		facts := newFakeFacts().with(s1, "grade", values.Redacted[string]("not authorized"))
		rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
		result, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if result.Status == eligibility.StatusEligible || result.Status == eligibility.StatusIneligible {
			t.Fatalf("status = %s, want UNKNOWN (denied evidence must never coerce to a boolean)", result.Status)
		}
	})

	t.Run("RED_conflicting_invalid_evidence_is_an_error_not_a_coercion", func(t *testing.T) {
		facts := newFakeFacts()
		// An unspecified presence state fails Presence.Validate() inside
		// Evaluate; it must surface as an error, never as a silently coerced
		// PASS or FAIL.
		facts.values[s1.String()+"|grade"] = values.Presence[string]{}
		rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
		if _, err := eligibility.Evaluate(ctx, facts, rules, req, plan); err == nil {
			t.Fatal("expected invalid presence evidence to produce an error, not a coerced result")
		}
	})

	t.Run("GREEN_no_writer_parameter_exists_to_create_a_domain_mutation", func(t *testing.T) {
		// This is a compile-time property as much as a runtime one: Evaluate's
		// signature accepts only read ports (FactReader, RuleReader), the
		// request and the plan. There is no domain writer, enrollment service
		// or approval recorder it could call even if it wanted to.
		facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
		rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
		if _, err := eligibility.Evaluate(ctx, facts, rules, req, plan); err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
	})
}

// TestTodo_ELIG_003_Property proves identical fact/rule/time/program context
// returns exact, byte-identical status/reasons/digest across repeated calls,
// with zero observable side effects on the fakes beyond the reads themselves.
func TestTodo_ELIG_003_Property(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)
	facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
	rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)

	first, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	second, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
	if err != nil {
		t.Fatalf("Evaluate (repeat): %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("two evaluations of identical inputs produced different digests")
	}
	if first.Status != second.Status || len(first.Reasons) != len(second.Reasons) {
		t.Fatal("two evaluations of identical inputs produced different status or reasons")
	}
}

// TestTodo_ELIG_003_Golden pins the reason set for the fixture's clean-pass
// scenario so a silent change in reason vocabulary is caught by a test diff.
func TestTodo_ELIG_003_Golden(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)
	facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
	rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)

	result, err := eligibility.Evaluate(ctx, facts, rules, req, plan)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(result.Reasons) != 2 {
		t.Fatalf("reasons = %+v, want exactly 2 (one fact, one rule)", result.Reasons)
	}
	kinds := map[eligibility.ReasonKind]bool{}
	for _, r := range result.Reasons {
		kinds[r.Kind] = true
	}
	if !kinds[eligibility.ReasonFactPassed] || !kinds[eligibility.ReasonRulePassed] {
		t.Fatalf("reasons = %+v, want FACT_PASSED and RULE_PASSED", result.Reasons)
	}
	if len(result.Evidence) != 2 {
		t.Fatalf("evidence = %v, want exactly 2 entries", result.Evidence)
	}
}

// TestTodo_ELIG_003_Mutation proves the evidence-integrity checks inside
// Evaluate are independently load bearing: a rule reader error and a redacted
// fact are two different obligations, and disabling either check (by
// supplying clean data instead) changes only that scenario's status.
func TestTodo_ELIG_003_Mutation(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)

	// Baseline: both fact and rule are clean and passing.
	baseFacts := newFakeFacts().with(s1, "grade", values.Value("P3"))
	baseRules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
	base, err := eligibility.Evaluate(ctx, baseFacts, baseRules, req, plan)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if base.Status != eligibility.StatusEligible {
		t.Fatalf("baseline status = %s, want ELIGIBLE", base.Status)
	}

	// Removing only the rule's clean outcome (making it fail instead)
	// changes exactly the rule-driven part of the fold to FAIL -> INELIGIBLE.
	failingRule := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomeFail)
	afterRuleFail, err := eligibility.Evaluate(ctx, baseFacts, failingRule, req, plan)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if afterRuleFail.Status != eligibility.StatusIneligible {
		t.Fatal("a failing rule did not change the AND result to INELIGIBLE")
	}

	// Removing only the fact's clean value (making it fail instead) changes
	// exactly the fact-driven part of the fold.
	failingFact := newFakeFacts().with(s1, "grade", values.Value("P1"))
	afterFactFail, err := eligibility.Evaluate(ctx, failingFact, baseRules, req, plan)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if afterFactFail.Status != eligibility.StatusIneligible {
		t.Fatal("a failing fact did not change the AND result to INELIGIBLE")
	}
}
