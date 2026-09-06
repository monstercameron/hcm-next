package performance

import (
	"errors"
	"strings"
	"testing"
)

func testRefs() (PopulationBindingRef, CalendarBindingRef, RatingScaleVersionRef, EvidenceRef) {
	return PopulationBindingRef{DefinitionID: "population-2026", RevisionVersion: "v4", Digest: "sha256:population"},
		CalendarBindingRef{Ref: "US.gregorian", Version: "v2026", Digest: "sha256:calendar"},
		RatingScaleVersionRef{ID: "performance-scale", Version: "v3", Digest: "sha256:scale"},
		EvidenceRef{ID: "calibration-record", Version: "v2", Digest: "sha256:evidence"}
}

func newTestCycle(t *testing.T) PerformanceCycle {
	t.Helper()
	population, calendar, scale, _ := testRefs()
	cycle, err := NewPerformanceCycle("cycle-2026", population, calendar, scale)
	if err != nil {
		t.Fatalf("NewPerformanceCycle: %v", err)
	}
	return cycle
}

func TestTodo_PERFORMANCE_001(t *testing.T) {
	cycle := newTestCycle(t)
	if cycle.State != PerformanceCyclePlanned || cycle.Revision != 1 || cycle.CanonicalDigest == "" {
		t.Fatalf("initial cycle = %+v", cycle)
	}
	evidence := testRefsEvidence(t)
	open, err := cycle.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	calibrating, err := open.BeginCalibration(evidence...)
	if err != nil {
		t.Fatalf("BeginCalibration: %v", err)
	}
	closed, err := calibrating.Close()
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	locked, err := closed.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if locked.State != PerformanceCycleLocked || locked.Revision != 5 || locked.SupersedesRevision != 4 {
		t.Fatalf("locked cycle = %+v", locked)
	}
	if cycle.State != PerformanceCyclePlanned || cycle.Revision != 1 {
		t.Fatal("transition mutated the prior revision")
	}
	if got, err := locked.Digest(); err != nil || got != locked.CanonicalDigest {
		t.Fatalf("Digest = %q, %v; canonical = %q", got, err, locked.CanonicalDigest)
	}
}

func testRefsEvidence(t *testing.T) []EvidenceRef {
	t.Helper()
	_, _, _, first := testRefs()
	return []EvidenceRef{first}
}

func TestTodo_PERFORMANCE_001_Property(t *testing.T) {
	cycle := newTestCycle(t)
	population, calendar, scale, first := testRefs()
	withA, err := NewPerformanceCycle("cycle-2026", population, calendar, scale)
	if err != nil {
		t.Fatalf("NewPerformanceCycle A: %v", err)
	}
	withB, err := NewPerformanceCycle("cycle-2026", population, calendar, scale)
	if err != nil {
		t.Fatalf("NewPerformanceCycle B: %v", err)
	}
	if withA.CanonicalDigest != withB.CanonicalDigest {
		t.Fatal("identical cycles have different canonical digests")
	}
	a, err := cycle.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	b, err := a.BeginCalibration(first)
	if err != nil {
		t.Fatalf("BeginCalibration: %v", err)
	}
	if got := b.CalibrationEvidenceRefs(); len(got) != 1 || got[0] != first {
		t.Fatalf("CalibrationEvidenceRefs = %+v", got)
	}
	got := b.CalibrationEvidenceRefs()
	got[0].Digest = "sha256:tampered"
	if b.CalibrationEvidence[0].Digest == got[0].Digest {
		t.Fatal("CalibrationEvidenceRefs returned aliased storage")
	}
	if strings.Contains(string(b.Canonical()), "tampered") {
		t.Fatal("canonical bytes exposed a mutated detached evidence ref")
	}
}

func TestTodo_PERFORMANCE_001_Conformance(t *testing.T) {
	cycle := newTestCycle(t)
	if _, err := cycle.Lock(); !errors.Is(err, ErrLockWithoutClose) || !errors.Is(err, ErrTransitionRejected) {
		t.Fatalf("lock from planned error = %v, want typed lock refusal", err)
	}
	var refusal *TransitionRefusal
	if _, err := cycle.Lock(); !errors.As(err, &refusal) {
		t.Fatalf("lock refusal = %v, want *TransitionRefusal", err)
	}
	if refusal.Field != "state" || refusal.From != PerformanceCyclePlanned || refusal.To != PerformanceCycleLocked || refusal.Revision != 1 {
		t.Fatalf("refusal = %+v", refusal)
	}
	open, err := cycle.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := open.Close(); !errors.Is(err, ErrOutOfOrderTransition) {
		t.Fatalf("close from open error = %v, want out-of-order refusal", err)
	}
	if _, err := open.BeginCalibration(); !errors.Is(err, ErrCalibrationEvidence) {
		t.Fatalf("calibration without evidence error = %v, want evidence refusal", err)
	}
}

func TestTodo_PERFORMANCE_001_Mutation(t *testing.T) {
	cycle := newTestCycle(t)
	cycle.CanonicalDigest = "sha256:forged"
	if err := cycle.Validate(); err == nil {
		t.Fatal("forged canonical digest passed validation")
	}
	open, err := newTestCycle(t).Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := open.Transition(PerformanceCycleClosed); err == nil {
		t.Fatal("out-of-order close without calibration passed")
	}
	if open.State != PerformanceCycleOpen || open.Revision != 2 {
		t.Fatalf("failed transition mutated source revision: %+v", open)
	}
}
