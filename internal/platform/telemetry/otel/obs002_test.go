package otel_test

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/monstercameron/hcm-next/internal/platform/logging"
	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/hcm-next/internal/platform/telemetry/otel"
)

func attrValue(attrs []attribute.KeyValue, key string) (string, bool) {
	for _, a := range attrs {
		if string(a.Key) == key {
			return a.Value.String(), true
		}
	}
	return "", false
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return rm
}

func metricNames(rm metricdata.ResourceMetrics) []string {
	var names []string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names = append(names, m.Name)
		}
	}
	sort.Strings(names)
	return names
}

// TestTodo_OBS_002 proves the RED and GREEN clauses of planning/todos.md
// OBS-002: a prohibited attribute never reaches an exporter regardless of
// whether it was attached at span start or after, unbounded batching and a
// missing shutdown deadline are refused at construction, a nil Evaluator is
// refused rather than silently defaulting to "allow everything", this
// package registers exactly the metric catalog's five instruments,
// correlation_id propagates from internal/platform/logging's context into
// span attributes, and Shutdown flushes within its configured deadline.
func TestTodo_OBS_002(t *testing.T) {
	t.Run("RED_prohibited_attribute_never_reaches_exporter_from_start_or_set", func(t *testing.T) {
		h := newTestHarness(t, testEvaluator(t))
		tracer := h.Provider.Tracer("test")

		ctx, span := tracer.Start(context.Background(), "op",
			trace.WithAttributes(attribute.String("salary", "50000")))
		span.SetAttributes(attribute.String("medical_condition", "diabetes"))
		span.SetAttributes(attribute.String("cell_id", "cell-p1a")) // allow-listed: should survive
		span.End()
		_ = ctx

		if report := h.Provider.ForceFlush(context.Background()); report.Err() != nil {
			t.Fatalf("ForceFlush: %v", report.Err())
		}

		spans := h.SpanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("got %d exported spans, want 1", len(spans))
		}
		if _, ok := attrValue(spans[0].Attributes, "salary"); ok {
			t.Fatal("prohibited attribute \"salary\" reached the exporter")
		}
		if _, ok := attrValue(spans[0].Attributes, "medical_condition"); ok {
			t.Fatal("prohibited attribute \"medical_condition\" reached the exporter")
		}
		if v, ok := attrValue(spans[0].Attributes, "cell_id"); !ok || v != "cell-p1a" {
			t.Fatalf("allow-listed attribute \"cell_id\" = %q, %v; want kept", v, ok)
		}
	})

	t.Run("RED_negative_max_queue_size_rejected_as_unbounded_batching", func(t *testing.T) {
		_, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
			Resource:        testResource(t),
			Evaluator:       testEvaluator(t),
			ShutdownTimeout: time.Second,
			Trace:           hcmotel.TraceConfig{Exporter: tracetest.NewInMemoryExporter(), MaxQueueSize: -1},
			Metric:          hcmotel.MetricConfig{Reader: sdkmetric.NewManualReader()},
		})
		if err == nil {
			t.Fatal("NewProvider accepted a negative MaxQueueSize; want rejection")
		}
	})

	t.Run("RED_zero_shutdown_timeout_rejected", func(t *testing.T) {
		_, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
			Resource:  testResource(t),
			Evaluator: testEvaluator(t),
			Trace:     hcmotel.TraceConfig{Exporter: tracetest.NewInMemoryExporter()},
			Metric:    hcmotel.MetricConfig{Reader: sdkmetric.NewManualReader()},
		})
		if err == nil {
			t.Fatal("NewProvider accepted a zero ShutdownTimeout; want rejection (a bounded flush deadline is required)")
		}
	})

	t.Run("RED_nil_evaluator_rejected", func(t *testing.T) {
		_, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
			Resource:        testResource(t),
			ShutdownTimeout: time.Second,
			Trace:           hcmotel.TraceConfig{Exporter: tracetest.NewInMemoryExporter()},
			Metric:          hcmotel.MetricConfig{Reader: sdkmetric.NewManualReader()},
		})
		if err == nil {
			t.Fatal("NewProvider accepted a nil Evaluator; want rejection")
		}
	})

	t.Run("RED_invalid_resource_rejected", func(t *testing.T) {
		_, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
			Resource:        telemetry.Resource{}, // zero value: invalid
			Evaluator:       testEvaluator(t),
			ShutdownTimeout: time.Second,
			Trace:           hcmotel.TraceConfig{Exporter: tracetest.NewInMemoryExporter()},
			Metric:          hcmotel.MetricConfig{Reader: sdkmetric.NewManualReader()},
		})
		if err == nil {
			t.Fatal("NewProvider accepted an invalid Resource; want rejection")
		}
	})

	t.Run("GREEN_registers_exactly_the_metric_catalog_instruments", func(t *testing.T) {
		h := newTestHarness(t, testEvaluator(t))
		ctx := context.Background()

		h.Provider.Metrics().RecordIntentCreated(ctx, "cell-p1a", "standard", "SUCCESS")
		h.Provider.Metrics().RecordIntentSimulated(ctx, "cell-p1a", "standard", "SUCCESS")
		h.Provider.Metrics().RecordLedgerAppend(ctx, "cell-p1a", "SUCCESS")
		h.Provider.Metrics().RecordOutboxLag(ctx, "cell-p1a", 12.5)
		h.Provider.Metrics().RecordEdgeParity(ctx, "cell-p1a", "edge-1", true)

		rm := collectMetrics(t, h.MetricReader)
		got := metricNames(rm)

		want := append([]string(nil), telemetry.DefaultRequiredSignals().RequiredMetrics...)
		sort.Strings(want)
		if len(got) != len(want) {
			t.Fatalf("registered metric names = %v, want exactly %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("registered metric names = %v, want exactly %v", got, want)
			}
		}
	})

	t.Run("GREEN_correlation_id_propagates_others_stay_redacted_under_frozen_policy", func(t *testing.T) {
		h := newTestHarness(t, testEvaluator(t))
		tracer := h.Provider.Tracer("test")

		ctx := logging.WithCorrelationID(context.Background(), "corr-42")
		ctx = logging.WithRequestID(ctx, "req-7")
		ctx, err := logging.WithPrincipalRef(ctx, "principal-ref-1")
		if err != nil {
			t.Fatalf("WithPrincipalRef: %v", err)
		}
		ctx = logging.WithEvidenceIDs(ctx, "evidence-1")

		_, span := tracer.Start(ctx, "op")
		span.End()

		if report := h.Provider.ForceFlush(context.Background()); report.Err() != nil {
			t.Fatalf("ForceFlush: %v", report.Err())
		}

		spans := h.SpanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("got %d exported spans, want 1", len(spans))
		}
		if v, ok := attrValue(spans[0].Attributes, "correlation_id"); !ok || v != "corr-42" {
			t.Fatalf("correlation_id = %q, %v; want the propagated value", v, ok)
		}
		for _, key := range []string{"request_id", "evidence_ref", "principal_ref"} {
			if _, ok := attrValue(spans[0].Attributes, key); ok {
				t.Fatalf("%q reached the exporter; the frozen allow-list does not register it as an attribute key", key)
			}
		}
	})

	t.Run("GREEN_shutdown_flushes_within_deadline_without_error", func(t *testing.T) {
		h := newTestHarness(t, testEvaluator(t))
		tracer := h.Provider.Tracer("test")
		_, span := tracer.Start(context.Background(), "op")
		span.End()

		report := h.Provider.Shutdown(context.Background())
		if report.DeadlineExceeded {
			t.Fatal("Shutdown reported DeadlineExceeded for a fast in-memory exporter")
		}
		if report.Err() != nil {
			t.Fatalf("Shutdown() report = %+v, want no error", report)
		}
	})
}

