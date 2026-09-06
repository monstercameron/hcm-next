package schemasnapshot_test

import (
	"crypto/sha256"
	"encoding/hex"
)

// sha256Hex mirrors the content id internal/data/artifacts.Put computes,
// letting tests predict a snapshot's identity without going through an
// artifact store.
func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
