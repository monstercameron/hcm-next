package execute

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/buildinfo"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	oteltest "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel/testexport"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type approvalExportInstrumentation struct {
	provider   *hcmotel.Provider
	starts     atomic.Int32
	ends       atomic.Int32
	duplicates atomic.Int32
}

func (i *approvalExportInstrumentation) TraceID(ctx context.Context) string {
	return hcmotel.AmbientTraceID(ctx)
}
func (i *approvalExportInstrumentation) StartNodeSpan(ctx context.Context, _ SpanAttributes) (context.Context, Span) {
	return ctx, noopSpan{}
}
func (i *approvalExportInstrumentation) StartTerminalSpan(ctx context.Context, _ SpanAttributes) (context.Context, Span) {
	return ctx, noopSpan{}
}
func (i *approvalExportInstrumentation) StartAdvanceSpan(ctx context.Context, _ SpanAttributes) (context.Context, Span) {
	i.starts.Add(1)
	ctx, span := i.provider.StartExecutionSpan(ctx, "approval-obs013-test", "hcmnext.workflow.advance", nil)
	return ctx, &approvalExportSpan{span: span, ends: &i.ends, duplicates: &i.duplicates}
}

type approvalExportSpan struct {
	span       hcmotel.ExecutionSpan
	ends       *atomic.Int32
	duplicates *atomic.Int32
	ended      atomic.Bool
}

func (s *approvalExportSpan) End(outcome string, err error) {
	if !s.ended.CompareAndSwap(false, true) {
		s.duplicates.Add(1)
		return
	}
	s.ends.Add(1)
	s.span.End(outcome, err != nil)
}
func (s *approvalExportSpan) CausalMetadata(id CausalIdentity) *runtime.CausalMetadata {
	link, ok := s.span.TraceLinkMetadata(id.ExpiresAt)
	if !ok {
		return nil
	}
	return &runtime.CausalMetadata{
		CorrelationID: id.CorrelationID, CausationID: id.CausationID, LogicalOperationID: id.LogicalOperationID, AttemptID: id.AttemptID,
		TraceLink: &runtime.TraceLinkMetadata{TraceID: link.TraceID, SpanID: link.SpanID, TraceFlags: link.TraceFlags, TraceState: link.TraceState, ExpiresAt: link.ExpiresAt},
	}
}

func TestTodo_OBS_013_CompleteApprovalPersistsRealExporterLinkAndEndsOnce(t *testing.T) {
	f := newWork006Fixture(t)
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	resource := telemetry.NewResourceFromBuild(buildinfo.Info{Revision: "obs013"}, "approval-test", "one", "test", "cell-local", "us-east-1", telemetry.ProcessRoleAPI, telemetry.TenantClassStandard)
	recorder := oteltest.NewSpanRecorder()
	provider, err := hcmotel.NewProvider(context.Background(), hcmotel.Config{Resource: resource, Evaluator: telemetry.NewEvaluator(allow, telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion), telemetry.DefaultSamplingPolicy()), Trace: hcmotel.TraceConfig{Exporter: recorder}, Metric: hcmotel.MetricConfig{Reader: sdkmetric.NewManualReader()}, ShutdownTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	inst := &approvalExportInstrumentation{provider: provider}
	driver, err := New(Options{DB: f.conn, Steps: work006EndRunner{}, Terminal: work006Terminal{}, Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 24 * time.Hour}, Instrumentation: inst})
	if err != nil {
		t.Fatal(err)
	}
	_, err = driver.CompleteApproval(context.Background(), f.request(&work006Authority{allowed: true}))
	if err != nil {
		t.Fatalf("CompleteApproval: %v", err)
	}
	if inst.starts.Load() != inst.ends.Load() || inst.duplicates.Load() != 0 {
		t.Fatalf("advance spans started=%d ended=%d duplicate ends=%d", inst.starts.Load(), inst.ends.Load(), inst.duplicates.Load())
	}
	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatal(report.Err())
	}
	if len(recorder.Spans()) != int(inst.ends.Load()) {
		t.Fatalf("exported spans = %d, ended = %d", len(recorder.Spans()), inst.ends.Load())
	}
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		var correlation, causation, logical, attempt, traceID, spanID string
		if err := tx.QueryRow(context.Background(), `SELECT correlation_id, causation_id, logical_operation_id, attempt_id, trace_id, trace_span_id FROM workflow_continuation WHERE tenant_id=$1 AND instance_id=$2 ORDER BY recorded_at LIMIT 1`, f.tenantID, f.instanceID).Scan(&correlation, &causation, &logical, &attempt, &traceID, &spanID); err != nil {
			return err
		}
		if correlation != "correlation:work-006" || causation == "" || logical != f.instanceID.String() || attempt == "" || traceID == "" || spanID == "" {
			t.Fatalf("persisted approval causal = correlation=%q causation=%q logical=%q attempt=%q trace=%q span=%q", correlation, causation, logical, attempt, traceID, spanID)
		}
		wantOperation := runtime.NodeExecutionID(f.tenantID, f.instanceID, f.item.NodeID, 1).String()
		approvalSpan := recorder.Spans()[0].SpanContext()
		if causation != wantOperation || attempt != wantOperation || traceID != approvalSpan.TraceID().String() || spanID != approvalSpan.SpanID().String() {
			t.Fatalf("persisted approval link/identity does not match exporter: cause=%q attempt=%q trace=%q span=%q exporter=%s/%s", causation, attempt, traceID, spanID, approvalSpan.TraceID(), approvalSpan.SpanID())
		}
		return nil
	})

	failed := newWork006Fixture(t)
	beforeStarts, beforeEnds, beforeExported := inst.starts.Load(), inst.ends.Load(), len(recorder.Spans())
	failureDriver, err := New(Options{
		DB: failed.conn, Steps: work006EndRunner{}, Terminal: work006Terminal{},
		Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 24 * time.Hour}, Instrumentation: inst,
		Advance: func(context.Context, runtime.Executor, runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			return runtime.AdvanceReceipt{}, errors.New("injected advancement failure")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failureDriver.CompleteApproval(context.Background(), failed.request(&work006Authority{allowed: true})); err == nil {
		t.Fatal("CompleteApproval succeeded despite injected advancement failure")
	}
	if report := provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatal(report.Err())
	}
	if inst.starts.Load() != beforeStarts+1 || inst.ends.Load() != beforeEnds+1 || inst.starts.Load() != inst.ends.Load() || inst.duplicates.Load() != 0 || len(recorder.Spans()) != beforeExported+1 {
		t.Fatalf("failure spans started=%d ended=%d exported=%d duplicate ends=%d", inst.starts.Load(), inst.ends.Load(), len(recorder.Spans()), inst.duplicates.Load())
	}
	work006Tx(t, failed.conn, failed.tenantID, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM workflow_continuation WHERE tenant_id=$1 AND instance_id=$2`, failed.tenantID, failed.instanceID).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("failed approval left %d durable continuations", count)
		}
		return nil
	})
}

var _ CausalSpan = (*approvalExportSpan)(nil)
var _ Instrumentation = (*approvalExportInstrumentation)(nil)
