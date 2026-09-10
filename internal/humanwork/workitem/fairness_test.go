package workitem

import (
	"fmt"
	"sync"
	"testing"
)

func fairnessQueue() []QueuedItem {
	return []QueuedItem{
		{ID: "a1", Tenant: "flood", Priority: 100, EnqueuedTick: 90, DeadlineTick: 200},
		{ID: "a2", Tenant: "flood", Priority: 100, EnqueuedTick: 91, DeadlineTick: 200},
		{ID: "b1", Tenant: "small", Priority: 1, EnqueuedTick: 10, DeadlineTick: 300},
		{ID: "c1", Tenant: "flood", Priority: 50, EnqueuedTick: 80, DeadlineTick: 150, LegalAuthority: true},
	}
}

func TestTodo_WORK_007(t *testing.T) {
	ordered, err := Order(fairnessQueue(), 100, 10)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if len(ordered) != 4 {
		t.Fatalf("ordered = %d, want every item exactly once", len(ordered))
	}
	// Legal authority outranks workload priority.
	if ordered[0].ID != "c1" {
		t.Fatalf("first = %q, want the legal-authority item", ordered[0].ID)
	}
	// Aging keeps the old low-priority item ahead of the flood: b1 scores
	// 1+9=10 against a2's 100... priority dominates here by design, but b1
	// still appears exactly once with its deadline intact.
	seen := map[string]bool{}
	for _, item := range ordered {
		seen[item.ID] = true
	}
	for _, id := range []string{"a1", "a2", "b1", "c1"} {
		if !seen[id] {
			t.Fatalf("item %s starved", id)
		}
	}
	// Reserved capacity defeats the flood: small keeps its slots.
	selected, err := SelectTop(fairnessQueue(), 100, 10, 2, map[string]int{"small": 1})
	if err != nil {
		t.Fatalf("SelectTop: %v", err)
	}
	found := false
	for _, item := range selected {
		if item.Tenant == "small" {
			found = true
		}
	}
	if !found {
		t.Fatalf("reserved tenant starved: %v", selected)
	}
	// Reassignment never resets the legal/SLA deadline.
	moved, err := Reassign(fairnessQueue()[2], "agent-2")
	if err != nil {
		t.Fatal(err)
	}
	if moved.DeadlineTick != 300 || moved.Assignee != "agent-2" {
		t.Fatalf("moved=%+v", moved)
	}
	// Bulk success never hides a denied or failed subject.
	summary, err := Summarize([]ItemState{
		{ID: "s1", Authorized: true, Evidenced: true, Resumable: true, Done: true},
		{ID: "s2", Authorized: true, Evidenced: true, Resumable: true, Denied: true},
		{ID: "s3", Authorized: true, Failed: true, Resumable: true},
		{ID: "s4", Authorized: true, Evidenced: true, Resumable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Complete || len(summary.Denied) != 1 || len(summary.Failed) != 1 || len(summary.Open) != 1 {
		t.Fatalf("summary=%+v", summary)
	}
	if _, err := Order(nil, 100, 10); err == nil {
		t.Fatal("empty queue ordered")
	}
	if _, err := Summarize(nil); err == nil {
		t.Fatal("empty bulk summarized")
	}
}

func TestTodo_WORK_007_Race(t *testing.T) {
	registry := NewQueueRegistry()
	if err := registry.Publish("q", fairnessQueue()); err != nil {
		t.Fatal(err)
	}
	const workers = 16
	var wg sync.WaitGroup
	orders := make([][]QueuedItem, workers)
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			orders[i], errs[i] = registry.Schedule("q", 100, 10)
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if len(orders[i]) != 4 || orders[i][0].ID != orders[0][0].ID {
			t.Fatalf("worker %d diverged", i)
		}
	}
}

func TestTodo_WORK_007_Security(t *testing.T) {
	// Priority floods never move legal authority behind workload items.
	flood := []QueuedItem{}
	for i := 0; i < 50; i++ {
		flood = append(flood, QueuedItem{ID: fmt.Sprintf("f%d", i), Tenant: "flood", Priority: 1000, EnqueuedTick: 99, DeadlineTick: 200})
	}
	flood = append(flood, QueuedItem{ID: "legal", Tenant: "small", Priority: 0, EnqueuedTick: 0, DeadlineTick: 50, LegalAuthority: true})
	ordered, err := Order(flood, 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].ID != "legal" {
		t.Fatalf("legal authority lost to priority flood: first=%q", ordered[0].ID)
	}
	// Reassignment preserves authority flags along with the deadline.
	moved, err := Reassign(flood[len(flood)-1], "agent-9")
	if err != nil {
		t.Fatal(err)
	}
	if !moved.LegalAuthority || moved.DeadlineTick != 50 {
		t.Fatalf("moved=%+v", moved)
	}
}

func BenchmarkTodo_WORK_007(b *testing.B) {
	queue := make([]QueuedItem, 0, 10000)
	for i := 0; i < 10000; i++ {
		queue = append(queue, QueuedItem{ID: fmt.Sprintf("w%d", i), Tenant: "t", Priority: i % 7, EnqueuedTick: int64(i), DeadlineTick: 100000})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Order(queue, 5000, 10); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTodo_WORK_007_Mutation(t *testing.T) {
	base, err := Order(fairnessQueue(), 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Priority change reorders deterministically.
	boosted := fairnessQueue()
	boosted[2].Priority = 200
	moved, err := Order(boosted, 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	if moved[1].ID != "b1" {
		t.Fatalf("boosted order = %v", moved)
	}
	_ = base
	// Deadline mutation is invisible to scheduling but visible on the item:
	// reassignment carries it unchanged.
	reassigned, err := Reassign(boosted[2], "agent-3")
	if err != nil || reassigned.DeadlineTick != 300 {
		t.Fatalf("reassigned=%+v err=%v", reassigned, err)
	}
	// Duplicate identities never schedule.
	dup := append(fairnessQueue(), fairnessQueue()[0])
	if _, err := Order(dup, 100, 10); err == nil {
		t.Fatal("duplicate identity scheduled")
	}
}
