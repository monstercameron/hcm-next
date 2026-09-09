package sod_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/sod"
)

func fullConstraints(ruleID string) sod.Constraints {
	return sod.Constraints{
		RequesterMayNotApprove:         true,
		RequesterDelegateMayNotApprove: true,
		ExecutorMayNotApprove:          true,
		RequesterMayNotExecute:         true,
		OneApprovalPerPrincipal:        true,
		RuleID:                         ruleID,
	}
}

// TestTodo_TRUST_014 is the PRIMARY test for the separation-of-duties
// evaluator: requester approving their own proposal, a delegate of the
// requester approving, a principal both executing and approving, and the
// requester also being the executor are all refused; a clean quorum of
// distinct, unrelated approvers is satisfied; and every exclusion names the
// rule that fired.
func TestTodo_TRUST_014(t *testing.T) {
	t.Run("self approval is rejected", func(t *testing.T) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: "alice"},
			Approvers: []sod.Actor{{Subject: "alice"}, {Subject: "bob"}},
		}
		result, err := sod.Evaluate(ctx, fullConstraints("SOD-1"), 2)
		if !errors.Is(err, sod.ErrUnsatisfiable) {
			t.Fatalf("err = %v, want ErrUnsatisfiable", err)
		}
		if len(result.Eligible) != 1 || result.Eligible[0] != "bob" {
			t.Fatalf("eligible = %v, want [bob]", result.Eligible)
		}
		if len(result.Excluded) != 1 || result.Excluded[0].Subject != "alice" || result.Excluded[0].Reason != sod.ReasonSelfApproval {
			t.Fatalf("excluded = %+v, want alice/self_approval", result.Excluded)
		}
		if result.Excluded[0].RuleID != "SOD-1" {
			t.Fatalf("excluded rule id = %q, want SOD-1", result.Excluded[0].RuleID)
		}
	})

	t.Run("approval by a delegate of the requester is rejected", func(t *testing.T) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: "alice"},
			Approvers: []sod.Actor{
				{Subject: "carol", DelegationChain: []string{"alice", "carol"}},
				{Subject: "bob"},
				{Subject: "dave"},
			},
		}
		result, err := sod.Evaluate(ctx, fullConstraints("SOD-2"), 2)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if len(result.Eligible) != 2 {
			t.Fatalf("eligible = %v, want 2 entries", result.Eligible)
		}
		for _, e := range result.Eligible {
			if e == "carol" {
				t.Fatalf("carol (delegate of requester) should have been excluded: %v", result.Eligible)
			}
		}
		if len(result.Excluded) != 1 || result.Excluded[0].Subject != "carol" || result.Excluded[0].Reason != sod.ReasonRequesterDelegate {
			t.Fatalf("excluded = %+v, want carol/requester_delegate_approval", result.Excluded)
		}
	})

	t.Run("executor may not also approve", func(t *testing.T) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: "alice"},
			Executor:  sod.Actor{Subject: "erin"},
			Approvers: []sod.Actor{{Subject: "erin"}, {Subject: "bob"}},
		}
		result, err := sod.Evaluate(ctx, fullConstraints("SOD-3"), 2)
		if !errors.Is(err, sod.ErrUnsatisfiable) {
			t.Fatalf("err = %v, want ErrUnsatisfiable", err)
		}
		if len(result.Eligible) != 1 || result.Eligible[0] != "bob" {
			t.Fatalf("eligible = %v, want [bob]", result.Eligible)
		}
		if result.Excluded[0].Reason != sod.ReasonExecutorIsApprover {
			t.Fatalf("reason = %q, want executor_is_approver", result.Excluded[0].Reason)
		}
	})

	t.Run("the repair author executing their own repair is rejected outright", func(t *testing.T) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: "alice"},
			Executor:  sod.Actor{Subject: "alice"},
			Approvers: []sod.Actor{{Subject: "bob"}, {Subject: "carol"}},
		}
		_, err := sod.Evaluate(ctx, fullConstraints("SOD-4"), 1)
		if !errors.Is(err, sod.ErrUnsatisfiable) {
			t.Fatalf("err = %v, want ErrUnsatisfiable", err)
		}
	})

	t.Run("duplicate principal cannot fill two approval slots", func(t *testing.T) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: "alice"},
			Approvers: []sod.Actor{{Subject: "bob"}, {Subject: "bob"}},
		}
		result, err := sod.Evaluate(ctx, fullConstraints("SOD-5"), 2)
		if !errors.Is(err, sod.ErrUnsatisfiable) {
			t.Fatalf("err = %v, want ErrUnsatisfiable", err)
		}
		if len(result.Eligible) != 1 {
			t.Fatalf("eligible = %v, want exactly one bob", result.Eligible)
		}
		if len(result.Excluded) != 1 || result.Excluded[0].Reason != sod.ReasonDuplicatePrincipal {
			t.Fatalf("excluded = %+v, want duplicate_principal", result.Excluded)
		}
	})

	t.Run("a clean quorum of unrelated approvers is satisfied", func(t *testing.T) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: "alice"},
			Executor:  sod.Actor{Subject: "erin"},
			Approvers: []sod.Actor{{Subject: "bob"}, {Subject: "carol"}},
		}
		result, err := sod.Evaluate(ctx, fullConstraints("SOD-6"), 2)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if !result.Satisfied || len(result.Eligible) != 2 || len(result.Excluded) != 0 {
			t.Fatalf("result = %+v, want satisfied with 2 eligible and 0 excluded", result)
		}
		if got := result.Explain(); got == "" {
			t.Fatalf("Explain() returned empty string")
		}
	})

	t.Run("constraints with no rule enabled are refused at construction", func(t *testing.T) {
		ctx := sod.DecisionContext{Requester: sod.Actor{Subject: "alice"}, Approvers: []sod.Actor{{Subject: "bob"}}}
		if _, err := sod.Evaluate(ctx, sod.Constraints{RuleID: "SOD-7"}, 1); !errors.Is(err, sod.ErrInvalidConstraints) {
			t.Fatalf("err = %v, want ErrInvalidConstraints", err)
		}
	})

	t.Run("constraints with no rule id are refused", func(t *testing.T) {
		ctx := sod.DecisionContext{Requester: sod.Actor{Subject: "alice"}, Approvers: []sod.Actor{{Subject: "bob"}}}
		c := sod.Constraints{RequesterMayNotApprove: true}
		if _, err := sod.Evaluate(ctx, c, 1); !errors.Is(err, sod.ErrInvalidConstraints) {
			t.Fatalf("err = %v, want ErrInvalidConstraints", err)
		}
	})
}

