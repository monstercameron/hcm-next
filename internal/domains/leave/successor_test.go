package leave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func extendIntent() SuccessorIntent {
	return SuccessorIntent{
		Kind: SuccessorExtend, LeaveRecordID: "leave:w1",
		PriorIntentDigest: "sha256:intent-1", PriorProposalDigest: "sha256:proposal-1",
		NewStart: 10, NewEnd: 24,
		ReplanDigest: "sha256:replan-2", ReplanPrior: "sha256:plan-1",
		ConflictRule: "prior-wins", ConflictWinner: "leave:w1",
	}
}

func TestTodo_LEAVE_010(t *testing.T) {
	registry := NewSuccessorRegistry()
	extended, err := registry.Declare(extendIntent(), "sha256:plan-1")
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if len(extended.Lineage) != 2 || extended.Lineage[0] != "sha256:intent-1" {
		t.Fatalf("lineage=%v", extended.Lineage)
	}
	if err := extended.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Shortenings and cancellations join the same related graph.
	shortened := extendIntent()
	shortened.Kind = SuccessorShorten
	shortened.NewEnd = 14
	if _, err := registry.Declare(shortened, "sha256:plan-1"); err == nil {
		t.Fatal("overlapping shorten committed alongside the extension")
	}
	// Cancellation settles consumed balance instead of rewriting it.
	cancelled, err := registry.Declare(SuccessorIntent{
		Kind: SuccessorCancel, LeaveRecordID: "leave:w1",
		PriorIntentDigest: "sha256:intent-1", PriorProposalDigest: "sha256:proposal-1",
		NewStart: 10, NewEnd: 17, ConsumedHours: 16, BalanceSettlement: "settle:pto-16",
	}, "sha256:plan-1")
	if err != nil {
		t.Fatalf("Declare cancel: %v", err)
	}
	if err := cancelled.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// RED: bypassed replanning, unsettled cancellation and hollow kinds refuse.
	bypass := extendIntent()
	bypass.ReplanDigest = ""
	if _, err := NewSuccessorRegistry().Declare(bypass, "sha256:plan-1"); err == nil {
		t.Fatal("replan bypass declared")
	}
	unsettled := SuccessorIntent{Kind: SuccessorCancel, LeaveRecordID: "leave:w1", PriorIntentDigest: "sha256:i", PriorProposalDigest: "sha256:p", NewStart: 10, NewEnd: 17, ConsumedHours: 4}
	if _, err := NewSuccessorRegistry().Declare(unsettled, "sha256:plan-1"); err == nil {
		t.Fatal("unsettled cancellation declared")
	}
	rogue := extendIntent()
	rogue.Kind = "MutateLeave"
	if _, err := NewSuccessorRegistry().Declare(rogue, "sha256:plan-1"); err == nil {
		t.Fatal("off-vocabulary successor declared")
	}
	var nilRegistry *SuccessorRegistry
	if _, err := nilRegistry.Declare(extendIntent(), "sha256:plan-1"); err == nil {
		t.Fatal("nil registry declared")
	}
}

func TestTodo_LEAVE_010_Property(t *testing.T) {
	registry := NewSuccessorRegistry()
	first, err := registry.Declare(extendIntent(), "sha256:plan-1")
	if err != nil {
		t.Fatal(err)
	}
	// Identical redeclaration refuses: the graph holds one chronology.
	if _, err := registry.Declare(extendIntent(), "sha256:plan-1"); err == nil {
		t.Fatal("identical successor redeclared")
	}
	// A disjoint later extension commits beside the first.
	later := extendIntent()
	later.NewStart = 30
	later.NewEnd = 35
	second, err := registry.Declare(later, "sha256:plan-1")
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if second.Digest == first.Digest {
		t.Fatal("disjoint successor reused the seal")
	}
}

func TestTodo_LEAVE_010_Golden(t *testing.T) {
	registry := NewSuccessorRegistry()
	extended, err := registry.Declare(extendIntent(), "sha256:plan-1")
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{
		"kind=" + extended.Kind,
		"record=" + extended.LeaveRecordID,
		"lineage=" + strings.Join(extended.Lineage, ","),
		"digest=" + extended.Digest,
	}
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "leave010_successor.golden")
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

func TestTodo_LEAVE_010_Security(t *testing.T) {
	// Cancellation of unconsumed leave needs no settlement but still
	// binds its lineage.
	registry := NewSuccessorRegistry()
	zero := SuccessorIntent{Kind: SuccessorCancel, LeaveRecordID: "leave:w2", PriorIntentDigest: "sha256:i", PriorProposalDigest: "sha256:p", NewStart: 10, NewEnd: 17}
	cancelled, err := registry.Declare(zero, "sha256:plan-1")
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if len(cancelled.Lineage) != 2 {
		t.Fatalf("lineage=%v", cancelled.Lineage)
	}
	// Forged successors never verify.
	forged := cancelled
	forged.NewEnd = 99
	if err := forged.Verify(); err == nil {
		t.Fatal("forged successor verified")
	}
}

func TestTodo_LEAVE_010_Mutation(t *testing.T) {
	registry := NewSuccessorRegistry()
	base, err := registry.Declare(extendIntent(), "sha256:plan-1")
	if err != nil {
		t.Fatal(err)
	}
	// Interval change re-identifies the successor.
	shifted := extendIntent()
	shifted.NewEnd = 28
	// Overlaps base (10-24 vs 10-28): declare on a fresh registry.
	fresh := NewSuccessorRegistry()
	moved, err := fresh.Declare(shifted, "sha256:plan-1")
	if err != nil {
		t.Fatal(err)
	}
	if moved.Digest == base.Digest {
		t.Fatal("interval mutation kept the successor seal")
	}
}
