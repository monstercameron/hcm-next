package runner

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/conformance/internal/reporoot"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/model"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab"
)

func testVocab(t *testing.T) *vocab.Vocabulary {
	t.Helper()
	root, err := reporoot.Find()
	if err != nil {
		t.Fatalf("reporoot.Find: %v", err)
	}
	v, err := vocab.Load(filepath.Join(root, "planning", "specs", "workflow-runtime.md"))
	if err != nil {
		t.Fatalf("vocab.Load: %v", err)
	}
	return v
}

func TestDocumentRunnerSimulateIsZeroEffectAndUsesGivenClock(t *testing.T) {
	dr := DocumentRunner{Vocab: testVocab(t)}
	wf := &model.Workflow{ID: "manager-change", Kind: model.KindDocument, RawText: "# Manager Change\n"}

	clk := FixedClock{At: time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC)}
	receipt, err := dr.Simulate(wf, clk)
	if err != nil {
		t.Fatalf("Simulate: %v", err)
	}
	if receipt.WorkflowID != "manager-change" {
		t.Errorf("WorkflowID = %q, want %q", receipt.WorkflowID, "manager-change")
	}
	if receipt.ExecutionMode != "SIMULATE" {
		t.Errorf("ExecutionMode = %q, want SIMULATE", receipt.ExecutionMode)
	}
	if !receipt.ClockAt.Equal(clk.At) {
		t.Errorf("ClockAt = %v, want %v", receipt.ClockAt, clk.At)
	}
	if len(receipt.Effects) != 0 {
		t.Errorf("Effects = %+v, want empty (SIMULATE never mutates or invokes an external effect)", receipt.Effects)
	}
	if len(receipt.Checks) == 0 {
		t.Error("Checks is empty, want every registered check to have run")
	}
}

func TestDefaultFixedClockIsStable(t *testing.T) {
	want := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := DefaultFixedClock.Now(); !got.Equal(want) {
		t.Errorf("DefaultFixedClock.Now() = %v, want %v", got, want)
	}
	// Calling Now() twice must return the identical instant: a runner
	// under test must never observe wall-clock drift mid-run.
	if a, b := DefaultFixedClock.Now(), DefaultFixedClock.Now(); !a.Equal(b) {
		t.Errorf("DefaultFixedClock.Now() not stable across calls: %v != %v", a, b)
	}
}

func TestSimulateNeverErrors(t *testing.T) {
	dr := DocumentRunner{Vocab: testVocab(t)}
	// Even a maximally empty workflow (no title, no diagrams, no
	// scenarios) must produce a full set of FAIL/UNKNOWN checks rather
	// than an error: every document-shape problem is a reportable check
	// result, not a runner failure.
	wf := &model.Workflow{}
	if _, err := dr.Simulate(wf, DefaultFixedClock); err != nil {
		t.Fatalf("Simulate(empty workflow): %v, want nil error", err)
	}
}
