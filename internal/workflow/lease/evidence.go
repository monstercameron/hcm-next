package lease

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// TransitionKind names one thing that happened to a lease. Every method on
// [Manager] that changes or checks a lease returns exactly one of these, so a
// caller can record what it did without having to describe it in prose.
type TransitionKind string

// The declared lease transitions.
const (
	// TransitionAcquired is a first claim on a resource nobody held.
	TransitionAcquired TransitionKind = "ACQUIRED"
	// TransitionTakenOver is a claim that retired a lapsed holder's lease
	// first. It is recorded distinctly from ACQUIRED because "somebody else's
	// claim was expired to make room for this one" is a materially different
	// operational fact.
	TransitionTakenOver TransitionKind = "TAKEN_OVER"
	// TransitionRenewed is a holder extending its own lease.
	TransitionRenewed TransitionKind = "RENEWED"
	// TransitionReleased is a holder giving the resource up.
	TransitionReleased TransitionKind = "RELEASED"
	// TransitionExpired is a caller-observed lapse retired explicitly.
	TransitionExpired TransitionKind = "EXPIRED"
	// TransitionRevoked is a lease an operator took away. This package never
	// produces one; it appears in [Manager.History] when the row says so.
	TransitionRevoked TransitionKind = "REVOKED"
	// TransitionVerified is a fence presented and accepted.
	TransitionVerified TransitionKind = "VERIFIED"
	// TransitionRefused is a fence presented and refused. It carries the
	// refusal code in Reason.
	TransitionRefused TransitionKind = "REFUSED"
)

const evidenceDigestProfile = "hcmnext.workflow.lease.Evidence/v1"

// Evidence is the record of one lease transition.
//
// It is derived data, never a second authority: [Manager.History]
// reconstructs the same ACQUIRED/TAKEN_OVER/RELEASED/EXPIRED/REVOKED records
// from the durable workflow_lease rows alone, so an evidence record a caller
// kept can be checked against the database rather than believed.
type Evidence struct {
	Kind     TransitionKind
	TenantID uuid.UUID
	LeaseID  uuid.UUID
	Resource Resource
	HolderID string

	// Token is the fence token in force after this transition. PriorToken is
	// the resource's previous token, zero when this is the first lease of the
	// resource's life.
	Token      uint64
	PriorToken uint64

	At     time.Time
	Reason string

	digest string
}

// Digest is the evidence record's content identity.
func (e Evidence) Digest() string { return e.digest }

// evidenceIdentity is the subset of [Evidence] its digest covers -- all of it
// except the digest field, rendered in a stable field order with times in
// canonical UTC text.
type evidenceIdentity struct {
	Kind         string `json:"kind"`
	TenantID     string `json:"tenant_id"`
	LeaseID      string `json:"lease_id"`
	ResourceKind string `json:"resource_kind"`
	ResourceID   string `json:"resource_id"`
	HolderID     string `json:"holder_id"`
	Token        uint64 `json:"token"`
	PriorToken   uint64 `json:"prior_token"`
	At           string `json:"at"`
	Reason       string `json:"reason,omitempty"`
}

func newEvidence(kind TransitionKind, tenantID, leaseID uuid.UUID, res Resource, holderID string,
	token, priorToken uint64, at time.Time, reason string,
) Evidence {
	e := Evidence{
		Kind: kind, TenantID: tenantID, LeaseID: leaseID, Resource: res, HolderID: holderID,
		Token: token, PriorToken: priorToken, At: at.UTC(), Reason: reason,
	}
	e.digest = canonicalDigest(evidenceDigestProfile, evidenceIdentity{
		Kind: string(e.Kind), TenantID: e.TenantID.String(), LeaseID: e.LeaseID.String(),
		ResourceKind: e.Resource.Kind, ResourceID: e.Resource.ID, HolderID: e.HolderID,
		Token: e.Token, PriorToken: e.PriorToken,
		At: e.At.Format(time.RFC3339Nano), Reason: e.Reason,
	})
	return e
}

// canonicalDigest hashes a value under a profile, following the same
// profile-prefixed sha256 style internal/workflow/runtime and
// internal/workflow/frontier use.
func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Every value this package digests is plain data it assembled itself,
		// so this is unreachable. If it ever happens, produce bytes that
		// cannot collide with a real digest rather than an empty one.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}
