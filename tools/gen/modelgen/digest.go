package modelgen

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// OutputDigest computes a stable sha256 digest over a rendered file set,
// hashing each file's name and content in sorted name order. Two renders of
// byte-identical content — regardless of map iteration order — always
// produce the same digest; this is what backs the MSRC-007 golden digest
// pinned in render_test.go.
func OutputDigest(files map[string][]byte) string {
	h := sha256.New()
	for _, name := range sortedFileKeys(files) {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(files[name])
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// GenerateAll compiles the model catalog and runs [Build] then [Render] in
// one call, for callers (cmd/modelgen, this package's own tests) that only
// need the final rendered file set.
func GenerateAll() (map[string][]byte, error) {
	reg, err := model.Catalog()
	if err != nil {
		return nil, err
	}
	ms, err := Build(reg)
	if err != nil {
		return nil, err
	}
	return Render(ms)
}
