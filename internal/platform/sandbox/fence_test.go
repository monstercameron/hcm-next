package sandbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
)

// stubConnector is a bare-bones [connectivity.Connector] a test can point at
// any SourceRef it likes, so [FencedConnector]'s admission decision can be
// exercised against both a synthetic and a production-looking descriptor.
type stubConnector struct {
	descriptor connectivity.Descriptor
	calls      int
}

func (s *stubConnector) Descriptor() connectivity.Descriptor     { return s.descriptor }
func (s *stubConnector) Bounds() connectivity.Bounds             { return connectivity.Bounds{} }
func (s *stubConnector) Capabilities() []connectivity.Capability { return nil }
func (s *stubConnector) SchemaVersion(context.Context, connectivity.ObjectKind) (string, error) {
	s.calls++
	return "v1", nil
}
func (s *stubConnector) Snapshot(context.Context, connectivity.ObjectKind) (string, error) {
	s.calls++
	return "snap-1", nil
}
func (s *stubConnector) Read(context.Context, connectivity.ReadRequest) (connectivity.Page, error) {
	s.calls++
	return connectivity.Page{}, nil
}

var _ connectivity.Connector = (*stubConnector)(nil)

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// TestFenceRefuseRecordsAndTypesTheError is the PRIMARY claim about [Fence]
// itself: every refusal is both a typed error naming the adapter, attempt and
// destination, and a durable entry [Fence.Refusals] never drops.
func TestFenceRefuseRecordsAndTypesTheError(t *testing.T) {
	at := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	f := NewFence(fixedClock(at))

	err := f.Refuse("connectivity.Connector", AttemptConnectorReach, "workday://harborcare/prod")
	if err == nil {
		t.Fatal("Refuse returned nil")
	}
	if !errors.Is(err, ErrExternalEffectFenced) {
		t.Fatalf("Refuse error does not wrap ErrExternalEffectFenced: %v", err)
	}
	var typed *FencedEffectError
	if !errors.As(err, &typed) {
		t.Fatalf("Refuse error is not a *FencedEffectError: %v", err)
	}
	if typed.Adapter != "connectivity.Connector" || typed.Attempt != AttemptConnectorReach || typed.Destination != "workday://harborcare/prod" {
		t.Fatalf("typed error = %+v, want adapter/attempt/destination preserved", typed)
	}

	refusals := f.Refusals()
	if len(refusals) != 1 {
		t.Fatalf("refusals = %d, want 1", len(refusals))
	}
	if refusals[0].Adapter != "connectivity.Connector" || refusals[0].Destination != "workday://harborcare/prod" || !refusals[0].At.Equal(at) {
		t.Fatalf("recorded refusal = %+v", refusals[0])
	}
	if f.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", f.Count())
	}
}

// TestFenceRefusalsIsACopy proves a caller mutating the returned slice cannot
// corrupt the fence's own record.
func TestFenceRefusalsIsACopy(t *testing.T) {
	f := NewFence(nil)
	_ = f.Refuse("a", AttemptMessageSend, "d1")
	got := f.Refusals()
	got[0].Destination = "tampered"
	if f.Refusals()[0].Destination != "d1" {
		t.Fatal("mutating the returned slice changed the fence's own record")
	}
}