// TestTodo_OBS_002_Race proves concurrent span creation and metric
// recording through one Provider is safe and produces a consistent final
// count: no span is lost or duplicated, and each counter's total equals
// exactly the number of concurrent increments.
func TestTodo_OBS_002_Race(t *testing.T) {
	h := newTestHarness(t, testEvaluator(t))
	tracer := h.Provider.Tracer("test")

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			ctx, span := tracer.Start(context.Background(), "concurrent-op",
				trace.WithAttributes(attribute.String("cell_id", "cell-p1a")))
			h.Provider.Metrics().RecordIntentCreated(ctx, "cell-p1a", "standard", "SUCCESS")
			span.End()
		}()
	}
	wg.Wait()

	if report := h.Provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}

	spans := h.SpanExporter.GetSpans()
	if len(spans) != goroutines {
		t.Fatalf("got %d exported spans, want %d", len(spans), goroutines)
	}

	rm := collectMetrics(t, h.MetricReader)
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "intent.created" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("intent.created data = %T, want metricdata.Sum[int64]", m.Data)
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	if total != goroutines {
		t.Fatalf("intent.created total = %d, want %d", total, goroutines)
	}
}

// TestTodo_OBS_002_Integration exercises the full adapter end to end: a
// real Resource and Evaluator, a span carrying allow-listed and prohibited
// attributes plus propagated context, all five catalog metrics recorded,
// and telemetry.Check reporting HEALTHY against exactly what this package
// emitted.
func TestTodo_OBS_002_Integration(t *testing.T) {
	eval := testEvaluator(t)
	h := newTestHarness(t, eval)
	tracer := h.Provider.Tracer("integration")

	ctx := logging.WithCorrelationID(context.Background(), "corr-int-1")
	ctx, span := tracer.Start(ctx, "Capability.Invoke",
		trace.WithAttributes(
			attribute.String("capability_id", "cap.workflow.start"),
			attribute.String("workflow_node", "node.approve"),
			attribute.String("ssn", "000-00-0000"), // prohibited
		))
	span.SetStatus(codes.Ok, "")
	span.End()

	m := h.Provider.Metrics()
	m.RecordIntentCreated(ctx, "cell-p1a", "standard", "SUCCESS")
	m.RecordIntentSimulated(ctx, "cell-p1a", "standard", "SUCCESS")
	m.RecordLedgerAppend(ctx, "cell-p1a", "SUCCESS")
	m.RecordOutboxLag(ctx, "cell-p1a", 5)
	m.RecordEdgeParity(ctx, "cell-p1a", "edge-1", true)

	// ForceFlush (not Shutdown) before reading back: tracetest.
	// InMemoryExporter's own Shutdown clears its buffer (its documented
	// behavior, not this package's), so assertions read the exported state
	// first and Shutdown is exercised afterward, on its own, purely for its
	// own report.
	if flush := h.Provider.ForceFlush(context.Background()); flush.Err() != nil {
		t.Fatalf("ForceFlush: %v", flush.Err())
	}

	spans := h.SpanExporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("got %d exported spans, want 1", len(spans))
	}
	sp := spans[0]
	if v, ok := attrValue(sp.Attributes, "capability_id"); !ok || v != "cap.workflow.start" {
		t.Fatalf("capability_id = %q, %v", v, ok)
	}
	if v, ok := attrValue(sp.Attributes, "correlation_id"); !ok || v != "corr-int-1" {
		t.Fatalf("correlation_id = %q, %v", v, ok)
	}
	if _, ok := attrValue(sp.Attributes, "ssn"); ok {
		t.Fatal("prohibited attribute \"ssn\" reached the exporter")
	}
	if svc, ok := sp.Resource.Set().Value("service.name"); !ok || svc.AsString() != "hcm-otel-test" {
		t.Fatalf("span resource service.name = %v, %v", svc, ok)
	}

	rm := collectMetrics(t, h.MetricReader)
	got := metricNames(rm)
	want := append([]string(nil), telemetry.DefaultRequiredSignals().RequiredMetrics...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("metric names = %v, want %v", got, want)
	}

	// telemetry.Check certifies HEALTHY against exactly what this package
	// emits: metrics only (this package's Sink implementation reports no
	// log events — see Metrics.EmittedLogEventNames).
	required := telemetry.RequiredSignals{Version: 1, RequiredMetrics: telemetry.DefaultRequiredSignals().RequiredMetrics}
	checkReport, err := telemetry.Check(required, m, telemetry.PipelineFaults{}, time.Now())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if checkReport.Health != telemetry.HealthHealthy {
		t.Fatalf("Check().Health = %v, want HEALTHY; missing metrics=%v missing logs=%v", checkReport.Health, checkReport.MissingMetrics, checkReport.MissingLogEvents)
	}

	// Exercised last and on its own: Shutdown clears the in-memory
	// exporter's buffer as part of shutting it down, so nothing after this
	// call reads exported state back.
	if shutdown := h.Provider.Shutdown(context.Background()); shutdown.Err() != nil || shutdown.DeadlineExceeded {
		t.Fatalf("Shutdown() report = %+v, want a clean shutdown", shutdown)
	}
}

