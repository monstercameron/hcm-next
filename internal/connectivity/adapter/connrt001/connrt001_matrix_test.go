package connrt001

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/adapter"
)

type record struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type recordSchema struct{}

func (recordSchema) Name() string                    { return "record" }
func (recordSchema) Version() string                 { return "v1" }
func (recordSchema) Encode(v record) ([]byte, error) { return json.Marshal(v) }
func (recordSchema) Decode(b []byte) (record, error) {
	var v record
	return v, json.Unmarshal(b, &v)
}

type matrixConnector struct {
	caps       adapter.CapabilitySet
	items      []any
	writeValue any
	observed   any
	eventValue any
	err        error
	received   []adapter.Operation
	negotiated adapter.CapabilitySet
}

func (c *matrixConnector) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{ID: "matrix", Version: "1", Vendor: "test"}
}
func (c *matrixConnector) Capabilities() adapter.CapabilitySet { return c.caps }
func (c *matrixConnector) Negotiate(_ context.Context, caps adapter.CapabilitySet) error {
	c.negotiated = append(adapter.CapabilitySet(nil), caps...)
	return c.err
}
func (c *matrixConnector) Read(_ context.Context, _ adapter.ReadRequest[any]) (adapter.Page[any], error) {
	c.received = append(c.received, adapter.Read)
	if c.err != nil {
		return adapter.Page[any]{}, c.err
	}
	return adapter.Page[any]{Items: c.items, NextCursor: "next", Complete: false, Watermark: time.Unix(10, 0).UTC()}, nil
}
func (c *matrixConnector) Write(_ context.Context, _ adapter.WriteRequest[any]) (adapter.WriteResult[any], error) {
	c.received = append(c.received, adapter.Write)
	if c.err != nil {
		return adapter.WriteResult[any]{}, c.err
	}
	return adapter.WriteResult[any]{Value: c.writeValue, ExternalID: "ext-1", Version: "v2"}, nil
}
func (c *matrixConnector) Observe(_ context.Context, _ adapter.ObserveRequest[any]) (adapter.Observation[any], error) {
	c.received = append(c.received, adapter.Observe)
	if c.err != nil {
		return adapter.Observation[any]{}, c.err
	}
	return adapter.Observation[any]{Value: c.observed, ExternalID: "ext-1", Version: "v2", ObservedAt: time.Unix(11, 0).UTC()}, nil
}
func (c *matrixConnector) Subscribe(_ context.Context, _ adapter.SubscriptionRequest[any]) (adapter.Subscription[any], error) {
	c.received = append(c.received, adapter.Subscribe)
	if c.err != nil {
		return nil, c.err
	}
	return matrixSubscription{value: c.eventValue}, nil
}

type matrixSubscription struct{ value any }

func (s matrixSubscription) Next(context.Context) (adapter.Event[any], error) {
	return adapter.Event[any]{Value: s.value, ID: "evt-1", Cursor: "cur-1", OccurredAt: time.Unix(12, 0).UTC()}, nil
}
func (matrixSubscription) Close() error { return nil }

func allCaps() adapter.CapabilitySet {
	return adapter.CapabilitySet{
		{Object: "record", Operation: adapter.Read, Version: "v1"},
		{Object: "record", Operation: adapter.Write, Version: "v1"},
		{Object: "record", Operation: adapter.Observe, Version: "v1"},
		{Object: "record", Operation: adapter.Subscribe, Version: "v1"},
	}
}

func typed(c *matrixConnector) adapter.Typed[record] {
	return adapter.Typed[record]{Connector: c, Schema: recordSchema{}}
}

