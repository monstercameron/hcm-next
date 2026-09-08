package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/monstercameron/hcm-next/internal/data/outbox"
	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
	"github.com/monstercameron/hcm-next/internal/platform/buildinfo"
	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/hcm-next/internal/platform/telemetry/otel"
)

type telemetryOrderDispatcher struct {
	message  outbox.Record
	provider *hcmotel.Provider
	exporter *tracetest.InMemoryExporter
	acked    bool
	failed   bool
	t        *testing.T
}

func (d *telemetryOrderDispatcher) Poll(context.Context, uuid.UUID) ([]outbox.Record, error) {
	return []outbox.Record{d.message}, nil
}

func (d *telemetryOrderDispatcher) Ack(context.Context, uuid.UUID, uuid.UUID) error {
	d.assertSpanEnded()
	d.acked = true
	return nil
}

func (d *telemetryOrderDispatcher) Fail(context.Context, uuid.UUID, uuid.UUID, error) error {
	d.assertSpanEnded()
	d.failed = true
	return nil
}

func (d *telemetryOrderDispatcher) assertSpanEnded() {
	d.t.Helper()
	if report := d.provider.ForceFlush(context.Background()); report.Err() != nil {
		d.t.Fatalf("ForceFlush before settlement: %v", report.Err())
	}
	if len(d.exporter.GetSpans()) != 1 {
		d.t.Fatal("outbox lease was settled before its finite dispatch span ended")
	}
}

