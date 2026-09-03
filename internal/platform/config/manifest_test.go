package config_test

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/platform/config"
)

func fixedSeed(b byte) []byte {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = b ^ byte(i)
	}
	return seed
}

func keyPair(t *testing.T, seedByte byte) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	priv := ed25519.NewKeyFromSeed(fixedSeed(seedByte))
	return priv.Public().(ed25519.PublicKey), priv
}

func digestHex(t *testing.T, content byte) string {
	t.Helper()
	b := make([]byte, 32)
	for i := range b {
		b[i] = content
	}
	return hex.EncodeToString(b)
}

func validBundle(t *testing.T) config.Bundle {
	t.Helper()
	return config.Bundle{
		BundleID:        "bundle-prod-2026-09",
		ManifestVersion: 1,
		Dependencies: []config.Dependency{
			{Kind: config.DependencySchema, Name: "hcmnext.intents.v1", Version: "1.4.0", Digest: digestHex(t, 0x01)},
			{Kind: config.DependencyWorkflow, Name: "onboarding", Version: "2.0.0", Digest: digestHex(t, 0x02)},
			{Kind: config.DependencyMapping, Name: "workday-crosswalk", Version: "0.9.1", Digest: digestHex(t, 0x03)},
		},
		CompatibilityRange: ">=1.0.0,<2.0.0",
		Signer:             "signer:platform:key-1",
		Provenance:         "build:ci-run-4821",
		CredentialRefs:     []string{config.CredentialRefPrefix + "vault/hcmnext/smtp"},
	}
}

