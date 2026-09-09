package position_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/position"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

func TestTodo_PERSIST_POSITION_001_DomainPort(t *testing.T) {
	rev := validRevision(t, "tenant-a", "pos-1")
	request := reservationRequest(t, rev, "proposal-port", canonicalbytes.Digest([]byte("proposal-port")))
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store := position.NewPositionReservationStore()
	r := position.PositionReservation{ID: "reservation-port", Request: request, State: position.PositionReservationHeld, Fence: 1, CreatedAt: now, UpdatedAt: now}
	e := position.PositionReservationEvent{Sequence: 1, ReservationID: r.ID, To: position.PositionReservationHeld, Fence: 1, At: now}
	if err := store.Put(context.Background(), r, e); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), request.Tenant, r.ID)
	if err != nil || loaded.ID != r.ID {
		t.Fatalf("Load = %#v, %v", loaded, err)
	}
	if _, err := store.Transition(context.Background(), request.Tenant, r.ID, 0, position.PositionReservationReleased, "RELEASED", now); !errors.Is(err, position.ErrReservationFence) {
		t.Fatalf("stale transition error = %v", err)
	}
	updated, err := store.Transition(context.Background(), request.Tenant, r.ID, 1, position.PositionReservationReleased, "RELEASED", now.Add(time.Minute))
	if err != nil || updated.Fence != 2 {
		t.Fatalf("Transition = %#v, %v", updated, err)
	}
	events, err := store.Events(context.Background(), request.Tenant, r.ID)
	if err != nil || len(events) != 2 || events[1].Fence != 2 {
		t.Fatalf("Events = %#v, %v", events, err)
	}
}

func TestTodo_PERSIST_POSITION_001_DomainPortExplain(t *testing.T) {
	got := position.ExplainRepository("tenant-a", "reservation-1")
	if got.Tenant != "tenant-a" || got.ReservationID != "reservation-1" {
		t.Fatalf("explanation = %#v", got)
	}
}
