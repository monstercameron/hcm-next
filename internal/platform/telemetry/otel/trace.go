package otel

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"

	"github.com/monstercameron/hcm-next/internal/platform/logging"
	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
)

// Context-propagation attribute keys this package attempts to attach to
// every span it starts, sourced from internal/platform/logging's
// request-scoped context (the task's own accessors, not
// internal/platform/telemetry's separate in-process copy — see doc.go and
// internal/platform/logging/context.go). Each name matches
// internal/platform/telemetry's own vocabulary (definitions/telemetry/
// resource-contract.yaml's context_propagation_keys and attribute_allowlist)
// so the same Evaluator that governs every other span attribute governs
// these identically: correlation_id is a registered SignalSpan attribute and
// survives; request_id, evidence_ref and principal_ref are not registered
// attribute keys today and are therefore redacted the same way an
// unregistered caller-supplied key would be. That is not a bug in this
// package — it is the frozen OBS-001/004 policy applied without exception.
const (
	attrKeyCorrelationID = "correlation_id"
	attrKeyRequestID     = "request_id"
	attrKeyEvidenceRef   = "evidence_ref"
	attrKeyPrincipalRef  = "principal_ref"
)

// contextAttributes reads internal/platform/logging's context values and
// returns them as candidate span attributes. It never decides what survives
// export: filterAttributes (via the Evaluator) makes that decision uniformly
// for these and for every other attribute a caller supplies.
func contextAttributes(ctx context.Context) []attribute.KeyValue {
	var out []attribute.KeyValue
	if v, ok := logging.CorrelationID(ctx); ok && v != "" {
		out = append(out, attribute.String(attrKeyCorrelationID, v))
	}
	if v, ok := logging.RequestID(ctx); ok && v != "" {
		out = append(out, attribute.String(attrKeyRequestID, v))
	}
	if ids, ok := logging.EvidenceIDs(ctx); ok && len(ids) > 0 {
		out = append(out, attribute.String(attrKeyEvidenceRef, strings.Join(ids, ",")))
	}
	if v, ok := logging.PrincipalRef(ctx); ok && v != "" {
		out = append(out, attribute.String(attrKeyPrincipalRef, v))
	}
	return out
}

// filterAttributes classifies every attribute in attrs against eval for the
// given signal kind and returns only the ones the Evaluator kept, in their
// original order and with their original value unchanged (a span/log
// attribute is never cardinality-capped — only SignalMetric is; see
// telemetry.Evaluator.EvaluateAttribute). A denied attribute is dropped here,
// before it is ever handed to a real go.opentelemetry.io/otel/sdk/trace
// object, so no exporter attached to that object — including an in-memory
// one — ever observes it.
//
// A nil eval fails closed: telemetry.Evaluator.EvaluateAttribute already
// treats a nil-Allow evaluator as drop-everything, and a nil *Evaluator
// itself (this package never constructs a Provider with one — see
// provider.go's Config validation) would panic on the method call, which is
// why NewProvider refuses a nil Evaluator up front rather than relying on
// this call site to guard it.
func filterAttributes(eval *telemetry.Evaluator, kind telemetry.SignalKind, attrs []attribute.KeyValue) []attribute.KeyValue {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]attribute.KeyValue, 0, len(attrs))
	for _, kv := range attrs {
		d := eval.EvaluateAttribute(kind, string(kv.Key), kv.Value.String())
		if d.Kept {
			out = append(out, kv)
		}
	}
	return out
}

// tracerProvider wraps a real trace.TracerProvider (an *sdktrace.
// TracerProvider in production, or any trace.TracerProvider a test
// supplies) so every Tracer it returns filters span/event attributes
// through eval before they reach the wrapped provider's spans.
type tracerProvider struct {
	embedded.TracerProvider
	real trace.TracerProvider
	eval *telemetry.Evaluator
}

func newTracerProvider(real trace.TracerProvider, eval *telemetry.Evaluator) trace.TracerProvider {
	return &tracerProvider{real: real, eval: eval}
}

