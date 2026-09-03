package repair_test

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/domains/dataops"
	"github.com/monstercameron/hcm-next/internal/domains/repair"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// TestSimulateRepairReturnsBoundImpactAndZeroCorrectiveEffects is the
// REPAIR-003 primary test: the simulation binds the exact plan and the current
// evidence, projects the post-state onto a copy, reports residual
// disagreements, risks, approvals and cost, and causes nothing.
func TestSimulateRepairReturnsBoundImpactAndZeroCorrectiveEffects(t *testing.T) {
	s := safeScenario(t)
	p := plan(t, s)

	t.Run("GREEN: the result binds the exact plan and the current evidence", func(t *testing.T) {
		sim, err := repair.SimulateRepair(simulateRequest(t, s, p))
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		if sim.PlanDigest != p.Digest || sim.PlanID != p.ID {
			t.Fatalf("simulation is not bound to the plan: %s/%s", sim.PlanID, sim.PlanDigest)
		}
		if sim.DiffDigest != p.DiffDigest || sim.CurrentDiffDigest != s.diff.Digest {
			t.Fatal("the simulation does not cite both the planned and the current comparison")
		}
		if sim.Status != repair.StatusValid || sim.StatusReason != repair.ReasonEvidenceBinds {
			t.Fatalf("status = %s (%s), want VALID", sim.Status, sim.StatusReason)
		}
	})

	t.Run("GREEN: the projection reaches the post-state and leaves no residual", func(t *testing.T) {
		sim, err := repair.SimulateRepair(simulateRequest(t, s, p))
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		if len(sim.Projected) != len(p.Steps) {
			t.Fatalf("%d projection(s) for %d step(s)", len(sim.Projected), len(p.Steps))
		}
		for _, proj := range sim.Projected {
			if !proj.Changed {
				t.Fatalf("%s: a writing step projected no change (%s)", proj.Field, proj.Reason)
			}
			if !proj.After.IsValue() {
				t.Fatalf("%s: post-state is not a readable value", proj.Field)
			}
		}
		if len(sim.ResidualFindings) != 0 {
			t.Fatalf("residual disagreements after a complete repair: %v", sim.ResidualFindings)
		}
		if sim.ResidualCounts.Mismatch != 0 {
			t.Fatalf("projected comparison still shows %d mismatch(es)", sim.ResidualCounts.Mismatch)
		}
		if sim.ProjectedDiffDigest == "" || sim.ProjectedDiffDigest == s.diff.Digest {
			t.Fatal("the projected comparison is not distinct from the current one")
		}
	})

	t.Run("GREEN: risks, approvals, SoD and cost are reported as proposed, not performed", func(t *testing.T) {
		sim, err := repair.SimulateRepair(simulateRequest(t, s, p))
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		if len(sim.Risks) != len(p.Steps) {
			t.Fatalf("%d risk note(s) for %d step(s)", len(sim.Risks), len(p.Steps))
		}
		if !sim.RequiresApproval || len(sim.ApprovalRoles) == 0 || len(sim.SoDExcludedRoles) == 0 {
			t.Fatal("the simulation dropped the governance requirement")
		}
		if sim.Cost.Steps != 2 || sim.Cost.LocalWrites != 1 || sim.Cost.ExternalWrites != 1 {
			t.Fatalf("cost = %+v", sim.Cost)
		}
		if sim.Cost.Approvals != 1 || sim.Cost.MaxAttempts != 2 {
			t.Fatalf("cost approvals=%d attempts=%d", sim.Cost.Approvals, sim.Cost.MaxAttempts)
		}
		if len(sim.ObservationExpectations) == 0 || len(sim.SuccessCriteria) == 0 ||
			len(sim.RollbackExpectations) == 0 {
			t.Fatal("the simulation reports no verification or rollback expectations")
		}
	})

	t.Run("GREEN: every corrective, provider, outbox and human-work count is zero", func(t *testing.T) {
		sim, err := repair.SimulateRepair(simulateRequest(t, s, p))
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		if !sim.Effects.IsZero() {
			t.Fatalf("the simulation caused effects: %v", sim.Effects.NonZero())
		}
		if err := sim.Receipt.Validate(); err != nil {
			t.Fatalf("receipt: %v", err)
		}
		if sim.Receipt.ExecutionState != "NOT_PLANNED" {
			t.Fatalf("execution state = %q", sim.Receipt.ExecutionState)
		}
		// The proposed cost is non-zero at the same time, which is the point:
		// a plan that would write is described without anything being written.
		if sim.Cost.ExternalWrites == 0 {
			t.Fatal("the simulation described no proposed external write to contrast with")
		}
	})

	t.Run("RED: the simulation claims a guaranteed correction", func(t *testing.T) {
		sim, err := repair.SimulateRepair(simulateRequest(t, s, p))
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		found := false
		for _, c := range sim.Caveats {
			if c == repair.NoGuaranteeToken {
				found = true
			}
		}
		if !found {
			t.Fatalf("caveats = %v, want the no-guarantee token", sim.Caveats)
		}
	})

	t.Run("RED: the simulation mutates the records it was given", func(t *testing.T) {
		req := simulateRequest(t, s, p)
		beforeCanonical := req.Canonical.Canonical()
		beforeObserved := req.Observed.Canonical()
		if _, err := repair.SimulateRepair(req); err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		if string(req.Canonical.Canonical()) != string(beforeCanonical) {
			t.Fatal("the simulation mutated the canonical record")
		}
		if string(req.Observed.Canonical()) != string(beforeObserved) {
			t.Fatal("the simulation mutated the observed record")
		}
	})

	t.Run("RED: the simulation broadens the target or the authority", func(t *testing.T) {
		req := simulateRequest(t, s, p)
		req.Fields = []dataops.FieldID{fieldGrade}
		req.Authorization = authorize(fieldGrade)
		if _, err := repair.SimulateRepair(req); !errors.Is(err, repair.ErrTargetBroadened) {
			t.Fatalf("err = %v, want ErrTargetBroadened", err)
		}

		req = simulateRequest(t, s, p)
		req.LocalSystem = "some-other-system"
		if _, err := repair.SimulateRepair(req); !errors.Is(err, repair.ErrTargetBroadened) {
			t.Fatalf("a step targeting an unnamed system: err = %v", err)
		}
	})
}

