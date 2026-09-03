package testexport

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// newRecorder returns a SpanRecorder wired to a real SDK provider through a
// SimpleSpanProcessor, with one sampled span ended per name in the order
// given. SimpleSpanProcessor exports a span the instant it ends, so the
// recorder's order matches the order of names.
func newRecorder(t *testing.T, names ...string) *SpanRecorder {
	t.Helper()
	rec := NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(rec)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	tracer := provider.Tracer("testexport")
	for _, name := range names {
		_, span := tracer.Start(context.Background(), name)
		span.End()
	}
	return rec
}

// TestSpanRecorderRecordsInExportOrder proves the recorder keeps every
// exported span, in the order the processor exported them, and hands out an
// independent snapshot rather than a live view.
func TestSpanRecorderRecordsInExportOrder(t *testing.T) {
	rec := newRecorder(t, "first", "second", "third")

	spans := rec.Spans()
	if len(spans) != 3 {
		t.Fatalf("%d spans recorded, want 3", len(spans))
	}
	for i, want := range []string{"first", "second", "third"} {
		if got := spans[i].Name(); got != want {
			t.Errorf("Spans()[%d].Name() = %q, want %q", i, got, want)
		}
	}

	// Re-export the existing snapshot: the recorder appends, the earlier
	// snapshot is untouched.
	first := rec.Spans()
	if err := rec.ExportSpans(context.Background(), first); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}
	if got := len(rec.Spans()); got != 6 {
		t.Errorf("Spans() after re-export = %d, want 6", got)
	}
	if got := len(first); got != 3 {
		t.Errorf("earlier snapshot grew to %d, want it to stay 3", got)
	}
}

// TestSpanRecorderSpanNamed proves SpanNamed returns the first exact-name
// match and reports absence rather than returning a zero span.
func TestSpanRecorderSpanNamed(t *testing.T) {
	rec := newRecorder(t, "alpha", "beta", "alpha")

	span, ok := rec.SpanNamed("beta")
	if !ok || span.Name() != "beta" {
		t.Fatalf("SpanNamed(beta) = (%q, %v), want (beta, true)", span.Name(), ok)
	}
	span, ok = rec.SpanNamed("alpha")
	if !ok || span.Name() != "alpha" {
		t.Fatalf("SpanNamed(alpha) = (%q, %v), want the first alpha", span.Name(), ok)
	}
	if _, ok := rec.SpanNamed("gamma"); ok {
		t.Error("SpanNamed(gamma) reported found, want false")
	}
}

// TestSpanRecorderShutdownAndReset proves Shutdown keeps recorded spans
// readable (a test reads them after teardown) and Reset discards them.
func TestSpanRecorderShutdownAndReset(t *testing.T) {
	rec := newRecorder(t, "a", "b")
	if err := rec.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if got := len(rec.Spans()); got != 2 {
		t.Errorf("Spans() after Shutdown = %d, want 2 (Shutdown never discards)", got)
	}
	rec.Reset()
	if got := len(rec.Spans()); got != 0 {
		t.Errorf("Spans() after Reset = %d, want 0", got)
	}
}
