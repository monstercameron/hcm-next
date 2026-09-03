package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Canonicalization profile identities, following the same profile-prefixed
// sha256 style internal/workflow/digest.go, internal/workflow/version/digest.go
// and internal/workflow/frontier/digest.go all use: a digest is always
// prefixed with the profile that produced it, struct fields render in
// declaration order, and every slice a caller of this package builds is
// sorted before it reaches here.
const (
	startFingerprintDigestProfile  = "hcmnext.workflow.runtime.StartFingerprint/v1"
	controlSnapshotDigestProfile   = "hcmnext.workflow.runtime.ControlSnapshotDigest/v1"
	startReceiptDigestProfile      = "hcmnext.workflow.runtime.StartReceipt/v1"
	advanceReceiptDigestProfile    = "hcmnext.workflow.runtime.AdvanceReceipt/v1"
	continuationRecordDigestProfile = "hcmnext.workflow.runtime.ContinuationRecord/v1"
)

// canonicalDigest hashes a value under a profile.
func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// Every value this package digests is plain data it assembled itself,
		// so this is unreachable. If it ever happens, produce bytes that
		// cannot collide with a real digest rather than silently returning an
		// empty one.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// startReceiptIdentity is the subset of [StartReceipt] content its digest
// covers -- everything except the digest field itself.
type startReceiptIdentity struct {
	TenantID           string
	InstanceID         string
	WorkflowID         string
	WorkflowVersion    uint32
	CompiledPlanDigest string
	SemanticVersion    string
	ExecutionMode      string
	InstanceVersion    int64
	Frontier           []string
	CorrelationID      string
	Replay             bool
}

func computeStartReceiptDigest(r StartReceipt) string {
	id := startReceiptIdentity{
		TenantID:           r.TenantID.String(),
		InstanceID:         r.InstanceID.String(),
		WorkflowID:         r.WorkflowID,
		WorkflowVersion:    r.WorkflowVersion,
		CompiledPlanDigest: r.CompiledPlanDigest,
		SemanticVersion:    r.SemanticVersion,
		ExecutionMode:      string(r.ExecutionMode),
		InstanceVersion:    r.InstanceVersion,
		Frontier:           append([]string(nil), r.Frontier...),
		CorrelationID:      r.CorrelationID,
		Replay:             r.Replay,
	}
	return canonicalDigest(startReceiptDigestProfile, id)
}

// advanceReceiptIdentity is the subset of [AdvanceReceipt] content its digest
// covers. CreatedAt-like wall-clock fields are deliberately absent: a
// deterministic replay reconstructs this receipt from durable rows written at
// a different instant than the original call, and the receipt's identity
// must not move because of that.
type advanceReceiptIdentity struct {
	TenantID        string
	InstanceID      string
	NodeID          string
	Attempt         int
	CompletedState  string
	RouteKey        string
	OutputDigest    string
	NewVersion      int64
	Frontier        []string
	Complete        bool
	TerminalCode    string
	Continuations   []continuationIdentity
	Replay          bool
}

type continuationIdentity struct {
	TargetNodeID string
	Kind         string
	RouteKey     string
	Ref          string
	TerminalCode string
}

func computeAdvanceReceiptDigest(r AdvanceReceipt) string {
	conts := make([]continuationIdentity, 0, len(r.Continuations))
	for _, c := range r.Continuations {
		conts = append(conts, continuationIdentity{
			TargetNodeID: c.TargetNodeID,
			Kind:         string(c.Kind),
			RouteKey:     c.RouteKey,
			Ref:          c.Ref,
			TerminalCode: c.TerminalCode,
		})
	}
	id := advanceReceiptIdentity{
		TenantID:       r.TenantID.String(),
		InstanceID:     r.InstanceID.String(),
		NodeID:         r.NodeID,
		Attempt:        r.Attempt,
		CompletedState: r.CompletedState,
		RouteKey:       r.RouteKey,
		OutputDigest:   r.OutputDigest,
		NewVersion:     r.NewInstanceVersion,
		Frontier:       append([]string(nil), r.Frontier...),
		Complete:       r.Complete,
		TerminalCode:   r.TerminalCode,
		Continuations:  conts,
		Replay:         r.Replay,
	}
	return canonicalDigest(advanceReceiptDigestProfile, id)
}
