package otel_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
)

func TestDurableAsyncContinuationCreatesExactSpanLinksWithoutOpenParentSpan(t *testing.T) {
	h := newTestHarness(t, testEvaluator(t))
	sourceCtx, source := h.Provider.StartExecutionSpan(context.Background(), "source", "hcmnext.workflow.node", map[string]string{
		"logical_operation_id": "op-1",
	})
	link, ok := source.TraceLinkMetadata(time.Unix(200, 0))
	if !ok {
		t.Fatal("source span did not produce link metadata")
	}
	if source.TraceID() != link.TraceID || hcmotel.AmbientTraceID(source.Context()) != link.TraceID {
		t.Fatal("execution span accessors did not retain the exact source trace identity")
	}
	source.End("SUCCESS", false)

	continuation := hcmotel.DurableAsyncContinuation{
		CorrelationID:      "corr-1",
		CausationID:        "cause-1",
		LogicalOperationID: "op-1",
		AttemptID:          "attempt-2",
		TraceLink:          &link,
	}
	_, child, err := h.Provider.StartDurableAsyncSpan(sourceCtx, "worker", "hcmnext.timer.resume", continuation, nil, time.Unix(100, 0))
	if err != nil {
		t.Fatalf("StartDurableAsyncSpan: %v", err)
	}
	child.End("SUCCESS", false)

	redelivery := continuation
	redelivery.AttemptID = "attempt-3"
	_, redeliveredChild, err := h.Provider.StartDurableAsyncSpan(sourceCtx, "worker", "hcmnext.queue.deliver", redelivery, nil, time.Unix(100, 0))
	if err != nil {
		t.Fatalf("StartDurableAsyncSpan redelivery: %v", err)
	}
	redeliveredChild.End("SUCCESS", false)

	fanout := continuation
	fanout.AttemptID = "partition-1"
	_, partition, err := h.Provider.StartDurableAsyncSpan(sourceCtx, "worker", "hcmnext.job.partition", fanout, nil, time.Unix(100, 0))
	if err != nil {
		t.Fatalf("StartDurableAsyncSpan fan-out: %v", err)
	}
	partition.End("SUCCESS", false)

	if report := h.Provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}
	spans := h.SpanExporter.GetSpans()
	if len(spans) != 4 {
		t.Fatalf("got %d spans, want source and three finite continuations", len(spans))
	}
	for i, linked := range spans[1:] {
		if linked.Parent.IsValid() {
			t.Fatalf("continuation %d retained an open parent span", i)
		}
		if len(linked.Links) != 1 || linked.Links[0].SpanContext.TraceID().String() != link.TraceID || linked.Links[0].SpanContext.SpanID().String() != link.SpanID {
			t.Fatalf("continuation %d links = %#v, want exact source trace and span IDs", i, linked.Links)
		}
		if linked.SpanContext.TraceID() == spans[0].SpanContext.TraceID() {
			t.Fatalf("continuation %d reused source trace instead of starting a new root", i)
		}
	}
	for i, wantAttempt := range []string{"attempt-2", "attempt-3", "partition-1"} {
		if got, ok := attrValue(spans[i+1].Attributes, "correlation_id"); !ok || got != "corr-1" {
			t.Fatalf("continuation %d correlation_id = %q, %v", i, got, ok)
		}
		if got, ok := attrValue(spans[i+1].Attributes, "logical_operation_id"); !ok || got != "op-1" {
			t.Fatalf("continuation %d logical_operation_id = %q, %v", i, got, ok)
		}
		if got, ok := attrValue(spans[i+1].Attributes, "attempt_id"); !ok || got != wantAttempt {
			t.Fatalf("continuation %d attempt_id = %q, %v; want %q", i, got, ok, wantAttempt)
		}
	}
	if redelivery.LogicalOperationID != continuation.LogicalOperationID || redelivery.AttemptID == continuation.AttemptID {
		t.Fatal("redelivery did not reuse logical-operation identity with a distinct attempt")
	}
	if fanout.LogicalOperationID != continuation.LogicalOperationID || fanout.AttemptID == continuation.AttemptID {
		t.Fatal("fan-out did not reuse logical-operation identity with a distinct partition attempt")
	}
}

