package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// EnvelopeSchemaVersion tags the signal-neutral envelope encoding.
const EnvelopeSchemaVersion = 1

// Outcome is the typed result of the operation a telemetry signal
// describes (structured-logging-and-opentelemetry.md "Severity and
// outcome"). A retry attempt and its logical operation carry different
// records; neither is expressed by overloading a free-text message.
type Outcome string

// Published outcomes.
const (
	OutcomeUnspecified Outcome = ""
	OutcomeSuccess     Outcome = "SUCCESS"
	OutcomeFailure     Outcome = "FAILURE"
	OutcomePartial     Outcome = "PARTIAL"
	OutcomeUnknown     Outcome = "UNKNOWN"
	OutcomeDenied      Outcome = "DENIED"
	OutcomeCancelled   Outcome = "CANCELLED"
	OutcomeDegraded    Outcome = "DEGRADED"
)

// Valid reports whether o is a published, non-empty outcome.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomeSuccess, OutcomeFailure, OutcomePartial, OutcomeUnknown, OutcomeDenied, OutcomeCancelled, OutcomeDegraded:
		return true
	default:
		return false
	}
}

// Envelope validation errors.
var (
	ErrEnvelopeCorrelationID = errors.New("telemetry: envelope has no correlation id")
	ErrEnvelopeOutcome       = errors.New("telemetry: envelope outcome is not published")
	ErrEnvelopePolicyVersion = errors.New("telemetry: envelope policy version is missing")
	ErrAttributeValueTooLong = errors.New("telemetry: attribute value exceeds the bounded length")
)

// maxAttributeValueLen bounds every attribute value regardless of class:
// telemetry is diagnostic evidence, not an arbitrary payload channel
// (structured-logging-and-opentelemetry.md "Scope").
const maxAttributeValueLen = 4096

// Envelope is the one typed, signal-neutral wrapper every log, span and
// metric observation is built through (OBS-001 GREEN). It never carries a
// backend (OTel, Prometheus, ...) type; those adapters are OBS-002's job,
// entirely outside this package.
type Envelope struct {
	SchemaVersion int
	Resource      Resource
	CorrelationID string
	RequestID     string
	EvidenceRef   string
	PrincipalRef  string
	Outcome       Outcome
	PolicyVersion int
	// Attributes holds only keys that passed Allowlist classification; a
	// caller cannot bypass classification by writing directly to this map
	// because BuildEnvelope is the only constructor that populates it.
	Attributes map[string]string
}

// BuildEnvelope validates the resource, the context-propagation values
// carried on ctx, the outcome, the policy version and every attribute
// against allow, and returns the first field-level RejectionError before
// any of it reaches a handler or exporter (OBS-001 GREEN). Attributes is
// never mutated; the returned Envelope holds its own copy.
func BuildEnvelope(ctx context.Context, res Resource, outcome Outcome, policyVersion int, attrs map[string]string, allow *Allowlist) (Envelope, error) {
	if err := res.Validate(); err != nil {
		return Envelope{}, err
	}
	correlationID, ok := CorrelationID(ctx)
	if !ok {
		return Envelope{}, newRejection("OBS_001_REJECTED", "context.correlation_id", "missing", EnvelopeSchemaVersion, ErrEnvelopeCorrelationID)
	}
	if len(correlationID) > MaxCorrelationIDLen {
		return Envelope{}, newRejection("OBS_001_REJECTED", "context.correlation_id", "too_long", EnvelopeSchemaVersion, ErrEnvelopeCorrelationID)
	}
	if !outcome.Valid() {
		return Envelope{}, newRejection("OBS_001_REJECTED", "envelope.outcome", "missing_or_unknown", EnvelopeSchemaVersion, ErrEnvelopeOutcome)
	}
	if policyVersion <= 0 {
		return Envelope{}, newRejection("OBS_001_REJECTED", "envelope.policy_version", "missing", EnvelopeSchemaVersion, ErrEnvelopePolicyVersion)
	}

	requestID, _ := RequestID(ctx)
	if len(requestID) > MaxRequestIDLen {
		return Envelope{}, newRejection("OBS_001_REJECTED", "context.request_id", "too_long", EnvelopeSchemaVersion, fmt.Errorf("telemetry: request id too long"))
	}
	evidenceRef, _ := EvidenceRef(ctx)
	if len(evidenceRef) > MaxEvidenceRefLen {
		return Envelope{}, newRejection("OBS_001_REJECTED", "context.evidence_ref", "too_long", EnvelopeSchemaVersion, fmt.Errorf("telemetry: evidence ref too long"))
	}
	principalRef, _ := PrincipalRef(ctx)
	if len(principalRef) > MaxPrincipalRefLen {
		return Envelope{}, newRejection("OBS_001_REJECTED", "context.principal_ref", "too_long", EnvelopeSchemaVersion, fmt.Errorf("telemetry: principal ref too long"))
	}

	// Deterministic key order so a rejection is reproducible regardless of
	// Go's randomized map iteration.
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make(map[string]string, len(attrs))
	for _, k := range keys {
		v := attrs[k]
		class := allow.Classify(k)
		if class != ClassOperationalPublic && class != ClassOperationalRestricted {
			return Envelope{}, newRejection("OBS_001_REJECTED", "attributes."+k, "prohibited_or_unknown_key", EnvelopeSchemaVersion, ErrAttributeProhibited(k))
		}
		if !allow.AllowsSignal(k, SignalLog) && !allow.AllowsSignal(k, SignalSpan) && !allow.AllowsSignal(k, SignalMetric) {
			return Envelope{}, newRejection("OBS_001_REJECTED", "attributes."+k, "signal_not_allowed", EnvelopeSchemaVersion, ErrAttributeProhibited(k))
		}
		if len(v) > maxAttributeValueLen {
			return Envelope{}, newRejection("OBS_001_REJECTED", "attributes."+k, "value_too_long", EnvelopeSchemaVersion, ErrAttributeValueTooLong)
		}
		out[k] = v
	}

	return Envelope{
		SchemaVersion: EnvelopeSchemaVersion,
		Resource:      res,
		CorrelationID: correlationID,
		RequestID:     requestID,
		EvidenceRef:   evidenceRef,
		PrincipalRef:  principalRef,
		Outcome:       outcome,
		PolicyVersion: policyVersion,
		Attributes:    out,
	}, nil
}

// attributeProhibitedErr is a sentinel-shaped error family: one instance
// per key so errors.Is can still distinguish which key failed when a test
// wants to, while every instance also unwraps to the shared class.
type attributeProhibitedErr struct{ key string }

func (e attributeProhibitedErr) Error() string {
	return fmt.Sprintf("telemetry: attribute %q is prohibited or unregistered", e.key)
}

func (e attributeProhibitedErr) Is(target error) bool {
	return target == ErrAttributeProhibitedSentinel
}

// ErrAttributeProhibitedSentinel is the class every per-key prohibited
// attribute error matches with errors.Is.
var ErrAttributeProhibitedSentinel = errors.New("telemetry: attribute prohibited")

// ErrAttributeProhibited builds the per-key prohibited-attribute error.
func ErrAttributeProhibited(key string) error { return attributeProhibitedErr{key: key} }