// TestTodo_CONFIG_002 proves the RED and GREEN clauses for the signed
// dependency manifest: a floating dependency, a missing pinned version or
// digest, an unknown dependency kind, a duplicate dependency, and raw
// secret material posing as a credential reference are all rejected before
// a bundle is considered valid; a valid bundle produces an immutable digest
// independent of dependency order, and signs and verifies as VALID.
func TestTodo_CONFIG_002(t *testing.T) {
	pub, priv := keyPair(t, 0x10)

	t.Run("GREEN_valid_bundle_signs_and_verifies", func(t *testing.T) {
		b := validBundle(t)
		signed, err := config.SignBundle(b, "signer:platform:key-1", priv)
		if err != nil {
			t.Fatalf("SignBundle: %v", err)
		}
		status, err := config.VerifyBundle(signed, pub)
		if err != nil {
			t.Fatalf("VerifyBundle: %v", err)
		}
		if status != config.VerifyStatusValid {
			t.Fatalf("status = %s, want VALID", status)
		}
	})

	t.Run("RED_floating_version_rejected", func(t *testing.T) {
		b := validBundle(t)
		b.Dependencies[0].Version = "latest"
		if _, err := config.BundleDigest(b); !errors.Is(err, config.ErrMissingVersion) {
			t.Fatalf("error = %v, want ErrMissingVersion", err)
		}
	})

	t.Run("RED_range_version_rejected_as_floating", func(t *testing.T) {
		b := validBundle(t)
		b.Dependencies[0].Version = ">=1.0.0"
		if _, err := config.BundleDigest(b); !errors.Is(err, config.ErrFloatingDependency) {
			t.Fatalf("error = %v, want ErrFloatingDependency", err)
		}
	})

	t.Run("RED_missing_digest_rejected", func(t *testing.T) {
		b := validBundle(t)
		b.Dependencies[0].Digest = ""
		if _, err := config.BundleDigest(b); !errors.Is(err, config.ErrMissingDigest) {
			t.Fatalf("error = %v, want ErrMissingDigest", err)
		}
	})

	t.Run("RED_unknown_dependency_kind_rejected", func(t *testing.T) {
		b := validBundle(t)
		b.Dependencies[0].Kind = config.DependencyUnspecified
		if _, err := config.BundleDigest(b); !errors.Is(err, config.ErrUnknownDependencyKind) {
			t.Fatalf("error = %v, want ErrUnknownDependencyKind", err)
		}
	})

	t.Run("RED_duplicate_dependency_rejected", func(t *testing.T) {
		b := validBundle(t)
		b.Dependencies = append(b.Dependencies, b.Dependencies[0])
		if _, err := config.BundleDigest(b); !errors.Is(err, config.ErrDuplicateDependency) {
			t.Fatalf("error = %v, want ErrDuplicateDependency", err)
		}
	})

	t.Run("RED_raw_credential_material_rejected", func(t *testing.T) {
		b := validBundle(t)
		b.CredentialRefs = []string{"sk_live_51H8doNotLeakThisApiKeyValue"}
		_, err := config.BundleDigest(b)
		if !errors.Is(err, config.ErrRawCredential) {
			t.Fatalf("error = %v, want ErrRawCredential", err)
		}
		if strings.Contains(err.Error(), "sk_live_51H8doNotLeakThisApiKeyValue") {
			t.Fatalf("error message leaked the raw credential: %v", err)
		}
	})

	t.Run("RED_unverifiable_signature_rejected", func(t *testing.T) {
		otherPub, _ := keyPair(t, 0x99)
		signed, err := config.SignBundle(validBundle(t), "signer:platform:key-1", priv)
		if err != nil {
			t.Fatalf("SignBundle: %v", err)
		}
		status, err := config.VerifyBundle(signed, otherPub)
		if err == nil {
			t.Fatalf("expected verification to fail against the wrong public key")
		}
		if status != config.VerifyStatusInvalidSignature {
			t.Fatalf("status = %s, want INVALID_SIGNATURE", status)
		}
	})

	t.Run("RED_tampered_bundle_rejected", func(t *testing.T) {
		signed, err := config.SignBundle(validBundle(t), "signer:platform:key-1", priv)
		if err != nil {
			t.Fatalf("SignBundle: %v", err)
		}
		signed.Bundle.CompatibilityRange = ">=9.0.0,<10.0.0"
		status, err := config.VerifyBundle(signed, pub)
		if err == nil {
			t.Fatalf("expected verification to fail for tampered content")
		}
		if status != config.VerifyStatusTampered {
			t.Fatalf("status = %s, want TAMPERED", status)
		}
	})

	t.Run("GREEN_dependency_order_does_not_affect_digest", func(t *testing.T) {
		b1 := validBundle(t)
		b2 := validBundle(t)
		b2.Dependencies[0], b2.Dependencies[2] = b2.Dependencies[2], b2.Dependencies[0]

		d1, err := config.BundleDigest(b1)
		if err != nil {
			t.Fatalf("BundleDigest(b1): %v", err)
		}
		d2, err := config.BundleDigest(b2)
		if err != nil {
			t.Fatalf("BundleDigest(b2): %v", err)
		}
		if d1 != d2 {
			t.Fatalf("digest depends on dependency slice order: %s vs %s", d1, d2)
		}
	})
}

