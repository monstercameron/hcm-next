package cycle

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func currentPhaseFixture(t *testing.T) (GovernedCycle, PhaseGraph, []CutoffResolution, time.Time) {
	t.Helper()
	c := validCycle()
	c.Phases[0].AllowedOperations = []string{string(OperationClose), "SUBMIT_ADJUSTMENT"}
	r, err := NewRevision(c, "cycle-009", "v1", c.Periods[0].Start, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	governed, err := NewOpenCycle(r)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := CompilePhaseGraph(c)
	if err != nil {
		t.Fatal(err)
	}
	date, err := values.ParseLocalDate("2026-01-15")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	return governed, graph, []CutoffResolution{{
		PhaseID: "open", Status: CutoffResolved, EffectiveDate: date,
		Instant: time.Date(2026, 1, 30, 21, 0, 0, 0, time.UTC), Rule: "fixture cutoff pinned by calendar revision calendar.us/1",
	}}, at
}

// TestTodo_CYCLE_009 is the registry's exact primary acceptance symbol and
// pins the current phase, capabilities, transitions and zero-side-effect
// rejection decision to one explicit effective/known instant.
func TestTodo_CYCLE_009(t *testing.T) {
	governed, graph, cutoffs, at := currentPhaseFixture(t)
	explanation, err := ExplainCurrentPhase(governed, graph, cutoffs, at)
	if err != nil {
		t.Fatal(err)
	}
	if !explanation.HasPhase || explanation.Phase.ID != "open" || explanation.Window.Start != graph.Phases[0].Start || explanation.Window.End != graph.Phases[0].End {
		t.Fatalf("phase/window = %+v, want the active open phase window", explanation)
	}
	if explanation.State != StateOpen || explanation.KnownAt != at.UTC() || len(explanation.Cutoffs) != 1 {
		t.Fatalf("state/time/cutoffs = %+v", explanation)
	}
	if !containsString(explanation.PermittedOperations, string(OperationClose)) || containsString(explanation.PermittedOperations, string(OperationLock)) {
		t.Fatalf("permitted operations = %v", explanation.PermittedOperations)
	}
	decision := explanation.DecideOperation(string(OperationLock))
	if decision.Code != Cycle009Rejected || decision.Field != "operation" || decision.State != StateOpen || decision.RevisionVersion != "v1" {
		t.Fatalf("out-of-window decision = %+v, want CYCLE_009_REJECTED with operation/state/version", decision)
	}
	outside := DecideOperationAt(governed, graph, cutoffs, string(OperationClose), graph.Phases[0].End)
	if outside.Code != Cycle009Rejected || outside.Field != "phase.window" || outside.State != StateOpen || outside.RevisionVersion != "v1" {
		t.Fatalf("outside-window decision = %+v, want CYCLE_009_REJECTED with phase/state/version", outside)
	}
	if len(explanation.AvailableTransitions) != 1 || explanation.AvailableTransitions[0] != OperationClose {
		t.Fatalf("available transitions = %v, want CLOSE only", explanation.AvailableTransitions)
	}
	if explanation.Rendering == "" || explanation.ResultDigest == "" || explanation.InputsDigest == "" {
		t.Fatal("explanation is missing human rendering or digests")
	}
	// No engine API in this package has a persistence boundary. A rejection is
	// therefore represented only by this typed value and cannot create rows,
	// events, work or provider requests.
}

// TestTodo_CYCLE_009_Property proves the explanation is detached from the
// compiled graph and stable when cutoff input order changes.
func TestTodo_CYCLE_009_Property(t *testing.T) {
	governed, graph, cutoffs, at := currentPhaseFixture(t)
	one, err := ExplainCurrentPhase(governed, graph, cutoffs, at)
	if err != nil {
		t.Fatal(err)
	}
	two, err := ExplainCurrentPhase(governed, graph, append([]CutoffResolution(nil), cutoffs...), at)
	if err != nil {
		t.Fatal(err)
	}
	if one.ResultDigest != two.ResultDigest || string(one.Canonical()) != string(two.Canonical()) {
		t.Fatalf("identical current-phase explanations are not stable: %q != %q", one.ResultDigest, two.ResultDigest)
	}
	one.Phase.AllowedOperations[0] = "MUTATED"
	if graph.Phases[0].AllowedOperations[0] == "MUTATED" {
		t.Fatal("explanation phase aliases the compiled graph")
	}
}

// TestTodo_CYCLE_009_Mutation covers phase-boundary and malformed-cutoff
// failures without any state mutation.
func TestTodo_CYCLE_009_Mutation(t *testing.T) {
	governed, graph, cutoffs, at := currentPhaseFixture(t)
	if _, err := ExplainCurrentPhase(governed, graph, cutoffs, graph.Phases[0].End); !errors.Is(err, ErrNoActivePhase) {
		t.Fatalf("end-of-window explanation = %v, want ErrNoActivePhase", err)
	}
	bad := append([]CutoffResolution(nil), cutoffs...)
	bad[0].Status = "UNKNOWN"
	if _, err := ExplainCurrentPhase(governed, graph, bad, at); !errors.Is(err, ErrCutoffExplanation) {
		t.Fatalf("invalid cutoff explanation = %v, want ErrCutoffExplanation", err)
	}
	if decision := (&CurrentPhaseExplanation{State: StateOpen, CycleRevisionVersion: "v1"}).DecideOperation(string(OperationLock)); decision.Code != Cycle009Rejected {
		t.Fatalf("incomplete explanation decision = %+v, want rejection", decision)
	}
}

// TestTodo_CYCLE_009_Golden pins the fixture's stable rendered explanation.
func TestTodo_CYCLE_009_Golden(t *testing.T) {
	governed, graph, cutoffs, at := currentPhaseFixture(t)
	explanation, err := ExplainCurrentPhase(governed, graph, cutoffs, at)
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "sha256:f1ac4bf9cf66490fb253ba2a57ee0f8611addcca186ac6653cc104219b43b67a"
	if explanation.ResultDigest != wantDigest {
		t.Fatalf("golden result digest = %q, want %q", explanation.ResultDigest, wantDigest)
	}
	if explanation.Rendering != explanation.HumanReadable || explanation.Rendering == "" {
		t.Fatal("golden rendering alias is not stable")
	}
}
