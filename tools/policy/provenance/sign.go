package provenance

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// digestPayload is the canonical projection CanonicalDigest hashes and
// Sign/Verify cover: every Statement field except Signature itself, so a
// signature can never be computed over its own bytes. Field order matches
// Statement's declaration order (see types.go).
type digestPayload struct {
	SchemaVersion int           `json:"schema_version"`
	PredicateType string        `json:"predicate_type"`
	GeneratedAt   string        `json:"generated_at"`
	Subjects      []Subject     `json:"subjects"`
	Builder       Builder       `json:"builder"`
	Source        SourceRef     `json:"source"`
	BuildConfig   BuildConfig   `json:"build_config"`
	SBOM          SBOMReference `json:"sbom"`
}

func (s Statement) payload() digestPayload {
	return digestPayload{
		SchemaVersion: s.SchemaVersion,
		PredicateType: s.PredicateType,
		GeneratedAt:   s.GeneratedAt,
		Subjects:      s.Subjects,
		Builder:       s.Builder,
		Source:        s.Source,
		BuildConfig:   s.BuildConfig,
		SBOM:          s.SBOM,
	}
}

// CanonicalDigest returns the hex-encoded sha256 digest of s's canonical
// JSON projection (every field except Signature). Two statements with
// identical content but a different (or absent) signature digest
// identically; this is exactly the value SignStatement covers and
// VerifyStatementSignature checks - the same convention
// tools/planning/gateevidence.P1AManifest.CanonicalDigest uses for the
// signed P1A manifest (NEXT-002).
func (s Statement) CanonicalDigest() (string, error) {
	b, err := json.Marshal(s.payload())
	if err != nil {
		return "", fmt.Errorf("provenance: marshal canonical statement: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// SignDigest signs a hex-encoded digest with priv and returns the
// hex-encoded Ed25519 signature. Mirrors
// tools/planning/gateevidence.SignDigest's convention exactly.
func SignDigest(priv ed25519.PrivateKey, digestHex string) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("provenance: private key has %d bytes, want %d", len(priv), ed25519.PrivateKeySize)
	}
	digest, err := hex.DecodeString(digestHex)
	if err != nil {
		return "", fmt.Errorf("provenance: decode digest: %w", err)
	}
	sig := ed25519.Sign(priv, digest)
	return hex.EncodeToString(sig), nil
}

// VerifyDigestSignature reports whether sigHex is a valid Ed25519
// signature by the key pubHex over digestHex. All three are hex-encoded.
// Mirrors tools/planning/gateevidence.VerifyDigestSignature's convention
// exactly.
func VerifyDigestSignature(pubHex, digestHex, sigHex string) (bool, error) {
	pub, err := hex.DecodeString(pubHex)
	if err != nil {
		return false, fmt.Errorf("provenance: decode public key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return false, fmt.Errorf("provenance: public key has %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	digest, err := hex.DecodeString(digestHex)
	if err != nil {
		return false, fmt.Errorf("provenance: decode digest: %w", err)
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false, fmt.Errorf("provenance: decode signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return false, fmt.Errorf("provenance: signature has %d bytes, want %d", len(sig), ed25519.SignatureSize)
	}
	return ed25519.Verify(ed25519.PublicKey(pub), digest, sig), nil
}

// SignStatement returns a copy of s with Signature set to an Ed25519
// signature (by priv) over s.CanonicalDigest(). keyFixture, when non-empty,
// is recorded in Signature.KeyFixture purely as provenance-of-the-signing-
// key documentation (e.g. "testdata/dev-signing-key.json"); it plays no
// role in verification.
func SignStatement(priv ed25519.PrivateKey, s Statement, keyFixture string) (Statement, error) {
	digest, err := s.CanonicalDigest()
	if err != nil {
		return Statement{}, err
	}
	sigHex, err := SignDigest(priv, digest)
	if err != nil {
		return Statement{}, err
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return Statement{}, fmt.Errorf("provenance: private key's Public() did not return an ed25519.PublicKey")
	}
	signed := s
	signed.Signature = &Signature{
		Algorithm:  AlgorithmEd25519,
		PublicKey:  hex.EncodeToString(pub),
		Value:      sigHex,
		KeyFixture: keyFixture,
	}
	return signed, nil
}

// VerifyStatementSignature recomputes s's CanonicalDigest and checks it
// against s.Signature. It returns a non-nil error for any structural
// problem (missing signature, unsupported algorithm, bad hex, wrong key
// size) and (false, nil) for a well-formed but invalid signature - callers
// that only care whether s is trustworthy should treat either as "not
// verified" (see Verify, which does exactly that plus trust-set and SBOM
// cross-checks).
func VerifyStatementSignature(s Statement) (bool, error) {
	if s.Signature == nil {
		return false, fmt.Errorf("provenance: statement has no signature")
	}
	if s.Signature.Algorithm != AlgorithmEd25519 {
		return false, fmt.Errorf("provenance: unsupported signature algorithm %q", s.Signature.Algorithm)
	}
	digest, err := s.CanonicalDigest()
	if err != nil {
		return false, err
	}
	return VerifyDigestSignature(s.Signature.PublicKey, digest, s.Signature.Value)
}
