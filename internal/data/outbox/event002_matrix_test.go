package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
)

func TestTodo_EVENT_002_Golden(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.MustParse("0196a3f1-8f2e-7c4d-9b1a-2d3e4f5a6b7c")
	group, store := event002Group()
	var calls int
	record := event002Record(tenant, "golden-effect", "golden-partition")
	outcome, err := group.Dispatch(ctx, record, acceptHandler(&calls))
	if err != nil {
		t.Fatal(err)
	}
	if err := group.CommitCheckpoint(ctx, "golden-partition", tenant, "golden-effect"); err != nil {
		t.Fatal(err)
	}
	checkpoint, ok := store.CheckpointFor("projection-rebuild", tenant.String()+"\x00golden-partition")
	if !ok {
		t.Fatal("checkpoint missing")
	}
	got, err := json.MarshalIndent(struct {
		Outcome    outbox.ApplyOutcome `json:"outcome"`
		Checkpoint outbox.Checkpoint   `json:"checkpoint"`
	}{outcome, checkpoint}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const goldenPath = "testdata/event002_golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_EVENT_002_Race(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	group, _ := event002Group()
	const workers = 16
	type result struct {
		outcome outbox.ApplyOutcome
		err     error
	}
	results := make(chan result, workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	calls := 0
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcome, err := group.Dispatch(ctx, event002Record(tenant, "race-effect", "race-partition"),
				func(ctx context.Context, record outbox.Record, fencing outbox.Fencing) error {
					mu.Lock()
					calls++
					mu.Unlock()
					return nil
				})
			results <- result{outcome, err}
		}()
	}
	wg.Wait()
	close(results)
	applied := 0
	for r := range results {
		if r.err != nil {
			t.Fatalf("concurrent Dispatch failed: %v", r.err)
		}
		switch r.outcome {
		case outbox.ApplyApplied:
			applied++
		case outbox.ApplyDuplicateFenced:
		default:
			t.Fatalf("unexpected outcome %s", r.outcome)
		}
	}
	if applied != 1 || calls != 1 {
		t.Fatalf("applied = %d, calls = %d; want exactly one application", applied, calls)
	}
}

func TestTodo_EVENT_002_Integration(t *testing.T) {
	ctx := context.Background()
	tenantA, tenantB := uuid.New(), uuid.New()
	store := outbox.NewMemoryCheckpointStore()
	newGroup := func() *outbox.ConsumerGroup {
		group, err := outbox.NewConsumerGroup(outbox.GroupPolicy{
			Group: "projection-rebuild", MaxAttempts: 2, PoisonOwner: "data-owners",
			PoisonTTL: time.Hour, RepairRoute: "repair/poison-review",
			Clock: func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) },
		})
		if err != nil {
			t.Fatal(err)
		}
		group.Bind(store)
		return group
	}
	group := newGroup()
	var calls int
	flaky := map[string]int{"flaky-effect": 0}
	handler := func(ctx context.Context, record outbox.Record, fencing outbox.Fencing) error {
		if record.EffectIdentity == "flaky-effect" {
			flaky["flaky-effect"]++
			if flaky["flaky-effect"] < 2 {
				return errors.New("transient")
			}
		}
		if record.EffectIdentity == "doomed-effect" {
			return errors.New("permanent")
		}
		calls++
		return nil
	}
	plan := []outbox.Record{
		event002Record(tenantA, "good-1", "p1"),
		event002Record(tenantA, "flaky-effect", "p1"),
		event002Record(tenantB, "doomed-effect", "p1"),
		event002Record(tenantA, "good-2", "p2"),
	}
	for _, record := range plan {
		if _, err := group.Dispatch(ctx, record, handler); err != nil {
			t.Fatalf("Dispatch %s: %v", record.EffectIdentity, err)
		}
	}
	if _, err := group.Dispatch(ctx, plan[1], handler); err != nil {
		t.Fatal(err)
	}
	if outcome, err := group.Dispatch(ctx, plan[2], handler); err != nil || outcome != outbox.ApplyPoisonIsolated {
		t.Fatalf("doomed redelivery = %s, %v; want POISON_ISOLATED", outcome, err)
	}
	if err := group.CommitCheckpoint(ctx, "p1", tenantA, "good-1"); err != nil {
		t.Fatal(err)
	}
	if err := group.CommitCheckpoint(ctx, "p1", tenantA, "flaky-effect"); err != nil {
		t.Fatal(err)
	}
	if err := group.CommitCheckpoint(ctx, "p2", tenantA, "good-2"); err != nil {
		t.Fatal(err)
	}
	if poisoned := group.Poisoned("projection-rebuild"); len(poisoned) != 1 {
		t.Fatalf("poisoned = %+v, want the doomed record only", poisoned)
	}
	restarted := newGroup()
	replay, err := restarted.Dispatch(ctx, plan[0], handler)
	if err != nil {
		t.Fatal(err)
	}
	if replay != outbox.ApplyDuplicateFenced {
		t.Fatalf("post-restart replay = %s, want DUPLICATE_FENCED", replay)
	}
	if calls != 3 {
		t.Fatalf("handler calls = %d, want 3 applied effects", calls)
	}
}