// FuzzTodo_TRUST_014 is the FUZZ matrix test. It treats subject identifiers
// as untrusted input: Evaluate may reject or exclude, but every eligible
// subject returned must be one that was actually offered as an approver and,
// under a full constraint set, must never equal the requester or executor.
func FuzzTodo_TRUST_014(f *testing.F) {
	f.Add("alice", "bob", "carol", "erin", true)
	f.Add("alice", "alice", "alice", "alice", false)
	f.Add("", "bob", "", "erin", true)
	f.Fuzz(func(t *testing.T, requester, approverA, approverB, executor string, dedupe bool) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: requester},
			Executor:  sod.Actor{Subject: executor},
			Approvers: []sod.Actor{{Subject: approverA}, {Subject: approverB}},
		}
		c := sod.Constraints{
			RequesterMayNotApprove:         true,
			ExecutorMayNotApprove:          true,
			OneApprovalPerPrincipal:        dedupe,
			RequesterDelegateMayNotApprove: true,
			RuleID:                         "FUZZ-SOD",
		}
		result, err := sod.Evaluate(ctx, c, 1)
		if err != nil {
			return
		}
		for _, e := range result.Eligible {
			if e == "" {
				t.Fatalf("eligible list contains an empty subject: %v", result.Eligible)
			}
			if requester != "" && e == requester {
				t.Fatalf("eligible list contains the requester %q under a full constraint set", requester)
			}
			if executor != "" && e == executor {
				t.Fatalf("eligible list contains the executor %q under a full constraint set", executor)
			}
			if e != approverA && e != approverB {
				t.Fatalf("eligible subject %q was never offered as an approver", e)
			}
		}
	})
}

