package schemaflux

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// Digest returns the "sha256:<hex>" content digest of b. It is a plain
// content digest for qualification-fixture bookkeeping (definitions/
// generation/*.yaml), not the canonical envelope digest
// planning/specs/canonical-envelope-and-digest.md defines for proposal
// binding, idempotency or signed evidence; nothing in this package feeds
// that pipeline.
func Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// CombinedDigest returns one digest over several byte slices without their
// concatenation aliasing across a boundary (each part is length-prefixed
// before hashing, so {"ab","c"} and {"a","bc"} never collide).
func CombinedDigest(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(p)))
		h.Write(length[:])
		h.Write(p)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
