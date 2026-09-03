package frontier

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Canonicalization profile identities. A digest is always prefixed with the
// profile that produced it, so bytes canonicalized as an instance state can
// never be mistaken for a transition's digest.
//
// These follow the canonicalization the rest of the workflow layer already
// uses for its own artifacts — internal/workflow.computePlanDigest and
// internal/workflow/simulate.computeReceiptDigest — rather than
// internal/engines/wire/digest. The wire engine mints digests over a
// registered canonical profile bound to a Protobuf message name, and there is
// no published proto for a frontier transition; inventing one to reach that
// encoder would put a wire schema in front of an internal runtime value. The
// determinism the ticket asks for comes from the same three properties either
// way: a profile-prefixed hash, struct fields in declaration order and no
// unordered collection anywhere in the value. Every slice this package emits
// is sorted before it is digested, and the state carries no maps at all.
const (
	transitionDigestProfile = "hcmnext.workflow.frontier.Transition/v1"
	stateDigestProfile      = "hcmnext.workflow.frontier.InstanceState/v1"
)

// canonicalDigest hashes a value under a profile. The canonical form is the
// encoding/json rendering: struct fields in declaration order, every slice
// already sorted by the code that built it, and no floating point anywhere.
func canonicalDigest(profile string, v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		// A transition is plain data, so this is unreachable. If it ever
		// happens, produce bytes that cannot collide with a real digest rather
		// than silently returning an empty one.
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// computeTransitionDigest hashes a transition's canonical bytes. The
// unexported digest field is excluded by construction, so recomputing over a
// transition reproduces the digest it was minted with.
func computeTransitionDigest(t Transition) string {
	return canonicalDigest(transitionDigestProfile, t)
}

// Digest is the instance state's content identity. It exists so a runtime can
// pin the snapshot a transition was derived from without re-deriving the
// transition.
func (s InstanceState) Digest() string {
	return canonicalDigest(stateDigestProfile, s)
}

// JSON renders a transition as indented, deterministic JSON with its digest
// attached. Two identical advancements render identical bytes, which is what
// makes a checked-in golden a contract rather than a snapshot.
func (t Transition) JSON() ([]byte, error) {
	type rendered struct {
		Transition
		Digest string `json:"digest"`
	}
	b, err := json.MarshalIndent(rendered{Transition: t, Digest: t.digest}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
