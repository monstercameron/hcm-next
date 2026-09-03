package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
)

const MaxTraceHeaderLen = 512
const MaxBaggageHeaderLen = 2048

var forbiddenBaggageKeys = map[string]bool{
	"tenant": true, "tenant_id": true, "actor": true, "principal": true,
	"purpose": true, "authz": true, "evidence": true, "authorization": true,
}

type TraceContext struct {
	TraceID string
	SpanID  string
	Sampled bool
	Valid   bool
}

type BaggageEntry struct {
	Key   string
	Value string
}

type PropagationResult struct {
	Trace          TraceContext
	Baggage        []BaggageEntry
	FreshTrace     bool
	SecuritySignal string
}

func ParseTraceParent(header string) (TraceContext, bool) {
	if len(header) == 0 || len(header) > MaxTraceHeaderLen {
		return TraceContext{}, false
	}
	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return TraceContext{}, false
	}
	if parts[0] != "00" {
		return TraceContext{}, false
	}
	if len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return TraceContext{}, false
	}
	for _, c := range parts[1] + parts[2] + parts[3] {
		if !isHex(byte(c)) {
			return TraceContext{}, false
		}
	}
	if isAllZero(parts[1]) || isAllZero(parts[2]) {
		return TraceContext{}, false
	}
	sampled := parts[3][1] == '1'
	return TraceContext{TraceID: parts[1], SpanID: parts[2], Sampled: sampled, Valid: true}, true
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isAllZero(s string) bool {
	for _, c := range s {
		if c != '0' {
			return false
		}
	}
	return true
}

func newTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func newSpanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func FreshTraceContext() TraceContext {
	return TraceContext{TraceID: newTraceID(), SpanID: newSpanID(), Valid: true}
}

func ParseBaggage(header string) ([]BaggageEntry, bool) {
	if len(header) > MaxBaggageHeaderLen {
		return nil, false
	}
	if strings.TrimSpace(header) == "" {
		return nil, true
	}
	parts := strings.Split(header, ",")
	if len(parts) > MaxBaggageEntries {
		return nil, false
	}
	var out []BaggageEntry
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			return nil, false
		}
		k := strings.TrimSpace(kv[0])
		v := strings.TrimSpace(kv[1])
		if len(k) == 0 || len(k) > MaxBaggageKeyLen || len(v) > MaxBaggageValueLen {
			return nil, false
		}
		if forbiddenBaggageKeys[strings.ToLower(k)] {
			return nil, false
		}
		if IsSensitiveBaggageKey(k) || IsAuthorityBearingBaggageKey(k) {
			return nil, false
		}
		if !IsAllowedBaggageKey(k) {
			continue
		}
		out = append(out, BaggageEntry{Key: k, Value: v})
	}
	return out, true
}

func FilterBaggageForEgress(entries []BaggageEntry) []BaggageEntry {
	var out []BaggageEntry
	for _, e := range entries {
		if IsAllowedBaggageKey(e.Key) && !IsSensitiveBaggageKey(e.Key) && !IsAuthorityBearingBaggageKey(e.Key) {
			out = append(out, e)
		}
	}
	return out
}

func ExtractPropagation(traceHeader, baggageHeader string) PropagationResult {
	tc, ok := ParseTraceParent(traceHeader)
	if !ok {
		return PropagationResult{Trace: FreshTraceContext(), FreshTrace: true, SecuritySignal: "invalid_trace_context"}
	}
	bag, ok := ParseBaggage(baggageHeader)
	if !ok {
		return PropagationResult{Trace: FreshTraceContext(), FreshTrace: true, SecuritySignal: "invalid_baggage"}
	}
	filtered := FilterBaggageForEgress(bag)
	return PropagationResult{Trace: tc, Baggage: filtered}
}

func InjectTraceParent(tc TraceContext) string {
	flag := "00"
	if tc.Sampled {
		flag = "01"
	}
	return "00-" + tc.TraceID + "-" + tc.SpanID + "-" + flag
}

func InjectBaggage(entries []BaggageEntry) string {
	filtered := FilterBaggageForEgress(entries)
	parts := make([]string, 0, len(filtered))
	for _, e := range filtered {
		parts = append(parts, e.Key+"="+e.Value)
	}
	return strings.Join(parts, ",")
}
