package gateevidence

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
)

// SignDigest signs a hex-encoded digest with priv and returns the
// hex-encoded Ed25519 signature.
func SignDigest(priv ed25519.PrivateKey, digestHex string) (string, error) {
	digest, err := hex.DecodeString(digestHex)
	if err != nil {
		return "", fmt.Errorf("decode digest: %w", err)
	}
	sig := ed25519.Sign(priv, digest)
	return hex.EncodeToString(sig), nil
}

// VerifyDigestSignature reports whether sigHex is a valid Ed25519 signature
// by the key pubHex over digestHex. All three are hex-encoded.
func VerifyDigestSignature(pubHex, digestHex, sigHex string) (bool, error) {
	pub, err := hex.DecodeString(pubHex)
	if err != nil {
		return false, fmt.Errorf("decode public key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return false, fmt.Errorf("public key has %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	digest, err := hex.DecodeString(digestHex)
	if err != nil {
		return false, fmt.Errorf("decode digest: %w", err)
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false, fmt.Errorf("decode signature: %w", err)
	}
	return ed25519.Verify(ed25519.PublicKey(pub), digest, sig), nil
}

// VerifyManifestSignature recomputes m's CanonicalDigest and checks it
// against m.Signature. It returns a non-nil error for any structural
// problem (missing signature, bad hex, wrong key size) and (false, nil)
// for a well-formed but invalid signature - callers that only care whether
// the manifest is trustworthy should treat either as "not verified".
func VerifyManifestSignature(m P1AManifest) (bool, error) {
	if m.Signature == nil {
		return false, fmt.Errorf("manifest has no signature")
	}
	if m.Signature.Algorithm != "ed25519" {
		return false, fmt.Errorf("unsupported signature algorithm %q", m.Signature.Algorithm)
	}
	digest, err := m.CanonicalDigest()
	if err != nil {
		return false, err
	}
	return VerifyDigestSignature(m.Signature.PublicKey, digest, m.Signature.Value)
}
