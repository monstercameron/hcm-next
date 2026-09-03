package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type person struct {
	ID string `json:"id"`
}
type personSchema struct{}

func (personSchema) Name() string                    { return "person" }
func (personSchema) Version() string                 { return "v1" }
func (personSchema) Encode(v person) ([]byte, error) { return json.Marshal(v) }
func (personSchema) Decode(b []byte) (person, error) {
	var v person
	err := json.Unmarshal(b, &v)
	return v, err
}
func TestTypedReadRoundTripAndCapability(t *testing.T) {
	c := fake{caps: CapabilitySet{{Object: "person", Operation: Read, Version: "v1"}}}
	p, e := (Typed[person]{Connector: c, Schema: personSchema{}}).Read(context.Background(), "", 10, time.Time{})
	if e != nil || len(p.Items) != 1 || p.Items[0].ID != "p1" {
		t.Fatalf("%v %#v", e, p)
	}
}
func TestTypedRejectsUnadvertised(t *testing.T) {
	_, e := (Typed[person]{Connector: fake{}, Schema: personSchema{}}).Read(context.Background(), "", 1, time.Time{})
	if !errors.Is(e, ErrUnsupported) {
		t.Fatalf("got %v", e)
	}
}

func TestTypedWriteObserveSubscribe(t *testing.T) {
	c := fake{caps: CapabilitySet{{Object: "person", Operation: Write, Version: "v1"}, {Object: "person", Operation: Observe, Version: "v1"}, {Object: "person", Operation: Subscribe, Version: "v1"}}}
	tc := Typed[person]{Connector: c, Schema: personSchema{}}
	if _, err := tc.Write(context.Background(), person{ID: "p1"}, "idem-1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := tc.Observe(context.Background(), "p1"); err != nil {
		t.Fatal(err)
	}
	s, err := tc.Subscribe(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTypedCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (Typed[person]{Connector: fake{}, Schema: personSchema{}}).Read(ctx, "", 1, time.Time{})
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("got %v", err)
	}
}

type fake struct{ caps CapabilitySet }

func (f fake) Descriptor() Descriptor                         { return Descriptor{ID: "f", Version: "1", Vendor: "test"} }
func (f fake) Capabilities() CapabilitySet                    { return f.caps }
func (f fake) Negotiate(context.Context, CapabilitySet) error { return nil }
func (f fake) Read(context.Context, ReadRequest[any]) (Page[any], error) {
	return Page[any]{Items: []any{person{ID: "p1"}}, Complete: true}, nil
}
func (f fake) Write(context.Context, WriteRequest[any]) (WriteResult[any], error) {
	return WriteResult[any]{Value: person{ID: "p1"}}, nil
}
func (f fake) Observe(context.Context, ObserveRequest[any]) (Observation[any], error) {
	return Observation[any]{Value: person{ID: "p1"}}, nil
}
func (f fake) Subscribe(context.Context, SubscriptionRequest[any]) (Subscription[any], error) {
	return fakeSubscription{}, nil
}

type fakeSubscription struct{}

func (fakeSubscription) Next(context.Context) (Event[any], error) {
	return Event[any]{Value: person{ID: "p1"}}, nil
}
func (fakeSubscription) Close() error { return nil }
