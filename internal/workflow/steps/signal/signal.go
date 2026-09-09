package signal

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Signal is one inbound correlated event Accept evaluates against a
// [SignalSubscription]. It is a value: Accept never mutates it and a
// [LogEntry] stores an independent copy.
type Signal struct {
	Tenant           values.TenantId
	Source           string
	EventType        string
	SchemaRef        string
	CorrelationKey   string
	CorrelationValue string

	// SequenceNumber orders signals on the same subscription when Ordering is
	// OrderingMonotonicSequence. It is otherwise advisory.
	SequenceNumber uint64

	// IdempotencyKey identifies "the same delivery attempt" for exactly-once
	// acceptance: two signals with the same key and the same Payload bytes
	// are one delivery, replayed; the same key with different bytes is the
	// security incident workflow-context-contract.md §3 names ("same scoped
	// provider ID with different bytes"). Empty means the signal carries no
	// idempotency identity and is never deduplicated against.
	IdempotencyKey string

	Payload   []byte
	Taint     workflow.TaintLevel
	Signature []byte

	ReceivedAt values.Instant
}

// payloadDigest returns the sha256 hex of the payload bytes. It is computed
// from the bytes themselves rather than trusted from a caller-supplied field,
// so a caller cannot claim two different payloads are byte-identical.
func (s Signal) payloadDigest() string {
	h := sha256.Sum256(s.Payload)
	return hex.EncodeToString(h[:])
}

// copyPayload returns an independent copy of the payload bytes, so a LogEntry
// cannot be mutated through the caller's slice after the fact.
func (s Signal) copyPayload() []byte {
	if s.Payload == nil {
		return nil
	}
	out := make([]byte, len(s.Payload))
	copy(out, s.Payload)
	return out
}

func (s Signal) copySignature() []byte {
	if s.Signature == nil {
		return nil
	}
	out := make([]byte, len(s.Signature))
	copy(out, s.Signature)
	return out
}

// immutable returns a defensively-copied Signal safe to retain inside a
// LogEntry.
func (s Signal) immutable() Signal {
	s.Payload = s.copyPayload()
	s.Signature = s.copySignature()
	return s
}

// Verifier checks a signal's signature. It is a port: production wiring binds
// it to whatever key/cert material the integration's IngressTrustProfile
// names; tests may use a plain HMAC implementation.
type Verifier interface {
	// Verify returns nil when sig's signature is valid over its payload for
	// its declared source. Any non-nil error is treated as an invalid
	// signature.
	Verify(sig Signal) error
}
