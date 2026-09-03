package execute

import "context"

// OBS-023 outcome vocabulary. These are plain strings, not
// internal/platform/telemetry's own Outcome type: this package must not
// import an exporter or a telemetry backend (REFACTOR: "no engine package
// imports an exporter"), so it owns the smallest vocabulary its own callers
// need and leaves classification onto the structured-logging spec's
// Outcome set to the Instrumentation implementation.
const (
	OutcomeSuccess = "SUCCESS"
	OutcomeFailure = "FAILURE"
	OutcomeParked  = "PARKED"
	// OutcomeDenied reports a governed refusal that is not a technical
	// failure — e.g. a currency check blocking an advancement (WF-RUN-029).
	OutcomeDenied = "DENIED"
)

// SpanAttributes is the bounded, non-sensitive attribute set OBS-023 allows
// on a node/advancement/terminal-write span or log line: instance, node,
// attempt and terminal code. It never carries a payload, a proposal digest,
// an approver identity or authority-bearing baggage
// (structured-logging-and-opentelemetry.md "Scope"; OBS-022's baggage
// allowlist rule applies identically here even though this package does
// not itself touch baggage).
type SpanAttributes struct {
	InstanceID   string
	NodeID       string
	Attempt      int
	TerminalCode string
}

// Span is one open span/log bracket around a node run, an advancement or a
// terminal write. End closes it, records the typed outcome and (for the
// advancement and terminal-write kinds) is also where the one required
// log/slog envelope line for that operation is emitted.
type Span interface {
	// End closes the span with outcome (one of the Outcome* constants, or a
	// caller's own bounded string). err, when non-nil, marks the span and
	// log line as failed; only its presence is recorded — an
	// Instrumentation implementation must classify/redact err.Error()
	// itself before it may appear on any exported signal, and the shipped
	// implementations never emit it verbatim.
	End(outcome string, err error)
}

// Instrumentation is OBS-023's port: the driver never opens a raw
// OpenTelemetry span or writes a raw log/slog record itself, so this
// package's own compilation never depends on an exporter or a backend SDK.
//
// [NoopInstrumentation] is the default a [Driver] uses when
// [Options.Instrumentation] is nil, which is also what every existing test
// and composition that predates OBS-023 keeps running under unchanged.
type Instrumentation interface {
	// TraceID reports the W3C trace id already ambient on ctx — whatever
	// the caller's transport (or a prior span this package itself opened)
	// established — or "" when ctx carries none. This is the exact value
	// the driver stamps onto StepRequest.TraceID and
	// runtime.AdvanceRequest.TraceID so a stored node execution can be
	// pivoted back to the trace and log line that produced it.
	TraceID(ctx context.Context) string

	// StartNodeSpan opens the span for one READY node's synchronous run.
	StartNodeSpan(ctx context.Context, attrs SpanAttributes) (context.Context, Span)
	// StartAdvanceSpan opens the span for one runtime.Advance call. Its
	// Span.End is also where the one required per-advancement log/slog
	// envelope line is emitted.
	StartAdvanceSpan(ctx context.Context, attrs SpanAttributes) (context.Context, Span)
	// StartTerminalSpan opens the span for one governed terminal write.
	// Its Span.End is also where the one required per-terminal-write
	// log/slog envelope line is emitted.
	StartTerminalSpan(ctx context.Context, attrs SpanAttributes) (context.Context, Span)
}

// NoopInstrumentation is the zero-cost [Instrumentation] a [Driver] uses
// when none is configured: every method is a true no-op, so a caller that
// ignores telemetry entirely still runs exactly as it did before OBS-023.
type NoopInstrumentation struct{}

var _ Instrumentation = NoopInstrumentation{}

// TraceID implements Instrumentation.
func (NoopInstrumentation) TraceID(context.Context) string { return "" }

// StartNodeSpan implements Instrumentation.
func (NoopInstrumentation) StartNodeSpan(ctx context.Context, _ SpanAttributes) (context.Context, Span) {
	return ctx, noopSpan{}
}

// StartAdvanceSpan implements Instrumentation.
func (NoopInstrumentation) StartAdvanceSpan(ctx context.Context, _ SpanAttributes) (context.Context, Span) {
	return ctx, noopSpan{}
}

// StartTerminalSpan implements Instrumentation.
func (NoopInstrumentation) StartTerminalSpan(ctx context.Context, _ SpanAttributes) (context.Context, Span) {
	return ctx, noopSpan{}
}

type noopSpan struct{}

func (noopSpan) End(string, error) {}

var _ Span = noopSpan{}