// TestTodo_REPAIR_003_Property proves the invariants the simulation holds over
// every scenario, not only the sampled one.
func TestTodo_REPAIR_003_Property(t *testing.T) {
	for name, build := range map[string]func(*testing.T) scenario{
		"safe":   safeScenario,
		"unsafe": unsafeScenario,
		"stale":  staleScenario,
	} {
		s := build(t)
		p := plan(t, s)
		sim, err := repair.SimulateRepair(simulateRequest(t, s, p))
		if err != nil {
			t.Fatalf("%s: SimulateRepair: %v", name, err)
		}

		// Property: the status is always one of the five, and always reasoned.
		if !sim.Status.Valid() || sim.StatusReason == "" {
			t.Fatalf("%s: status=%s reason=%q", name, sim.Status, sim.StatusReason)
		}
		// Property: effects are always zero, whatever the status.
		if !sim.Effects.IsZero() {
			t.Fatalf("%s: effects %v", name, sim.Effects.NonZero())
		}
		// Property: one projection per step, and no projection for a field the
		// plan does not target.
		if len(sim.Projected) != len(p.Steps) {
			t.Fatalf("%s: %d projection(s) for %d step(s)", name, len(sim.Projected), len(p.Steps))
		}
		for _, proj := range sim.Projected {
			if _, ok := p.Step(proj.Field); !ok {
				t.Fatalf("%s: projected %s with no step", name, proj.Field)
			}
		}
		// Property: the residual counts partition the projected comparison.
		if sim.ResidualCounts.Total() != len(s.fields) {
			t.Fatalf("%s: residual counts total %d over %d field(s)",
				name, sim.ResidualCounts.Total(), len(s.fields))
		}
		// Property: replay is byte-identical.
		again, err := repair.SimulateRepair(simulateRequest(t, s, p))
		if err != nil {
			t.Fatalf("%s: replay: %v", name, err)
		}
		if again.ResultDigest != sim.ResultDigest {
			t.Fatalf("%s: the simulation is not reproducible", name)
		}
	}
}

