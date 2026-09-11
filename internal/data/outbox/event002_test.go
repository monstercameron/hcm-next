package outbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
)

var (
	errFencedHandler = errors.New("handler ran fenced")
	errAlwaysFails   = errors.New("effect always fails")
)

func event002Record(tenant uuid.UUID, effect, ordering string) outbox.Record {
	return outbox.Record{
		Tenant:         tenant,
		OutboxID:       uuid.New(),
		EffectIdentity: effect,
		OrderingKey:    ordering,
		Criticality:    "NORMAL",
		SchemaRef:      event001SchemaRef,
		Payload:        []byte("payload:" + effect),
	}
}

func event002Group() (*outbox.ConsumerGroup, *outbox.MemoryCheckpointStore) {
	store := outbox.NewMemoryCheckpointStore()
	group, err := outbox.NewConsumerGroup(outbox.GroupPolicy{
		Group:       "projection-rebuild",
		MaxAttempts: 3,
		PoisonOwner: "data-owners",
		PoisonTTL:   time.Hour,
		RepairRoute: "repair/poison-review",
		Clock:       func() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		panic(err)
	}
	group.Bind(store)
	return group, store
}

func acceptHandler(calls *int) outbox.EffectHandler {
	return func(ctx context.Context, record outbox.Record, fencing outbox.Fencing) error {
		if !fencing.AllowExternalEffects {
			return errFencedHandler
		}
		*calls++
		return nil
	}
}

// TestTodo_EVENT_002 proves consumer-group checkpoints commit with
// idempotent application, poison isolates per record with an owner, expiry
// and repair route, and replay never dispatches external effects twice.
func TestTodo_EVENT_002(t *testing.T) {
	ctx := context.Background()
	tenantA, tenantB := uuid.New(), uuid.New()
	group, _ := event002Group()
	var calls int
	handler := acceptHandler(&calls)

	first := event002Record(tenantA, "effect-1", "partition-1")
	outcome, err := group.Dispatch(ctx, first, handler)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if outcome != outbox.ApplyApplied || calls != 1 {
		t.Fatalf("outcome = %s, calls = %d; want APPLIED once", outcome, calls)
	}
	replay, err := group.Dispatch(ctx, first, handler)
	if err != nil {
		t.Fatalf("replay Dispatch: %v", err)
	}
	if replay != outbox.ApplyDuplicateFenced || calls != 1 {
		t.Fatalf("replay = %s, calls = %d; want fenced duplicate", replay, calls)
	}
	if err := group.CommitCheckpoint(ctx, "partition-1", tenantA, "effect-1"); err != nil {
		t.Fatalf("CommitCheckpoint: %v", err)
	}
	if err := group.CommitCheckpoint(ctx, "partition-1", tenantA, "effect-unknown"); err == nil {
		t.Fatal("checkpoint advanced before its effect applied")
	}

	t.Run("poison isolates without blocking its tenant", func(t *testing.T) {
		group, _ := event002Group()
		var calls int
		failing := func(ctx context.Context, record outbox.Record, fencing outbox.Fencing) error {
			return errAlwaysFails
		}
		poison := event002Record(tenantA, "poison-effect", "partition-9")
		var outcome outbox.ApplyOutcome
		var err error
		for i := 0; i < 3; i++ {
			outcome, err = group.Dispatch(ctx, poison, failing)
			if err != nil {
				t.Fatalf("Dispatch %d: %v", i, err)
			}
		}
		if outcome != outbox.ApplyPoisonIsolated {
			t.Fatalf("third failure = %s, want POISON_ISOLATED", outcome)
		}
		entries := group.Poisoned("projection-rebuild")
		if len(entries) != 1 || entries[0].Owner != "data-owners" || entries[0].RepairRoute != "repair/poison-review" {
			t.Fatalf("poison entry lacks owner/route: %+v", entries)
		}
		if !entries[0].ExpiresAt.After(time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)) {
			t.Fatalf("poison entry lacks expiry: %+v", entries[0])
		}
		healthy := event002Record(tenantA, "healthy-effect", "partition-9")
		handler := acceptHandler(&calls)
		if outcome, err := group.Dispatch(ctx, healthy, handler); err != nil || outcome != outbox.ApplyApplied {
			t.Fatalf("healthy record blocked by poison: %s, %v", outcome, err)
		}
		other := event002Record(tenantB, "tenant-b-effect", "partition-9")
		if outcome, err := group.Dispatch(ctx, other, handler); err != nil || outcome != outbox.ApplyApplied {
			t.Fatalf("unrelated tenant blocked by poison: %s, %v", outcome, err)
		}
	})
}