// TestFenceIsSafeForConcurrentRefusals is a light concurrency smoke test: many
// goroutines refusing at once neither races nor drops an entry.
func TestFenceIsSafeForConcurrentRefusals(t *testing.T) {
	f := NewFence(nil)
	const n = 50
	done := make(chan struct{})
	for i := 0; i < n; i++ {
		go func(i int) {
			_ = f.Refuse("adapter", AttemptWebhookDispatch, "dest")
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < n; i++ {
		<-done
	}
	if got := f.Count(); got != n {
		t.Fatalf("Count() = %d, want %d", got, n)
	}
}

// TestFencedConnectorAdmitsTheAllowlistedSource is the companion case to
// [TestTodo_SANDBOX_001_Security]: a connector whose descriptor matches the
// sandbox's own synthetic fixture source is forwarded to, and nothing is
// refused.
func TestFencedConnectorAdmitsTheAllowlistedSource(t *testing.T) {
	inner := &stubConnector{descriptor: connectivity.Descriptor{SourceRef: "fakeincumbent://harborcare/prod"}}
	f := NewFence(nil)
	c := NewFencedConnector(inner, f, "fakeincumbent://harborcare/prod")

	if _, err := c.SchemaVersion(context.Background(), "worker"); err != nil {
		t.Fatalf("SchemaVersion refused an allow-listed source: %v", err)
	}
	if _, err := c.Snapshot(context.Background(), "worker"); err != nil {
		t.Fatalf("Snapshot refused an allow-listed source: %v", err)
	}
	if _, err := c.Read(context.Background(), connectivity.ReadRequest{}); err != nil {
		t.Fatalf("Read refused an allow-listed source: %v", err)
	}
	if inner.calls != 3 {
		t.Fatalf("inner connector received %d calls, want 3", inner.calls)
	}
	if f.Count() != 0 {
		t.Fatalf("fence recorded %d refusals for an allow-listed source", f.Count())
	}
}

// TestFencedConnectorRefusesAnUnlistedSource is SANDBOX-001's RED case made
// concrete: a connector that would resolve a production destination is
// refused before it is ever called, regardless of what wired it in.
func TestFencedConnectorRefusesAnUnlistedSource(t *testing.T) {
	inner := &stubConnector{descriptor: connectivity.Descriptor{SourceRef: "workday://harborcare/prod"}}
	f := NewFence(nil)
	c := NewFencedConnector(inner, f, "fakeincumbent://harborcare/prod")

	_, err := c.Read(context.Background(), connectivity.ReadRequest{})
	if err == nil {
		t.Fatal("Read admitted a production-looking source")
	}
	var typed *FencedEffectError
	if !errors.As(err, &typed) {
		t.Fatalf("error is not a *FencedEffectError: %v", err)
	}
	if typed.Destination != "workday://harborcare/prod" {
		t.Fatalf("typed error destination = %q, want the refused source", typed.Destination)
	}
	if inner.calls != 0 {
		t.Fatalf("inner connector was called %d times; it must never be reached", inner.calls)
	}

	if _, err := c.SchemaVersion(context.Background(), "worker"); err == nil {
		t.Fatal("SchemaVersion admitted a production-looking source")
	}
	if _, err := c.Snapshot(context.Background(), "worker"); err == nil {
		t.Fatal("Snapshot admitted a production-looking source")
	}
	if inner.calls != 0 {
		t.Fatalf("inner connector was called %d times across three refused attempts; want 0", inner.calls)
	}
	if got := f.Count(); got != 3 {
		t.Fatalf("fence recorded %d refusals, want 3 (one per gated method)", got)
	}
}

// TestFencedConnectorForwardsMetadataUnconditionally proves Descriptor,
// Bounds and Capabilities are never gated: they describe the connector, they
// do not reach it, so even a production-looking descriptor is read back
// unchanged.
func TestFencedConnectorForwardsMetadataUnconditionally(t *testing.T) {
	inner := &stubConnector{descriptor: connectivity.Descriptor{SourceRef: "workday://harborcare/prod", ConnectorID: "prod-connector"}}
	f := NewFence(nil)
	c := NewFencedConnector(inner, f /* no allow-listed sources at all */)

	if got := c.Descriptor(); got.ConnectorID != "prod-connector" {
		t.Fatalf("Descriptor() = %+v, want it forwarded unconditionally", got)
	}
	_ = c.Bounds()
	_ = c.Capabilities()
	if f.Count() != 0 {
		t.Fatalf("fence recorded %d refusals for metadata-only calls, want 0", f.Count())
	}
}