func TestTodo_OBS_013_Property(t *testing.T) {
	base := hcmotel.DurableAsyncContinuation{CorrelationID: "corr", CausationID: "cause", LogicalOperationID: "op", AttemptID: "attempt"}
	for _, field := range []struct {
		name string
		set  func(*hcmotel.DurableAsyncContinuation, string)
	}{
		{"correlation", func(c *hcmotel.DurableAsyncContinuation, v string) { c.CorrelationID = v }},
		{"causation", func(c *hcmotel.DurableAsyncContinuation, v string) { c.CausationID = v }},
		{"logical operation", func(c *hcmotel.DurableAsyncContinuation, v string) { c.LogicalOperationID = v }},
		{"attempt", func(c *hcmotel.DurableAsyncContinuation, v string) { c.AttemptID = v }},
	} {
		t.Run(field.name+" missing", func(t *testing.T) {
			got := base
			field.set(&got, " ")
			if !errors.Is(got.Validate(), hcmotel.ErrContinuationMetadata) {
				t.Fatalf("Validate() = %v", got.Validate())
			}
		})
		t.Run(field.name+" bounded", func(t *testing.T) {
			got := base
			field.set(&got, strings.Repeat("x", 129))
			if !errors.Is(got.Validate(), hcmotel.ErrContinuationMetadata) {
				t.Fatalf("Validate() = %v", got.Validate())
			}
		})
	}
}

