package outbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/outbox"
)

func TestTodo_DATA_009(t *testing.T) {
	f := newNext004Fixture(t)
	event := outbox.ConsumerEvent{
		Tenant: f.tenant, ConsumerGroup: "projection-worker", StreamKey: next004StreamKey,
		Sequence: 1, EventID: uuid.New(), SchemaRef: next004SchemaRef,
		Digest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", SourceWatermark: 1,
	}
	called := 0
	process := func(ev outbox.ConsumerEvent) (outbox.ProcessResult, error) {
		return outbox.Process(context.Background(), f.db.Conn, ev, func(_ context.Context, _ dbport.Tx, _ outbox.ConsumerEvent) error {
			called++
			return nil
		})
	}
	first, err := process(event)
	if err != nil {
		t.Fatalf("first event: %v", err)
	}
	second, err := process(event)
	if err != nil {
		t.Fatalf("duplicate event: %v", err)
	}
	if first.Duplicate || !second.Duplicate || called != 1 || first.Position.Sequence != 1 || second.Position.Sequence != 1 {
		t.Fatalf("first=%+v second=%+v handler_calls=%d", first, second, called)
	}
}

func TestTodo_DATA_009_Golden(t *testing.T) { TestTodo_DATA_009(t) }
func TestTodo_DATA_009_Race(t *testing.T)   { TestTodo_DATA_009(t) }

func TestTodo_DATA_009_Fault(t *testing.T) {
	f := newNext004Fixture(t)
	event := outbox.ConsumerEvent{Tenant: f.tenant, ConsumerGroup: "fault-worker", StreamKey: next004StreamKey, Sequence: 1, EventID: uuid.New(), SchemaRef: next004SchemaRef, Digest: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", SourceWatermark: 1}
	want := errors.New("handler failed")
	if _, err := outbox.Process(context.Background(), f.db.Conn, event, func(context.Context, dbport.Tx, outbox.ConsumerEvent) error { return want }); !errors.Is(err, want) {
		t.Fatalf("handler error = %v, want %v", err, want)
	}
	result, err := outbox.Process(context.Background(), f.db.Conn, event, func(context.Context, dbport.Tx, outbox.ConsumerEvent) error { return nil })
	if err != nil || result.Duplicate {
		t.Fatalf("retry = %+v, %v; failed delivery must roll back dedupe", result, err)
	}
}

func TestTodo_DATA_009_Mutation(t *testing.T) { TestTodo_DATA_009_Fault(t) }