// TestTodo_REPAIR_003_Golden pins the status vocabulary against the exact
// evidence conditions the contract names.
func TestTodo_REPAIR_003_Golden(t *testing.T) {
	s := safeScenario(t)
	p := plan(t, s)

	t.Run("VALID when the pinned evidence still binds", func(t *testing.T) {
		sim, err := repair.SimulateRepair(simulateRequest(t, s, p))
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		if sim.Status != repair.StatusValid {
			t.Fatalf("status = %s (%s)", sim.Status, sim.StatusReason)
		}
	})

	t.Run("REPLAN_REQUIRED when the observation has been replaced", func(t *testing.T) {
		req := simulateRequest(t, s, p)
		req.Observation.Digest = "sha256:a-newer-page"
		sim, err := repair.SimulateRepair(req)
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		if sim.Status != repair.StatusReplanRequired ||
			sim.StatusReason != repair.ReasonObservationMoved {
			t.Fatalf("status = %s (%s), want REPLAN_REQUIRED", sim.Status, sim.StatusReason)
		}
	})

	t.Run("REPLAN_REQUIRED when the authority policy has changed", func(t *testing.T) {
		req := simulateRequest(t, s, p)
		req.AuthorityPolicyVersion = "authority.by_field/2027.1"
		sim, err := repair.SimulateRepair(req)
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		if sim.Status != repair.StatusReplanRequired ||
			sim.StatusReason != repair.ReasonAuthorityMoved {
			t.Fatalf("status = %s (%s), want REPLAN_REQUIRED", sim.Status, sim.StatusReason)
		}
	})

	t.Run("NO_LONGER_REQUIRED when the targets already agree", func(t *testing.T) {
		// Both sides now hold the canonical value, so the comparison shows no
		// mismatch on the planned fields.
		settled := safeScenario(t)
		for i := range settled.observed.Fields {
			switch settled.observed.Fields[i].Field {
			case fieldBase:
				settled.observed.Fields[i].Value = values.Value("135000.00 USD")
			case fieldJobCode:
				settled.observed.Fields[i].Value = values.Value("ENG-3")
			}
		}
		resolved, err := dataops.DiffRecord(dataops.DiffRecordRequest{
			Canonical:       settled.canonical,
			Observed:        settled.observed,
			ObservedPresent: true,
			Observation:     settled.watermark,
			Fields:          settled.fields,
			Authorization:   settled.auth,
			Freshness:       freshness(),
			EvaluatedAt:     instantAt(t, evaluatedAt),
		})
		if err != nil {
			t.Fatalf("DiffRecord: %v", err)
		}
		req := simulateRequest(t, settled, p)
		req.CurrentDiff = resolved
		sim, err := repair.SimulateRepair(req)
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		if sim.Status != repair.StatusNoLongerRequired ||
			sim.StatusReason != repair.ReasonAlreadySatisfied {
			t.Fatalf("status = %s (%s), want NO_LONGER_REQUIRED", sim.Status, sim.StatusReason)
		}
	})

	t.Run("BLOCKED when a step needs a human ruling", func(t *testing.T) {
		unsafe := unsafeScenario(t)
		unsafePlan := plan(t, unsafe)
		sim, err := repair.SimulateRepair(simulateRequest(t, unsafe, unsafePlan))
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		if sim.Status != repair.StatusBlocked || sim.StatusReason != repair.ReasonNeedsHumanRuling {
			t.Fatalf("status = %s (%s), want BLOCKED", sim.Status, sim.StatusReason)
		}
		if sim.ResidualCounts.Mismatch == 0 {
			t.Fatal("a blocked plan reported a clean projected comparison")
		}
	})

	t.Run("NO_LONGER_REQUIRED when the plan is empty", func(t *testing.T) {
		stale := staleScenario(t)
		stalePlan := plan(t, stale)
		sim, err := repair.SimulateRepair(simulateRequest(t, stale, stalePlan))
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		if sim.Status != repair.StatusNoLongerRequired || sim.StatusReason != repair.ReasonNoSteps {
			t.Fatalf("status = %s (%s), want NO_LONGER_REQUIRED/%s",
				sim.Status, sim.StatusReason, repair.ReasonNoSteps)
		}
	})
}

