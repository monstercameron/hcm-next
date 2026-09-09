package checkpoint_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/checkpoint"
)

func devSigner(t *testing.T) *checkpoint.Ed25519Signer {
	t.Helper()
	_, priv := loadDevKey(t)
	signer, err := checkpoint.NewEd25519Signer(devKeyID, priv)
	if err != nil {
		t.Fatalf("build signer: %v", err)
	}
	return signer
}

// signableManifest is validManifest with the fixture key's real public key,
// so Sign and Verify have something they can actually check.
func signableManifest(t *testing.T) checkpoint.Manifest {
	t.Helper()
	m := validManifest(t)
	m.Signature = &checkpoint.Signature{
		Algorithm: checkpoint.AlgorithmEd25519,
		KeyID:     devKeyID,
		PublicKey: devSigner(t).PublicKey(),
	}
	return m
}

func liveDirectory(t *testing.T) checkpoint.KeyDirectory {
	t.Helper()
	return checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
		KeyID: devKeyID, PublicKey: devSigner(t).PublicKey(), NotBefore: keyValidFrom,
	})
}

func TestNewEd25519Signer(t *testing.T) {
	_, priv := loadDevKey(t)

	if _, err := checkpoint.NewEd25519Signer("", priv); err == nil {
		t.Error("a signer was built without a key identifier")
	}
	if _, err := checkpoint.NewEd25519Signer(devKeyID, ed25519.PrivateKey("too short")); err == nil {
		t.Error("a signer was built from a key of the wrong size")
	}

	signer := devSigner(t)
	if signer.Algorithm() != checkpoint.AlgorithmEd25519 {
		t.Errorf("signer algorithm is %q, want %q", signer.Algorithm(), checkpoint.AlgorithmEd25519)
	}
	if signer.KeyID() != devKeyID {
		t.Errorf("signer key id is %q, want %q", signer.KeyID(), devKeyID)
	}
	pub, err := hex.DecodeString(signer.PublicKey())
	if err != nil || len(pub) != ed25519.PublicKeySize {
		t.Fatalf("signer public key %q is not a %d-byte hex key", signer.PublicKey(), ed25519.PublicKeySize)
	}

	t.Run("an empty digest is never signed", func(t *testing.T) {
		if _, err := signer.Sign(nil); err == nil {
			t.Fatal("the signer signed an empty digest")
		}
	})
}

func TestStaticKeyDirectory(t *testing.T) {
	dir := checkpoint.NewStaticKeyDirectory(
		checkpoint.KeyStatus{KeyID: "a", PublicKey: hashA},
		checkpoint.KeyStatus{KeyID: "b", PublicKey: hashB},
	)
	if status, ok := dir.Lookup("a"); !ok || status.PublicKey != hashA {
		t.Fatalf("Lookup(a) = %+v, %t; want the recorded status", status, ok)
	}
	if _, ok := dir.Lookup("missing"); ok {
		t.Fatal("an unknown key was found in the directory")
	}
	if _, ok := checkpoint.NewStaticKeyDirectory().Lookup("a"); ok {
		t.Fatal("an empty directory produced a key")
	}

	t.Run("a nil directory refuses rather than panics", func(t *testing.T) {
		var nilDir *checkpoint.StaticKeyDirectory
		if _, ok := nilDir.Lookup("a"); ok {
			t.Fatal("a nil directory produced a key")
		}
	})
}

