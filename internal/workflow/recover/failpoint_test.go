package recover

import (
	"context"
	"testing"
)

func TestFailpoint_EveryDeclaredPhaseIsListedOnceAndIsValid(t *testing.T) {
	phases := Phases()
	if len(phases) != 4 {
		t.Fatalf("Phases() returned %d boundaries, want the four WF-RUN-003 declares: %v", len(phases), phases)
	}
	seen := map[Phase]bool{}
	for _, p := range phases {
		if !p.Valid() {
			t.Fatalf("Phases() returned %q, which Valid() rejects", p)
		}
		if seen[p] {
			t.Fatalf("Phases() returned %q twice", p)
		}
		seen[p] = true
	}
	for _, want := range []Phase{
		PhaseBeforeNodeStateCommit,
		PhaseAfterNodeStateCommitBeforeDispatch,
		PhaseAfterDispatchBeforeResultCommit,
		PhaseAfterResultCommit,
	} {
		if !seen[want] {
			t.Fatalf("Phases() omits %q", want)
		}
	}
	if Phase("SOMETHING_ELSE").Valid() {
		t.Fatal("an undeclared phase reported itself valid")
	}
}

func TestFailpoint_PhasesReturnsACopy(t *testing.T) {
	first := Phases()
	first[0] = "MUTATED"
	if Phases()[0] != PhaseBeforeNodeStateCommit {
		t.Fatal("Phases() handed out its own backing array")
	}
}

func TestFailpoint_NoFailpointNeverCrashes(t *testing.T) {
	for _, p := range Phases() {
		if err := (NoFailpoint{}).Check(context.Background(), p); err != nil {
			t.Fatalf("NoFailpoint crashed at %s: %v", p, err)
		}
	}
}

func TestFailpoint_CrashAtFiresOnceAtOneBoundaryOnly(t *testing.T) {
	ctx := context.Background()
	fp := &CrashAt{Phase: PhaseAfterDispatchBeforeResultCommit}
	if fp.Fired() {
		t.Fatal("a fresh CrashAt reported it had already fired")
	}
	for _, p := range Phases() {
		if p == PhaseAfterDispatchBeforeResultCommit {
			continue
		}
		if err := fp.Check(ctx, p); err != nil {
			t.Fatalf("CrashAt(%s) crashed at %s", fp.Phase, p)
		}
	}
	if err := fp.Check(ctx, PhaseAfterDispatchBeforeResultCommit); err == nil {
		t.Fatal("CrashAt did not crash at its own boundary")
	}
	if !fp.Fired() {
		t.Fatal("CrashAt did not record that it had fired")
	}
	if err := fp.Check(ctx, PhaseAfterDispatchBeforeResultCommit); err != nil {
		t.Fatalf("CrashAt crashed a second time at the same boundary: %v", err)
	}
}

func TestFailpoint_NilCrashAtIsInert(t *testing.T) {
	var fp *CrashAt
	if err := fp.Check(context.Background(), PhaseBeforeNodeStateCommit); err != nil {
		t.Fatalf("a nil CrashAt crashed: %v", err)
	}
	if fp.Fired() {
		t.Fatal("a nil CrashAt reported it had fired")
	}
}