// TestTodo_REPAIR_003_Integration runs the whole P1A operations chain over one
// fixture: compare, diagnose, plan, simulate - and proves the chain writes
// nothing at any step.
func TestTodo_REPAIR_003_Integration(t *testing.T) {
	s := safeScenario(t)
	d := diagnose(t, s)
	p, err := repair.CreateRepairPlan(planRequest(t, s))
	if err != nil {
		t.Fatalf("CreateRepairPlan: %v", err)
	}
	sim, err := repair.SimulateRepair(simulateRequest(t, s, p))
	if err != nil {
		t.Fatalf("SimulateRepair: %v", err)
	}

	// The chain is bound end to end by digest.
	if d.DiffDigest != s.diff.Digest {
		t.Fatal("the diagnosis is not bound to the comparison")
	}
	if p.DiffDigest != d.DiffDigest {
		t.Fatal("the plan is not bound to the diagnosis")
	}
	if sim.PlanDigest != p.Digest {
		t.Fatal("the simulation is not bound to the plan")
	}
	// And nothing anywhere on it counted an effect.
	if !p.Effects.IsZero() || !sim.Effects.IsZero() {
		t.Fatal("the chain counted an effect")
	}
	if p.Executable {
		t.Fatal("the chain produced an executable plan")
	}
}

