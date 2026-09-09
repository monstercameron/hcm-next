package runtimestate_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
)

func TestTodo_OBS_013_RuntimeSchedulingCausalMetadataRoundTripsThroughConsumerQueries(t *testing.T) {
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "obs-013-runtime")
	ctx := context.Background()
	expires := fixedInstant.Add(24 * time.Hour)
	causal := &runtimestate.CausalMetadata{
		CorrelationID: "corr-runtime", CausationID: "cause-runtime", LogicalOperationID: "logical-runtime", AttemptID: "attempt-1",
		TraceLink: &runtimestate.TraceLinkMetadata{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", TraceFlags: 1, TraceState: "vendor=value", ExpiresAt: expires},
	}
	readyStore, timerStore := runtimestate.ReadyWorkStore{}, runtimestate.TimerStore{}
	ready := runtimestate.ReadyWork{TenantID: f.tenant, ReadyWorkID: uuid.New(), InstanceID: f.instance.InstanceID, NodeID: "fanout-a", Attempt: 1, EligibleAt: fixedInstant, EnqueuedAt: fixedInstant, Causal: causal}
	timer := runtimestate.Timer{TenantID: f.tenant, TimerID: uuid.New(), InstanceID: f.instance.InstanceID, NodeID: "wait", Key: "wake", Kind: runtimestate.TimerDeadline, FiresAt: fixedInstant.Add(time.Hour), CreatedAt: fixedInstant, Causal: causal}
	f.do(t, func(tx dbport.Tx) error {
		if err := readyStore.Enqueue(ctx, tx, ready); err != nil {
			return err
		}
		return timerStore.Set(ctx, tx, timer)
	})
	f.do(t, func(tx dbport.Tx) error {
		pending, err := readyStore.PendingForInstance(ctx, tx, f.tenant, f.instance.InstanceID)
		if err != nil || len(pending) != 1 {
			t.Fatalf("pending ready work = %#v, %v", pending, err)
		}
		assertRuntimeCausal(t, pending[0].Causal, causal)
		due, err := timerStore.Due(ctx, tx, f.tenant, timer.FiresAt, 10)
		if err != nil || len(due) != 1 {
			t.Fatalf("due timers = %#v, %v", due, err)
		}
		assertRuntimeCausal(t, due[0].Causal, causal)
		return nil
	})
}

func TestTodo_OBS_013_RuntimeInvalidOptionalCausalMetadataDoesNotChangeScheduling(t *testing.T) {
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "obs-013-runtime-invalid")
	ctx := context.Background()
	store := runtimestate.ReadyWorkStore{}
	for i, causal := range []*runtimestate.CausalMetadata{
		{CorrelationID: "corr", CausationID: "", LogicalOperationID: "logical", AttemptID: "attempt"},
		{CorrelationID: "corr", CausationID: "cause", LogicalOperationID: "logical", AttemptID: "attempt", TraceLink: &runtimestate.TraceLinkMetadata{TraceID: "bad", SpanID: "bad"}},
	} {
		row := runtimestate.ReadyWork{TenantID: f.tenant, ReadyWorkID: uuid.New(), InstanceID: f.instance.InstanceID, NodeID: "invalid-optional-" + string(rune('a'+i)), Attempt: 1, EligibleAt: fixedInstant, EnqueuedAt: fixedInstant, Causal: causal}
		f.do(t, func(tx dbport.Tx) error { return store.Enqueue(ctx, tx, row) })
		f.do(t, func(tx dbport.Tx) error {
			got, err := store.Load(ctx, tx, f.tenant, row.ReadyWorkID)
			if err != nil {
				return err
			}
			if i == 0 && got.Causal != nil {
				t.Fatalf("partial causal metadata persisted: %#v", got.Causal)
			}
			if i == 1 && (got.Causal == nil || got.Causal.TraceLink != nil) {
				t.Fatalf("invalid optional trace link changed causal persistence: %#v", got.Causal)
			}
			return nil
		})
	}
}

func TestTodo_OBS_013_ReadyWorkClaimKeepsLogicalIdentityAndRefreshesAttempt(t *testing.T) {
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "obs-013-runtime-redelivery")
	ctx := context.Background()
	store := runtimestate.ReadyWorkStore{}
	row := runtimestate.ReadyWork{
		TenantID: f.tenant, ReadyWorkID: uuid.New(), InstanceID: f.instance.InstanceID,
		NodeID: "claim", Attempt: 1, EligibleAt: fixedInstant, EnqueuedAt: fixedInstant,
		Causal: &runtimestate.CausalMetadata{CorrelationID: "corr", CausationID: "cause", LogicalOperationID: "logical", AttemptID: "producer"},
	}
	f.do(t, func(tx dbport.Tx) error { return store.Enqueue(ctx, tx, row) })
	var first runtimestate.ReadyWork
	f.do(t, func(tx dbport.Tx) error {
		var err error
		first, err = store.Claim(ctx, tx, f.tenant, row.ReadyWorkID, 1)
		return err
	})
	if first.Causal == nil || first.Causal.LogicalOperationID != "logical" || first.Causal.AttemptID == "producer" || first.Causal.AttemptID == "" {
		t.Fatalf("first claim causal = %#v", first.Causal)
	}
	f.do(t, func(tx dbport.Tx) error {
		return store.Transition(ctx, tx, f.tenant, row.ReadyWorkID, first.Version, runtimestate.ReadyReady, time.Time{})
	})
	var second runtimestate.ReadyWork
	f.do(t, func(tx dbport.Tx) error {
		var err error
		second, err = store.Claim(ctx, tx, f.tenant, row.ReadyWorkID, first.Version+1)
		return err
	})
	if second.Causal == nil || second.Causal.LogicalOperationID != first.Causal.LogicalOperationID || second.Causal.AttemptID == first.Causal.AttemptID {
		t.Fatalf("redelivery causal = %#v, first %#v", second.Causal, first.Causal)
	}
}

func assertRuntimeCausal(t *testing.T, got, want *runtimestate.CausalMetadata) {
	t.Helper()
	if got == nil || got.CorrelationID != want.CorrelationID || got.CausationID != want.CausationID || got.LogicalOperationID != want.LogicalOperationID || got.AttemptID != want.AttemptID || got.TraceLink == nil {
		t.Fatalf("causal metadata = %#v, want %#v", got, want)
	}
	if got.TraceLink.TraceID != want.TraceLink.TraceID || got.TraceLink.SpanID != want.TraceLink.SpanID || got.TraceLink.TraceFlags != want.TraceLink.TraceFlags || got.TraceLink.TraceState != want.TraceLink.TraceState || !got.TraceLink.ExpiresAt.Equal(want.TraceLink.ExpiresAt) {
		t.Fatalf("trace link = %#v, want %#v", got.TraceLink, want.TraceLink)
	}
}