func TestSign(t *testing.T) {
	signer := devSigner(t)
	dir := liveDirectory(t)

	t.Run("a signed manifest verifies", func(t *testing.T) {
		signed, err := checkpoint.Sign(signableManifest(t), signer, dir)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		if signed.Signature.Value == "" {
			t.Fatal("Sign returned a manifest with no signature value")
		}
		if err := checkpoint.Verify(signed, dir); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})

	t.Run("the input manifest is not modified", func(t *testing.T) {
		m := signableManifest(t)
		if _, err := checkpoint.Sign(m, signer, dir); err != nil {
			t.Fatalf("sign: %v", err)
		}
		if m.Signature.Value != "" {
			t.Fatal("Sign wrote a signature onto the caller's own manifest")
		}
	})

	t.Run("an invalid manifest is never signed", func(t *testing.T) {
		m := signableManifest(t)
		m.Streams = nil
		_, err := checkpoint.Sign(m, signer, dir)
		if _, ok := errors.AsType[checkpoint.ErrManifestInvalid](err); !ok {
			t.Fatalf("Sign returned %v, want ErrManifestInvalid", err)
		}
	})

	t.Run("a nil signer is refused", func(t *testing.T) {
		if _, err := checkpoint.Sign(signableManifest(t), nil, dir); err == nil {
			t.Fatal("Sign accepted a nil signer")
		}
	})

	t.Run("a key the directory does not know never signs", func(t *testing.T) {
		_, err := checkpoint.Sign(signableManifest(t), signer, checkpoint.NewStaticKeyDirectory())
		if _, ok := errors.AsType[checkpoint.ErrKeyNotUsable](err); !ok {
			t.Fatalf("Sign returned %v, want ErrKeyNotUsable", err)
		}
	})

	t.Run("a key presenting an unrecognised public key never signs", func(t *testing.T) {
		wrong := checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
			KeyID: devKeyID, PublicKey: hashB, NotBefore: keyValidFrom,
		})
		_, err := checkpoint.Sign(signableManifest(t), signer, wrong)
		notUsable, ok := errors.AsType[checkpoint.ErrKeyNotUsable](err)
		if !ok {
			t.Fatalf("Sign returned %v, want ErrKeyNotUsable", err)
		}
		if !strings.Contains(notUsable.Error(), "does not recognise") {
			t.Fatalf("error %q does not say the public key was unrecognised", notUsable)
		}
	})
}

func TestVerify(t *testing.T) {
	signer := devSigner(t)
	dir := liveDirectory(t)
	signed, err := checkpoint.Sign(signableManifest(t), signer, dir)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*checkpoint.Manifest)
	}{
		{"no signature at all", func(m *checkpoint.Manifest) { m.Signature = nil }},
		{"a public key that is not hex", func(m *checkpoint.Manifest) {
			sig := *m.Signature
			sig.PublicKey = "zzzz"
			m.Signature = &sig
		}},
		{"a public key of the wrong size", func(m *checkpoint.Manifest) {
			sig := *m.Signature
			sig.PublicKey = hex.EncodeToString([]byte("short"))
			m.Signature = &sig
		}},
		{"a signature that is not hex", func(m *checkpoint.Manifest) {
			sig := *m.Signature
			sig.Value = "zzzz"
			m.Signature = &sig
		}},
		{"a signature over different bytes", func(m *checkpoint.Manifest) {
			sig := *m.Signature
			sig.Value = strings.Repeat("0", 128)
			m.Signature = &sig
		}},
		{"an edited covered window", func(m *checkpoint.Manifest) { m.CoversTo = m.CoversTo.Add(time.Hour) }},
		{"an edited epoch number", func(m *checkpoint.Manifest) { m.EpochNumber = 5 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := signed
			tc.mutate(&m)
			if err := checkpoint.Verify(m, dir); err == nil {
				t.Fatalf("Verify accepted a manifest with %s", tc.name)
			}
		})
	}

	t.Run("a key revoked before signing is refused even though the maths verifies", func(t *testing.T) {
		revoked := checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
			KeyID: devKeyID, PublicKey: signer.PublicKey(), NotBefore: keyValidFrom,
			RevokedAt: signed.CreatedAt.Add(-time.Hour),
		})
		err := checkpoint.Verify(signed, revoked)
		if _, ok := errors.AsType[checkpoint.ErrKeyNotUsable](err); !ok {
			t.Fatalf("Verify returned %v, want ErrKeyNotUsable", err)
		}
	})

	t.Run("a key revoked after signing does not invalidate the signature", func(t *testing.T) {
		revokedLater := checkpoint.NewStaticKeyDirectory(checkpoint.KeyStatus{
			KeyID: devKeyID, PublicKey: signer.PublicKey(), NotBefore: keyValidFrom,
			RevokedAt: signed.CreatedAt.Add(time.Hour),
		})
		if err := checkpoint.Verify(signed, revokedLater); err != nil {
			t.Fatalf("a later revocation invalidated an earlier valid signature: %v", err)
		}
	})

	t.Run("verification needs no database", func(t *testing.T) {
		// This whole test file touches no Querier: Verify's inputs are a
		// manifest and a key directory, which is what makes offline
		// verification possible at all.
		if err := checkpoint.Verify(signed, dir); err != nil {
			t.Fatalf("verify: %v", err)
		}
	})
}
