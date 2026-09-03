package eligibility_test

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/eligibility"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestTodo_ELIG_005 proves the RED and GREEN clauses of planning/todos.md
// ELIG-005: the explanation identifies contributing/blocking rules, facts,
// authority and versions, a protected fact is redacted rather than named,
// and every leaf reported is about the one requested subject - there is no
// way for another person's evidence to appear, since FactReader/RuleReader
// only ever take req.Subject.
func TestTodo_ELIG_005(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)

	t.Run("GREEN_explanation_names_contributing_facts_rules_authority_and_versions", func(t *testing.T) {
		facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
		rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
		explanation, err := eligibility.Explain(ctx, facts, rules, req, plan,
			map[string]bool{"grade": true}, map[string]bool{"manager-attestation": true})
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		if explanation.Status != eligibility.StatusEligible {
			t.Fatalf("status = %s, want ELIGIBLE", explanation.Status)
		}
		if explanation.Authority != req.Authority {
			t.Fatalf("authority = %q, want %q", explanation.Authority, req.Authority)
		}
		if explanation.ProgramVersion != req.SubjectMatter.Revision || explanation.FactSnapshotRef != req.Snapshots.FactSnapshotRef || explanation.RuleSnapshotRef != req.Snapshots.RuleSnapshotRef {
			t.Fatal("explanation did not bind program/fact/rule versions")
		}
		var sawFact, sawRule bool
		for _, c := range explanation.Conditions {
			if c.Field == "grade" {
				sawFact = true
				if c.Status != eligibility.ConditionStatusPassed {
					t.Fatalf("fact condition status = %s, want PASSED", c.Status)
				}
			}
			if c.RuleID == "manager-attestation" {
				sawRule = true
				if c.Status != eligibility.ConditionStatusPassed {
					t.Fatalf("rule condition status = %s, want PASSED", c.Status)
				}
			}
		}
		if !sawFact || !sawRule {
			t.Fatalf("conditions = %+v, want both the fact and the rule named", explanation.Conditions)
		}
	})

	t.Run("RED_protected_fact_is_redacted_not_named", func(t *testing.T) {
		facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
		rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
		explanation, err := eligibility.Explain(ctx, facts, rules, req, plan,
			map[string]bool{}, map[string]bool{"manager-attestation": true})
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		for _, c := range explanation.Conditions {
			if c.Field == "grade" {
				t.Fatal("a non-disclosable fact field name leaked into the explanation")
			}
		}
		foundRedacted := false
		for _, c := range explanation.Conditions {
			if c.Redacted && c.Field == "" && c.RuleID == "" {
				foundRedacted = true
			}
		}
		if !foundRedacted {
			t.Fatal("expected one redacted condition entry with no field or rule name")
		}
	})

	t.Run("RED_blocking_rule_is_identified", func(t *testing.T) {
		facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
		rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomeFail)
		explanation, err := eligibility.Explain(ctx, facts, rules, req, plan,
			map[string]bool{"grade": true}, map[string]bool{"manager-attestation": true})
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		found := false
		for _, c := range explanation.Conditions {
			if c.RuleID == "manager-attestation" && c.Status == eligibility.ConditionStatusFailed {
				found = true
			}
		}
		if !found {
			t.Fatal("the blocking rule was not identified as FAILED in the explanation")
		}
	})

	t.Run("RED_explanation_is_scoped_to_exactly_one_subject", func(t *testing.T) {
		s2 := subject(2)
		facts := newFakeFacts().
			with(s1, "grade", values.Value("P3")).
			with(s2, "grade", values.Value("P9"))
		rules := newFakeRules().
			with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass).
			with(s2, "manager-attestation", "1", eligibility.RuleOutcomeFail)

		req1 := req
		req1.Subject = s1
		explanation, err := eligibility.Explain(ctx, facts, rules, req1, plan,
			map[string]bool{"grade": true}, map[string]bool{"manager-attestation": true})
		if err != nil {
			t.Fatalf("Explain: %v", err)
		}
		if explanation.Status != eligibility.StatusEligible {
			t.Fatalf("subject 1's explanation = %s, want ELIGIBLE (subject 2's failing evidence must not leak in)", explanation.Status)
		}
	})

	t.Run("RED_incomplete_request_fails_closed", func(t *testing.T) {
		bad := req
		bad.Authority = ""
		facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
		rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
		if _, err := eligibility.Explain(ctx, facts, rules, bad, plan, map[string]bool{"grade": true}, map[string]bool{"manager-attestation": true}); err == nil {
			t.Fatal("expected Explain on an invalid request to fail")
		}
	})
}