// TestTodo_CONFIG_002_Property checks, over many randomly generated valid
// bundles, that Build/Sign/Verify always succeeds as VALID, that flipping
// any single byte of the signature or the recorded digest always breaks
// verification, and that shuffling dependency order never changes the
// digest.
func TestTodo_CONFIG_002_Property(t *testing.T) {
	pub, priv := keyPair(t, 0x42)

	seed := uint64(0xBADC0FFEE0DDF00D)
	next := func() uint64 {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return seed
	}

	kinds := []config.DependencyKind{config.DependencySchema, config.DependencyRule, config.DependencyWorkflow, config.DependencyMapping}

	for i := 0; i < 200; i++ {
		depCount := int(next()%4) + 1
		deps := make([]config.Dependency, 0, depCount)
		for j := 0; j < depCount; j++ {
			kind := kinds[next()%uint64(len(kinds))]
			digest := make([]byte, 32)
			for k := range digest {
				digest[k] = byte(next())
			}
			deps = append(deps, config.Dependency{
				Kind:    kind,
				Name:    strings.Repeat("n", int(next()%5)+1) + string(rune('a'+j)),
				Version: "1.0." + strings.Repeat("1", int(next()%3)+1),
				Digest:  hex.EncodeToString(digest),
			})
		}
		b := config.Bundle{
			BundleID:           "bundle-" + string(rune('a'+i%26)),
			ManifestVersion:    uint32(i),
			Dependencies:       deps,
			CompatibilityRange: ">=1.0.0,<2.0.0",
			Signer:             "signer:test",
			Provenance:         "build:property-test",
		}

		signed, err := config.SignBundle(b, "signer:test", priv)
		if err != nil {
			t.Fatalf("iteration %d: SignBundle: %v", i, err)
		}
		status, err := config.VerifyBundle(signed, pub)
		if err != nil || status != config.VerifyStatusValid {
			t.Fatalf("iteration %d: VerifyBundle = %s, %v, want VALID, nil", i, status, err)
		}

		mutatedSig := signed
		sigBytes, _ := hex.DecodeString(mutatedSig.Signature)
		sigBytes[0] ^= 0xFF
		mutatedSig.Signature = hex.EncodeToString(sigBytes)
		if status, _ := config.VerifyBundle(mutatedSig, pub); status == config.VerifyStatusValid {
			t.Fatalf("iteration %d: flipped signature byte still verified as VALID", i)
		}

		mutatedDigest := signed
		digestBytes, _ := hex.DecodeString(mutatedDigest.Digest)
		digestBytes[0] ^= 0xFF
		mutatedDigest.Digest = hex.EncodeToString(digestBytes)
		if status, _ := config.VerifyBundle(mutatedDigest, pub); status == config.VerifyStatusValid {
			t.Fatalf("iteration %d: flipped digest byte still verified as VALID", i)
		}

		if len(deps) > 1 {
			shuffled := append([]config.Dependency(nil), deps...)
			shuffled[0], shuffled[len(shuffled)-1] = shuffled[len(shuffled)-1], shuffled[0]
			shuffledBundle := b
			shuffledBundle.Dependencies = shuffled
			d1, err1 := config.BundleDigest(b)
			d2, err2 := config.BundleDigest(shuffledBundle)
			if err1 != nil || err2 != nil {
				t.Fatalf("iteration %d: BundleDigest errors: %v, %v", i, err1, err2)
			}
			if d1 != d2 {
				t.Fatalf("iteration %d: shuffled dependency order changed the digest", i)
			}
		}
	}
}

// TestTodo_CONFIG_002_Golden proves stable digest and signature output for
// a fixed bundle and a fixed ed25519 seed.
func TestTodo_CONFIG_002_Golden(t *testing.T) {
	_, priv := keyPair(t, 0x07)
	b := validBundle(t)

	signed, err := config.SignBundle(b, "signer:platform:key-1", priv)
	if err != nil {
		t.Fatalf("SignBundle: %v", err)
	}

	const wantDigest = "f86fd9258958a60a1fd65342614dfedd49f48d629427fc54a01a24c9f232ea0f"
	const wantSignature = "3481733a1019f2c26db0fd51a7d4e7152b9bc500ebed68696b2bf54f04ea315" +
		"e04101c99180d16d1c582a9c5c0c6dda83967ee244fa9042a54ee90bfa7f9980b"

	if signed.Digest != wantDigest {
		t.Fatalf("digest = %s, want %s", signed.Digest, wantDigest)
	}
	if signed.Signature != wantSignature {
		t.Fatalf("signature = %s, want %s", signed.Signature, wantSignature)
	}
	if len(signed.Signature) != ed25519.SignatureSize*2 {
		t.Fatalf("signature hex length = %d, want %d", len(signed.Signature), ed25519.SignatureSize*2)
	}
}

