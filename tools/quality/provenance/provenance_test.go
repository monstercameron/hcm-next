package provenance_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/provenance"
)

func statement() provenance.Statement {
	return provenance.Statement{Schema: provenance.Schema, Subject: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Source: "git:abc", Build: "build:123", Toolchain: "go1.26.3", Builder: "ci.example", SBOMDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
}

func TestTodo_TOOL_018(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := provenance.Sign(statement(), "release-2026", priv)
	if err != nil {
		t.Fatal(err)
	}
	policy := provenance.Policy{Builder: "ci.example", Source: "git:abc", Toolchain: "go1.26.3", KeyID: "release-2026"}
	if err := provenance.Verify(s, pub, policy, statement().Subject); err != nil {
		t.Fatalf("valid provenance rejected: %v", err)
	}
	for name, mutate := range map[string]func(*provenance.Signed){
		"tampered statement": func(x *provenance.Signed) { x.Statement.Build = "build:other" },
		"unknown builder":    func(x *provenance.Signed) { x.Statement.Builder = "untrusted" },
		"wrong subject":      func(x *provenance.Signed) { x.Statement.Subject = "sha256:" + "c" + string(make([]byte, 63)) },
	} {
		t.Run(name, func(t *testing.T) {
			bad := s
			mutate(&bad)
			if err := provenance.Verify(bad, pub, policy, statement().Subject); !errors.Is(err, provenance.ErrInvalidSignature) && !errors.Is(err, provenance.ErrSubjectMismatch) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	unsigned := provenance.Signed{Statement: statement()}
	if !errors.Is(provenance.Verify(unsigned, pub, policy, statement().Subject), provenance.ErrUnsigned) {
		t.Fatal("unsigned statement was accepted")
	}
}

func TestStatementDigestIsDeterministic(t *testing.T) {
	a, err := statement().Digest()
	if err != nil {
		t.Fatal(err)
	}
	b, err := statement().Digest()
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("digest changed: %q != %q", a, b)
	}
}
