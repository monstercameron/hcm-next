package intent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
)

func batchSnapshot() population.Snapshot {
	return population.Snapshot{
		DefinitionID:     "change-request/bulk",
		DefinitionDigest: "sha256:def",
		RevisionVersion:  "2026.1",
		SubjectIDs:       []string{"s1", "s2", "s3", "s4", "denied:s5", "unknown:s6"},
		Digest:           "sha256:frozen",
	}
}

func batchSpec() BatchSpec {
	return BatchSpec{
		DefinitionID:      "change-request/bulk",
		PopulationScope:   "population/acme",
		Snapshot:          batchSnapshot(),
		OperationTemplate: "leave-accrue",
		OperationVersion:  "v7",
		ChildFamily:       "change-request",
		Exclusions:        []string{"s4"},
		PartitionSize:     2,
		Limits:            BatchLimits{MaxSubjects: 10, MaxCost: 100, MaxRate: 5, CostPerChild: 3},
		CompletionPolicy:  "all-or-repair",
	}
}

type stubExecutor struct {
	outcome string
	calls   int
}

func (s *stubExecutor) Execute(child ChildIntent) ChildResult {
	s.calls++
	return ChildResult{Outcome: s.outcome, Detail: "stub"}
}

func TestBatchIntentUsesFrozenPopulationAndBoundedChildren(t *testing.T) {
	first, err := CompileBatch(batchSpec())
	if err != nil {
		t.Fatalf("CompileBatch: %v", err)
	}
	second, err := CompileBatch(batchSpec())
	if err != nil {
		t.Fatalf("CompileBatch: %v", err)
	}
	if first.Digest != second.Digest || first.BatchID != second.BatchID {
		t.Fatal("identical frozen input compiled to different batches")
	}
	for i := range first.Children {
		if first.Children[i].ChildID != second.Children[i].ChildID {
			t.Fatal("child identities are not deterministic")
		}
	}
	// Exact partition: 3 eligible, 1 excluded, 1 denied, 1 unknown.
	if first.Counts != (BatchCounts{Eligible: 3, Ineligible: 1, Denied: 1, Unknown: 1}) {
		t.Fatalf("counts = %+v", first.Counts)
	}
	if len(first.Children) != 3 {
		t.Fatalf("children = %d, want 3", len(first.Children))
	}
	for _, child := range first.Children {
		if strings.HasPrefix(child.SubjectID, "denied:") {
			t.Fatalf("denied subject %q gained a child identity", child.SubjectID)
		}
		if child.Family == "batch" || child.Family == "bulk" {
			t.Fatalf("child carries batch kernel family %q", child.Family)
		}
	}
	if first.Children[2].Partition != 1 || first.Children[0].Partition != 0 {
		t.Fatalf("partitions are not bounded: %+v", first.Children)
	}
	// Execution partitions exactly; resume never duplicates.
	executor := &stubExecutor{outcome: BatchSucceeded}
	results, counts, err := ApplyBatch(first, executor, nil)
	if err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}
	if counts.Succeeded != 3 || executor.calls != 3 {
		t.Fatalf("counts=%+v calls=%d", counts, executor.calls)
	}
	resumedExecutor := &stubExecutor{outcome: BatchFailed}
	resumed, resumedCounts, err := ApplyBatch(first, resumedExecutor, results)
	if err != nil {
		t.Fatalf("resume ApplyBatch: %v", err)
	}
	if resumedExecutor.calls != 0 || resumedCounts.Succeeded != 3 {
		t.Fatalf("resume duplicated work: calls=%d counts=%+v", resumedExecutor.calls, resumedCounts)
	}
	for i := range results {
		if resumed[i].ChildID != results[i].ChildID {
			t.Fatal("resume changed recorded child identities")
		}
	}
	// RED cases refuse with zero partial batch.
	spec := batchSpec()
	spec.Snapshot = population.Snapshot{}
	if _, err := CompileBatch(spec); err == nil {
		t.Fatal("unfrozen snapshot compiled")
	}
	spec = batchSpec()
	spec.Snapshot.MembershipProtected = true
	if _, err := CompileBatch(spec); err == nil {
		t.Fatal("protected snapshot enumerated")
	}
	spec = batchSpec()
	spec.ChildFamily = "batch"
	if _, err := CompileBatch(spec); err == nil {
		t.Fatal("batch kernel family accepted")
	}
	spec = batchSpec()
	spec.Limits.MaxSubjects = 2
	if _, err := CompileBatch(spec); err == nil {
		t.Fatal("blast-radius breach compiled")
	}
	spec = batchSpec()
	spec.Limits.MaxCost = 8
	if _, err := CompileBatch(spec); err == nil {
		t.Fatal("cost breach compiled")
	}
	if _, _, err := ApplyBatch(first, nil, nil); err == nil {
		t.Fatal("nil executor executed")
	}
	dup := append([]ChildResult(nil), results...)
	dup = append(dup, results[0])
	if _, _, err := ApplyBatch(first, executor, dup); err == nil {
		t.Fatal("duplicated prior result applied")
	}
	rebound := append([]ChildResult(nil), results...)
	rebound[0].SubjectID = "mallory"
	if _, _, err := ApplyBatch(first, executor, rebound); err == nil {
		t.Fatal("rebound prior result applied")
	}
	rogue := &stubExecutor{outcome: "evaporated"}
	if _, _, err := ApplyBatch(first, rogue, nil); err == nil {
		t.Fatal("off-vocabulary child outcome applied")
	}
}

