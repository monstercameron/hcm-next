package telemetry

import (
	"strings"
)

const (
	BaggageHeaderW3C         = "baggage"
	MaxBaggageEntries        = 8
	MaxBaggageKeyLen         = 64
	MaxBaggageValueLen       = 256
	SecuritySignalCode       = "OBS_022_REJECTED"
	SecuritySignalReasonLeak = "sensitive_baggage_stripped"
	SecuritySignalReasonCard = "cardinality_overflow_stripped"
)

var allowedBaggageKeys = map[string]bool{
	"correlation_id": true,
	"request_id":     true,
}

var thirdPartySafeBaggageKeys = map[string]bool{
	"correlation_id": true,
}

var sensitiveBaggageSubstrings = []string{
	"worker",
	"person",
	"email",
	"medical",
	"pay",
	"bank",
	"salary",
	"compensation",
	"case",
	"prompt",
	"payload",
	"free_text",
	"free-text",
	"ssn",
	"diagnosis",
	"routing",
	"attacker",
}

var authorityBearingSubstrings = []string{
	"authz",
	"authorization",
	"tenant",
	"routing",
	"principal",
	"session",
	"role",
	"scope",
	"permission",
}

func IsSensitiveBaggageKey(key string) bool {
	lk := strings.ToLower(key)
	for _, sub := range sensitiveBaggageSubstrings {
		if strings.Contains(lk, sub) {
			return true
		}
	}
	return false
}

func IsAuthorityBearingBaggageKey(key string) bool {
	lk := strings.ToLower(key)
	for _, sub := range authorityBearingSubstrings {
		if strings.Contains(lk, sub) {
			return true
		}
	}
	return false
}

func IsAllowedBaggageKey(key string) bool {
	return allowedBaggageKeys[strings.ToLower(key)]
}

func IsThirdPartySafeBaggageKey(key string) bool {
	return thirdPartySafeBaggageKeys[strings.ToLower(key)]
}

type BaggageSanitizer struct {
	Allow *Allowlist
	Eval  *Evaluator
}

type SanitizedBaggage struct {
	Kept     map[string]string
	Stripped int
	Signals  []SecuritySignal
}

type SecuritySignal struct {
	Code    string
	Reason  string
	Bounded bool
}

func NewBaggageSanitizer(allow *Allowlist, eval *Evaluator) *BaggageSanitizer {
	return &BaggageSanitizer{Allow: allow, Eval: eval}
}

func (s *BaggageSanitizer) Sanitize(baggage map[string]string) SanitizedBaggage {
	kept := make(map[string]string)
	stripped := 0
	var signals []SecuritySignal
	for k, v := range baggage {
		if len(k) == 0 || len(k) > MaxBaggageKeyLen || len(v) > MaxBaggageValueLen {
			stripped++
			signals = append(signals, SecuritySignal{Code: SecuritySignalCode, Reason: SecuritySignalReasonLeak, Bounded: true})
			continue
		}
		if IsSensitiveBaggageKey(k) || IsAuthorityBearingBaggageKey(k) {
			stripped++
			signals = append(signals, SecuritySignal{Code: SecuritySignalCode, Reason: SecuritySignalReasonLeak, Bounded: true})
			continue
		}
		if !IsAllowedBaggageKey(k) {
			stripped++
			signals = append(signals, SecuritySignal{Code: SecuritySignalCode, Reason: SecuritySignalReasonLeak, Bounded: true})
			continue
		}
		if len(kept) >= MaxBaggageEntries {
			stripped++
			signals = append(signals, SecuritySignal{Code: SecuritySignalCode, Reason: SecuritySignalReasonCard, Bounded: true})
			continue
		}
		kept[k] = v
	}
	return SanitizedBaggage{Kept: kept, Stripped: stripped, Signals: signals}
}

func (s *BaggageSanitizer) SanitizeForThirdParty(baggage map[string]string) SanitizedBaggage {
	kept := make(map[string]string)
	stripped := 0
	var signals []SecuritySignal
	for k, v := range baggage {
		if !IsThirdPartySafeBaggageKey(k) {
			stripped++
			continue
		}
		if len(k) > MaxBaggageKeyLen || len(v) > MaxBaggageValueLen {
			stripped++
			signals = append(signals, SecuritySignal{Code: SecuritySignalCode, Reason: SecuritySignalReasonLeak, Bounded: true})
			continue
		}
		if len(kept) >= MaxBaggageEntries {
			stripped++
			signals = append(signals, SecuritySignal{Code: SecuritySignalCode, Reason: SecuritySignalReasonCard, Bounded: true})
			continue
		}
		kept[k] = v
	}
	return SanitizedBaggage{Kept: kept, Stripped: stripped, Signals: signals}
}

func (s *BaggageSanitizer) StripHeadersForThirdParty(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		lk := strings.ToLower(k)
		if lk == BaggageHeaderW3C || lk == "x-baggage" || strings.HasPrefix(lk, "baggage") {
			continue
		}
		out[k] = v
	}
	return out
}

func (s *BaggageSanitizer) CollectorRedact(attrs map[string]string, kind SignalKind) map[string]string {
	if s.Eval == nil {
		return map[string]string{}
	}
	out := make(map[string]string)
	for k, v := range attrs {
		d := s.Eval.EvaluateAttribute(kind, k, v)
		if d.Kept {
			out[k] = d.Value
		}
	}
	return out
}

func (s *BaggageSanitizer) EvaluateCardinalitySafe(key, value string) (string, bool) {
	if s.Eval == nil || s.Eval.Cardinality == nil {
		return "__overflow__", false
	}
	capped := s.Eval.Cardinality.Cap(key, value)
	return capped, capped != "__overflow__"
}
