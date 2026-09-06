package repair

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func testPlan() RepairPlan {
	return RepairPlan{
		ID: "repair-promotion-1", Digest: "sha256:plan-1", FindingDigest: "sha256:finding-1",
		ObservationDigest: "sha256:observation-1", AuthorityPolicy: "authority.promotion/1",
		MappingVersion: "mapping.iam/1", CredentialRef: "credential:iam-1", TargetVersion: "iam:worker-1@7",
		OriginalSemanticKey: "promotion:worker-1:proposal-1", FailedEffectKey: "effect:iam-provision",
		Steps: []Step{{Ordinal: 1, EffectKey: "effect:iam-provision", EffectRef: "operation:iam-1", Target: "iam:worker-1", ExpectedVersion: "iam:worker-1@7", MaxAttempts: 2}},
	}
}

func testEvidence() CurrentEvidence {
	return CurrentEvidence{
		PlanDigest: "sha256:plan-1", FindingDigest: "sha256:finding-1", ObservationDigest: "sha256:observation-1",
		AuthorityPolicy: "authority.promotion/1", MappingVersion: "mapping.iam/1", CredentialRef: "credential:iam-1",
		TargetVersion: "iam:worker-1@7", Facts: map[string]string{"target": "iam:worker-1"},
	}
}

func testRequest() RevalidationRequest {
	return RevalidationRequest{Plan: testPlan(), Current: testEvidence(), Now: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC), Actor: "operator:repair"}
}

// TestTodo_REPAIR_002 proves the execution-time contract: exact current
// evidence admits one fenced attempt, while every material change refuses
// before a corrective effect can be represented.
func TestTodo_REPAIR_002(t *testing.T) {
	store := NewMemoryStore()
	first, err := store.RevalidateAndFence(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != StatusReady || first.Fence.FenceID == "" {
		t.Fatalf("admission = %+v, want READY with a fence", first)
	}
	if first.Fence.OriginalSemanticKey != testPlan().OriginalSemanticKey {
		t.Fatalf("semantic identity = %q, want original %q", first.Fence.OriginalSemanticKey, testPlan().OriginalSemanticKey)
	}
	if first.Fence.FenceID == first.Fence.OriginalSemanticKey {
		t.Fatal("repair fence reused the parent semantic identity")
	}
	second, err := store.RevalidateAndFence(context.Background(), testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if second.Fence != first.Fence {
		t.Fatalf("replay fence = %+v, want %+v", second.Fence, first.Fence)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*CurrentEvidence)
		want   Status
	}{
		{name: "finding", mutate: func(e *CurrentEvidence) { e.FindingDigest = "sha256:new" }, want: StatusReplanRequired},
		{name: "observation", mutate: func(e *CurrentEvidence) { e.ObservationDigest = "sha256:new" }, want: StatusReplanRequired},
		{name: "authority", mutate: func(e *CurrentEvidence) { e.AuthorityPolicy = "authority.promotion/2" }, want: StatusReplanRequired},
		{name: "mapping", mutate: func(e *CurrentEvidence) { e.MappingVersion = "mapping.iam/2" }, want: StatusReplanRequired},
		{name: "credential", mutate: func(e *CurrentEvidence) { e.CredentialRef = "credential:iam-2" }, want: StatusReplanRequired},
		{name: "target", mutate: func(e *CurrentEvidence) { e.TargetVersion = "iam:worker-1@8" }, want: StatusReplanRequired},
		{name: "satisfied", mutate: func(e *CurrentEvidence) { e.TargetAlreadySatisfied = true }, want: StatusNoLongerRequired},
		{name: "superseded", mutate: func(e *CurrentEvidence) { e.SupersedingTransaction = "tx:new" }, want: StatusReplanRequired},
		{name: "unknown", mutate: func(e *CurrentEvidence) { e.Unknowns = []string{"credential outcome"} }, want: StatusUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := testRequest()
			tc.mutate(&req.Current)
			got, err := Revalidate(req)
			if err != nil || got.Status != tc.want || got.Fence.FenceID != "" {
				t.Fatalf("result = %+v err=%v, want %s and no fence", got, err, tc.want)
			}
		})
	}
}

func TestTodo_REPAIR_002_Golden(t *testing.T) {
	got, err := Revalidate(testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusReady || !strings.HasPrefix(got.EvidenceHash, "sha256:") {
		t.Fatalf("result = %+v, want deterministic evidence hash", got)
	}
}

func TestTodo_REPAIR_002_Race(t *testing.T) {
	store := NewMemoryStore()
	var wg sync.WaitGroup
	results := make(chan Result, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := store.RevalidateAndFence(context.Background(), testRequest())
			if err == nil {
				results <- result
			}
		}()
	}
	wg.Wait()
	close(results)
	var first Fence
	for result := range results {
		if first.FenceID == "" {
			first = result.Fence
		}
		if result.Fence != first {
			t.Fatalf("concurrent fence = %+v, want %+v", result.Fence, first)
		}
	}
}

func TestTodo_REPAIR_002_Fault(t *testing.T) {
	req := testRequest()
	req.Plan.Steps[0].MaxAttempts = 0
	if _, err := Revalidate(req); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("invalid plan error = %v, want ErrInvalidPlan", err)
	}
	req = testRequest()
	req.Current.PlanDigest = ""
	got, err := Revalidate(req)
	if err != nil || got.Status != StatusUnknown {
		t.Fatalf("incomplete evidence = %+v err=%v, want UNKNOWN", got, err)
	}
}

func TestTodo_REPAIR_002_Security(t *testing.T) {
	got, err := Revalidate(testRequest())
	if err != nil {
		t.Fatal(err)
	}
	text := Explain(got)
	if strings.Contains(text, "credential:iam-1") || strings.Contains(text, "promotion:worker-1") {
		t.Fatalf("explanation leaked sensitive identity: %s", text)
	}
}

func TestTodo_REPAIR_002_Mutation(t *testing.T) {
	req := testRequest()
	req.Plan.FailedEffectKey = "effect:other"
	if _, err := Revalidate(req); !errors.Is(err, ErrInvalidPlan) {
		t.Fatalf("unbound failed effect error = %v, want ErrInvalidPlan", err)
	}
}
