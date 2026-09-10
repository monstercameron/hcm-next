package intent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func discoveryChildren() []ChildTruth {
	return []ChildTruth{
		{ChildKey: "child-a", Outcome: ChildSucceeded, Scope: []string{"leave.read"}, Classification: "internal", IdempotencyKey: "idem-a", Evidence: "ev-a"},
		{ChildKey: "child-b", Outcome: ChildFailed, Scope: []string{"leave.read"}, Classification: "internal", IdempotencyKey: "idem-b", Evidence: "ev-b"},
		{ChildKey: "child-c", Outcome: ChildUnknown, Scope: []string{"leave.read"}, Classification: "internal", IdempotencyKey: "idem-c", Evidence: "ev-c"},
	}
}

func TestWorkflowDesignCompositionNeverCollapsesChildOrTriggerOutcome(t *testing.T) {
	aggregate, err := DeriveTruth("comp-1", []string{"leave.read", "leave.write"}, "internal", discoveryChildren(), []TriggerFiring{{FiringKey: "fire-1", TargetIntentID: "intent-1", IdempotencyKey: "fire-idem-1"}})
	if err != nil {
		t.Fatalf("DeriveTruth: %v", err)
	}
	// A failed child survives: the parent never reports success over it.
	if aggregate.Status != ChildFailed {
		t.Fatalf("status = %q, want failed", aggregate.Status)
	}
	if len(aggregate.Children) != 3 {
		t.Fatalf("aggregate collapsed %d children", len(aggregate.Children))
	}
	// Repair targets only affected nodes.
	if len(aggregate.RepairOnly) != 2 || aggregate.RepairOnly[0] != "child-b" || aggregate.RepairOnly[1] != "child-c" {
		t.Fatalf("repair targets = %v", aggregate.RepairOnly)
	}
	// Batch retry reruns successful items is refused at record time:
	// re-recording identical truth is idempotent, conflicting truth refuses.
	registry := NewTruthRegistry()
	if _, err := registry.Record("comp-1", []string{"leave.read", "leave.write"}, "internal", discoveryChildren(), nil); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := registry.Record("comp-1", []string{"leave.read", "leave.write"}, "internal", discoveryChildren(), nil); err != nil {
		t.Fatalf("idempotent re-record refused: %v", err)
	}
	// Trigger retry duplicating a target intent refuses.
	dup := []TriggerFiring{
		{FiringKey: "fire-1", TargetIntentID: "intent-1", IdempotencyKey: "same-idem"},
		{FiringKey: "fire-2", TargetIntentID: "intent-1", IdempotencyKey: "same-idem"},
	}
	if _, err := DeriveTruth("comp-2", []string{"leave.read"}, "internal", discoveryChildren()[:1], dup); err == nil {
		t.Fatal("duplicated trigger firing derived")
	}
	// Cancellation crossing an irreversible boundary breaches explicitly.
	irreversible := []ChildTruth{{ChildKey: "sealed", Outcome: ChildSucceeded, Scope: []string{"leave.read"}, Classification: "internal", IdempotencyKey: "idem-s", Evidence: "ev-s", Irreversible: true}}
	if _, err := Cancel(irreversible, "sealed"); err == nil {
		t.Fatal("silent irreversible cancellation")
	}
	cancelled, err := Cancel(discoveryChildren(), "child-a")
	if err != nil || !cancelled[0].Cancelled {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
}

func TestTodo_WF_DISC_011_Property(t *testing.T) {
	// Derivation is idempotent and lossless across orderings.
	first, err := DeriveTruth("comp-p", []string{"leave.read"}, "internal", discoveryChildren(), nil)
	if err != nil {
		t.Fatal(err)
	}
	reversed := discoveryChildren()
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	second, err := DeriveTruth("comp-p", []string{"leave.read"}, "internal", reversed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatal("child ordering changed derived truth")
	}
	// Repair targets always stay within affected nodes.
	for _, child := range second.Children {
		if child.Outcome == ChildSucceeded {
			for _, target := range second.RepairOnly {
				if target == child.ChildKey {
					t.Fatalf("successful child %s targeted for repair", child.ChildKey)
				}
			}
		}
	}
	// All-success derives success; all-unknown derives unknown.
	healthy := []ChildTruth{{ChildKey: "ok", Outcome: ChildSucceeded, Scope: []string{"s"}, Classification: "internal", IdempotencyKey: "i", Evidence: "e"}}
	good, err := DeriveTruth("comp-good", []string{"s"}, "internal", healthy, nil)
	if err != nil || good.Status != ChildSucceeded || len(good.RepairOnly) != 0 {
		t.Fatalf("good=%+v err=%v", good, err)
	}
}

func TestTodo_WF_DISC_011_Golden(t *testing.T) {
	aggregate, err := DeriveTruth("comp-1", []string{"leave.read", "leave.write"}, "internal", discoveryChildren(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	lines = append(lines, "composition="+aggregate.Composition+" status="+aggregate.Status)
	for _, child := range aggregate.Children {
		lines = append(lines, "child="+child.ChildKey+" outcome="+child.Outcome+" evidence="+child.Evidence)
	}
	lines = append(lines, "repair="+strings.Join(aggregate.RepairOnly, ","))
	lines = append(lines, "digest="+aggregate.Digest)
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "wf_disc011_discovery.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set HCMNEXT_UPDATE_GOLDEN=1)", err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestTodo_WF_DISC_011_Race(t *testing.T) {
	registry := NewTruthRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("comp-race-%d", i%4)
			if _, err := registry.Record(name, []string{"leave.read"}, "internal", discoveryChildren(), nil); err != nil {
				t.Errorf("Record(%s): %v", name, err)
			}
		}(i)
	}
	wg.Wait()
	for i := 0; i < 4; i++ {
		aggregate, ok := registry.Lookup(fmt.Sprintf("comp-race-%d", i))
		if !ok || aggregate.Status != ChildFailed || len(aggregate.Children) != 3 {
			t.Fatalf("race aggregate %d = %+v ok=%v", i, aggregate, ok)
		}
	}
}

func TestTodo_WF_DISC_011_Fault(t *testing.T) {
	if _, err := DeriveTruth("", []string{"s"}, "internal", discoveryChildren(), nil); err == nil {
		t.Fatal("empty composition derived")
	}
	dup := append(discoveryChildren(), discoveryChildren()[0])
	if _, err := DeriveTruth("comp-f", []string{"leave.read"}, "internal", dup, nil); err == nil {
		t.Fatal("duplicate child key derived")
	}
	rogue := discoveryChildren()
	rogue[0].Outcome = "evaporated"
	if _, err := DeriveTruth("comp-f", []string{"leave.read"}, "internal", rogue, nil); err == nil {
		t.Fatal("off-vocabulary outcome derived")
	}
	keyless := discoveryChildren()
	keyless[0].IdempotencyKey = ""
	if _, err := DeriveTruth("comp-f", []string{"leave.read"}, "internal", keyless, nil); err == nil {
		t.Fatal("keyless child derived")
	}
	if _, err := Cancel(discoveryChildren(), "ghost"); err == nil {
		t.Fatal("unknown child cancelled")
	}
	registry := NewTruthRegistry()
	if _, err := registry.Record("comp-c", []string{"leave.read"}, "internal", discoveryChildren(), nil); err != nil {
		t.Fatal(err)
	}
	conflict := discoveryChildren()
	conflict[1].Outcome = ChildSucceeded
	if _, err := registry.Record("comp-c", []string{"leave.read"}, "internal", conflict, nil); err == nil {
		t.Fatal("conflicting truth recorded")
	}
}

func TestTodo_WF_DISC_011_Security(t *testing.T) {
	// A child with broader authority than its parent never derives.
	broad := discoveryChildren()
	broad[0].Scope = []string{"leave.read", "payroll.write"}
	if _, err := DeriveTruth("comp-s", []string{"leave.read"}, "internal", broad, nil); err == nil {
		t.Fatal("authority expansion derived")
	}
	// Classification broadening refuses the same way.
	reclassified := discoveryChildren()
	reclassified[1].Classification = "restricted"
	if _, err := DeriveTruth("comp-s", []string{"leave.read"}, "internal", reclassified, nil); err == nil {
		t.Fatal("classification broadening derived")
	}
}

func TestTodo_WF_DISC_011_Conformance(t *testing.T) {
	cases := []struct {
		outcomes []string
		status   string
		repair   int
	}{
		{[]string{ChildSucceeded, ChildSucceeded}, ChildSucceeded, 0},
		{[]string{ChildSucceeded, ChildFailed}, ChildFailed, 1},
		{[]string{ChildFailed, ChildFailed}, ChildFailed, 2},
		{[]string{ChildSucceeded, ChildUnknown}, ChildUnknown, 1},
		{[]string{ChildUnknown, ChildUnknown}, ChildUnknown, 2},
		{[]string{ChildFailed, ChildUnknown}, ChildFailed, 2},
	}
	for i, tc := range cases {
		var children []ChildTruth
		for j, outcome := range tc.outcomes {
			children = append(children, ChildTruth{ChildKey: fmt.Sprintf("c%d", j), Outcome: outcome, Scope: []string{"s"}, Classification: "internal", IdempotencyKey: fmt.Sprintf("i%d", j), Evidence: "e"})
		}
		aggregate, err := DeriveTruth(fmt.Sprintf("comp-conf-%d", i), []string{"s"}, "internal", children, nil)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if aggregate.Status != tc.status || len(aggregate.RepairOnly) != tc.repair {
			t.Fatalf("case %d: status=%q repair=%v", i, aggregate.Status, aggregate.RepairOnly)
		}
	}
}

func TestTodo_WF_DISC_011_Mutation(t *testing.T) {
	base, err := DeriveTruth("comp-m", []string{"leave.read"}, "internal", discoveryChildren(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Flipping one child outcome moves the aggregate to its neighbor.
	flipped := discoveryChildren()
	flipped[1].Outcome = ChildSucceeded
	changed, err := DeriveTruth("comp-m", []string{"leave.read"}, "internal", flipped, nil)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Status != ChildUnknown || changed.Digest == base.Digest {
		t.Fatalf("flipped aggregate = %q (digest reused: %v)", changed.Status, changed.Digest == base.Digest)
	}
	// Evidence tampering re-identifies the aggregate.
	forged := discoveryChildren()
	forged[0].Evidence = "forged"
	sealed, err := DeriveTruth("comp-m", []string{"leave.read"}, "internal", forged, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.Digest == base.Digest {
		t.Fatal("forged evidence kept the aggregate digest")
	}
}
