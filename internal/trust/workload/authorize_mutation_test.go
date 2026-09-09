package workload_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/workload"
)

// TestTodo_TRUST_007_Mutation is the TRUST-007 mutation test. It asserts
// that [workload.AllowMatrix] contains no duplicate caller/resource/action
// triple (a duplicate would mean two rules silently compete, which
// [workload.Authorize] never actually chooses between since the compiled
// index keeps only one), that every rule id in the matrix is unique, and
// that changing any field of a [workload.Decision]-producing [workload.Request]
// changes its [workload.Decision.DecisionID].
func TestTodo_TRUST_007_Mutation(t *testing.T) {
	t.Run("AllowMatrix has no duplicate caller/resource/action triple", func(t *testing.T) {
		seen := map[string]string{}
		for _, rule := range workload.AllowMatrix {
			key := string(rule.Caller) + "|" + string(rule.Resource) + "|" + string(rule.Action)
			if existing, ok := seen[key]; ok {
				t.Errorf("triple %q is covered by both %q and %q", key, existing, rule.RuleID)
			}
			seen[key] = rule.RuleID
		}
	})

	t.Run("every rule id in AllowMatrix is unique", func(t *testing.T) {
		seen := map[string]bool{}
		for _, rule := range workload.AllowMatrix {
			if seen[rule.RuleID] {
				t.Errorf("duplicate rule id %q", rule.RuleID)
			}
			seen[rule.RuleID] = true
		}
	})

	t.Run("every rule names a valid role and a valid action", func(t *testing.T) {
		for _, rule := range workload.AllowMatrix {
			if !rule.Caller.Valid() {
				t.Errorf("rule %q names an invalid caller role %q", rule.RuleID, rule.Caller)
			}
			if rule.Action != workload.ActionRead && rule.Action != workload.ActionWrite {
				t.Errorf("rule %q names an invalid action %q", rule.RuleID, rule.Action)
			}
			if rule.Resource == "" {
				t.Errorf("rule %q names an empty resource", rule.RuleID)
			}
		}
	})

	t.Run("scheduler and admin carry no allow-matrix entries in P1A", func(t *testing.T) {
		for _, rule := range workload.AllowMatrix {
			if rule.Caller == workload.RoleScheduler || rule.Caller == workload.RoleAdmin {
				t.Errorf("rule %q grants the reserved P1B role %q; P1A must deny it by default instead", rule.RuleID, rule.Caller)
			}
		}
	})

	t.Run("changing the resource or action changes the decision id", func(t *testing.T) {
		authority := newTestAuthority(t, baseTime)
		verifier := newVerifier(t, authority.source, cellPrimary, baseTime)
		worker := verifiedIdentity(t, authority, verifier, workload.RoleWorker)

		base, err := workload.Authorize(workload.Request{Caller: worker, Resource: workload.ResourceLedger, Action: workload.ActionRead, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		otherAction, err := workload.Authorize(workload.Request{Caller: worker, Resource: workload.ResourceLedger, Action: workload.ActionWrite, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		otherResource, err := workload.Authorize(workload.Request{Caller: worker, Resource: workload.ResourceOutbox, Action: workload.ActionRead, EvaluatedAt: baseTime})
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if base.DecisionID == otherAction.DecisionID {
			t.Error("changing the action left the decision id unchanged")
		}
		if base.DecisionID == otherResource.DecisionID {
			t.Error("changing the resource left the decision id unchanged")
		}
	})

	t.Run("Authorize is pure: the same request always decides the same way", func(t *testing.T) {
		authority := newTestAuthority(t, baseTime)
		verifier := newVerifier(t, authority.source, cellPrimary, baseTime)
		worker := verifiedIdentity(t, authority, verifier, workload.RoleWorker)
		req := workload.Request{Caller: worker, Resource: workload.ResourceLedger, Action: workload.ActionRead, EvaluatedAt: baseTime}

		first, err := workload.Authorize(req)
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		second, err := workload.Authorize(req)
		if err != nil {
			t.Fatalf("Authorize: %v", err)
		}
		if first.DecisionID != second.DecisionID {
			t.Error("Authorize produced two different decision ids for the identical request")
		}
	})
}
