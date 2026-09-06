package legal

import "testing"

func TestSignDigestRoundTripsThroughVerifySignature(t *testing.T) {
	signer := fixedSigner(t, 0x11)
	data := []byte("arbitrary canonical bytes an author or reviewer would sign")

	digest, sig := signer.SignDigest(data)
	if digest == "" {
		t.Fatal("SignDigest returned an empty digest")
	}
	if err := VerifySignature(data, digest, sig); err != nil {
		t.Fatalf("VerifySignature: %v", err)
	}
}

func TestSignDigestIsDeterministicForTheSameKeyAndBytes(t *testing.T) {
	signer := fixedSigner(t, 0x22)
	data := []byte("draft candidate digest input")

	digest1, sig1 := signer.SignDigest(data)
	digest2, sig2 := signer.SignDigest(data)

	if digest1 != digest2 {
		t.Fatalf("digest not stable: %s vs %s", digest1, digest2)
	}
	if string(sig1.Bytes) != string(sig2.Bytes) {
		t.Fatal("ed25519 signature over identical bytes with the same key should be identical")
	}
}

func TestVerifySignatureFailsClosedOnForgedSignature(t *testing.T) {
	signer := fixedSigner(t, 0x33)
	other := fixedSigner(t, 0x44)
	data := []byte("review record digest input")

	digest, sig := signer.SignDigest(data)
	_, forged := other.SignDigest(data)
	tampered := sig
	tampered.Bytes = forged.Bytes

	if err := VerifySignature(data, digest, tampered); err == nil {
		t.Fatal("expected a forged signature over a valid digest to fail verification")
	}
}

func TestVerifySignatureFailsClosedOnStaleDigest(t *testing.T) {
	signer := fixedSigner(t, 0x55)
	data := []byte("original bytes")

	digest, sig := signer.SignDigest(data)
	if err := VerifySignature([]byte("tampered bytes"), digest, sig); err == nil {
		t.Fatal("expected a valid signature over changed content to fail verification")
	}
}
