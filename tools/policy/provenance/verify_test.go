package provenance_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/provenance"
)

func mustSignGolden(t *testing.T) provenance.Statement {
	t.Helper()
	priv, err := provenance.LoadSigningKeyFixture(devSigningKeyFixture)
	if err != nil {
		t.Fatalf("LoadSigningKeyFixture: %v", err)
	}
	signed, err := provenance.SignStatement(priv, goldenStatement(), devSigningKeyFixture)
	if err != nil {
		t.Fatalf("SignStatement: %v", err)
	}
	return signed
}

// TestTodo_TOOL_018 is TOOL-018's named PRIMARY test: it proves Verify is a
// real deployment-admission gate that rejects an unsigned artifact, a
// digest-mismatched (tampered) artifact, an unknown-builder/signer
// artifact, and a policy-incomplete artifact - and admits only a Statement
// that is signed by a trusted key, unmodified since signing, and carries a
// matching SBOM reference.
func TestTodo_TOOL_018(t *testing.T) {
	signed := mustSignGolden(t)
	trusted := map[string]bool{signed.Signature.PublicKey: true}

	t.Run("accepts a signed, untampered, trusted, SBOM-matched statement", func(t *testing.T) {
		if err := provenance.Verify(signed, provenance.VerifyOptions{
			TrustedPublicKeys: trusted,
			SBOMDigest:        signed.SBOM.SHA256,
		}); err != nil {
			t.Fatalf("Verify(valid statement) = %v, want nil", err)
		}
	})

	t.Run("rejects an unsigned statement", func(t *testing.T) {
		unsigned := signed
		unsigned.Signature = nil
		if err := provenance.Verify(unsigned, provenance.VerifyOptions{TrustedPublicKeys: trusted}); err == nil {
			t.Fatal("expected an error for an unsigned statement")
		}
	})

	t.Run("rejects a digest-mismatched (tampered) subject", func(t *testing.T) {
		tampered := signed
		tampered.Subjects = append([]provenance.Subject(nil), signed.Subjects...)
		tampered.Subjects[0].SHA256 = strings.Repeat("00", 32)
		err := provenance.Verify(tampered, provenance.VerifyOptions{TrustedPublicKeys: trusted})
		if err == nil {
			t.Fatal("expected an error for a tampered subject digest")
		}
	})

	t.Run("rejects an unknown builder/signer key", func(t *testing.T) {
		err := provenance.Verify(signed, provenance.VerifyOptions{
			TrustedPublicKeys: map[string]bool{strings.Repeat("11", 32): true},
		})
		if err == nil {
			t.Fatal("expected an error when the signer key is not in the trusted set")
		}
	})

	t.Run("rejects a nil trusted-key set (trusts no one)", func(t *testing.T) {
		if err := provenance.Verify(signed, provenance.VerifyOptions{}); err == nil {
			t.Fatal("expected an error when no keys are trusted")
		}
	})

	t.Run("rejects a policy-incomplete statement even if trusted and signed", func(t *testing.T) {
		incomplete := goldenStatement()
		incomplete.Builder.ID = "" // missing builder identity
		priv, err := provenance.LoadSigningKeyFixture(devSigningKeyFixture)
		if err != nil {
			t.Fatalf("LoadSigningKeyFixture: %v", err)
		}
		signedIncomplete, err := provenance.SignStatement(priv, incomplete, devSigningKeyFixture)
		if err != nil {
			t.Fatalf("SignStatement: %v", err)
		}
		err = provenance.Verify(signedIncomplete, provenance.VerifyOptions{
			TrustedPublicKeys: map[string]bool{signedIncomplete.Signature.PublicKey: true},
		})
		if err == nil {
			t.Fatal("expected an error for a policy-incomplete (missing builder id) statement")
		}
	})

	t.Run("rejects a missing SBOM digest match", func(t *testing.T) {
		err := provenance.Verify(signed, provenance.VerifyOptions{
			TrustedPublicKeys: trusted,
			SBOMDigest:        strings.Repeat("99", 32), // does not match signed.SBOM.SHA256
		})
		if err == nil {
			t.Fatal("expected an error when the SBOM digest does not match the statement's SBOM reference")
		}
	})

	t.Run("skips the SBOM cross-check when no SBOMDigest is supplied", func(t *testing.T) {
		if err := provenance.Verify(signed, provenance.VerifyOptions{TrustedPublicKeys: trusted}); err != nil {
			t.Fatalf("Verify without SBOMDigest = %v, want nil", err)
		}
	})
}

// TestTodo_TOOL_018_Race exercises Verify and VerifyStatementSignature from
// many goroutines against the same shared, already-signed Statement value,
// confirming concurrent read-only verification is safe: no shared mutable
// state, and every goroutine reaches the same true/nil verdict.
func TestTodo_TOOL_018_Race(t *testing.T) {
	signed := mustSignGolden(t)
	trusted := map[string]bool{signed.Signature.PublicKey: true}

	const workers = 32
	done := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			done <- provenance.Verify(signed, provenance.VerifyOptions{
				TrustedPublicKeys: trusted,
				SBOMDigest:        signed.SBOM.SHA256,
			})
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent Verify call failed: %v", err)
		}
	}

	verifyDone := make(chan struct{ ok bool }, workers)
	for i := 0; i < workers; i++ {
		go func() {
			ok, err := provenance.VerifyStatementSignature(signed)
			if err != nil {
				ok = false
			}
			verifyDone <- struct{ ok bool }{ok}
		}()
	}
	for i := 0; i < workers; i++ {
		result := <-verifyDone
		if !result.ok {
			t.Error("concurrent VerifyStatementSignature call reported false for a valid signature")
		}
	}
}