// TestTodo_CONN_RT_001 is the primary typed connector SPI matrix.
func TestTodo_CONN_RT_001(t *testing.T) {
	want := record{ID: "r1", Name: "Ada"}
	c := &matrixConnector{caps: allCaps(), items: []any{want}, writeValue: want, observed: want, eventValue: want}
	tc := typed(c)
	page, err := tc.Read(context.Background(), "cursor-0", 2, time.Unix(1, 0))
	if err != nil || len(page.Items) != 1 || page.Items[0] != want || page.NextCursor != "next" || page.Complete {
		t.Fatalf("read page=%+v err=%v", page, err)
	}
	if _, err = tc.Write(context.Background(), want, "idem-1", "v1"); err != nil {
		t.Fatal(err)
	}
	if _, err = tc.Observe(context.Background(), "ext-1"); err != nil {
		t.Fatal(err)
	}
	s, err := tc.Subscribe(context.Background(), "cursor-0")
	if err != nil {
		t.Fatal(err)
	}
	if event, err := s.Next(context.Background()); err != nil || event.Value != want || event.Cursor != "cur-1" {
		t.Fatalf("event=%+v err=%v", event, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_CONN_RT_001_Fault covers cancellation, capability and schema/error propagation.
func TestTodo_CONN_RT_001_Fault(t *testing.T) {
	c := &matrixConnector{caps: allCaps(), items: []any{"wrong"}, writeValue: "wrong", observed: "wrong", eventValue: "wrong"}
	if _, err := typed(c).Read(context.Background(), "", 1, time.Time{}); !errors.Is(err, adapter.ErrSchema) {
		t.Fatalf("read schema error=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := typed(&matrixConnector{}).Read(ctx, "", 1, time.Time{}); !errors.Is(err, adapter.ErrCanceled) {
		t.Fatalf("cancel error=%v", err)
	}
	if _, err := typed(&matrixConnector{}).Read(context.Background(), "", 1, time.Time{}); !errors.Is(err, adapter.ErrUnsupported) {
		t.Fatalf("capability error=%v", err)
	}
	c = &matrixConnector{caps: allCaps(), err: adapter.ErrTransient}
	if _, err := typed(c).Write(context.Background(), record{ID: "r1"}, "idem-1", ""); !errors.Is(err, adapter.ErrTransient) {
		t.Fatalf("adapter error=%v", err)
	}
}

// FuzzTodo_CONN_RT_001 verifies schema round trips remain typed for arbitrary JSON-safe records.
func FuzzTodo_CONN_RT_001(f *testing.F) {
	f.Add("r1", "Ada")
	f.Fuzz(func(t *testing.T, id, name string) {
		want := record{ID: id, Name: name}
		b, err := (recordSchema{}).Encode(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := (recordSchema{}).Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("got=%+v want=%+v", got, want)
		}
	})
}

// TestTodo_CONN_RT_001_Golden pins the stable schema and page metadata contract.
func TestTodo_CONN_RT_001_Golden(t *testing.T) {
	b, err := (recordSchema{}).Encode(record{ID: "r1", Name: "Ada"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"id":"r1","name":"Ada"}`; got != want {
		t.Fatalf("encoded=%q want %q", got, want)
	}
	c := &matrixConnector{caps: allCaps(), items: []any{record{ID: "r1"}}}
	p, err := typed(c).Read(context.Background(), "", 1, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if p.NextCursor != "next" || p.Watermark != time.Unix(10, 0).UTC() {
		t.Fatalf("page metadata=%+v", p)
	}
}

// TestTodo_CONN_RT_001_Integration verifies negotiation and all typed operation boundaries.
func TestTodo_CONN_RT_001_Integration(t *testing.T) {
	want := record{ID: "r1"}
	c := &matrixConnector{caps: allCaps(), items: []any{want}, writeValue: want, observed: want, eventValue: want}
	tc := typed(c)
	if err := tc.Negotiate(context.Background(), adapter.Read, adapter.Write, adapter.Observe, adapter.Subscribe); err != nil {
		t.Fatal(err)
	}
	if len(c.negotiated) != 4 || c.negotiated[0].Operation != adapter.Read || c.negotiated[3].Operation != adapter.Subscribe {
		t.Fatalf("negotiated=%+v", c.negotiated)
	}
	if _, err := tc.Read(context.Background(), "", 1, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := tc.Write(context.Background(), want, "idem-1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tc.Observe(context.Background(), "ext-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := tc.Subscribe(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if got := len(c.received); got != 4 {
		t.Fatalf("received operations=%v", c.received)
	}
}
