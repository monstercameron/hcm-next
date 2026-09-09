package simulate_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// WF-RUN-012 formalises the SIMULATE mode this package already implements.
// Its GREEN clause is one sentence with three claims:
//
//	reads/pure rules/plans/obligations/cost execute and mutation attempt
//	returns SIMULATION_SIDE_EFFECT_FORBIDDEN; persistent evidence is limited
//	to authorized simulation artifacts.
//
// and its RED clause names the two ways a simulation mode goes wrong:
// creating something (task/message/charge/write/outbox/external call), or
// quietly dropping an effect it should have planned. The four tests below are
// that matrix. They add no runtime primitive: WF-RUN-000 gates the scheduler,
// leases and timers, so a P1A simulation is still one in-process walk against
// a fake clock.

// TestTodo_WF_RUN_012 is the PRIMARY case: the reference plan's reads, pure
// rules, transforms, plan/cost computation and obligations all execute, every
// effect counter is zero, and the planned obligation is stated rather than
// dropped.
//
// The last half is the RED clause's second failure mode, and it is the easy
// one to get wrong: a simulator that suppressed effects by pretending they
// were not planned would pass every zero-counter assertion above while
// silently telling an operator that a promotion needs no Finance approval.
func TestTodo_WF_RUN_012(t *testing.T) {
	setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
	receipt := mustRun(t, setup)

	if receipt.Mode != workflow.ModeSimulate {
		t.Errorf("mode = %s, want %s", receipt.Mode, workflow.ModeSimulate)
	}

	// Reads, pure rules, transforms and the terminal all executed: the trace
	// is a real walk, not an admission check that returned early.
	if len(receipt.Trace) < 2 {
		t.Fatalf("trace has %d entries; a simulation that executed nothing is not a simulation", len(receipt.Trace))
	}
	ranStepType := map[workflow.StepType]bool{}
	for _, entry := range receipt.Trace {
		ranStepType[entry.Type] = true
		if entry.EffectClass.IsWrite() {
			t.Errorf("node %s executed with write effect class %s", entry.NodeID, entry.EffectClass)
		}
		if entry.InputDigest == "" || entry.OutputDigest == "" {
			t.Errorf("node %s recorded no input/output digest; its execution cannot be evidenced", entry.NodeID)
		}
	}
	for _, want := range []workflow.StepType{workflow.StepCapability, workflow.StepDecision, workflow.StepEnd} {
		if !ranStepType[want] {
			t.Errorf("no %s node executed; the reference plan's %s work did not run", want, want)
		}
	}

	// Nothing was created. Every counter, not just the interesting ones: the
	// RED clause lists task, message, charge, write, outbox and external call,
	// and evidence.EffectCounters is where all six live.
	counters := receipt.EffectCounters()
	if counters != (evidence.EffectCounters{}) {
		t.Fatalf("simulation recorded effects %+v, want every counter zero", counters)
	}

	// Persistent evidence is limited to authorized simulation artifacts: the
	// zero-effect receipt, whose own constructor refuses to mint over a
	// non-zero count.
	if receipt.ZeroEffectDigest == "" {
		t.Error("receipt cites no zero-effect receipt digest")
	}
	if err := receipt.ZeroEffect.Validate(); err != nil {
		t.Errorf("zero-effect receipt does not validate: %v", err)
	}
	if got := receipt.ZeroEffect.Mode; got != evidence.ModeSimulate {
		t.Errorf("zero-effect receipt mode = %v, want %v", got, evidence.ModeSimulate)
	}

	// The obligation was planned, not omitted. The reference promotion changes
	// grade, so the threshold table escalates it: the terminal must say so.
	if len(receipt.Terminal.OutstandingObligationRefs) == 0 {
		t.Error("terminal declares no outstanding obligation; the escalated approval was dropped")
	}
	if len(receipt.WorkItems) == 0 {
		t.Fatal("simulation raised no work item; the approval it would await was not planned")
	}
	for _, item := range receipt.WorkItems {
		if item.State != simulate.WouldAwait {
			t.Errorf("work item %s is %s, want %s: a simulation may state what would be awaited, never await it",
				item.RequirementID, item.State, simulate.WouldAwait)
		}
	}
	// A planned human task is not a created one: WorkItems above are receipt
	// statements, and the effect counter for work items stays zero.
	if counters.WorkItems != 0 {
		t.Errorf("work-item effect counter = %d, want 0", counters.WorkItems)
	}
}

