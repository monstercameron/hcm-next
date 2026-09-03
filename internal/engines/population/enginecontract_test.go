package population_test

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/population"
)

// TestVersionIsStable is the ARCH-GO-009 engine package contract test for
// population's Version(): it reports a fixed, positive contract version
// with no dependency on any Definition, Revision or Result.
func TestVersionIsStable(t *testing.T) {
	if v := population.Version(); v != population.Version() || v <= 0 {
		t.Fatalf("Version() = %d, want a stable positive contract version", v)
	}
}

// TestCompiledPlanEvaluateMatchesResolve is the ARCH-GO-009 engine package
// contract test proving Compile and Evaluate are a real pair here, not just
// two names present in the source: CompiledPlan.Evaluate must answer
// exactly what Resolve answers for the same plan, subject and time context.
func TestCompiledPlanEvaluateMatchesResolve(t *testing.T) {
	ctx := context.Background()
	asOf := mustInstant(t, 1_000_000)
	knownAt := mustKnownAt(t, 1_000_000)
	plan := mustPlan(t, validDefinition())
	w1 := worker(1)

	reader := newFakeReader().
		withSubject(w1).
		withFact(w1, "grade", valueFact("P3", knownAt)).
		withFact(w1, "active", valueFact("true", knownAt)).
		withWatermark(mustInstant(t, 1_000_000))

	viaResolve, err := population.Resolve(ctx, reader, population.SubjectWorker, plan, asOf, knownAt)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	viaEvaluate, err := plan.Evaluate(ctx, reader, population.SubjectWorker, asOf, knownAt)
	if err != nil {
		t.Fatalf("CompiledPlan.Evaluate: %v", err)
	}

	if len(viaResolve.Members) != len(viaEvaluate.Members) {
		t.Fatalf("Resolve produced %d members, Evaluate produced %d", len(viaResolve.Members), len(viaEvaluate.Members))
	}
	for i := range viaResolve.Members {
		if viaResolve.Members[i].Subject.String() != viaEvaluate.Members[i].Subject.String() ||
			viaResolve.Members[i].Outcome != viaEvaluate.Members[i].Outcome {
			t.Fatalf("member %d differs: Resolve=%+v Evaluate=%+v", i, viaResolve.Members[i], viaEvaluate.Members[i])
		}
	}
	if viaResolve.Completeness != viaEvaluate.Completeness {
		t.Fatalf("completeness differs: Resolve=%s Evaluate=%s", viaResolve.Completeness, viaEvaluate.Completeness)
	}
}