func workerTestProvider(t *testing.T) (*hcmotel.Provider, *tracetest.InMemoryExporter) {
	t.Helper()
	allowlist, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	exporter := tracetest.NewInMemoryExporter()
	provider, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{
		Resource:        telemetry.NewResourceFromBuild(buildinfo.Info{Revision: "test"}, "worker-test", "instance", "test", "cell-local", "", telemetry.ProcessRoleWorker, telemetry.TenantClassStandard),
		Evaluator:       telemetry.NewEvaluator(allowlist, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy()),
		ShutdownTimeout: time.Second,
		Trace:           hcmotel.TraceConfig{Exporter: exporter},
		Metric:          hcmotel.MetricConfig{Reader: sdkmetric.NewManualReader()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return provider, exporter
}

func TestTodo_OBS_013_WorkerDispatchCreatesFiniteLinkedAttemptSpanBeforeSettlement(t *testing.T) {
	for _, tc := range []struct {
		name       string
		handlerErr error
		status     codes.Code
	}{
		{"success", nil, codes.Ok},
		{"failure", errors.New("provider failed"), codes.Error},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, exporter := workerTestProvider(t)
			tenant, messageID := uuid.New(), uuid.New()
			msg := outbox.Record{Tenant: tenant, OutboxID: messageID, SchemaRef: "hcmnext.test.v1.Event@1", Causal: &outbox.CausalMetadata{
				CorrelationID: "corr-1", CausationID: "cause-1", LogicalOperationID: "logical-1", AttemptID: "attempt-2",
				TraceLink: &outbox.TraceLink{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", TraceFlags: 1, ExpiresAt: time.Unix(200, 0)},
			}}
			dispatcher := &telemetryOrderDispatcher{message: msg, provider: provider, exporter: exporter, t: t}
			didWork, err := sweepWithTelemetry(context.Background(), discardLogger(), fakeTenantLister{tenants: []uuid.UUID{tenant}}, dispatcher, func(ctx context.Context, _ outbox.Record) error {
				if hcmotel.AmbientTraceID(ctx) == "" {
					t.Fatal("handler did not receive the finite attempt span context")
				}
				return tc.handlerErr
			}, provider, func() time.Time { return time.Unix(100, 0) })
			if err != nil || !didWork {
				t.Fatalf("sweep = %v, %v", didWork, err)
			}
			if (tc.handlerErr == nil && (!dispatcher.acked || dispatcher.failed)) || (tc.handlerErr != nil && (!dispatcher.failed || dispatcher.acked)) {
				t.Fatalf("settlement acked=%v failed=%v", dispatcher.acked, dispatcher.failed)
			}
			span := exporter.GetSpans()[0]
			if span.Name != "hcmnext.queue.deliver" || span.Parent.IsValid() || span.Status.Code != tc.status {
				t.Fatalf("span topology/status = name %q parent %v status %v", span.Name, span.Parent, span.Status.Code)
			}
			if len(span.Links) != 1 || span.Links[0].SpanContext.TraceID().String() != msg.Causal.TraceLink.TraceID || span.Links[0].SpanContext.SpanID().String() != msg.Causal.TraceLink.SpanID {
				t.Fatalf("links = %#v", span.Links)
			}
			attrs := map[string]string{}
			for _, attr := range span.Attributes {
				attrs[string(attr.Key)] = attr.Value.AsString()
			}
			if attrs["correlation_id"] != "corr-1" || attrs["logical_operation_id"] != "logical-1" || attrs["attempt_id"] != "attempt-2" || attrs["message_kind"] != string(workerMessageKindOutbox) {
				t.Fatalf("span attrs = %#v", attrs)
			}
		})
	}
}

func TestTodo_OBS_013_WorkerMissingOrInvalidMetadataPreservesDispatch(t *testing.T) {
	provider, exporter := workerTestProvider(t)
	for _, causal := range []*outbox.CausalMetadata{
		nil,
		{CorrelationID: "", CausationID: "cause", LogicalOperationID: "logical", AttemptID: "attempt"},
	} {
		tenant, messageID := uuid.New(), uuid.New()
		called := false
		dispatcher := &fakeDispatcher{batches: map[uuid.UUID][]outbox.Record{tenant: {{Tenant: tenant, OutboxID: messageID, Causal: causal}}}}
		didWork, err := sweepWithTelemetry(context.Background(), discardLogger(), fakeTenantLister{tenants: []uuid.UUID{tenant}}, dispatcher, func(context.Context, outbox.Record) error {
			called = true
			return nil
		}, provider, time.Now)
		if err != nil || !didWork || !called || len(dispatcher.acked) != 1 || len(dispatcher.failed) != 0 {
			t.Fatalf("business dispatch changed: didWork=%v called=%v acked=%v failed=%v err=%v", didWork, called, dispatcher.acked, dispatcher.failed, err)
		}
	}
	if report := provider.ForceFlush(context.Background()); report.Err() != nil || len(exporter.GetSpans()) != 0 {
		t.Fatalf("invalid metadata export = %d spans, %v", len(exporter.GetSpans()), report.Err())
	}
}

func TestTodo_OBS_013_WorkerTelemetryUsesConfiguredProductionResource(t *testing.T) {
	values, err := bootstrap.ParseConfig([]string{"-otel-exporter=stdout", "-deployment-environment=production", "-cell-id=cell-prod"}, func(string) (string, bool) { return "", false }, workerConfigFields())
	if err != nil {
		t.Fatal(err)
	}
	provider, err := newWorkerTelemetryProvider(context.Background(), "worker-prod-1", values)
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil {
		t.Fatal("configured exporter produced no provider")
	}
	attrs := map[string]string{}
	for _, attr := range provider.Resource().Attributes() {
		attrs[string(attr.Key)] = attr.Value.AsString()
	}
	if attrs["deployment.environment"] != "production" || attrs["cell.id"] != "cell-prod" || attrs["process.role"] != "worker" {
		t.Fatalf("worker resource attrs = %#v", attrs)
	}
	allowlist, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	eval := telemetry.NewEvaluator(allowlist, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy())
	for _, signal := range []telemetry.SignalKind{telemetry.SignalLog, telemetry.SignalMetric} {
		if decision := eval.EvaluateAttribute(signal, "message_kind", string(workerMessageKindOutbox)); decision.Kept {
			t.Fatalf("%s retained span-only message_kind", signal)
		}
	}
	_ = provider.Shutdown(context.Background())
}
