package ledger

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
)

// DigestProfile names the canonicalization profile these bytes belong to. The
// profile is bound into the preimage so that two schema versions can never share
// a digest meaning merely because their bytes happen to match.
const DigestProfile = "hcmnext.canonical.LEDGER_EVENT.v1"

// Digester turns a typed payload and its schema reference into the canonical
// digest recorded on a ledger event. It is injected so that the canonicalization
// registry can replace the algorithm without the append path changing, and so
// that no caller can ever supply a digest of its own.
type Digester interface {
	// Digest returns the algorithm identifier, the hex digest and the canonical
	// length of the bytes that were hashed.
	Digest(payload []byte, schemaRef string) (algorithm, digest string, length int, err error)
}

// SHA256Digester is the default profile: sha256 over a length-prefixed
// concatenation of the profile identifier, the schema reference and the payload.
// Length prefixing is what keeps the encoding unambiguous, so no two distinct
// (schema, payload) pairs can produce the same preimage.
type SHA256Digester struct{}

// Algorithm is the identifier this digester records.
const Algorithm = "sha256"

// Digest implements Digester.
func (SHA256Digester) Digest(payload []byte, schemaRef string) (string, string, int, error) {
	if schemaRef == "" {
		return "", "", 0, errors.New("ledger: canonical digest requires a schema reference")
	}

	h := sha256.New()
	writeField := func(b []byte) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(b)))
		_, _ = h.Write(length[:])
		_, _ = h.Write(b)
	}
	writeField([]byte(DigestProfile))
	writeField([]byte(schemaRef))
	writeField(payload)

	return Algorithm, hex.EncodeToString(h.Sum(nil)), len(payload), nil
}
