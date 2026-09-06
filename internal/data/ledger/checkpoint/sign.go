package checkpoint

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"time"
)

// Signer produces the signature over a manifest's canonical digest. It is an
// interface so that no key material ever has to live in this package, this
// repository, or a caller's process: a KMS-backed or HSM-backed
// implementation exposes exactly these four methods.
type Signer interface {
	// KeyID identifies the key in a [KeyDirectory].
	KeyID() string
	// Algorithm names the signature algorithm; only [AlgorithmEd25519] is
	// accepted today.
	Algorithm() string
	// PublicKey returns the hex-encoded public key, recorded inside the
	// signed bytes so a signature cannot be re-attributed.
	PublicKey() string
	// Sign returns the hex-encoded signature over the raw digest bytes.
	Sign(digest []byte) (string, error)
}

// KeyDirectory states what is known about a signing key: its public key, the
// window it may sign in, and whether it has been revoked. It is the seam
// that makes "do not sign with a revoked or expired key" enforceable without
// this package owning key lifecycle.
type KeyDirectory interface {
	// Lookup returns the key's status. ok is false for a key the directory
	// has never heard of, which is refused exactly like a revoked one: an
	// unknown key is not a trusted key.
	Lookup(keyID string) (KeyStatus, bool)
}

// StaticKeyDirectory is a fixed, in-memory [KeyDirectory]. It is what a
// composition root builds from configuration, and what tests build from a
// fixture.
type StaticKeyDirectory struct {
	keys map[string]KeyStatus
}

// NewStaticKeyDirectory returns a directory over the given statuses, keyed
// by their own KeyID. It copies the input, so a later mutation of the
// caller's slice cannot silently change what is trusted.
func NewStaticKeyDirectory(statuses ...KeyStatus) *StaticKeyDirectory {
	keys := make(map[string]KeyStatus, len(statuses))
	for _, status := range statuses {
		keys[status.KeyID] = status
	}
	return &StaticKeyDirectory{keys: keys}
}

// Lookup implements [KeyDirectory].
func (d *StaticKeyDirectory) Lookup(keyID string) (KeyStatus, bool) {
	if d == nil {
		return KeyStatus{}, false
	}
	status, ok := d.keys[keyID]
	return status, ok
}

// Ed25519Signer signs with a crypto/ed25519 private key the caller already
// holds. It stores the key only for the lifetime of the value and exposes no
// accessor for it: [Signer] deliberately has no method that returns private
// material.
type Ed25519Signer struct {
	keyID string
	priv  ed25519.PrivateKey
}

// NewEd25519Signer binds a private key to a key identifier. It refuses a key
// of the wrong size rather than producing signatures nothing can verify.
func NewEd25519Signer(keyID string, priv ed25519.PrivateKey) (*Ed25519Signer, error) {
	if keyID == "" {
		return nil, fmt.Errorf("checkpoint: a signing key must have an identifier")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("checkpoint: ed25519 private key has %d bytes, want %d", len(priv), ed25519.PrivateKeySize)
	}
	return &Ed25519Signer{keyID: keyID, priv: priv}, nil
}

// KeyID implements [Signer].
func (s *Ed25519Signer) KeyID() string { return s.keyID }

// Algorithm implements [Signer].
func (s *Ed25519Signer) Algorithm() string { return AlgorithmEd25519 }

// PublicKey implements [Signer].
func (s *Ed25519Signer) PublicKey() string {
	pub, ok := s.priv.Public().(ed25519.PublicKey)
	if !ok {
		return ""
	}
	return hex.EncodeToString(pub)
}

// Sign implements [Signer].
func (s *Ed25519Signer) Sign(digest []byte) (string, error) {
	if len(digest) == 0 {
		return "", fmt.Errorf("checkpoint: refusing to sign an empty digest")
	}
	return hex.EncodeToString(ed25519.Sign(s.priv, digest)), nil
}

