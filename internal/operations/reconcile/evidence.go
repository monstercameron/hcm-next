package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// EventKind names one thing that happened to a reconciliation job. Every
// method on [Coordinator] that writes returns exactly one of these.
type EventKind string

// The declared reconciliation events.
const (
	// EventTriggered is a job newly created.
	EventTriggered EventKind = "TRIGGERED"
	// EventReplayed is a duplicate trigger for an effect and policy that
	// already had a job: nothing was written, and the existing job is
	// reported.
	EventReplayed EventKind = "REPLAYED"
	// EventObserved is an observation attempt that did not meet the job's
	// required freshness: an attempt was recorded, but the job kept polling.
	EventObserved EventKind = "OBSERVED"
	// EventSettled is a job that reached a comparison verdict -- terminal
	// (PASS, MISMATCH, PARTIAL) or the resting StatusUnknown.
	EventSettled EventKind = "SETTLED"
	// EventExhausted is a job that reached its deadline with no determinate
	// verdict and settled to EXPIRED or REPAIR_REQUIRED.
	EventExhausted EventKind = "EXHAUSTED"
)

const evidenceDigestProfile = "hcmnext.operations.reconcile.Evidence/v1"

// Evidence is the record of one reconciliation event.
//
// Every field except Reason is derived from the durable job row and the
// caller's own instant, never a second authority: it is what makes an
// evidence record a caller kept checkable against the effect_reconciliation_job
// table itself.
type Evidence struct {
	Kind     EventKind
	TenantID uuid.UUID
	JobID    uuid.UUID

	EffectRef string
	PolicyRef string
	Status    Status

	ObservationAttempts int
	// FenceToken is the lease token the write was performed under.
	FenceToken uint64

	At     time.Time
	Reason string

	digest string
}

// Digest is the evidence record's content identity.
func (e Evidence) Digest() string { return e.digest }

type evidenceIdentity struct {
	Kind                string `json:"kind"`
	TenantID            string `json:"tenant_id"`
	JobID               string `json:"job_id"`
	EffectRef           string `json:"effect_ref"`
	PolicyRef           string `json:"policy_ref"`
	Status              string `json:"status"`
	ObservationAttempts int    `json:"observation_attempts"`
	FenceToken          uint64 `json:"fence_token"`
	At                  string `json:"at"`
	Reason              string `json:"reason,omitempty"`
}

func newEvidence(kind EventKind, job Job, at time.Time, fenceToken uint64, reason string) Evidence {
	e := Evidence{
		Kind: kind, TenantID: job.TenantID, JobID: job.JobID,
		EffectRef: job.EffectRef, PolicyRef: job.PolicyRef, Status: job.Status,
		ObservationAttempts: job.ObservationAttempts, FenceToken: fenceToken,
		At: at.UTC(), Reason: reason,
	}
	e.digest = canonicalDigest(evidenceDigestProfile, evidenceIdentity{
		Kind: string(e.Kind), TenantID: e.TenantID.String(), JobID: e.JobID.String(),
		EffectRef: e.EffectRef, PolicyRef: e.PolicyRef, Status: string(e.Status),
		ObservationAttempts: e.ObservationAttempts, FenceToken: e.FenceToken,
		At: e.At.Format(time.RFC3339Nano), Reason: e.Reason,
	})
	return e
}

// canonicalDigest hashes a value under a profile, following the same
// profile-prefixed sha256 style internal/workflow/timer and
// internal/workflow/lease already use.
func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
