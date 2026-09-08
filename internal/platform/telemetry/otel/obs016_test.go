package otel_test

import (
	"context"
	"testing"

	hcmotel "github.com/monstercameron/hcm-next/internal/platform/telemetry/otel"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"
)

// histogramPoints returns the Histogram[float64] datapoints recorded for
// effect.dispatch.duration in one collection.
func histogramPoints(t *testing.T, h *testHarness) []metricdata.HistogramDataPoint[float64] {
	t.Helper()
	rm := collectMetrics(t, h.MetricReader)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "effect.dispatch.duration" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("effect.dispatch.duration data = %T, want metricdata.Histogram[float64]", m.Data)
			}
			return hist.DataPoints
		}
	}
	t.Fatal("effect.dispatch.duration not present in collected metrics")
	return nil
}

// TestTodo_OBS_016_ExemplarLinksObservationToItsTrace proves the RED and
// GREEN clauses of planning/todos.md OBS-016 for the exemplar half: an
// effect.dispatch.duration observation recorded inside a sampled span
// carries an exemplar whose trace and span IDs are exactly that span's,
// so a slow dispatch can be joined back to its trace.
func TestTodo_OBS_016_ExemplarLinksObservationToItsTrace(t *testing.T) {
	h := newTestHarness(t, testEvaluator(t))

	ctx, span := h.Provider.Tracer("exemplar").Start(context.Background(), "Effect.Dispatch")
	spanCtx := span.SpanContext()
	if !spanCtx.IsSampled() {
		t.Fatal("test span is not sampled; exemplar attachment requires a sampled span context")
	}
	h.Provider.Metrics().RecordEffectDispatchLatency(ctx, "cell-p1a", "SUCCESS", 0.25)
	span.End()

	points := histogramPoints(t, h)
	if len(points) != 1 {
		t.Fatalf("got %d histogram datapoints, want 1", len(points))
	}
	exemplars := hcmotel.ExemplarsOf(points[0])
	if len(exemplars) == 0 {
		t.Fatal("histogram datapoint carries no exemplars; want at least one linking back to the dispatch span")
	}
	found := false
	for _, e := range exemplars {
		if e.TraceID == spanCtx.TraceID() && e.SpanID == spanCtx.SpanID() {
			found = true
			if e.Value != 0.25 {
				t.Fatalf("exemplar value = %v, want 0.25", e.Value)
			}
		}
	}
	if !found {
		t.Fatalf("no exemplar names the dispatch span (trace %s span %s): %+v",
			spanCtx.TraceID(), spanCtx.SpanID(), exemplars)
	}
	var _ trace.TraceID
}

// TestTodo_OBS_016_NoExemplarWithoutSampledContext documents the sampling
// precondition the other direction: an observation recorded with no span
// in context carries no exemplar, so correlation consumers must treat an
// empty exemplar set as "unsampled", never as a lookup failure.
func TestTodo_OBS_016_NoExemplarWithoutSampledContext(t *testing.T) {
	h := newTestHarness(t, testEvaluator(t))

	h.Provider.Metrics().RecordEffectDispatchLatency(context.Background(), "cell-p1a", "SUCCESS", 1.5)

	points := histogramPoints(t, h)
	if len(points) != 1 {
		t.Fatalf("got %d histogram datapoints, want 1", len(points))
	}
	if exemplars := hcmotel.ExemplarsOf(points[0]); len(exemplars) != 0 {
		t.Fatalf("unsampled observation carries %d exemplars, want 0: %+v", len(exemplars), exemplars)
	}
}
