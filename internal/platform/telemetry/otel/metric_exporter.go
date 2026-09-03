package otel

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"sync/atomic"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// stdoutMetricExporter is a minimal, hand-written sdkmetric.Exporter that
// JSON-encodes each collected metricdata.ResourceMetrics to a writer.
//
// This package does not import
// go.opentelemetry.io/otel/exporters/stdout/stdoutmetric: OBS-002's pinned
// dependency set (go.mod) adds go.opentelemetry.io/otel/exporters/stdout/
// stdouttrace for traces but deliberately does not add the metrics sibling,
// so the "stdout" metric exporter kind is implemented directly against
// go.opentelemetry.io/otel/sdk/metric's own Exporter interface instead
// (metricdata's own types already implement json.Marshaler where it
// matters — attribute.Set does — so a plain json.Encoder produces readable
// output without hand-rolling per-aggregation-type formatting).
type stdoutMetricExporter struct {
	mu       sync.Mutex
	w        io.Writer
	shutdown atomic.Bool
}

func newStdoutMetricExporter(w io.Writer) *stdoutMetricExporter {
	return &stdoutMetricExporter{w: w}
}

// Temporality implements sdkmetric.Exporter using the SDK's own default
// selection (cumulative for every instrument kind except an async
// UpDownCounter, which this package never creates).
func (e *stdoutMetricExporter) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	return sdkmetric.DefaultTemporalitySelector(k)
}

// Aggregation implements sdkmetric.Exporter using the SDK's own default
// aggregation per instrument kind.
func (e *stdoutMetricExporter) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(k)
}

// Export implements sdkmetric.Exporter.
func (e *stdoutMetricExporter) Export(_ context.Context, rm *metricdata.ResourceMetrics) error {
	if e.shutdown.Load() {
		return sdkmetric.ErrExporterShutdown
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return json.NewEncoder(e.w).Encode(rm)
}

// ForceFlush implements sdkmetric.Exporter. There is nothing buffered inside
// this exporter to flush; the PeriodicReader wrapping it owns batching.
func (e *stdoutMetricExporter) ForceFlush(context.Context) error { return nil }

// Shutdown implements sdkmetric.Exporter.
func (e *stdoutMetricExporter) Shutdown(context.Context) error {
	e.shutdown.Store(true)
	return nil
}
