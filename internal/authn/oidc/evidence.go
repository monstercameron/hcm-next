package oidc

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Outcome is the closed set of results [Flow.HandleCallback] records
// evidence for.
type Outcome string

const (
	OutcomeSuccess Outcome = "SUCCESS"
	OutcomeDenied  Outcome = "DENIED"
)

// Evidence is the durable record [Flow.HandleCallback] produces for every
// callback it processes, success or refusal. It follows the same
// "ev:<domain>:<digest>" identifier convention as internal/trust.Principal.EvidenceID
// and internal/trust/attest's own evidence records.
//
// Subject is empty when a refusal occurs before an ID token's subject claim
// is ever read (an unknown state, a rejected grant); Reason is empty only on
// [OutcomeSuccess].
type Evidence struct {
	EvidenceID string

	Tenant    values.TenantId
	IssuerURL string
	Subject   string

	Outcome Outcome
	Reason  string

	At time.Time
}

// EvidenceSink is where [Flow] hands off the [Evidence] it produces. A nil
// sink is valid: [Flow.HandleCallback] still returns the evidence value to
// its caller either way, so a caller that only wants the returned value
// (rather than a durable side channel) never needs to configure one.
type EvidenceSink interface {
	Record(Evidence)
}

// newEvidence builds one evidence record. reason is a stable, evidence-safe
// token (an error's message text, or "" on success) -- never a raw claim or
// credential value.
func newEvidence(tenant values.TenantId, issuerURL, subject string, outcome Outcome, reason string, at time.Time) Evidence {
	at = at.UTC()
	sum := sha256.Sum256([]byte("oidc:" + string(tenant) + ":" + issuerURL + ":" + subject + ":" + string(outcome) + ":" + reason + ":" + at.Format(time.RFC3339Nano)))
	return Evidence{
		EvidenceID: "ev:oidc:" + hex.EncodeToString(sum[:])[:32],
		Tenant:     tenant,
		IssuerURL:  issuerURL,
		Subject:    subject,
		Outcome:    outcome,
		Reason:     reason,
		At:         at,
	}
}
