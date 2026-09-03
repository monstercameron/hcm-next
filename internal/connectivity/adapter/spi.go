package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Operation is a capability advertised by an adapter.
type Operation string

const (
	Read      Operation = "read"
	Write     Operation = "write"
	Observe   Operation = "observe"
	Subscribe Operation = "subscribe"
)

// Capability identifies one typed object/operation pair.
type Capability struct {
	Object    string
	Operation Operation
	Version   string
}

func (c Capability) valid() bool { return c.Object != "" && c.Version != "" && c.Operation != "" }

// CapabilitySet is immutable from the caller's perspective.
type CapabilitySet []Capability

func (s CapabilitySet) Supports(want Capability) bool {
	if !want.valid() {
		return false
	}
	for _, have := range s {
		if have == want {
			return true
		}
	}
	return false
}

// Descriptor identifies the adapter without exposing a vendor SDK type.
type Descriptor struct{ ID, Version, Vendor string }

// Schema is the generated SDK's typed serialization boundary. Implementations
// must reject values that are not accepted by this schema.
type Schema[T any] interface {
	Name() string
	Version() string
	Encode(T) ([]byte, error)
	Decode([]byte) (T, error)
}

// Page is a bounded typed read page. Cursor is opaque to callers.
type Page[T any] struct {
	Items      []T
	NextCursor string
	Complete   bool
	Watermark  time.Time
}

type ReadRequest[T any] struct {
	Schema Schema[T]
	Cursor string
	Limit  int
	Since  time.Time
}
type WriteRequest[T any] struct {
	Schema          Schema[T]
	Value           T
	IdempotencyKey  string
	ExpectedVersion string
}
type ObserveRequest[T any] struct {
	Schema     Schema[T]
	ExternalID string
}
type SubscriptionRequest[T any] struct {
	Schema Schema[T]
	Cursor string
}

type WriteResult[T any] struct {
	Schema              Schema[T]
	Value               T
	ExternalID, Version string
}
type Observation[T any] struct {
	Schema              Schema[T]
	Value               T
	ExternalID, Version string
	ObservedAt          time.Time
}
type Event[T any] struct {
	Schema     Schema[T]
	Value      T
	ID, Cursor string
	OccurredAt time.Time
}

// Subscription is pull-based to make cancellation and backpressure explicit.
type Subscription[T any] interface {
	Next(context.Context) (Event[T], error)
	Close() error
}

// Connector is the complete adapter SPI. The type parameters on each request
// ensure generated SDK methods remain typed while one connector can serve many
// object schemas. Implementations must not retain request values after return.
type Connector interface {
	Descriptor() Descriptor
	Capabilities() CapabilitySet
	Negotiate(context.Context, CapabilitySet) error
	Read(context.Context, ReadRequest[any]) (Page[any], error)
	Write(context.Context, WriteRequest[any]) (WriteResult[any], error)
	Observe(context.Context, ObserveRequest[any]) (Observation[any], error)
	Subscribe(context.Context, SubscriptionRequest[any]) (Subscription[any], error)
}

// Typed adapts a generated schema to Connector while checking capability and
// encoding the operation as a typed request. It is the API generated clients
// use, so application code never has to pass an untyped map or vendor object.
type Typed[T any] struct {
	Connector Connector
	Schema    Schema[T]
}

func (t Typed[T]) capability(op Operation) Capability {
	return Capability{Object: t.Schema.Name(), Operation: op, Version: t.Schema.Version()}
}
func (t Typed[T]) check(ctx context.Context, op Operation) error {
	if err := CheckContext(ctx); err != nil {
		return err
	}
	if t.Connector == nil || t.Schema == nil {
		return fail("typed.check", ErrInvalid, "connector and schema are required")
	}
	if !t.capability(op).valid() {
		return fail("typed.check", ErrInvalid, "schema identity is incomplete")
	}
	if !t.Connector.Capabilities().Supports(t.capability(op)) {
		return fail("typed.check", ErrUnsupported, "capability %s/%s/%s is not advertised", t.Schema.Name(), op, t.Schema.Version())
	}
	return nil
}

// Negotiate asks the underlying adapter to establish the exact capabilities
// required by this typed client. Negotiation is kept on the typed boundary so
// generated SDKs cannot accidentally negotiate an untyped/vendor surface.
func (t Typed[T]) Negotiate(ctx context.Context, operations ...Operation) error {
	if err := CheckContext(ctx); err != nil {
		return err
	}
	if t.Connector == nil || t.Schema == nil {
		return fail("typed.negotiate", ErrInvalid, "connector and schema are required")
	}
	if len(operations) == 0 {
		return fail("typed.negotiate", ErrInvalid, "at least one operation is required")
	}
	want := make(CapabilitySet, 0, len(operations))
	for _, op := range operations {
		c := t.capability(op)
		if !c.valid() {
			return fail("typed.negotiate", ErrInvalid, "schema identity is incomplete")
		}
		want = append(want, c)
	}
	return t.Connector.Negotiate(ctx, want)
}