// Tracer implements trace.TracerProvider.
func (p *tracerProvider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	return &tracer{real: p.real.Tracer(name, opts...), eval: p.eval}
}

type tracer struct {
	embedded.Tracer
	real trace.Tracer
	eval *telemetry.Evaluator
}

// Start implements trace.Tracer. It merges the caller's own span-start
// attributes with the automatic context-propagation attributes
// (contextAttributes), filters the combined set through eval, and rebuilds
// every other span-start option unchanged before delegating to the wrapped
// Tracer.
func (t *tracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	cfg := trace.NewSpanStartConfig(opts...)

	combined := append(contextAttributes(ctx), cfg.Attributes()...)
	filtered := filterAttributes(t.eval, telemetry.SignalSpan, combined)

	rebuilt := make([]trace.SpanStartOption, 0, 6)
	rebuilt = append(rebuilt, trace.WithAttributes(filtered...))
	if !cfg.Timestamp().IsZero() {
		rebuilt = append(rebuilt, trace.WithTimestamp(cfg.Timestamp()))
	}
	if len(cfg.Links()) > 0 {
		rebuilt = append(rebuilt, trace.WithLinks(cfg.Links()...))
	}
	if cfg.NewRoot() {
		rebuilt = append(rebuilt, trace.WithNewRoot())
	}
	if cfg.SpanKind() != trace.SpanKindUnspecified {
		rebuilt = append(rebuilt, trace.WithSpanKind(cfg.SpanKind()))
	}
	// SpanConfig.StackTrace() has no span-start reapplication: WithStackTrace
	// is only a SpanEndEventOption (trace/config.go), so NewSpanStartConfig
	// could never have set it from opts in the first place.

	ctx, span := t.real.Start(ctx, spanName, rebuilt...)
	return ctx, &redactingSpan{Span: span, eval: t.eval}
}

// redactingSpan wraps a real trace.Span so SetAttributes and AddEvent filter
// through eval before mutating the wrapped span. Every other method is
// promoted unchanged from the embedded Span, including End, RecordError,
// SetStatus and SetName, none of which accept attribute-shaped free-form
// content this package needs to police (RecordError attaches an
// exception.* event through AddEvent's own options, which this wrapper
// still overrides — see RecordError below).
type redactingSpan struct {
	trace.Span
	eval *telemetry.Evaluator
}

// SetAttributes implements trace.Span.
func (s *redactingSpan) SetAttributes(kv ...attribute.KeyValue) {
	filtered := filterAttributes(s.eval, telemetry.SignalSpan, kv)
	if len(filtered) > 0 {
		s.Span.SetAttributes(filtered...)
	}
}

// AddEvent implements trace.Span, filtering the event's own attributes the
// same way span attributes are filtered.
func (s *redactingSpan) AddEvent(name string, opts ...trace.EventOption) {
	s.Span.AddEvent(name, s.filterEventOptions(opts)...)
}

// RecordError implements trace.Span, filtering the exception event's
// attributes the same way any other event's are filtered.
func (s *redactingSpan) RecordError(err error, opts ...trace.EventOption) {
	s.Span.RecordError(err, s.filterEventOptions(opts)...)
}

func (s *redactingSpan) filterEventOptions(opts []trace.EventOption) []trace.EventOption {
	cfg := trace.NewEventConfig(opts...)
	filtered := filterAttributes(s.eval, telemetry.SignalSpan, cfg.Attributes())

	rebuilt := make([]trace.EventOption, 0, 3)
	rebuilt = append(rebuilt, trace.WithAttributes(filtered...))
	if !cfg.Timestamp().IsZero() {
		rebuilt = append(rebuilt, trace.WithTimestamp(cfg.Timestamp()))
	}
	if cfg.StackTrace() {
		rebuilt = append(rebuilt, trace.WithStackTrace(true))
	}
	return rebuilt
}