func TestTodo_OBS_013_Fault(t *testing.T) {
	h := newTestHarness(t, testEvaluator(t))
	base := hcmotel.DurableAsyncContinuation{CorrelationID: "corr", CausationID: "cause", LogicalOperationID: "op", AttemptID: "attempt"}
	for _, tc := range []struct {
		name string
		link *hcmotel.TraceLinkMetadata
	}{
		{"missing", nil},
		{"malformed", &hcmotel.TraceLinkMetadata{TraceID: "not-a-trace", SpanID: "not-a-span"}},
		{"malformed span", &hcmotel.TraceLinkMetadata{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "not-a-span"}},
		{"oversized trace state", &hcmotel.TraceLinkMetadata{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", TraceState: strings.Repeat("x", 257)}},
		{"malformed trace state", &hcmotel.TraceLinkMetadata{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", TraceState: "invalid"}},
		{"expired", &hcmotel.TraceLinkMetadata{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", ExpiresAt: time.Unix(99, 0)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			continuation := base
			continuation.TraceLink = tc.link
			_, span, err := h.Provider.StartDurableAsyncSpan(context.Background(), "worker", "hcmnext.queue.deliver", continuation, map[string]string{
				"correlation_id": "forged-correlation", "logical_operation_id": "forged-op", "attempt_id": "forged-attempt", "tenant_id": "tenant-authority",
			}, time.Unix(100, 0))
			if err != nil {
				t.Fatalf("optional link changed business path: %v", err)
			}
			span.End("SUCCESS", false)
		})
	}
	if report := h.Provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}
	for i, span := range h.SpanExporter.GetSpans() {
		if span.Parent.IsValid() || len(span.Links) != 0 {
			t.Fatalf("fallback span %d topology: parent=%v links=%#v", i, span.Parent, span.Links)
		}
		if got, _ := attrValue(span.Attributes, "correlation_id"); got != "corr" {
			t.Fatalf("fallback span %d correlation_id = %q", i, got)
		}
		if got, _ := attrValue(span.Attributes, "logical_operation_id"); got != "op" {
			t.Fatalf("fallback span %d logical_operation_id = %q", i, got)
		}
		if got, _ := attrValue(span.Attributes, "attempt_id"); got != "attempt" {
			t.Fatalf("fallback span %d attempt_id = %q", i, got)
		}
		if _, ok := attrValue(span.Attributes, "tenant_id"); ok {
			t.Fatalf("fallback span %d exported authority-bearing tenant_id", i)
		}
	}
	invalid := base
	invalid.AttemptID = ""
	if _, _, err := h.Provider.StartDurableAsyncSpan(context.Background(), "worker", "hcmnext.queue.deliver", invalid, nil, time.Unix(100, 0)); !errors.Is(err, hcmotel.ErrContinuationMetadata) {
		t.Fatalf("invalid durable metadata error = %v", err)
	}
}

func TestTodo_OBS_013_Conformance(t *testing.T) {
	eval := testEvaluator(t)
	for _, key := range []string{"logical_operation_id", "attempt_id"} {
		if d := eval.EvaluateAttribute(telemetry.SignalSpan, key, "owned-id"); !d.Kept {
			t.Fatalf("span %s was rejected: %+v", key, d)
		}
		for _, signal := range []telemetry.SignalKind{telemetry.SignalLog, telemetry.SignalMetric} {
			if d := eval.EvaluateAttribute(signal, key, "owned-id"); d.Kept {
				t.Fatalf("%s retained span-only %s", signal, key)
			}
		}
	}
	if _, ok := (hcmotel.ExecutionSpan{}).TraceLinkMetadata(time.Time{}); ok {
		t.Fatal("zero execution span produced forgeable trace metadata")
	}
	if got := hcmotel.AmbientTraceID(context.Background()); got != "" {
		t.Fatalf("background context trace id = %q", got)
	}
}

// TestTodo_OBS_013_Golden pins the exact durable-async span shape for fixed
// inputs: span name, new-root parent, the one exact link, and the sorted
// attribute set, digested. A digest change is a deliberate encoding change,
// never silent drift: the new-root trace id itself is excluded because it is
// random per span by construction.
func TestTodo_OBS_013_Golden(t *testing.T) {
	h := newTestHarness(t, testEvaluator(t))
	continuation := hcmotel.DurableAsyncContinuation{
		CorrelationID: "corr-golden", CausationID: "cause-golden",
		LogicalOperationID: "op-golden", AttemptID: "attempt-golden",
		TraceLink: &hcmotel.TraceLinkMetadata{
			TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7",
			TraceFlags: 1, TraceState: "vendor=value",
			ExpiresAt: time.Unix(200, 0),
		},
	}
	_, span, err := h.Provider.StartDurableAsyncSpan(context.Background(), "worker", "hcmnext.queue.deliver", continuation,
		map[string]string{"message_kind": "outbox"}, time.Unix(100, 0))
	if err != nil {
		t.Fatalf("StartDurableAsyncSpan: %v", err)
	}
	span.End("SUCCESS", false)
	if report := h.Provider.ForceFlush(context.Background()); report.Err() != nil {
		t.Fatalf("ForceFlush: %v", report.Err())
	}
	got := h.SpanExporter.GetSpans()
	if len(got) != 1 {
		t.Fatalf("got %d spans, want exactly 1", len(got))
	}
	s := got[0]
	if s.Name != "hcmnext.queue.deliver" || s.Parent.IsValid() {
		t.Fatalf("span = name %q parent %v, want new root hcmnext.queue.deliver", s.Name, s.Parent)
	}
	if len(s.Links) != 1 {
		t.Fatalf("links = %#v, want exactly the stored trace link", s.Links)
	}
	link := s.Links[0].SpanContext
	if link.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" || link.SpanID().String() != "00f067aa0ba902b7" || link.TraceFlags().String() != "01" {
		t.Fatalf("link = %v, want the stored trace/span IDs with sampled flags", link)
	}
	var pairs []string
	for _, attr := range s.Attributes {
		pairs = append(pairs, fmt.Sprintf("%s=%s", string(attr.Key), attr.Value.AsString()))
	}
	sort.Strings(pairs)
	// NOTE: causation_id is validated and persisted on the envelope but is
	// not a span attribute (the forced span identities are correlation,
	// logical-operation and attempt); outcome arrives via Span.End.
	wantAttrs := []string{
		"attempt_id=attempt-golden",
		"correlation_id=corr-golden",
		"logical_operation_id=op-golden",
		"message_kind=outbox",
		"outcome=SUCCESS",
	}
	if fmt.Sprintf("%q", pairs) != fmt.Sprintf("%q", wantAttrs) {
		t.Fatalf("attrs = %q, want %q", pairs, wantAttrs)
	}
	digest := sha256.Sum256([]byte("hcmnext.queue.deliver|" + link.TraceID().String() + "|" + link.SpanID().String() + "|01|" + strings.Join(pairs, ",")))
	const wantDigest = "sha256:f6ad928d3af3d343a70b1bac4b85d4b775f1de0d759179aeb532a3a155f395b5"
	if got := "sha256:" + hex.EncodeToString(digest[:]); got != wantDigest {
		t.Fatalf("golden digest = %s, want %s", got, wantDigest)
	}
}
