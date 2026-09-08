package correlation

import (
	"context"
	"errors"
	"strings"
)

// Validation bounds mirror the sibling telemetry authorities:
// identifiers fit in 128 bytes (telemetry.maxContinuationIDLen,
// lifecycle.invalidToken) and tenant tokens additionally admit no
// whitespace.
const maxIdentifierLen = 128

var (
	// ErrCorrelation rejects a Correlation whose owned identity or trace
	// linkage is missing, oversized or malformed.
	ErrCorrelation = errors.New("telemetry correlation: invalid correlation")
	// ErrJoinScope rejects one signal join whose scope names no purpose,
	// names a tenant other than the correlation's own, or is otherwise
	// not an exact tenant/purpose authorization for this join.
	ErrJoinScope = errors.New("telemetry correlation: join is not authorized for this scope")
	// ErrMetricLabel rejects an unbounded-cardinality or
	// authority-bearing key as a metric label: owned correlation, intent
	// and tenant identity plus raw trace linkage must never become label
	// dimensions.
	ErrMetricLabel = errors.New("telemetry correlation: key must not be a metric label")
)

// Correlation joins one unit of diagnosable work across signals. The
// owned fields (IntentID, CorrelationID, TenantToken) are stable business
// causation; the trace fields name one span for diagnosis. The zero value
// is invalid: build one with New.
type Correlation struct {
	IntentID      string
	CorrelationID string
	TenantToken   string
	TraceID       string
	SpanID        string
	Sampled       bool
}

// New builds the owned identity of one correlation. Trace linkage is
// deliberately absent: attach it with WithTrace once a span exists, so a
// correlation never inherits trace identity as its business key.
func New(intentID, correlationID, tenantToken string) (Correlation, error) {
	c := Correlation{IntentID: intentID, CorrelationID: correlationID, TenantToken: tenantToken}
	if err := c.Validate(); err != nil {
		return Correlation{}, err
	}
	return c, nil
}

// WithTrace rebinds the telemetry linkage to a new span and reports the
// updated correlation. Owned identity is untouched: a long-running intent
// spans many traces while its business causation — and its idempotency
// key — stay stable, including across sampled-out traces.
func (c Correlation) WithTrace(traceID, spanID string, sampled bool) (Correlation, error) {
	c.TraceID = traceID
	c.SpanID = spanID
	c.Sampled = sampled
	if err := c.Validate(); err != nil {
		return Correlation{}, err
	}
	return c, nil
}

// Validate rejects a blank or oversized owned field, a whitespace-bearing
// tenant token, and malformed trace linkage. Trace linkage is optional
// (a correlation exists before its first span), but when present both IDs
// must be well-formed nonzero hex.
func (c Correlation) Validate() error {
	if strings.TrimSpace(c.IntentID) == "" || len(c.IntentID) > maxIdentifierLen {
		return ErrCorrelation
	}
	if strings.TrimSpace(c.CorrelationID) == "" || len(c.CorrelationID) > maxIdentifierLen {
		return ErrCorrelation
	}
	if strings.TrimSpace(c.TenantToken) == "" || len(c.TenantToken) > maxIdentifierLen ||
		strings.ContainsAny(c.TenantToken, " \t\r\n") {
		return ErrCorrelation
	}
	if c.TraceID == "" && c.SpanID == "" && !c.Sampled {
		return nil
	}
	if !isHexID(c.TraceID, 32) || !isHexID(c.SpanID, 16) {
		return ErrCorrelation
	}
	return nil
}

func isHexID(s string, hexLen int) bool {
	if len(s) != hexLen {
		return false
	}
	nonzero := false
	for i := 0; i < len(s); i++ {
		b := s[i]
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') && (b < 'A' || b > 'F') {
			return false
		}
		if b != '0' {
			nonzero = true
		}
	}
	return nonzero
}

// IdempotencyKey returns the business authority for this correlation,
// derived from owned identity only. Rebinding trace linkage never changes
// it; changing owned identity always does.
func (c Correlation) IdempotencyKey() string {
	return c.IntentID + ":" + c.CorrelationID
}

// LogField is one permitted log field of a joined correlation: the owned
// correlation plus the active trace/span linkage and its sampling flag.
type LogField struct {
	Key   string
	Value string
}

// LogFields renders the correlation for structured logs in a fixed,
// pinned order: owned correlation first, then the active trace linkage
// and whether that trace was sampled. The tenant token is not a log
// field: authorization material does not spread into log streams.
func (c Correlation) LogFields() []LogField {
	sampled := "false"
	if c.Sampled {
		sampled = "true"
	}
	return []LogField{
		{Key: "correlation_id", Value: c.CorrelationID},
		{Key: "intent_id", Value: c.IntentID},
		{Key: "span_id", Value: c.SpanID},
		{Key: "trace_id", Value: c.TraceID},
		{Key: "trace_sampled", Value: sampled},
	}
}

// forbiddenMetricLabels are the correlation and trace identities that
// must never become metric label dimensions: unbounded cardinality per
// series, and authority-bearing values in a billing/aggregation path.
var forbiddenMetricLabels = map[string]bool{
	"correlation_id": true,
	"intent_id":      true,
	"tenant_token":   true,
	"trace_id":       true,
	"span_id":        true,
}

// CheckMetricLabel rejects an owned or trace identity as a metric label.
// Bounded catalog keys (cell_id, tenant_class, outcome, edge) pass: the
// catalog itself owns which of those exist.
func CheckMetricLabel(key string) error {
	if strings.TrimSpace(key) == "" || forbiddenMetricLabels[key] {
		return ErrMetricLabel
	}
	return nil
}

// JoinScope authorizes exactly one signal join: the tenant performing the
// join and the purpose it joins for.
type JoinScope struct {
	TenantToken string
	Purpose     string
}

// JoinTicket binds one authorized join to its scope and the joined
// owned correlation.
type JoinTicket struct {
	TenantToken   string
	Purpose       string
	CorrelationID string
	IntentID      string
}

// Join authorizes one signal join under the given scope. The scope must
// name the correlation's own tenant exactly (no trimming, no padding)
// and a nonempty purpose; anything else — including a scope for another
// tenant riding a shared correlation ID — is rejected. Each call decides
// independently: tickets are not cached and a previous verdict never
// authorizes a later join.
func (c Correlation) Join(scope JoinScope) (JoinTicket, error) {
	if err := c.Validate(); err != nil {
		return JoinTicket{}, err
	}
	if scope.TenantToken == "" || scope.TenantToken != c.TenantToken {
		return JoinTicket{}, ErrJoinScope
	}
	if strings.TrimSpace(scope.Purpose) == "" {
		return JoinTicket{}, ErrJoinScope
	}
	return JoinTicket{
		TenantToken:   scope.TenantToken,
		Purpose:       scope.Purpose,
		CorrelationID: c.CorrelationID,
		IntentID:      c.IntentID,
	}, nil
}

type contextKey struct{}

// WithCorrelation carries a Correlation in the context through typed
// helpers instead of manual propagation of raw identifiers.
func WithCorrelation(ctx context.Context, c Correlation) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// FromContext retrieves the Correlation a typed helper stored, if any.
func FromContext(ctx context.Context) (Correlation, bool) {
	c, ok := ctx.Value(contextKey{}).(Correlation)
	return c, ok
}