// TestTodo_CONFIG_002_Security proves the manifest resists tamper and
// forgery: an attacker who edits the bundle after signing is caught as
// TAMPERED, an attacker who resigns a modified bundle with their own key is
// caught as INVALID_SIGNATURE against the real signer's public key, raw
// secret material never becomes a credential reference (and is never
// echoed whole in the resulting error), and the signed envelope never
// carries private key material.
func TestTodo_CONFIG_002_Security(t *testing.T) {
	pub, priv := keyPair(t, 0x55)

	t.Run("attacker_cannot_forge_with_a_different_key", func(t *testing.T) {
		_, attackerPriv := keyPair(t, 0x66)
		tampered := validBundle(t)
		tampered.Provenance = "build:attacker-supplied"

		forged, err := config.SignBundle(tampered, "signer:platform:key-1", attackerPriv)
		if err != nil {
			t.Fatalf("SignBundle: %v", err)
		}
		status, err := config.VerifyBundle(forged, pub)
		if err == nil {
			t.Fatalf("expected verification against the real signer key to fail")
		}
		if status != config.VerifyStatusInvalidSignature {
			t.Fatalf("status = %s, want INVALID_SIGNATURE", status)
		}
	})

	t.Run("raw_credential_rejected_and_not_echoed_whole", func(t *testing.T) {
		const rawSecret = "AKIA1234567890EXAMPLESECRET"
		b := validBundle(t)
		b.CredentialRefs = []string{rawSecret}
		_, err := config.BundleDigest(b)
		if !errors.Is(err, config.ErrRawCredential) {
			t.Fatalf("error = %v, want ErrRawCredential", err)
		}
		if strings.Contains(err.Error(), rawSecret) {
			t.Fatalf("error echoed the raw secret in full: %v", err)
		}
	})

	t.Run("signed_envelope_carries_no_private_key_material", func(t *testing.T) {
		signed, err := config.SignBundle(validBundle(t), "signer:platform:key-1", priv)
		if err != nil {
			t.Fatalf("SignBundle: %v", err)
		}
		encoded, err := json.Marshal(signed)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		if strings.Contains(string(encoded), hex.EncodeToString(priv)) {
			t.Fatalf("signed envelope leaked the private key")
		}
	})

	t.Run("tampered_bundle_after_signing_is_caught", func(t *testing.T) {
		signed, err := config.SignBundle(validBundle(t), "signer:platform:key-1", priv)
		if err != nil {
			t.Fatalf("SignBundle: %v", err)
		}
		signed.Bundle.Dependencies[0].Version = "9.9.9"
		status, err := config.VerifyBundle(signed, pub)
		if err == nil || status != config.VerifyStatusTampered {
			t.Fatalf("status = %s, err = %v, want TAMPERED", status, err)
		}
	})
}

// TestTodo_CONFIG_002_Conformance proves the manifest survives a lossy-free
// transport round trip and that two independently ordered but otherwise
// equal bundles produce byte-identical digests and signatures under the
// same key, which is what lets two build pipelines agree on one manifest
// without agreeing on dependency declaration order.
func TestTodo_CONFIG_002_Conformance(t *testing.T) {
	pub, priv := keyPair(t, 0x77)

	t.Run("json_round_trip_preserves_verifiability", func(t *testing.T) {
		signed, err := config.SignBundle(validBundle(t), "signer:platform:key-1", priv)
		if err != nil {
			t.Fatalf("SignBundle: %v", err)
		}
		encoded, err := json.Marshal(signed)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		var roundTripped config.SignedBundle
		if err := json.Unmarshal(encoded, &roundTripped); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		status, err := config.VerifyBundle(roundTripped, pub)
		if err != nil || status != config.VerifyStatusValid {
			t.Fatalf("VerifyBundle after round trip = %s, %v, want VALID, nil", status, err)
		}
	})

	t.Run("independently_ordered_builds_agree", func(t *testing.T) {
		b1 := validBundle(t)
		b2 := validBundle(t)
		b2.Dependencies = []config.Dependency{b2.Dependencies[2], b2.Dependencies[0], b2.Dependencies[1]}

		signed1, err := config.SignBundle(b1, "signer:platform:key-1", priv)
		if err != nil {
			t.Fatalf("SignBundle(b1): %v", err)
		}
		signed2, err := config.SignBundle(b2, "signer:platform:key-1", priv)
		if err != nil {
			t.Fatalf("SignBundle(b2): %v", err)
		}
		if signed1.Digest != signed2.Digest {
			t.Fatalf("digests differ across dependency order: %s vs %s", signed1.Digest, signed2.Digest)
		}
		if signed1.Signature != signed2.Signature {
			t.Fatalf("signatures differ across dependency order: %s vs %s", signed1.Signature, signed2.Signature)
		}
	})
}