// Sign validates the manifest, refuses the key if the directory says it was
// not permitted to sign at the manifest's own creation instant, and then
// signs the manifest's canonical digest.
//
// The order matters. The key check happens before any signature is produced,
// so a revoked or expired key never yields a signature at all - there is
// nothing for a caller to accidentally persist and nothing for a verifier to
// have to reason about later.
//
// It returns a copy of the manifest carrying the signature; the manifest
// passed in is not modified, so a caller can never end up holding two
// values that disagree about whether a checkpoint was signed.
func Sign(m Manifest, signer Signer, dir KeyDirectory) (Manifest, error) {
	if signer == nil {
		return Manifest{}, fmt.Errorf("checkpoint: a signer is required")
	}
	if signer.Algorithm() != AlgorithmEd25519 {
		return Manifest{}, ErrSignatureInvalid{
			EpochNumber: m.EpochNumber, KeyID: signer.KeyID(),
			Reason: fmt.Sprintf("unsupported signature algorithm %q", signer.Algorithm()),
		}
	}

	// The key identity is inside the signed bytes, so it has to be on the
	// manifest before the digest is computed.
	m.Signature = &Signature{
		Algorithm: signer.Algorithm(),
		KeyID:     signer.KeyID(),
		PublicKey: signer.PublicKey(),
		SignedAt:  m.CreatedAt,
	}
	if err := m.Validate(); err != nil {
		return Manifest{}, err
	}
	if err := checkKeyUsable(dir, signer.KeyID(), signer.PublicKey(), m.CreatedAt); err != nil {
		return Manifest{}, err
	}

	digestHex, err := m.CanonicalDigest()
	if err != nil {
		return Manifest{}, err
	}
	raw, err := hex.DecodeString(digestHex)
	if err != nil {
		return Manifest{}, fmt.Errorf("checkpoint: decode manifest digest: %w", err)
	}
	value, err := signer.Sign(raw)
	if err != nil {
		return Manifest{}, fmt.Errorf("checkpoint: sign epoch %d: %w", m.EpochNumber, err)
	}
	m.Signature.Value = value
	return m, nil
}

// checkKeyUsable refuses an unknown key, a key whose recorded public key is
// not the one presented, and a key that was expired or revoked at the given
// instant. A nil directory means the caller declared no key governance at
// all, which is permitted only because [Service] requires one.
func checkKeyUsable(dir KeyDirectory, keyID, publicKey string, at time.Time) error {
	if dir == nil {
		return nil
	}
	status, ok := dir.Lookup(keyID)
	if !ok {
		return ErrKeyNotUsable{KeyID: keyID, At: at, Reason: "is not in the key directory"}
	}
	if status.PublicKey != "" && publicKey != "" && status.PublicKey != publicKey {
		return ErrKeyNotUsable{KeyID: keyID, At: at, Reason: "presents a public key the directory does not recognise"}
	}
	if !status.RevokedAt.IsZero() && !at.Before(status.RevokedAt) {
		return ErrKeyNotUsable{KeyID: keyID, At: at, Reason: "was revoked"}
	}
	if !status.UsableAt(at) {
		return ErrKeyNotUsable{KeyID: keyID, At: at, Reason: "was outside its validity window"}
	}
	return nil
}

// Verify checks a signed manifest end to end, offline: it needs the manifest
// and a key directory and touches no database.
//
// It re-derives everything it can rather than trusting what it was handed.
// The stream heads are re-folded into a root digest and compared; the whole
// manifest is re-projected into its canonical digest; the signature is
// checked against that digest and the public key recorded inside the signed
// bytes; and the key is checked against the directory as of the instant the
// manifest says it was created, so a signature produced by a key that was
// already revoked is refused even though the arithmetic verifies.
//
// A key revoked *after* the manifest was signed does not invalidate it. The
// checkpoint was validly made at the time it was made, and rewriting that
// conclusion afterwards is exactly what an append-only integrity record
// exists to prevent.
func Verify(m Manifest, dir KeyDirectory) error {
	if m.Signature == nil {
		return ErrSignatureInvalid{EpochNumber: m.EpochNumber, Reason: "manifest carries no signature"}
	}
	if err := m.Validate(); err != nil {
		return err
	}
	if err := checkKeyUsable(dir, m.Signature.KeyID, m.Signature.PublicKey, m.CreatedAt); err != nil {
		return err
	}

	pub, err := hex.DecodeString(m.Signature.PublicKey)
	if err != nil {
		return ErrSignatureInvalid{EpochNumber: m.EpochNumber, KeyID: m.Signature.KeyID, Reason: "public key is not valid hex"}
	}
	if len(pub) != ed25519.PublicKeySize {
		return ErrSignatureInvalid{
			EpochNumber: m.EpochNumber, KeyID: m.Signature.KeyID,
			Reason: fmt.Sprintf("public key has %d bytes, want %d", len(pub), ed25519.PublicKeySize),
		}
	}
	sig, err := hex.DecodeString(m.Signature.Value)
	if err != nil {
		return ErrSignatureInvalid{EpochNumber: m.EpochNumber, KeyID: m.Signature.KeyID, Reason: "signature is not valid hex"}
	}

	digestHex, err := m.CanonicalDigest()
	if err != nil {
		return err
	}
	raw, err := hex.DecodeString(digestHex)
	if err != nil {
		return fmt.Errorf("checkpoint: decode manifest digest: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), raw, sig) {
		return ErrSignatureInvalid{
			EpochNumber: m.EpochNumber, KeyID: m.Signature.KeyID,
			Reason: "signature does not verify against the manifest's canonical digest",
		}
	}
	return nil
}