// TestTodo_WF_RUN_012_Race drives independent simulations concurrently and
// proves they neither share state nor drift.
//
// SIMULATE mode's whole claim is that a run is a pure function of the plan and
// the inputs. If two concurrent runs could observe each other -- through a
// registry, an environment, a clock or a receipt buffer -- that claim would be
// false and the byte-stable digest would be an accident of serial execution.
func TestTodo_WF_RUN_012_Race(t *testing.T) {
	const runners = 8

	want := mustRun(t, mustSetup(t, simulate.PromotionWithinThresholdPay)).Digest()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		digests []string
	)
	for range runners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			setup, err := simulate.NewPromotionSetup(simulate.PromotionWithinThresholdPay)
			if err != nil {
				mu.Lock()
				digests = append(digests, "setup: "+err.Error())
				mu.Unlock()
				return
			}
			receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
			if err != nil {
				mu.Lock()
				digests = append(digests, "run: "+err.Error())
				mu.Unlock()
				return
			}
			if verifyErr := receipt.Verify(); verifyErr != nil {
				mu.Lock()
				digests = append(digests, "verify: "+verifyErr.Error())
				mu.Unlock()
				return
			}
			mu.Lock()
			digests = append(digests, receipt.Digest())
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(digests) != runners {
		t.Fatalf("%d concurrent runs reported %d results", runners, len(digests))
	}
	for i, got := range digests {
		if got != want {
			t.Fatalf("concurrent run %d produced %q, want the serial digest %q", i, got, want)
		}
	}

	// Admit is the safe point every node passes; it must be callable
	// concurrently over one shared plan without mutating it.
	shared := mustSetup(t, simulate.PromotionWithinThresholdPay)
	digestBefore := shared.Plan.Digest()
	var admitWG sync.WaitGroup
	for range runners {
		admitWG.Add(1)
		go func() {
			defer admitWG.Done()
			if err := simulate.Admit(shared.Plan); err != nil {
				t.Errorf("Admit refused the reference plan: %v", err)
			}
		}()
	}
	admitWG.Wait()
	if shared.Plan.Digest() != digestBefore {
		t.Fatal("concurrent Admit calls changed the plan they were only supposed to inspect")
	}
}

// TestTodo_WF_RUN_012_Fault is the RED case: a plan that can mutate is refused
// with the contract code, nothing it would have written is reached, and the
// refused run leaves no receipt behind.
//
// The refusal is structural rather than a suppression flag. That distinction
// is the whole todo: a mode that "suppresses" effects is one boolean away from
// performing them, while a mode that refuses to admit a write-class node has
// no path to one at all.
func TestTodo_WF_RUN_012_Fault(t *testing.T) {
	reached := false
	plan := mustCompileMutatingPlan(t, func() { reached = true })

	// The safe point in front of the whole plan.
	admitErr := simulate.Admit(plan)
	if admitErr == nil {
		t.Fatal("Admit accepted a plan that can mutate")
	}
	if code := simulate.CodeOf(admitErr); code != simulate.CodeSimulationSideEffectForbidden {
		t.Fatalf("Admit refusal code = %q, want %q (%v)",
			code, simulate.CodeSimulationSideEffectForbidden, admitErr)
	}

	registry := mutatingRegistry(t, func() { reached = true })
	receipt, err := simulate.Run(context.Background(), plan, simulate.Inputs{
		Values: simulate.Bag{"worker_id": simulate.NewBranded("WorkerID", "11111111-1111-4111-8111-111111111111")},
	}, simulate.Options{Capabilities: registry})
	if err == nil {
		t.Fatal("Run executed a plan that can mutate")
	}
	if !errors.Is(err, simulate.ErrSimulate) {
		t.Errorf("refusal does not unwrap to ErrSimulate: %v", err)
	}
	if code := simulate.CodeOf(err); code != simulate.CodeSimulationSideEffectForbidden {
		t.Fatalf("Run refusal code = %q, want %q (%v)", code, simulate.CodeSimulationSideEffectForbidden, err)
	}
	if reached {
		t.Fatal("the mutating handler was reached; SIMULATE must refuse, not suppress")
	}

	// A refused run mints nothing. "Persistent evidence is limited to
	// authorized simulation artifacts" fails just as badly if a refusal leaves
	// a half-built receipt claiming a run happened.
	if receipt.Digest() != "" || len(receipt.Trace) != 0 {
		t.Fatalf("a refused run produced a receipt: digest %q, %d trace entries",
			receipt.Digest(), len(receipt.Trace))
	}

	// The governed gateway refuses the same capability independently, so the
	// interpreter's refusal is not the only thing between a simulation and a
	// mutation.
	gateway := capability.NewGateway(registry, &countingSink{})
	if _, gwErr := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability:    capability.Key{ID: capSyncPayroll, Version: 1},
		Payload:       simulate.CapabilityRequest{NodeID: fxSync},
		Authorization: capability.Authorization{Decision: capability.Allow, Scopes: []string{"scope:payroll.write"}},
	}); gwErr == nil {
		t.Fatal("the governed gateway invoked a write-effect capability")
	}
	if reached {
		t.Fatal("the gateway reached the mutating handler")
	}
}

