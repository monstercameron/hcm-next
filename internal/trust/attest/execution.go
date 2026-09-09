package attest

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ExecutionRequirement is the exact attestation obligation a dependent effect
// must satisfy. It binds the effect to one statement and one evidence binding.
type ExecutionRequirement struct {
	Tenant           values.TenantId
	ObligationID     string
	StatementID      string
	StatementVersion uint64
	StatementDigest  string
	BindingDigest    string
	ResponseID       string
	ResponseRevision uint64
	TransactionID    string
}

// ExecutionDecision is an audit-safe result of the execution gate.
type ExecutionDecision struct {
	Allowed         bool
	Reason          string
	ObligationID    string
	ResponseDigest  string
	StatementDigest string
	BindingDigest   string
	CheckedAt       TrustedTime
}

// ValidateRequired checks a response against the exact obligation at the
// execution boundary. Refused and unknown are never agreement, and a
// correction/revocation does not become valid merely because it exists.
func ValidateRequired(req ExecutionRequirement, response Response, now TrustedTime) (ExecutionDecision, error) {
	if err := now.Validate(); err != nil {
		return ExecutionDecision{}, err
	}
	if strings.TrimSpace(req.Tenant.String()) == "" || strings.TrimSpace(req.ObligationID) == "" || strings.TrimSpace(req.StatementID) == "" || strings.TrimSpace(req.StatementDigest) == "" || strings.TrimSpace(req.BindingDigest) == "" || strings.TrimSpace(req.ResponseID) == "" || req.StatementVersion == 0 || req.ResponseRevision == 0 {
		return ExecutionDecision{}, fmt.Errorf("%w: incomplete execution requirement", ErrRequiredAttestation)
	}
	refuse := func(reason string) (ExecutionDecision, error) {
		return ExecutionDecision{Allowed: false, Reason: reason, ObligationID: req.ObligationID, ResponseDigest: response.Digest, StatementDigest: req.StatementDigest, BindingDigest: req.BindingDigest, CheckedAt: now}, nil
	}
	if response.Tenant != req.Tenant || response.ResponseID != req.ResponseID || response.Revision != req.ResponseRevision {
		return refuse("response_identity_mismatch")
	}
	if response.StatementID != req.StatementID || response.StatementVersion != req.StatementVersion || response.StatementDigest != req.StatementDigest || response.BindingDigest != req.BindingDigest {
		return refuse("statement_or_binding_mismatch")
	}
	if response.Status != ResponseAccepted {
		return refuse("response_not_accepted")
	}
	if response.Kind != AssertionResponse {
		return refuse("response_is_not_original_acceptance")
	}
	if req.TransactionID != "" && response.TransactionID != req.TransactionID {
		return refuse("transaction_mismatch")
	}
	if response.RecordedAt.At.After(now.At) {
		return refuse("response_recorded_in_future")
	}
	if response.RecordedAt.At.Before(now.At) && response.RecordedAt.Health == "" {
		return refuse("response_time_evidence_missing")
	}
	return ExecutionDecision{Allowed: true, Reason: "ATTESTATION_ACCEPTED", ObligationID: req.ObligationID, ResponseDigest: response.Digest, StatementDigest: req.StatementDigest, BindingDigest: req.BindingDigest, CheckedAt: now}, nil
}

// RevalidateAtExecution loads the exact persisted receipt and applies the
// execution gate. A store failure blocks the effect rather than guessing.
func RevalidateAtExecution(ctx context.Context, store ResponseStore, clock TrustedClock, req ExecutionRequirement) (ExecutionDecision, error) {
	if store == nil || clock == nil {
		return ExecutionDecision{}, fmt.Errorf("%w: response store and trusted clock are required", ErrRequiredAttestation)
	}
	response, err := store.GetResponse(ctx, req.Tenant, req.ResponseID, req.ResponseRevision)
	if err != nil {
		return ExecutionDecision{}, fmt.Errorf("%w: response lookup: %v", ErrRequiredAttestation, err)
	}
	now, err := clock.TrustedNow()
	if err != nil {
		return ExecutionDecision{}, fmt.Errorf("%w: execution time: %v", ErrRequiredAttestation, err)
	}
	return ValidateRequired(req, response, now)
}

// EnforceRequired is the concise name for the execution gate.
func EnforceRequired(ctx context.Context, store ResponseStore, clock TrustedClock, req ExecutionRequirement) (ExecutionDecision, error) {
	return RevalidateAtExecution(ctx, store, clock, req)
}

// FixedTrustedTime is useful for deterministic conformance callers.
func FixedTrustedTime(at time.Time) TrustedTime {
	return TrustedTime{At: at.UTC(), Source: "conformance", EvidenceID: "ev:time:conformance", Health: "TRUSTED"}
}