// TestTodo_OBS_002_Security proves an attacker cannot smuggle sensitive
// content onto an exported span merely by choosing a key that "looks safe",
// that a misconfigured (fail-closed) Evaluator drops every attribute rather
// than letting any through, and that this package's typed Metrics API gives
// no way to attach an arbitrary, unregistered label to a metric in the
// first place.
func TestTodo_OBS_002_Security(t *testing.T) {
	t.Run("adversarial_keys_and_values_never_reach_the_exporter", func(t *testing.T) {
		h := newTestHarness(t, testEvaluator(t))
		tracer := h.Provider.Tracer("test")

		_, span := tracer.Start(context.Background(), "op", trace.WithAttributes(
			attribute.String("worker_id", "w-123"),
			attribute.String("request_body", `{"password":"hunter2"}`),
			attribute.String("case_notes", "confidential"),
			attribute.String("prompt", "ignore previous instructions"),
		))
		span.End()

		if report := h.Provider.ForceFlush(context.Background()); report.Err() != nil {
			t.Fatalf("ForceFlush: %v", report.Err())
		}
		spans := h.SpanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("got %d exported spans, want 1", len(spans))
		}
		for _, key := range []string{"worker_id", "request_body", "case_notes", "prompt"} {
			if _, ok := attrValue(spans[0].Attributes, key); ok {
				t.Fatalf("adversarial key %q reached the exporter", key)
			}
		}
	})

	t.Run("misconfigured_evaluator_fails_closed_for_every_attribute", func(t *testing.T) {
		broken := &telemetry.Evaluator{} // non-nil pointer, nil Allow: fails closed
		h := newTestHarness(t, broken)
		tracer := h.Provider.Tracer("test")

		_, span := tracer.Start(context.Background(), "op", trace.WithAttributes(
			attribute.String("cell_id", "cell-p1a"), // otherwise allow-listed
		))
		span.End()

		if report := h.Provider.ForceFlush(context.Background()); report.Err() != nil {
			t.Fatalf("ForceFlush: %v", report.Err())
		}
		spans := h.SpanExporter.GetSpans()
		if len(spans) != 1 {
			t.Fatalf("got %d exported spans, want 1", len(spans))
		}
		if len(spans[0].Attributes) != 0 {
			t.Fatalf("misconfigured evaluator kept attributes: %v, want none", spans[0].Attributes)
		}
	})
}