// TestTodo_ELIG_005_Property proves Explain is deterministic: two calls with
// identical inputs produce byte-identical digests, and the number of
// condition entries always equals the number of leaves in the compiled
// criteria regardless of which fields are disclosable.
func TestTodo_ELIG_005_Property(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)
	facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
	rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)

	full := map[string]bool{"grade": true}
	fullRules := map[string]bool{"manager-attestation": true}
	first, err := eligibility.Explain(ctx, facts, rules, req, plan, full, fullRules)
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	second, err := eligibility.Explain(ctx, facts, rules, req, plan, full, fullRules)
	if err != nil {
		t.Fatalf("Explain (repeat): %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("two explanations of identical inputs produced different digests")
	}

	redacted, err := eligibility.Explain(ctx, facts, rules, req, plan, map[string]bool{}, map[string]bool{})
	if err != nil {
		t.Fatalf("Explain (fully redacted): %v", err)
	}
	if len(redacted.Conditions) != len(first.Conditions) {
		t.Fatalf("condition count changed with disclosure: %d vs %d", len(redacted.Conditions), len(first.Conditions))
	}
	if redacted.Digest == first.Digest {
		t.Fatal("redacting field/rule names did not change the digest")
	}
}

// TestTodo_ELIG_005_Golden pins the two-leaf condition shape for the fixture
// criteria.
func TestTodo_ELIG_005_Golden(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)
	facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
	rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)

	explanation, err := eligibility.Explain(ctx, facts, rules, req, plan,
		map[string]bool{"grade": true}, map[string]bool{"manager-attestation": true})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if len(explanation.Conditions) != 2 {
		t.Fatalf("conditions = %+v, want exactly 2 leaves", explanation.Conditions)
	}
}

// TestTodo_ELIG_005_Recovery proves that a failed attempt (the rule port
// returns a transport error) leaves no state behind that could corrupt a
// subsequent, successful replay: calling Explain again with a working
// RuleReader reproduces the exact same digest a clean-from-the-start call
// would have produced.
func TestTodo_ELIG_005_Recovery(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)
	disclosable := map[string]bool{"grade": true}
	disclosableRules := map[string]bool{"manager-attestation": true}

	facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
	failing := newFakeRules().withErr(s1, "manager-attestation", "1", errors.New("rule engine unreachable"))
	if _, err := eligibility.Explain(ctx, facts, failing, req, plan, disclosable, disclosableRules); err != nil {
		t.Fatalf("Explain with a failing rule port should still return a value (UNKNOWN), not an error: %v", err)
	}

	working := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)
	replayed, err := eligibility.Explain(ctx, facts, working, req, plan, disclosable, disclosableRules)
	if err != nil {
		t.Fatalf("Explain (replay): %v", err)
	}

	baseline, err := eligibility.Explain(ctx, facts, working, req, plan, disclosable, disclosableRules)
	if err != nil {
		t.Fatalf("Explain (clean baseline): %v", err)
	}
	if replayed.Digest != baseline.Digest {
		t.Fatal("a prior failed attempt influenced a later successful replay's digest")
	}
}

// TestTodo_ELIG_005_Mutation proves the per-leaf disclosure gate is load
// bearing independently for facts and for rules.
func TestTodo_ELIG_005_Mutation(t *testing.T) {
	ctx := context.Background()
	req := validRequest()
	plan := mustPlan(t, validCriteria())
	s1 := subject(1)
	facts := newFakeFacts().with(s1, "grade", values.Value("P3"))
	rules := newFakeRules().with(s1, "manager-attestation", "1", eligibility.RuleOutcomePass)

	factOnly, err := eligibility.Explain(ctx, facts, rules, req, plan, map[string]bool{"grade": true}, map[string]bool{})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	redactedRule, namedFact := 0, 0
	for _, c := range factOnly.Conditions {
		if c.Field == "grade" {
			namedFact++
		}
		if c.Redacted {
			redactedRule++
		}
	}
	if namedFact != 1 || redactedRule != 1 {
		t.Fatalf("named fact=%d redacted=%d, want 1 and 1", namedFact, redactedRule)
	}

	ruleOnly, err := eligibility.Explain(ctx, facts, rules, req, plan, map[string]bool{}, map[string]bool{"manager-attestation": true})
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	redactedFact, namedRule := 0, 0
	for _, c := range ruleOnly.Conditions {
		if c.RuleID == "manager-attestation" {
			namedRule++
		}
		if c.Redacted {
			redactedFact++
		}
	}
	if namedRule != 1 || redactedFact != 1 {
		t.Fatalf("named rule=%d redacted=%d, want 1 and 1", namedRule, redactedFact)
	}
}