func TestTodo_EVENT_002_Fault(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	group, _ := event002Group()
	if _, err := group.Dispatch(ctx, outbox.Record{}, acceptHandler(new(int))); !errors.Is(err, outbox.ErrInvalidRecord) {
		t.Fatalf("identity-free record accepted: %v", err)
	}
	if _, err := outbox.NewConsumerGroup(outbox.GroupPolicy{}); !errors.Is(err, outbox.ErrInvalidGroupPolicy) {
		t.Fatalf("empty policy accepted: %v", err)
	}
	unbound, err := outbox.NewConsumerGroup(outbox.GroupPolicy{
		Group: "g", MaxAttempts: 1, PoisonOwner: "o", PoisonTTL: time.Hour, RepairRoute: "r",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unbound.Dispatch(ctx, event002Record(tenant, "e", "p"), acceptHandler(new(int))); err == nil {
		t.Fatal("storeless group dispatched")
	}
	transient, err := group.Dispatch(ctx, event002Record(tenant, "t", "p"), func(ctx context.Context, record outbox.Record, fencing outbox.Fencing) error {
		return errors.New("boom")
	})
	if err != nil || transient != outbox.ApplyTransientFailed {
		t.Fatalf("first failure = %s, %v; want TRANSIENT_FAILED", transient, err)
	}
}

func TestTodo_EVENT_002_Security(t *testing.T) {
	ctx := context.Background()
	tenantA, tenantB := uuid.New(), uuid.New()
	group, _ := event002Group()
	failing := func(ctx context.Context, record outbox.Record, fencing outbox.Fencing) error {
		return errAlwaysFails
	}
	for i := 0; i < 3; i++ {
		if _, err := group.Dispatch(ctx, event002Record(tenantA, "a-poison", "shared"), failing); err != nil {
			t.Fatal(err)
		}
	}
	var calls int
	other, err := group.Dispatch(ctx, event002Record(tenantB, "b-healthy", "shared"), acceptHandler(&calls))
	if err != nil || other != outbox.ApplyApplied || calls != 1 {
		t.Fatalf("tenant B blocked by tenant A poison: %s, %v", other, err)
	}
	if err := group.CommitCheckpoint(ctx, "shared", tenantB, "b-healthy"); err != nil {
		t.Fatal(err)
	}
	if err := group.CommitCheckpoint(ctx, "shared", tenantA, "b-healthy"); err == nil {
		t.Fatal("tenant A checkpointed tenant B's offset")
	}
}

func TestTodo_EVENT_002_Recovery(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	store := outbox.NewMemoryCheckpointStore()
	group, err := outbox.NewConsumerGroup(outbox.GroupPolicy{
		Group: "projection-rebuild", MaxAttempts: 1, PoisonOwner: "data-owners",
		PoisonTTL: time.Hour, RepairRoute: "repair/poison-review",
		Clock: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	group.Bind(store)
	failing := func(ctx context.Context, record outbox.Record, fencing outbox.Fencing) error {
		return errAlwaysFails
	}
	if outcome, err := group.Dispatch(ctx, event002Record(tenant, "doomed", "p"), failing); err != nil || outcome != outbox.ApplyPoisonIsolated {
		t.Fatalf("doomed = %s, %v; want POISON_ISOLATED", outcome, err)
	}
	var calls int
	fixed := acceptHandler(&calls)
	replay, err := group.Dispatch(ctx, event002Record(tenant, "doomed", "p"), fixed)
	if err != nil || replay != outbox.ApplyPoisonIsolated || calls != 0 {
		t.Fatalf("poisoned replay redispatched: %s, calls = %d", replay, calls)
	}
	requeued := group.RequeueExpired(now.Add(2 * time.Hour))
	if len(requeued) != 1 || requeued[0] != "doomed" {
		t.Fatalf("requeued = %v, want [doomed]", requeued)
	}
	outcome, err := group.Dispatch(ctx, event002Record(tenant, "doomed", "p"), fixed)
	if err != nil || outcome != outbox.ApplyApplied || calls != 1 {
		t.Fatalf("repaired redispatch = %s, calls = %d; want one APPLIED", outcome, calls)
	}
	if err := group.CommitCheckpoint(ctx, "p", tenant, "doomed"); err != nil {
		t.Fatalf("checkpoint after repair failed: %v", err)
	}
}

func TestTodo_EVENT_002_Mutation(t *testing.T) {
	ctx := context.Background()
	tenant := uuid.New()
	t.Run("attempts below bound stay transient", func(t *testing.T) {
		group, _ := event002Group()
		failing := func(ctx context.Context, record outbox.Record, fencing outbox.Fencing) error {
			return errAlwaysFails
		}
		for i := 0; i < 2; i++ {
			outcome, err := group.Dispatch(ctx, event002Record(tenant, "m", "p"), failing)
			if err != nil || outcome != outbox.ApplyTransientFailed {
				t.Fatalf("attempt %d = %s, %v; want TRANSIENT_FAILED", i, outcome, err)
			}
		}
		if entries := group.Poisoned("projection-rebuild"); len(entries) != 0 {
			t.Fatalf("early poison: %+v", entries)
		}
	})
	t.Run("success resets attempts", func(t *testing.T) {
		group, _ := event002Group()
		failures := 0
		handler := func(ctx context.Context, record outbox.Record, fencing outbox.Fencing) error {
			if record.EffectIdentity == "flaky" && failures == 0 {
				failures++
				return errors.New("once")
			}
			return nil
		}
		if outcome, err := group.Dispatch(ctx, event002Record(tenant, "flaky", "p"), handler); err != nil || outcome != outbox.ApplyTransientFailed {
			t.Fatalf("first attempt = %s, %v; want TRANSIENT_FAILED", outcome, err)
		}
		if outcome, err := group.Dispatch(ctx, event002Record(tenant, "flaky", "p"), handler); err != nil || outcome != outbox.ApplyApplied {
			t.Fatalf("second attempt = %s, %v; want APPLIED", outcome, err)
		}
		if outcome, err := group.Dispatch(ctx, event002Record(tenant, "flaky", "p"), handler); err != nil || outcome != outbox.ApplyDuplicateFenced {
			t.Fatalf("recovered record = %s, %v", outcome, err)
		}
		if entries := group.Poisoned("projection-rebuild"); len(entries) != 0 {
			t.Fatalf("recovered record poisoned: %+v", entries)
		}
	})
}
