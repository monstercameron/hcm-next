// Package boundary owns the policy decision made when telemetry crosses a
// process or provider trust boundary. It deliberately returns owned values,
// rather than exposing an OpenTelemetry propagator or transport type.
package boundary

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	MaxTraceParentLen = 512
	MaxBaggageLen     = 2048
	MaxEntries        = 8
	MaxKeyLen         = 64
	MaxValueLen       = 256
)

var ErrRejected = errors.New("telemetry boundary: propagation rejected")

var forbiddenKeys = map[string]bool{
	"tenant": true, "tenant_id": true, "actor": true, "principal": true,
	"purpose": true, "authz": true, "authorization": true, "evidence": true,
}

// Trace is the operational parent context. It is never an authority or
// business identifier.
type Trace struct {
	TraceID string
	SpanID  string
	Sampled bool
}

type Baggage struct {
	Key   string
	Value string
}

type Decision struct {
	Trace          Trace
	Baggage        []Baggage
	FreshTrace     bool
	SecuritySignal string
}

// Policy is intentionally explicit: callers must name the destination before
// egress can carry any baggage. Empty AllowKeys is deny-all.
type Policy struct {
	AllowKeys map[string]map[string]bool // destination -> key set
}

func (p Policy) allowed(destination, key string) bool {
	return p.AllowKeys != nil && p.AllowKeys[destination][strings.ToLower(key)]
}

func ParseTraceParent(header string) (Trace, error) {
	if len(header) == 0 || len(header) > MaxTraceParentLen || !utf8.ValidString(header) {
		return Trace{}, ErrRejected
	}
	parts := strings.Split(header, "-")
	if len(parts) != 4 || parts[0] != "00" || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return Trace{}, ErrRejected
	}
	// W3C traceparent uses lowercase hexadecimal and only the sampled bit.
	if parts[1] == strings.Repeat("0", 32) || parts[2] == strings.Repeat("0", 16) || (parts[3][0] != '0' && parts[3][0] != '1') {
		return Trace{}, ErrRejected
	}
	if _, err := hex.DecodeString(parts[1] + parts[2] + parts[3]); err != nil || strings.ToLower(parts[1]+parts[2]+parts[3]) != parts[1]+parts[2]+parts[3] {
		return Trace{}, ErrRejected
	}
	return Trace{TraceID: parts[1], SpanID: parts[2], Sampled: parts[3][1] == '1'}, nil
}

func ParseBaggage(header string) ([]Baggage, error) {
	if len(header) > MaxBaggageLen || !utf8.ValidString(header) {
		return nil, ErrRejected
	}
	if strings.TrimSpace(header) == "" {
		return nil, nil
	}
	parts := strings.Split(header, ",")
	if len(parts) > MaxEntries {
		return nil, ErrRejected
	}
	out := make([]Baggage, 0, len(parts))
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			return nil, ErrRejected
		}
		key, value := strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
		if key == "" || len(key) > MaxKeyLen || len(value) > MaxValueLen || !utf8.ValidString(key) || !utf8.ValidString(value) || strings.ContainsAny(key, " ,\t\r\n") {
			return nil, ErrRejected
		}
		if forbiddenKeys[strings.ToLower(key)] {
			return nil, ErrRejected
		}
		out = append(out, Baggage{Key: key, Value: value})
	}
	return out, nil
}

// Inbound validates trace context and accepts only the destination-neutral
// reviewed keys. Invalid input yields a fresh-trace decision and safe signal;
// it does not turn a telemetry defect into a business failure.
func (p Policy) Inbound(traceHeader, baggageHeader string) Decision {
	trace, traceErr := ParseTraceParent(traceHeader)
	bag, bagErr := ParseBaggage(baggageHeader)
	if traceErr != nil || bagErr != nil {
		return Decision{FreshTrace: true, SecuritySignal: signal(traceErr, bagErr)}
	}
	return Decision{Trace: trace, Baggage: p.Filter("inbound", bag)}
}

func signal(traceErr, bagErr error) string {
	if traceErr != nil && bagErr != nil {
		return "invalid_trace_context_and_baggage"
	}
	if traceErr != nil {
		return "invalid_trace_context"
	}
	return "invalid_baggage"
}

// Filter applies the reviewed destination allowlist and bounds output. It
// never mutates its input and silently drops unreviewed baggage.
func (p Policy) Filter(destination string, entries []Baggage) []Baggage {
	out := make([]Baggage, 0, len(entries))
	for _, e := range entries {
		// Filter is also the egress trust boundary. Do not rely on ParseBaggage
		// having run: callers may have assembled entries in memory, and
		// authority-bearing or sensitive keys must never cross even when a
		// destination allowlist is accidentally over-broad.
		if !isForbiddenKey(e.Key) && p.allowed(destination, e.Key) && len(e.Key) <= MaxKeyLen && len(e.Value) <= MaxValueLen && utf8.ValidString(e.Key) && utf8.ValidString(e.Value) {
			out = append(out, e)
			if len(out) == MaxEntries {
				break
			}
		}
	}
	return out
}

func isForbiddenKey(key string) bool {
	lower := strings.ToLower(key)
	if forbiddenKeys[lower] {
		return true
	}
	// Keep the policy conservative for common aliases and classified fields;
	// baggage is operational context, not a transport for identity or data.
	for _, marker := range []string{"authorization", "authz", "identity", "principal", "tenant", "actor", "purpose", "evidence", "email", "name", "personal"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (p Policy) Egress(destination string, entries []Baggage) string {
	filtered := p.Filter(destination, entries)
	parts := make([]string, 0, len(filtered))
	for _, e := range filtered {
		parts = append(parts, e.Key+"="+e.Value)
	}
	return strings.Join(parts, ",")
}

func FormatTraceParent(t Trace) (string, error) {
	if _, err := ParseTraceParent(fmt.Sprintf("00-%s-%s-%02x", t.TraceID, t.SpanID, boolByte(t.Sampled))); err != nil {
		return "", err
	}
	return fmt.Sprintf("00-%s-%s-%02x", t.TraceID, t.SpanID, boolByte(t.Sampled)), nil
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}
