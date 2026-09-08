package otel

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	maxContinuationIDLen = 128
	maxTraceStateLen     = 256
)

var (
	ErrContinuationMetadata = errors.New("otel: invalid durable continuation metadata")
	ErrTraceLinkMetadata    = errors.New("otel: invalid trace link metadata")
)

// TraceLinkMetadata is optional operational context that may be persisted
// beside a durable work envelope. It is never a business identifier or an
// authority input, and it may be discarded without changing business
// behavior.
type TraceLinkMetadata struct {
	TraceID    string
	SpanID     string
	TraceFlags byte
	TraceState string
	ExpiresAt  time.Time
}

// DurableAsyncContinuation contains the stable identifiers needed to relate
// a continuation to its logical operation. These fields belong to the owner
// of the durable envelope; the optional TraceLink is only diagnostic context.
type DurableAsyncContinuation struct {
	CorrelationID      string
	CausationID        string
	LogicalOperationID string
	AttemptID          string
	TraceLink          *TraceLinkMetadata
}

// Validate checks bounded durable metadata without interpreting any field as
// authorization, tenant, actor, evidence, or other business authority.
func (c DurableAsyncContinuation) Validate() error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{"correlation_id", c.CorrelationID},
		{"causation_id", c.CausationID},
		{"logical_operation_id", c.LogicalOperationID},
		{"attempt_id", c.AttemptID},
	} {
		if strings.TrimSpace(field.value) == "" || len(field.value) > maxContinuationIDLen {
			return fmt.Errorf("%w: %s", ErrContinuationMetadata, field.name)
		}
	}
	return nil
}

func (m TraceLinkMetadata) validate() error {
	traceID, err := trace.TraceIDFromHex(m.TraceID)
	if err != nil || !traceID.IsValid() {
		return fmt.Errorf("%w: trace_id", ErrTraceLinkMetadata)
	}
	spanID, err := trace.SpanIDFromHex(m.SpanID)
	if err != nil || !spanID.IsValid() {
		return fmt.Errorf("%w: span_id", ErrTraceLinkMetadata)
	}
	if len(m.TraceState) > maxTraceStateLen {
		return fmt.Errorf("%w: trace_state", ErrTraceLinkMetadata)
	}
	if m.TraceState != "" {
		if _, err := trace.ParseTraceState(m.TraceState); err != nil {
			return fmt.Errorf("%w: trace_state", ErrTraceLinkMetadata)
		}
	}
	return nil
}

// TraceLinkMetadata returns a copy of this span's context suitable for
// optional persistence beside a durable envelope.
func (s ExecutionSpan) TraceLinkMetadata(expiresAt time.Time) (TraceLinkMetadata, bool) {
	sc := trace.SpanContextFromContext(s.ctx)
	if !sc.IsValid() {
		return TraceLinkMetadata{}, false
	}
	return TraceLinkMetadata{
		TraceID:    sc.TraceID().String(),
		SpanID:     sc.SpanID().String(),
		TraceFlags: byte(sc.TraceFlags()),
		TraceState: sc.TraceState().String(),
		ExpiresAt:  expiresAt,
	}, true
}

func (m TraceLinkMetadata) spanContext() (trace.SpanContext, bool) {
	if err := m.validate(); err != nil {
		return trace.SpanContext{}, false
	}
	tid, _ := trace.TraceIDFromHex(m.TraceID)
	sid, _ := trace.SpanIDFromHex(m.SpanID)
	state, _ := trace.ParseTraceState(m.TraceState)
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: tid, SpanID: sid, TraceFlags: trace.TraceFlags(m.TraceFlags),
		TraceState: state, Remote: true,
	}), true
}

// StartDurableAsyncSpan starts a finite new-root span for a continuation,
// partition, redelivery, or repair attempt. A valid, unexpired trace link is
// attached without keeping the prior span open. Missing or expired optional
// trace metadata produces the same new-root span and leaves all durable
// business identifiers unchanged.
func (p *Provider) StartDurableAsyncSpan(ctx context.Context, tracerName, spanName string, c DurableAsyncContinuation, attrs map[string]string, now time.Time) (context.Context, ExecutionSpan, error) {
	if err := c.Validate(); err != nil {
		return ctx, ExecutionSpan{}, err
	}
	tracer := p.Tracer(tracerName)
	startOpts := []trace.SpanStartOption{trace.WithNewRoot()}
	if c.TraceLink != nil && (c.TraceLink.ExpiresAt.IsZero() || now.Before(c.TraceLink.ExpiresAt)) {
		if linked, ok := c.TraceLink.spanContext(); ok {
			startOpts = append(startOpts, trace.WithLinks(trace.Link{SpanContext: linked}))
		}
	}
	spanAttrs := make(map[string]string, len(attrs)+3)
	for key, value := range attrs {
		spanAttrs[key] = value
	}
	// These are envelope-owned identities. A caller may add other attributes,
	// but cannot replace them with a value unrelated to the continuation.
	spanAttrs["correlation_id"] = c.CorrelationID
	spanAttrs["logical_operation_id"] = c.LogicalOperationID
	spanAttrs["attempt_id"] = c.AttemptID
	startOpts = append(startOpts, trace.WithAttributes(stringAttributes(spanAttrs)...))
	spanCtx, span := tracer.Start(ctx, spanName, startOpts...)
	return spanCtx, ExecutionSpan{span: span, ctx: spanCtx}, nil
}

func stringAttributes(attrs map[string]string) []attribute.KeyValue {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]attribute.KeyValue, 0, len(attrs))
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := attrs[key]
		out = append(out, attribute.String(key, value))
	}
	return out
}
