package aggregates

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// DigestAlgorithm is the only algorithm this package computes content digests
// with, matching the digest_algorithm domain migrations/00002_tenant_primitives.sql
// declares (CHECK (VALUE IN ('sha256'))).
const DigestAlgorithm = "sha256"

// computeDigest returns the sha256 hex digest over an ordered, length-prefixed
// sequence of fields. It is deliberately self-contained rather than importing
// internal/engines/canonicalbytes: this package's import list is scoped to
// internal/kernel/values and internal/ledger's port only (see doc.go), and a
// content digest over a handful of already-typed scalar fields does not need
// a general canonicalization engine. Length-prefixing each field (rather than
// joining with a separator byte) is what keeps "a", "bc" distinct from "ab",
// "c": two different field sequences never produce the same byte stream.
func computeDigest(fields ...string) string {
	h := sha256.New()
	var lenBuf [8]byte
	for _, f := range fields {
		binary.BigEndian.PutUint64(lenBuf[:], uint64(len(f)))
		h.Write(lenBuf[:])
		h.Write([]byte(f))
	}
	return hex.EncodeToString(h.Sum(nil))
}
