package workflow_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/workflow"
)

// concurrency.go's per-file suite. wfcomp004_test.go drives the analysis
// through Compile over real definitions; what is pinned here is the two
// exported readers a runtime actually calls at a frontier boundary --
// [workflow.CompiledWorkflow.InAtomicRegion] and its named negation
// [workflow.CompiledWorkflow.InterventionEligible] -- including the two
// degenerate receivers the runtime can legitimately hold: a nil plan, and a
// plan whose analysis found nothing to state.
//
// The degenerate cases matter more than they look. Compile leaves Concurrency
// nil for a plan with no PARALLEL and no unobserved external mutation, so that
// an already-published plan's canonical bytes are not re-digested by this
// analysis. A reader that panicked or answered "ineligible" on nil would turn
// that deliberate absence into a runtime that refuses to pause every
// zero-effect workflow.

func TestConcurrency_InterventionEligibilityOnAPlanWithNoAnalysis(t *testing.T) {
	t.Parallel()

	var nilPlan *workflow.CompiledWorkflow
	if nilPlan.InAtomicRegion("anything") {
		t.Fatalf("a nil plan reported a node inside an atomic region")
	}
	if !nilPlan.InterventionEligible("anything") {
		t.Fatalf("a nil plan refused intervention; the two readers disagree")
	}

	empty := &workflow.CompiledWorkflow{}
	if empty.InAtomicRegion("anything") {
		t.Fatalf("a plan with no concurrency summary reported an atomic region")
	}
	if !empty.InterventionEligible("anything") {
		t.Fatalf("a zero-effect plan refused intervention; every such workflow would be unpausable")
	}
}

func TestConcurrency_InterventionIsRefusedExactlyInsideARegion(t *testing.T) {
	t.Parallel()

	plan := &workflow.CompiledWorkflow{
		Concurrency: &workflow.ConcurrencySummary{
			AtomicRegions: []workflow.AtomicRegion{{
				EntryNodeID:     "mutate",
				ExitNodeIDs:     []string{"observe"},
				InteriorNodeIDs: []string{"interior", "observe"},
				EffectKeys:      []string{"effect.payroll.write"},
			}},
			InterventionIneligibleNodes: []string{"interior", "observe"},
		},
	}

	// The entry is itself a safe point: the mutation has not run yet when the
	// frontier sits on it, so a pause there abandons nothing.
	for _, eligible := range []string{"mutate", "before", "after"} {
		if plan.InAtomicRegion(eligible) {
			t.Errorf("node %q reported inside the atomic region", eligible)
		}
		if !plan.InterventionEligible(eligible) {
			t.Errorf("intervention refused at %q, which is outside every region", eligible)
		}
	}

	// The interior and the exits are where the instance holds an effect
	// nothing has observed yet.
	for _, ineligible := range []string{"interior", "observe"} {
		if !plan.InAtomicRegion(ineligible) {
			t.Errorf("node %q reported outside the atomic region", ineligible)
		}
		if plan.InterventionEligible(ineligible) {
			t.Errorf("intervention allowed at %q, inside an unobserved effect", ineligible)
		}
	}
}

func TestConcurrency_EligibilityReadsTheIneligibleSetNotTheRegionShape(t *testing.T) {
	t.Parallel()

	// InterventionIneligibleNodes is the union the compiler derived, and it is
	// what the readers consult. A summary carrying regions but an empty union
	// -- which is what a plan whose regions were all analysed away looks like
	// -- must not resurrect the region interiors from the region records.
	plan := &workflow.CompiledWorkflow{
		Concurrency: &workflow.ConcurrencySummary{
			AtomicRegions: []workflow.AtomicRegion{{
				EntryNodeID:     "mutate",
				InteriorNodeIDs: []string{"interior"},
			}},
		},
	}
	if plan.InAtomicRegion("interior") {
		t.Fatalf("eligibility was derived from a region record rather than the compiled ineligible set")
	}
}
