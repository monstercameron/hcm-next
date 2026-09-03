// Package testexport builds OBS-015's deterministic in-memory span
// exporter. It lives under internal/platform/telemetry/otel (rather than
// alongside internal/platform/telemetry/testexport's own log recorder)
// because it implements a real go.opentelemetry.io/otel/sdk/trace exporter
// interface, and definitions/architecture/dependency-roles.yaml (LIB-007)
// admits only internal/platform/telemetry/otel and internal/transport/
// otelmw to import go.opentelemetry.io/otel directly. Living under that
// root — a subpackage of an admitted root is itself admitted
// (tools/policy/libfirewall's own prefix match) — is what keeps this a
// real exporter without widening the firewall's allowed-roots manifest.
//
// See internal/platform/telemetry/testexport for the companion
// [log/slog]-side in-memory recorder, which has no OTel dependency of its
// own and stays at that unprefixed path.
package testexport

import (
	"context"
	"sync"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// SpanRecorder is a [sdktrace.SpanExporter] that keeps every exported span
// in memory, in export order. Plug it into
// internal/platform/telemetry/otel.Config.Trace.Exporter (or directly into
// an sdktrace.NewBatchSpanProcessor/NewSimpleSpanProcessor in a narrower
// test) and call [SpanRecorder.Spans] after a ForceFlush (or immediately,
// behind a SimpleSpanProcessor).
type SpanRecorder struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

// NewSpanRecorder returns an empty SpanRecorder.
func NewSpanRecorder() *SpanRecorder { return &SpanRecorder{} }

// ExportSpans implements sdktrace.SpanExporter.
func (r *SpanRecorder) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, spans...)
	return nil
}

// Shutdown implements sdktrace.SpanExporter. It never discards recorded
// spans — a test reads them after Shutdown exactly as it would before.
func (r *SpanRecorder) Shutdown(context.Context) error { return nil }

var _ sdktrace.SpanExporter = (*SpanRecorder)(nil)

// Spans returns a snapshot of every span recorded so far, in export order.
func (r *SpanRecorder) Spans() []sdktrace.ReadOnlySpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]sdktrace.ReadOnlySpan(nil), r.spans...)
}

// SpanNamed returns the first recorded span with the exact given name, and
// whether one was found. Span names are supposed to be low-cardinality
// (structured-logging-and-opentelemetry.md "Trace topology"), so "first" is
// deterministic for any test that opens one span per name per assertion
// window.
func (r *SpanRecorder) SpanNamed(name string) (sdktrace.ReadOnlySpan, bool) {
	for _, s := range r.Spans() {
		if s.Name() == name {
			return s, true
		}
	}
	return nil, false
}

// Reset discards every recorded span, for a test that reuses one recorder
// across several assertion windows.
func (r *SpanRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = nil
}