func (t Typed[T]) Read(ctx context.Context, cursor string, limit int, since time.Time) (Page[T], error) {
	var zero Page[T]
	if err := t.check(ctx, Read); err != nil {
		return zero, err
	}
	if limit <= 0 {
		return zero, fail("typed.read", ErrInvalid, "limit must be positive")
	}
	p, err := t.Connector.Read(ctx, ReadRequest[any]{Schema: anySchema[T]{t.Schema}, Cursor: cursor, Limit: limit, Since: since})
	if err != nil {
		return zero, err
	}
	items := make([]T, len(p.Items))
	for i, v := range p.Items {
		x, ok := v.(T)
		if !ok {
			return zero, fail("typed.read", ErrSchema, "item %d has incompatible type %T", i, v)
		}
		b, err := t.Schema.Encode(x)
		if err != nil {
			return zero, fail("typed.read", ErrSchema, "item %d: %v", i, err)
		}
		items[i], err = t.Schema.Decode(b)
		if err != nil {
			return zero, fail("typed.read", ErrSchema, "item %d: %v", i, err)
		}
	}
	return Page[T]{Items: items, NextCursor: p.NextCursor, Complete: p.Complete, Watermark: p.Watermark}, nil
}

func (t Typed[T]) Write(ctx context.Context, value T, key, expected string) (WriteResult[T], error) {
	var z WriteResult[T]
	if err := t.check(ctx, Write); err != nil {
		return z, err
	}
	if key == "" {
		return z, fail("typed.write", ErrInvalid, "idempotency key is required")
	}
	r, e := t.Connector.Write(ctx, WriteRequest[any]{Schema: anySchema[T]{t.Schema}, Value: value, IdempotencyKey: key, ExpectedVersion: expected})
	if e != nil {
		return z, e
	}
	v, ok := r.Value.(T)
	if !ok {
		return z, fail("typed.write", ErrSchema, "adapter returned incompatible value %T", r.Value)
	}
	return WriteResult[T]{Schema: t.Schema, Value: v, ExternalID: r.ExternalID, Version: r.Version}, nil
}
func (t Typed[T]) Observe(ctx context.Context, id string) (Observation[T], error) {
	var z Observation[T]
	if err := t.check(ctx, Observe); err != nil {
		return z, err
	}
	if id == "" {
		return z, fail("typed.observe", ErrInvalid, "external id is required")
	}
	r, e := t.Connector.Observe(ctx, ObserveRequest[any]{Schema: anySchema[T]{t.Schema}, ExternalID: id})
	if e != nil {
		return z, e
	}
	v, ok := r.Value.(T)
	if !ok {
		return z, fail("typed.observe", ErrSchema, "adapter returned incompatible value")
	}
	return Observation[T]{Schema: t.Schema, Value: v, ExternalID: r.ExternalID, Version: r.Version, ObservedAt: r.ObservedAt}, nil
}
func (t Typed[T]) Subscribe(ctx context.Context, cursor string) (Subscription[T], error) {
	if err := t.check(ctx, Subscribe); err != nil {
		return nil, err
	}
	s, err := t.Connector.Subscribe(ctx, SubscriptionRequest[any]{Schema: anySchema[T]{t.Schema}, Cursor: cursor})
	if err != nil {
		return nil, err
	}
	return typedSubscription[T]{raw: s, schema: t.Schema}, nil
}

type typedSubscription[T any] struct {
	raw    Subscription[any]
	schema Schema[T]
}

func (s typedSubscription[T]) Next(ctx context.Context) (Event[T], error) {
	e, err := s.raw.Next(ctx)
	if err != nil {
		return Event[T]{}, err
	}
	v, ok := e.Value.(T)
	if !ok {
		return Event[T]{}, fail("typed.subscribe", ErrSchema, "adapter returned incompatible value %T", e.Value)
	}
	return Event[T]{Schema: s.schema, Value: v, ID: e.ID, Cursor: e.Cursor, OccurredAt: e.OccurredAt}, nil
}
func (s typedSubscription[T]) Close() error { return s.raw.Close() }

type anySchema[T any] struct{ Schema[T] }

func (s anySchema[T]) Encode(v any) ([]byte, error) {
	x, ok := v.(T)
	if !ok {
		return nil, fmt.Errorf("incompatible value %T", v)
	}
	return s.Schema.Encode(x)
}
func (s anySchema[T]) Decode(b []byte) (any, error) { return s.Schema.Decode(b) }

var (
	ErrInvalid     = errors.New("adapter: invalid request")
	ErrUnsupported = errors.New("adapter: unsupported capability")
	ErrSchema      = errors.New("adapter: schema mismatch")
	ErrTransient   = errors.New("adapter: transient failure")
	ErrCanceled    = errors.New("adapter: canceled")
)

type Error struct {
	Op     string
	Cause  error
	Detail string
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause == nil {
		return e.Op + ": " + e.Detail
	}
	return e.Op + ": " + e.Cause.Error() + ": " + e.Detail
}
func (e *Error) Unwrap() error { return e.Cause }
func fail(op string, cause error, f string, a ...any) error {
	return &Error{Op: op, Cause: cause, Detail: fmt.Sprintf(f, a...)}
}

// CheckContext preserves cancellation identity for adapters and generated SDKs.
func CheckContext(ctx context.Context) error {
	if ctx == nil {
		return fail("adapter.context", ErrInvalid, "context is required")
	}
	select {
	case <-ctx.Done():
		return fail("adapter.context", ErrCanceled, "%v", ctx.Err())
	default:
		return nil
	}
}