// TestTodo_TRUST_014_Security proves the evaluator fails closed against
// adversarial inputs: an attacker cannot smuggle a delegation chain that
// hides the requester in a non-terminal position, cannot bypass exclusion by
// supplying an executor that only case-differs from an approver (identifiers
// are opaque and compared exactly, so a real bypass would require an actual
// distinct identifier - this proves no implicit normalization silently
// widens a match), and an invalid decision context (no requester, or an
// approver with no subject) is always refused rather than silently
// evaluated as "no exclusions apply".
func TestTodo_TRUST_014_Security(t *testing.T) {
	t.Run("delegation chain hides requester in the middle, still excluded", func(t *testing.T) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: "alice"},
			Approvers: []sod.Actor{
				{Subject: "dave", DelegationChain: []string{"zeke", "alice", "dave"}},
				{Subject: "bob"},
			},
		}
		result, err := sod.Evaluate(ctx, fullConstraints("SEC-1"), 2)
		if !errors.Is(err, sod.ErrUnsatisfiable) {
			t.Fatalf("err = %v, want ErrUnsatisfiable", err)
		}
		for _, e := range result.Eligible {
			if e == "dave" {
				t.Fatalf("dave should have been excluded as a delegate of the requester")
			}
		}
	})

	t.Run("chain listing only the actor itself is not treated as delegation from anyone", func(t *testing.T) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: "alice"},
			Approvers: []sod.Actor{{Subject: "bob", DelegationChain: []string{"bob"}}},
		}
		result, err := sod.Evaluate(ctx, fullConstraints("SEC-2"), 1)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if len(result.Eligible) != 1 || result.Eligible[0] != "bob" {
			t.Fatalf("eligible = %v, want [bob]", result.Eligible)
		}
	})

	t.Run("case-distinct identifier is not conflated with the excluded subject", func(t *testing.T) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: "alice"},
			Approvers: []sod.Actor{{Subject: "Alice"}, {Subject: "bob"}},
		}
		result, err := sod.Evaluate(ctx, fullConstraints("SEC-3"), 2)
		if err != nil {
			t.Fatalf("Evaluate: %v (opaque identifiers must compare exactly, not case-insensitively)", err)
		}
		if len(result.Eligible) != 2 {
			t.Fatalf("eligible = %v, want 2 (Alice != alice)", result.Eligible)
		}
	})

	t.Run("no requester subject is refused", func(t *testing.T) {
		ctx := sod.DecisionContext{Approvers: []sod.Actor{{Subject: "bob"}}}
		if _, err := sod.Evaluate(ctx, fullConstraints("SEC-4"), 1); !errors.Is(err, sod.ErrInvalidContext) {
			t.Fatalf("err = %v, want ErrInvalidContext", err)
		}
	})

	t.Run("an approver with no subject is refused rather than silently skipped", func(t *testing.T) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: "alice"},
			Approvers: []sod.Actor{{Subject: "bob"}, {Subject: ""}},
		}
		if _, err := sod.Evaluate(ctx, fullConstraints("SEC-5"), 1); !errors.Is(err, sod.ErrInvalidContext) {
			t.Fatalf("err = %v, want ErrInvalidContext", err)
		}
	})

	t.Run("negative or zero quorum is refused", func(t *testing.T) {
		ctx := sod.DecisionContext{Requester: sod.Actor{Subject: "alice"}, Approvers: []sod.Actor{{Subject: "bob"}}}
		if _, err := sod.Evaluate(ctx, fullConstraints("SEC-6"), 0); !errors.Is(err, sod.ErrInvalidContext) {
			t.Fatalf("err = %v, want ErrInvalidContext", err)
		}
	})
}