// TestTodo_WF_RUN_012_Mutation kills the mutants that would make the
// zero-effect claim unfalsifiable: a plan whose effect summary is flipped, a
// node whose admitted modes no longer include SIMULATE, a tampered plan, and a
// receipt edited after it was minted.
//
// Each of these is a one-line change to state the interpreter reads. If any of
// them still produced a clean run and a verifying receipt, the receipt would
// be a decoration rather than evidence.
func TestTodo_WF_RUN_012_Mutation(t *testing.T) {
	t.Run("a plan whose effect summary claims a mutation is refused", func(t *testing.T) {
		setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
		setup.Plan.Effects.ZeroEffect = false

		err := simulate.Admit(setup.Plan)
		if err == nil {
			t.Fatal("Admit accepted a plan that declares itself non-zero-effect")
		}
		if code := simulate.CodeOf(err); code != simulate.CodePlanNotZeroEffect {
			t.Fatalf("refusal code = %q, want %q (%v)", code, simulate.CodePlanNotZeroEffect, err)
		}
	})

	t.Run("a node that no longer admits SIMULATE is refused", func(t *testing.T) {
		setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
		var mutated bool
		for i := range setup.Plan.Nodes {
			modes := setup.Plan.Nodes[i].AllowedModes
			kept := modes[:0:0]
			for _, m := range modes {
				if m != workflow.ModeSimulate {
					kept = append(kept, m)
				}
			}
			if len(kept) != len(modes) {
				setup.Plan.Nodes[i].AllowedModes = kept
				mutated = true
				break
			}
		}
		if !mutated {
			t.Fatal("no node in the reference plan admitted SIMULATE; the fixture is wrong")
		}

		err := simulate.Admit(setup.Plan)
		if err == nil {
			t.Fatal("Admit accepted a node that does not admit SIMULATE")
		}
		if code := simulate.CodeOf(err); code != simulate.CodeModeNotAdmitted {
			t.Fatalf("refusal code = %q, want %q (%v)", code, simulate.CodeModeNotAdmitted, err)
		}
	})

	t.Run("a tampered plan is not simulated", func(t *testing.T) {
		setup := mustSetup(t, simulate.PromotionWithinThresholdPay)
		setup.Plan.Nodes[0].SafePoint = !setup.Plan.Nodes[0].SafePoint

		_, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
		if err == nil {
			t.Fatal("Run simulated a plan that no longer matches its own digest")
		}
		if code := simulate.CodeOf(err); code != simulate.CodePlanUnverified {
			t.Fatalf("refusal code = %q, want %q (%v)", code, simulate.CodePlanUnverified, err)
		}
	})

	t.Run("a receipt edited after minting no longer verifies", func(t *testing.T) {
		receipt := mustRun(t, mustSetup(t, simulate.PromotionWithinThresholdPay))
		if err := receipt.Verify(); err != nil {
			t.Fatalf("freshly minted receipt does not verify: %v", err)
		}

		// Claim one fewer node ran. Nothing else changes, and the digest must
		// catch it.
		tampered := receipt
		tampered.Trace = receipt.Trace[:len(receipt.Trace)-1]
		if err := tampered.Verify(); err == nil {
			t.Fatal("a receipt with a node removed from its trace still verified")
		}

		// Claim an effect was performed. The counter is the whole claim of a
		// zero-effect receipt, so editing it must break the digest too.
		forged := receipt
		forged.Counters.DomainWrites = 1
		if err := forged.Verify(); err == nil {
			t.Fatal("a receipt claiming a domain write still verified against its digest")
		}
	})
}
