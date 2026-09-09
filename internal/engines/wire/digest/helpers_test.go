package digest_test

import (
	"crypto/sha512"
	"hash"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
)

func mustComputeAt(t *testing.T, r *digest.Registry, msg proto.Message, key digest.Key) (digest.Reference, []byte) {
	t.Helper()
	ref, b, err := r.ComputeWith(msg, key, digest.Options{})
	if err != nil {
		t.Fatalf("compute under %s: %v", key, err)
	}
	return ref, b
}

func newSHA512_256() hash.Hash { return sha512.New512_256() }

// flipHex alters exactly one hex digit, producing a digest that differs from
// the original in a single nibble.
func flipHex(s string) string {
	if s == "" {
		return "0"
	}
	b := []byte(s)
	if b[0] == '0' {
		b[0] = '1'
	} else {
		b[0] = '0'
	}
	return string(b)
}

// reflectEqual compares two references field by field, including the pointer
// identity distinction between an absent optional and an empty one.
func reflectEqual(t *testing.T, a, b digest.Reference) bool {
	t.Helper()
	if a.ProfileID != b.ProfileID || a.ProfileVersion != b.ProfileVersion ||
		a.SchemaID != b.SchemaID || a.SchemaVersion != b.SchemaVersion ||
		a.AlgorithmID != b.AlgorithmID || a.CanonicalLength != b.CanonicalLength ||
		a.Digest != b.Digest || a.ScopeBindingDigest != b.ScopeBindingDigest {
		return false
	}
	return optionalEqual(a.CanonicalBytesArtifactRef, b.CanonicalBytesArtifactRef) &&
		optionalEqual(a.IntentID, b.IntentID) &&
		optionalEqual(a.ProposalRevisionID, b.ProposalRevisionID) &&
		optionalEqual(a.MaterialProfileRef, b.MaterialProfileRef)
}

func optionalEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