// TestTodo_TRUST_014_Mutation proves every exclusion branch is load-bearing:
// disabling any one constraint flag, while a scenario that only that flag
// would exclude is presented, must let the excluded candidate back in. A
// mutant that deletes or short-circuits one of Evaluate's branches would
// pass at least one of the "full constraints" cases above but fail here.
func TestTodo_TRUST_014_Mutation(t *testing.T) {
	cases := []struct {
		name    string
		ctx     sod.DecisionContext
		disable func(*sod.Constraints)
		subject string
	}{
		{
			name: "disabling RequesterMayNotApprove readmits the requester",
			ctx: sod.DecisionContext{
				Requester: sod.Actor{Subject: "alice"},
				Approvers: []sod.Actor{{Subject: "alice"}},
			},
			disable: func(c *sod.Constraints) { c.RequesterMayNotApprove = false },
			subject: "alice",
		},
		{
			name: "disabling RequesterDelegateMayNotApprove readmits the delegate",
			ctx: sod.DecisionContext{
				Requester: sod.Actor{Subject: "alice"},
				Approvers: []sod.Actor{{Subject: "carol", DelegationChain: []string{"alice", "carol"}}},
			},
			disable: func(c *sod.Constraints) { c.RequesterDelegateMayNotApprove = false },
			subject: "carol",
		},
		{
			name: "disabling ExecutorMayNotApprove readmits the executor",
			ctx: sod.DecisionContext{
				Requester: sod.Actor{Subject: "alice"},
				Executor:  sod.Actor{Subject: "erin"},
				Approvers: []sod.Actor{{Subject: "erin"}},
			},
			disable: func(c *sod.Constraints) { c.ExecutorMayNotApprove = false },
			subject: "erin",
		},
		{
			name: "disabling OneApprovalPerPrincipal keeps the duplicate",
			ctx: sod.DecisionContext{
				Requester: sod.Actor{Subject: "alice"},
				Approvers: []sod.Actor{{Subject: "bob"}, {Subject: "bob"}},
			},
			disable: func(c *sod.Constraints) { c.OneApprovalPerPrincipal = false },
			subject: "bob",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			full := fullConstraints("MUT-" + tc.name)
			if _, err := sod.Evaluate(tc.ctx, full, len(tc.ctx.Approvers)); !errors.Is(err, sod.ErrUnsatisfiable) {
				t.Fatalf("with full constraints: err = %v, want ErrUnsatisfiable (baseline must exclude)", err)
			}

			relaxed := full
			tc.disable(&relaxed)
			result, err := sod.Evaluate(tc.ctx, relaxed, 1)
			if err != nil {
				t.Fatalf("with relaxed constraint: Evaluate: %v", err)
			}
			found := false
			for _, e := range result.Eligible {
				if e == tc.subject {
					found = true
				}
			}
			if !found {
				t.Fatalf("relaxing the constraint under test did not readmit %q: eligible=%v", tc.subject, result.Eligible)
			}
		})
	}

	t.Run("disabling RequesterMayNotExecute lets the author execute their own repair", func(t *testing.T) {
		ctx := sod.DecisionContext{
			Requester: sod.Actor{Subject: "alice"},
			Executor:  sod.Actor{Subject: "alice"},
			Approvers: []sod.Actor{{Subject: "bob"}},
		}
		full := fullConstraints("MUT-execute")
		if _, err := sod.Evaluate(ctx, full, 1); !errors.Is(err, sod.ErrUnsatisfiable) {
			t.Fatalf("with full constraints: err = %v, want ErrUnsatisfiable", err)
		}
		relaxed := full
		relaxed.RequesterMayNotExecute = false
		if _, err := sod.Evaluate(ctx, relaxed, 1); err != nil {
			t.Fatalf("with RequesterMayNotExecute disabled: Evaluate: %v", err)
		}
	})
}

func TestSOD_ValidationOrderingAndExplanation(t *testing.T) {
	valid := fullConstraints("SOD-HARDENING")
	if err := valid.Validate(); err != nil {
		t.Fatalf("Constraints.Validate(valid) = %v", err)
	}
	for _, tc := range []struct {
		name string
		c    sod.Constraints
	}{
		{"missing rule id", sod.Constraints{RequesterMayNotApprove: true}},
		{"no exclusion enabled", sod.Constraints{RuleID: "SOD-NONE"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.c.Validate(); !errors.Is(err, sod.ErrInvalidConstraints) {
				t.Fatalf("Constraints.Validate() = %v, want ErrInvalidConstraints", err)
			}
		})
	}

	ctx := sod.DecisionContext{
		Requester: sod.Actor{Subject: "alice"},
		Executor:  sod.Actor{Subject: "erin"},
		Approvers: []sod.Actor{
			{Subject: "alice", DelegationChain: []string{"alice", "alice"}},
			{Subject: "erin"},
			{Subject: "bob"},
			{Subject: "bob"},
		},
	}
	result, err := sod.Evaluate(ctx, valid, 1)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.Satisfied || len(result.Eligible) != 1 || result.Eligible[0] != "bob" || len(result.Excluded) != 3 {
		t.Fatalf("result = %+v, want one eligible bob and three exclusions", result)
	}
	if result.Excluded[0].Reason != sod.ReasonSelfApproval || result.Excluded[1].Reason != sod.ReasonExecutorIsApprover || result.Excluded[2].Reason != sod.ReasonDuplicatePrincipal {
		t.Fatalf("exclusions = %+v, want fixed first-fired rule order", result.Excluded)
	}
	if got := result.Explain(); got != "sod decision rule=SOD-HARDENING eligible=1 excluded=3 satisfied=true" {
		t.Fatalf("Explain() = %q, want deterministic summary", got)
	}

	// A missing executor is intentionally a no-op for executor-dependent
	// rules, while a quorum larger than the surviving set is unsatisfiable.
	noExecutor := sod.DecisionContext{Requester: sod.Actor{Subject: "alice"}, Approvers: []sod.Actor{{Subject: "bob"}}}
	if result, err := sod.Evaluate(noExecutor, valid, 1); err != nil || len(result.Eligible) != 1 {
		t.Fatalf("missing executor evaluation = %+v, %v, want bob eligible", result, err)
	}
	if result, err := sod.Evaluate(noExecutor, valid, 2); !errors.Is(err, sod.ErrUnsatisfiable) || result.Satisfied {
		t.Fatalf("unmet quorum = %+v, %v, want ErrUnsatisfiable and false", result, err)
	}
}
