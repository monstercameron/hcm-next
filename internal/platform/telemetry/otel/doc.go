// Package otel implements planning/todos.md OBS-002: the Go OpenTelemetry
// adapter that sits behind internal/platform/telemetry's owned contracts,
// plus the Collector pipeline configuration those adapters export to
// (definitions/telemetry/collector/otel-collector.yaml).
//
// This package is the only place in the module that imports
// go.opentelemetry.io/otel and its SDK/exporter siblings
// (internal/platform/telemetry's own doc.go: "OBS-002 ... owns the
// OpenTelemetry adapter that will sit behind these contracts"). It never
// exposes go.opentelemetry.io types as the *only* way to use it for the
// signals internal/platform/telemetry already has typed, catalog-owned
// shapes for (Metrics' Record* methods), so a caller composing spans/metrics
// does not have to import OTel merely to stay within policy. Tracing is the
// one signal this package leaves open-ended (trace topology names dozens of
// span families — structured-logging-and-opentelemetry.md "Trace topology" —
// so a closed, typed API is not practical the way it is for the five-metric
// P1A catalog); Provider.Tracer returns a policy-filtering trace.Tracer so a
// caller can still name arbitrary spans/attributes without bypassing
// redaction.
//
// # Policy enforcement
//
// Every attribute a caller attempts to attach to a span (at Start, via
// SetAttributes, or via AddEvent) and every label a caller attempts to
// attach to a metric observation is evaluated against the frozen
// internal/platform/telemetry.Evaluator (OBS-004's classify -> cardinality
// -> export policy) before it ever reaches an SDK span/metric that a real
// exporter can read. A key the Evaluator does not keep is dropped silently
// at the wrapper boundary; the underlying go.opentelemetry.io/otel/sdk
// objects — and therefore every exporter, including the in-memory ones the
// test suite uses — never observe it. This is deliberately not implemented
// as an sdktrace.SpanProcessor: a SpanProcessor's OnEnd hook only receives a
// ReadOnlySpan (no attribute removal API), so filtering has to happen at the
// Tracer/Span decorator boundary, before an attribute is ever written to the
// real SDK span.
//
// Because internal/platform/telemetry's own attribute allow-list
// (definitions/telemetry/resource-contract.yaml) registers correlation_id
// for SignalSpan but does not register request_id, evidence_ref or
// principal_ref as attribute keys (they are context-propagation bounds, not
// allow-listed attributes — see internal/platform/telemetry/context.go and
// resource.go's Validate), Provider's automatic context-to-span-attribute
// propagation (context_propagation.go) attempts all four and lets the same
// Evaluator decide: correlation_id currently survives, the other three are
// redacted by the frozen policy exactly as they would be for any other
// caller-supplied attribute. Widening that requires an OBS-001/004 contract
// change, not a change here.
//
// # Metrics
//
// NewMetrics registers exactly the five instruments
// internal/platform/telemetry.MetricCatalog() declares — nothing else can be
// created through Metrics — so the completeness checker
// (internal/platform/telemetry.Check) can certify HEALTHY against precisely
// what this package emits (via Metrics.EmittedMetricNames, which implements
// telemetry.Sink's metric half). It does not implement the log-event half of
// completeness: OBS-010's event-name registry does not exist yet, so
// EmittedLogEventNames always returns nil; a caller checking full
// completeness composes this Sink with a log-side one once OBS-010 lands.
//
// # Exporters
//
// TraceConfig and MetricConfig each support "stdout" and "otlp" exporter
// kinds, or an explicit Exporter/Reader override for tests. The metric
// stdout exporter is hand-written (metric_exporter.go) against
// go.opentelemetry.io/otel/sdk/metric's own Exporter interface rather than
// importing go.opentelemetry.io/otel/exporters/stdout/stdoutmetric, which is
// not one of this repository's pinned OTel dependencies.
//
// # Out of scope
//
// gRPC/connect transport interceptors are explicitly out of this package's
// scope. The hook a transport composition root would add: a
// connect.UnaryInterceptorFunc (and its streaming equivalent) in
// internal/transport/edge that, per inbound call, (1) reads/derives
// request_id, correlation_id and evidence ids from inbound metadata and
// attaches them via internal/platform/logging's context setters, (2) calls
// Provider.Tracer(<service>).Start(ctx, <procedure>) before invoking the
// handler and Span.End() after, setting SetStatus on error, and (3) calls
// the matching Metrics.Record* method for the capability/procedure's
// outcome. That composition root is the only place allowed to import both
// this package and the transport layer; internal/platform/telemetry/otel
// itself never imports connect/grpc.
package otel
