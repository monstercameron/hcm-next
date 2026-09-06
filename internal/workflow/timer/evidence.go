package timer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// EventKind names one thing that happened to a durable timer. Every method on
// [Scheduler] that writes (or deliberately declines to write) returns exactly
// one of these.
type EventKind string

// The declared timer events.
const (
	// EventScheduled is a promise newly written.
	EventScheduled EventKind = "SCHEDULED"
	// EventReplayed is a promise a replayed advancement re-made and this
	// package recognised: nothing was written.
	EventReplayed EventKind = "REPLAYED"
	// EventFired is a promise kept: the timer settled and ready work was
	// enqueued.
	EventFired EventKind = "FIRED"
	// EventSkipped is an overdue promise a SKIP misfire policy abandoned: the
	// timer was cancelled and nothing was woken.
	EventSkipped EventKind = "SKIPPED"
	// EventCancelled is a promise withdrawn -- most often because the
	// instance that made it reached a terminal.
	EventCancelled EventKind = "CANCELLED"
	// EventDeferred is an overdue promise a REVIEW misfire policy left
	// pending for a human. It is the one event that records a decision not to
	// write.
	EventDeferred EventKind = "DEFERRED"
)

const evidenceDigestProfile = "hcmnext.workflow.timer.Evidence/v1"

// Evidence is the record of one timer event.
//
// It is derived from the durable row and the caller's own instant, never a
// second authority: every field except Reason is readable back out of
// workflow_timer and workflow_ready_work, which is what makes an evidence
// record a caller kept checkable against the database.
type Evidence struct {
	Kind       EventKind
	TenantID   uuid.UUID
	TimerID    uuid.UUID
	InstanceID uuid.UUID
	NodeID     string

	// RequirementDigest is the wake requirement the timer promised, which is
	// also its durable key.
	RequirementDigest string
	FiresAt           time.Time
	At                time.Time

	// Decision is the SCHED-002 misfire decision this event carried, empty
	// for an event no policy was applied to.
	Decision string
	// FenceToken is the lease token the settle was performed under, zero for
	// an event that was not fenced (a schedule or a cancellation).
	FenceToken uint64
	// ReadyWorkID is the work this event woke, zero when it woke none.
	ReadyWorkID uuid.UUID

	Reason string

	digest string
}

// Digest is the evidence record's content identity.
func (e Evidence) Digest() string { return e.digest }

type evidenceIdentity struct {
	Kind              string `json:"kind"`
	TenantID          string `json:"tenant_id"`
	TimerID           string `json:"timer_id"`
	InstanceID        string `json:"instance_id"`
	NodeID            string `json:"node_id"`
	RequirementDigest string `json:"requirement_digest"`
	FiresAt           string `json:"fires_at"`
	At                string `json:"at"`
	Decision          string `json:"decision,omitempty"`
	FenceToken        uint64 `json:"fence_token"`
	ReadyWorkID       string `json:"ready_work_id"`
	Reason            string `json:"reason,omitempty"`
}

func newEvidence(kind EventKind, row Timer, at time.Time, decision string, fenceToken uint64,
	readyWorkID uuid.UUID, reason string,
) Evidence {
	e := Evidence{
		Kind: kind, TenantID: row.TenantID, TimerID: row.TimerID, InstanceID: row.InstanceID,
		NodeID: row.NodeID, RequirementDigest: row.Key, FiresAt: row.FiresAt.UTC(), At: at.UTC(),
		Decision: decision, FenceToken: fenceToken, ReadyWorkID: readyWorkID, Reason: reason,
	}
	e.digest = canonicalDigest(evidenceDigestProfile, evidenceIdentity{
		Kind: string(e.Kind), TenantID: e.TenantID.String(), TimerID: e.TimerID.String(),
		InstanceID: e.InstanceID.String(), NodeID: e.NodeID, RequirementDigest: e.RequirementDigest,
		FiresAt: e.FiresAt.Format(time.RFC3339Nano), At: e.At.Format(time.RFC3339Nano),
		Decision: e.Decision, FenceToken: e.FenceToken, ReadyWorkID: e.ReadyWorkID.String(),
		Reason: e.Reason,
	})
	return e
}

// canonicalDigest hashes a value under a profile, following the same
// profile-prefixed sha256 style the rest of internal/workflow uses.
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
