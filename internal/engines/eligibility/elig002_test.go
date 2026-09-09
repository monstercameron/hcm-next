package eligibility_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility"
)

// TestTodo_ELIG_002 proves the RED and GREEN clauses of planning/todos.md
// ELIG-002: an unresolved fact, a non-readable fact, an unregistered rule, an
// unpinned (stale) rule version, an unbounded tree and an unspecified
// composition all fail to compile, and a well-formed criteria compiles into a
// plan that lists its typed reads, rules, obligations and a digest.
func TestTodo_ELIG_002(t *testing.T) {
	facts := testFactCatalog()
	rules := testRuleCatalog()

	t.Run("RED_unresolved_fact_fails", func(t *testing.T) {
		criteria := eligibility.Criteria{Root: factEquals("nickname", "Bob")}
		_, err := eligibility.Compile(criteria, facts, rules)
		if !errors.Is(err, eligibility.ErrUnresolvedFact) {
			t.Fatalf("error = %v, want ErrUnresolvedFact", err)
		}
	})

	t.Run("RED_non_readable_fact_fails", func(t *testing.T) {
		criteria := eligibility.Criteria{Root: factEquals("ssn", "123-45-6789")}
		_, err := eligibility.Compile(criteria, facts, rules)
		if !errors.Is(err, eligibility.ErrFactNotReadable) {
			t.Fatalf("error = %v, want ErrFactNotReadable", err)
		}
	})

	t.Run("RED_unregistered_rule_fails", func(t *testing.T) {
		criteria := eligibility.Criteria{Root: ruleCondition("unregistered-rule", "1")}
		_, err := eligibility.Compile(criteria, facts, rules)
		if !errors.Is(err, eligibility.ErrUnresolvedRule) {
			t.Fatalf("error = %v, want ErrUnresolvedRule", err)
		}
	})

	t.Run("RED_unpinned_stale_rule_version_fails", func(t *testing.T) {
		// The catalog registers manager-attestation at version 1; citing any
		// other version is a stale/unpinned reference.
		criteria := eligibility.Criteria{Root: ruleCondition("manager-attestation", "2")}
		_, err := eligibility.Compile(criteria, facts, rules)
		if !errors.Is(err, eligibility.ErrRuleUnpinned) {
			t.Fatalf("error = %v, want ErrRuleUnpinned", err)
		}
	})

	t.Run("RED_unbounded_tree_fails_on_depth", func(t *testing.T) {
		deep := factEquals("grade", "P3")
		for i := 0; i < eligibility.MaxConditionDepth+2; i++ {
			deep = eligibility.Condition{Kind: eligibility.ConditionNot, Children: []eligibility.Condition{deep}}
		}
		_, err := eligibility.Compile(eligibility.Criteria{Root: deep}, facts, rules)
		if !errors.Is(err, eligibility.ErrUnboundedTree) {
			t.Fatalf("error = %v, want ErrUnboundedTree", err)
		}
	})

	t.Run("RED_unbounded_tree_fails_on_node_count", func(t *testing.T) {
		var wide []eligibility.Condition
		for i := 0; i < eligibility.MaxConditionNodes+2; i++ {
			wide = append(wide, factEquals("grade", "P3"))
		}
		_, err := eligibility.Compile(eligibility.Criteria{Root: eligibility.Condition{Kind: eligibility.ConditionOr, Children: wide}}, facts, rules)
		if !errors.Is(err, eligibility.ErrUnboundedTree) {
			t.Fatalf("error = %v, want ErrUnboundedTree", err)
		}
	})

	t.Run("RED_unspecified_composition_fails", func(t *testing.T) {
		cases := []eligibility.Condition{
			{Kind: eligibility.ConditionAnd},                                                                                  // composite with no children
			{Kind: eligibility.ConditionEquals, Field: "grade"},                                                               // fact leaf with no value
			{Kind: eligibility.ConditionRule, RuleID: "manager-attestation"},                                                  // rule leaf with no version
			{Kind: eligibility.ConditionEquals, Field: "grade", Value: "P3", RuleID: "manager-attestation", RuleVersion: "1"}, // fact leaf carrying rule fields
		}
		for i, c := range cases {
			if _, err := eligibility.Compile(eligibility.Criteria{Root: c}, facts, rules); err == nil {
				t.Fatalf("case %d: expected an unspecified/malformed composition to be rejected", i)
			}
		}
	})

	t.Run("GREEN_well_formed_criteria_compiles_a_complete_plan", func(t *testing.T) {
		plan, err := eligibility.Compile(validCriteria(), facts, rules)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if len(plan.FactsRead) != 1 || plan.FactsRead[0] != "grade" {
			t.Fatalf("facts read = %v, want [grade]", plan.FactsRead)
		}
		if len(plan.Rules) != 1 || plan.Rules[0].RuleID != "manager-attestation" || plan.Rules[0].Version != "1" {
			t.Fatalf("rules = %+v, want manager-attestation@1", plan.Rules)
		}
		if len(plan.Obligations) != 1 || plan.Obligations[0].RuleID != "manager-attestation" {
			t.Fatalf("obligations = %+v, want one for manager-attestation", plan.Obligations)
		}
		if !strings.HasPrefix(plan.Digest, "sha256:") {
			t.Fatalf("digest = %q, want a sha256: prefix", plan.Digest)
		}
	})
}

// TestTodo_ELIG_002_Property proves compilation is deterministic: compiling
// the same criteria twice produces byte-identical digests, and a criteria
// tree can never express a cycle because Condition is a Go value tree (a
// child is a copy, never a reference back to an ancestor).
func TestTodo_ELIG_002_Property(t *testing.T) {
	facts := testFactCatalog()
	rules := testRuleCatalog()
	criteria := validCriteria()

	first, err := eligibility.Compile(criteria, facts, rules)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	second, err := eligibility.Compile(criteria, facts, rules)
	if err != nil {
		t.Fatalf("Compile (repeat): %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("two compiles of identical criteria produced different digests")
	}

	// A deeply nested but still-bounded tree still compiles: acyclicity is
	// structural, not merely a runtime property this test happens to observe.
	// validCriteria's own tree is 2 levels deep (AND -> leaf); each wrap below
	// adds 2 more, so 3 wraps reaches depth 8, exactly at MaxConditionDepth.
	nested := criteria
	const wraps = 3
	for i := 0; i < wraps; i++ {
		nested = eligibility.Criteria{Root: eligibility.Condition{Kind: eligibility.ConditionNot, Children: []eligibility.Condition{
			{Kind: eligibility.ConditionNot, Children: []eligibility.Condition{nested.Root}},
		}}}
	}
	if _, err := eligibility.Compile(nested, facts, rules); err != nil {
		t.Fatalf("a deeply nested but bounded tree failed to compile: %v", err)
	}
}

// TestTodo_ELIG_002_Golden pins the compiled plan's declared reads for the
// fixture criteria.
func TestTodo_ELIG_002_Golden(t *testing.T) {
	plan, err := eligibility.Compile(validCriteria(), testFactCatalog(), testRuleCatalog())
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(plan.FactsRead) != 1 || plan.FactsRead[0] != "grade" {
		t.Fatalf("facts read = %v, want [grade]", plan.FactsRead)
	}
	if len(plan.Rules) != 1 || plan.Rules[0].RuleID != "manager-attestation" {
		t.Fatalf("rules = %+v, want [manager-attestation@1]", plan.Rules)
	}
}