func TestTodo_INTENT_019_Golden(t *testing.T) {
	batch, err := CompileBatch(batchSpec())
	if err != nil {
		t.Fatalf("CompileBatch: %v", err)
	}
	var lines []string
	lines = append(lines, "batch="+batch.BatchID)
	lines = append(lines, fmt.Sprintf("counts=eligible:%d ineligible:%d denied:%d unknown:%d", batch.Counts.Eligible, batch.Counts.Ineligible, batch.Counts.Denied, batch.Counts.Unknown))
	for _, child := range batch.Children {
		lines = append(lines, "child="+child.ChildID+" subject="+child.SubjectID+" partition="+fmt.Sprint(child.Partition))
	}
	results, counts, err := ApplyBatch(batch, &stubExecutor{outcome: BatchSucceeded}, nil)
	if err != nil {
		t.Fatalf("ApplyBatch: %v", err)
	}
	lines = append(lines, fmt.Sprintf("results=%d succeeded=%d", len(results), counts.Succeeded))
	lines = append(lines, "digest="+batch.Digest)
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", "intent019_batch.golden")
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

func TestTodo_INTENT_019_Mutation(t *testing.T) {
	base, err := CompileBatch(batchSpec())
	if err != nil {
		t.Fatalf("CompileBatch: %v", err)
	}
	// Membership change changes the batch digest: no silent drift.
	mutated := batchSpec()
	mutated.Snapshot.SubjectIDs = append(append([]string(nil), batchSnapshot().SubjectIDs...), "s7")
	changed, err := CompileBatch(mutated)
	if err != nil {
		t.Fatalf("CompileBatch: %v", err)
	}
	if changed.Digest == base.Digest {
		t.Fatal("membership mutation kept the batch digest")
	}
	// Exclusion change shifts the exact partition.
	excluded := batchSpec()
	excluded.Exclusions = []string{"s3", "s4"}
	shifted, err := CompileBatch(excluded)
	if err != nil {
		t.Fatalf("CompileBatch: %v", err)
	}
	if shifted.Counts.Eligible != 2 || shifted.Counts.Ineligible != 2 {
		t.Fatalf("shifted counts = %+v", shifted.Counts)
	}
	// Template change re-identifies every child.
	retargeted := batchSpec()
	retargeted.OperationTemplate = "pay-adjust"
	reidentified, err := CompileBatch(retargeted)
	if err != nil {
		t.Fatalf("CompileBatch: %v", err)
	}
	for i := range base.Children {
		if reidentified.Children[i].ChildID == base.Children[i].ChildID {
			t.Fatal("template mutation reused a child identity")
		}
	}
}
