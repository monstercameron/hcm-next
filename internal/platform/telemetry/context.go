package telemetry

import "context"

// Bounds on the context-propagation values (OBS-001: "Context propagation
// keys (request/correlation/evidence/principal-reference)"). These are
// deliberately small: a trust boundary must never be able to smuggle an
// oversized or structured payload through what is supposed to be an opaque
// token (OBS-011 governs the wire-level version of this concern for trace
// baggage; this package enforces the same shape for its own in-process
// context values).
const (
	MaxCorrelationIDLen = 128
	MaxRequestIDLen     = 128
	MaxEvidenceRefLen   = 256
	MaxPrincipalRefLen  = 128
)

type ctxKey int

const (
	ctxKeyCorrelationID ctxKey = iota
	ctxKeyRequestID
	ctxKeyEvidenceRef
	ctxKeyPrincipalRef
)

// WithCorrelationID attaches the owned business correlation identifier that
// may span several requests or a long-running workflow
// (structured-logging-and-opentelemetry.md "Propagation": "Business
// correlation_id ... may correlate several traces across a long-running
// workflow"). It is opaque application state, never parsed from an inbound
// trace header by this package.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyCorrelationID, id)
}

// CorrelationID returns the correlation identifier attached to ctx, if any.
func CorrelationID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyCorrelationID).(string)
	return v, ok && v != ""
}

// WithRequestID attaches a per-call request identifier.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKeyRequestID, id)
}

// RequestID returns the request identifier attached to ctx, if any.
func RequestID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyRequestID).(string)
	return v, ok && v != ""
}

// WithEvidenceRef attaches a reference into an evidence store — never the
// evidence payload itself.
func WithEvidenceRef(ctx context.Context, ref string) context.Context {
	return context.WithValue(ctx, ctxKeyEvidenceRef, ref)
}

// EvidenceRef returns the evidence reference attached to ctx, if any.
func EvidenceRef(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyEvidenceRef).(string)
	return v, ok && v != ""
}

// WithPrincipalRef attaches a bounded, non-reversible reference to the
// acting principal — never the principal's name, email or other directly
// identifying attribute.
func WithPrincipalRef(ctx context.Context, ref string) context.Context {
	return context.WithValue(ctx, ctxKeyPrincipalRef, ref)
}

// PrincipalRef returns the principal reference attached to ctx, if any.
func PrincipalRef(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyPrincipalRef).(string)
	return v, ok && v != ""
}