// TestTodo_REPAIR_003_Fault proves that a malformed or unbound simulation
// request is refused rather than answered.
func TestTodo_REPAIR_003_Fault(t *testing.T) {
	s := safeScenario(t)
	p := plan(t, s)

	for _, tc := range []struct {
		name string
		edit func(*repair.SimulateRepairRequest)
		want error
	}{
		{"no evaluation instant", func(r *repair.SimulateRepairRequest) {
			r.EvaluatedAt = values.Instant{}
		}, repair.ErrSimulationInvalid},
		{"no authority policy", func(r *repair.SimulateRepairRequest) {
			r.AuthorityPolicyVersion = ""
		}, repair.ErrSimulationInvalid},
		{"wrong tenant", func(r *repair.SimulateRepairRequest) {
			r.Tenant = "another-tenant"
		}, repair.ErrSimulationInvalid},
		{"unusable freshness policy", func(r *repair.SimulateRepairRequest) {
			r.Freshness = dataops.FreshnessPolicy{}
		}, dataops.ErrFreshnessPolicy},
		{"comparison about another subject", func(r *repair.SimulateRepairRequest) {
			r.CurrentDiff.Subject = values.EntityRef{
				Tenant: r.Tenant, Kind: "worker", Id: "someone-else",
			}
		}, repair.ErrTargetBroadened},
		{"an unbuilt plan", func(r *repair.SimulateRepairRequest) {
			r.Plan.Digest = ""
		}, repair.ErrSimulationInvalid},
	} {
		req := simulateRequest(t, s, p)
		tc.edit(&req)
		if _, err := repair.SimulateRepair(req); !errors.Is(err, tc.want) {
			t.Fatalf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

// TestTodo_REPAIR_003_Security proves that a caller cannot simulate a repair
// on a field it may not see.
func TestTodo_REPAIR_003_Security(t *testing.T) {
	s := safeScenario(t)
	p := plan(t, s)

	t.Run("RED: a step targets a field this caller may not see", func(t *testing.T) {
		req := simulateRequest(t, s, p)
		denied := authorize(s.fields...)
		denied.Fields[fieldBase] = dataops.Ruling{
			Effect: dataops.EffectDeny, Reason: "compensation_restricted",
		}
		req.Authorization = denied
		if _, err := repair.SimulateRepair(req); !errors.Is(err, repair.ErrTargetBroadened) {
			t.Fatalf("err = %v, want ErrTargetBroadened", err)
		}
	})

	t.Run("RED: a simulation grants repair execution authority", func(t *testing.T) {
		sim, err := repair.SimulateRepair(simulateRequest(t, s, p))
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		// The simulated plan is still non-executable, and the simulation
		// exposes no execution surface of its own.
		if p.Executable {
			t.Fatal("simulating a plan made it executable")
		}
		if sim.Receipt.Mode != "SIMULATE" {
			t.Fatalf("receipt mode = %q", sim.Receipt.Mode)
		}
	})
}

// TestTodo_REPAIR_003_Conformance checks the intent identity and the pinned
// control set a stored simulation must carry.
func TestTodo_REPAIR_003_Conformance(t *testing.T) {
	s := safeScenario(t)
	sim, err := repair.SimulateRepair(simulateRequest(t, s, plan(t, s)))
	if err != nil {
		t.Fatalf("SimulateRepair: %v", err)
	}
	if sim.IntentType != "hcmnext.operations.simulate_repair" || sim.IntentVersion != "v1" {
		t.Fatalf("intent identity = %s/%s", sim.IntentType, sim.IntentVersion)
	}
	controls := map[string]bool{}
	for _, c := range sim.Receipt.Controls {
		controls[c.Name] = true
	}
	for _, want := range []string{
		"approval_policy", "authority_policy", "plan_rule_pack",
		"simulation_rule_pack", "diff_rule_pack", "freshness_policy",
	} {
		if !controls[want] {
			t.Fatalf("the receipt does not pin %s", want)
		}
	}
	if sim.Receipt.InputsDigest != sim.InputsDigest ||
		sim.Receipt.ResultDigest != sim.ResultDigest {
		t.Fatal("the receipt does not bind the simulation it certifies")
	}
}

// TestTodo_REPAIR_003_Mutation kills mutants a weaker implementation would
// survive: applying a review step, projecting an unknown as empty, or
// forgetting the residual comparison.
func TestTodo_REPAIR_003_Mutation(t *testing.T) {
	t.Run("a review step moves nothing", func(t *testing.T) {
		s := unsafeScenario(t)
		sim, err := repair.SimulateRepair(simulateRequest(t, s, plan(t, s)))
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		for _, proj := range sim.Projected {
			if proj.Field != fieldLocation {
				continue
			}
			if proj.Changed {
				t.Fatal("a review step changed the projected state")
			}
			if proj.Reason != "step_writes_nothing" {
				t.Fatalf("reason = %q", proj.Reason)
			}
			if proj.After.IsValue() {
				t.Fatal("a review step projected a readable post-state")
			}
		}
	})

	t.Run("the residual comparison is recomputed, not assumed", func(t *testing.T) {
		s := unsafeScenario(t)
		sim, err := repair.SimulateRepair(simulateRequest(t, s, plan(t, s)))
		if err != nil {
			t.Fatalf("SimulateRepair: %v", err)
		}
		// The unresolvable field must still disagree after every step.
		found := false
		for _, f := range sim.ResidualFindings {
			if f.Field == fieldLocation && f.Verdict == dataops.VerdictMismatch {
				found = true
			}
		}
		if !found {
			t.Fatalf("the unresolvable field is missing from the residual: %v", sim.ResidualFindings)
		}
	})
}

// FuzzTodo_REPAIR_003 drives the current comparison and the pinned evidence
// with arbitrary input. No input may panic, and no input may produce a
// simulation with a non-zero effect count or an unspecified status.
func FuzzTodo_REPAIR_003(f *testing.F) {
	f.Add("sha256:page-2026-08-31T00:00:00Z", "authority.by_field/2026.1", "2026-08-31T12:00:00Z")
	f.Add("", "", "")
	f.Add("sha256:moved", "authority.by_field/2027.1", "not-a-time")

	f.Fuzz(func(t *testing.T, observationDigest, authorityPolicy, evaluated string) {
		s := safeScenario(t)
		req := simulateRequest(t, s, plan(t, s))
		req.Observation.Digest = observationDigest
		req.AuthorityPolicyVersion = authorityPolicy
		if parsed, err := parseInstant(evaluated); err == nil {
			req.EvaluatedAt = parsed
		}
		sim, err := repair.SimulateRepair(req)
		if err != nil {
			return
		}
		if !sim.Status.Valid() {
			t.Fatalf("accepted request produced status %s", sim.Status)
		}
		if !sim.Effects.IsZero() {
			t.Fatalf("a fuzzed simulation counted effects: %v", sim.Effects.NonZero())
		}
		if sim.PlanDigest == "" || sim.ResultDigest == "" {
			t.Fatal("a completed simulation carries no digests")
		}
	})
}
