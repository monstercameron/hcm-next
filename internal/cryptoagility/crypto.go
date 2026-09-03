// Package cryptoagility provides a small, policy-driven migration contract for
// signed and encrypted evidence. History is immutable: migration produces
// receipts and new envelopes rather than changing an existing record.
package cryptoagility

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"
)

var (
	ErrAlgorithmRejected = errors.New("cryptoagility: algorithm rejected by policy")
	ErrInvalidSignature  = errors.New("cryptoagility: invalid signature")
	ErrInvalidEnvelope   = errors.New("cryptoagility: invalid envelope")
)

const AlgorithmEd25519 = "ed25519"

// Versioned identifiers demonstrate rotation without coupling policy to an
// implementation. Both versions use the standard-library Ed25519 primitive;
// deployments may register different primitives behind the same contract.
const (
	AlgorithmEd25519V1 = "ed25519-v1"
	AlgorithmEd25519V2 = "ed25519-v2"
)

func supportedEd25519(algorithm string) bool {
	return algorithm == AlgorithmEd25519 || algorithm == AlgorithmEd25519V1 || algorithm == AlgorithmEd25519V2
}

// AlgorithmPolicy makes the migration window explicit. Active is used for
// writes; Accepted contains algorithms that may still be read until Cutoff.
type AlgorithmPolicy struct {
	Active   string
	Accepted map[string]bool
	Retired  map[string]bool
	Cutoff   time.Time
}

func (p AlgorithmPolicy) permitsRead(algorithm string, at time.Time) bool {
	if algorithm == "" || !p.Accepted[algorithm] {
		return false
	}
	return !p.Retired[algorithm] || p.Cutoff.IsZero() || at.Before(p.Cutoff)
}

func (p AlgorithmPolicy) permitsWrite(algorithm string) bool {
	return algorithm != "" && algorithm == p.Active
}

type Key struct {
	ID, Algorithm string
	Private       ed25519.PrivateKey
	Public        ed25519.PublicKey
}

type Signature struct {
	KeyID, Algorithm string
	Value            []byte
}

type SignedEvidence struct {
	Payload    []byte
	Signatures []Signature
}

// Sign creates one signature under the active algorithm. Keys and policy are
// intentionally supplied by callers so rotation is registry configuration.
func Sign(payload []byte, key Key, policy AlgorithmPolicy) (Signature, error) {
	if !policy.permitsWrite(key.Algorithm) {
		return Signature{}, fmt.Errorf("%w: %s", ErrAlgorithmRejected, key.Algorithm)
	}
	if !supportedEd25519(key.Algorithm) || len(key.Private) != ed25519.PrivateKeySize {
		return Signature{}, errors.New("cryptoagility: unsupported signing key")
	}
	return Signature{KeyID: key.ID, Algorithm: key.Algorithm, Value: ed25519.Sign(key.Private, payload)}, nil
}

// DualSign signs with old and new keys during the bounded overlap window.
func DualSign(payload []byte, oldKey, newKey Key, policy AlgorithmPolicy, at time.Time) (SignedEvidence, error) {
	if !policy.Cutoff.IsZero() && !at.Before(policy.Cutoff) {
		return SignedEvidence{}, fmt.Errorf("%w: signing cutoff", ErrAlgorithmRejected)
	}
	old, err := Sign(payload, oldKey, AlgorithmPolicy{Active: oldKey.Algorithm, Accepted: map[string]bool{oldKey.Algorithm: true}})
	if err != nil {
		return SignedEvidence{}, err
	}
	neu, err := Sign(payload, newKey, AlgorithmPolicy{Active: newKey.Algorithm, Accepted: map[string]bool{newKey.Algorithm: true}})
	if err != nil {
		return SignedEvidence{}, err
	}
	return SignedEvidence{Payload: append([]byte(nil), payload...), Signatures: []Signature{old, neu}}, nil
}

func Verify(payload []byte, sig Signature, keys map[string]Key, policy AlgorithmPolicy, at time.Time) error {
	if !policy.permitsRead(sig.Algorithm, at) {
		return fmt.Errorf("%w: %s", ErrAlgorithmRejected, sig.Algorithm)
	}
	k, ok := keys[sig.KeyID]
	if !ok || k.Algorithm != sig.Algorithm || len(k.Public) != ed25519.PublicKeySize {
		return ErrInvalidSignature
	}
	if !supportedEd25519(sig.Algorithm) || !ed25519.Verify(k.Public, payload, sig.Value) {
		return ErrInvalidSignature
	}
	return nil
}

// VerifyDual accepts either signature before cutoff, while requiring both
// signatures when dual-signing is in effect. It returns the verified count.
func VerifyDual(evidence SignedEvidence, keys map[string]Key, policy AlgorithmPolicy, at time.Time) (int, error) {
	if len(evidence.Signatures) != 2 {
		return 0, ErrInvalidSignature
	}
	verified := 0
	for _, sig := range evidence.Signatures {
		if err := Verify(evidence.Payload, sig, keys, policy, at); err == nil {
			verified++
		}
	}
	if verified != 2 {
		return verified, ErrInvalidSignature
	}
	return verified, nil
}

type Receipt struct {
	ID, Kind, ArtifactID, FromAlgorithm, ToAlgorithm string
	At                                               time.Time
	SourceDigest                                     []byte
}

type Envelope struct {
	ArtifactID, Algorithm, KeyID string
	Nonce, Ciphertext            []byte
}

// ReSign verifies the historical signature and appends a new signature. The
// caller retains the original SignedEvidence as immutable history.
func ReSign(evidence SignedEvidence, keys map[string]Key, policy AlgorithmPolicy, newKey Key, artifactID string, at time.Time) (SignedEvidence, Receipt, error) {
	if _, err := VerifyDual(evidence, keys, policy, at); err != nil {
		return SignedEvidence{}, Receipt{}, err
	}
	sig, err := Sign(evidence.Payload, newKey, AlgorithmPolicy{Active: newKey.Algorithm, Accepted: map[string]bool{newKey.Algorithm: true}})
	if err != nil {
		return SignedEvidence{}, Receipt{}, err
	}
	updated := SignedEvidence{Payload: append([]byte(nil), evidence.Payload...), Signatures: append(append([]Signature(nil), evidence.Signatures...), sig)}
	return updated, Receipt{ID: artifactID + ":resign:" + at.UTC().Format(time.RFC3339Nano), Kind: "re-sign", ArtifactID: artifactID, FromAlgorithm: evidence.Signatures[0].Algorithm, ToAlgorithm: newKey.Algorithm, At: at}, nil
}

func encrypt(key []byte, plaintext []byte) (nonce, ciphertext []byte, err error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, g.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, g.Seal(nil, nonce, plaintext, nil), nil
}

func decrypt(key []byte, e Envelope) ([]byte, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil || len(e.Nonce) != g.NonceSize() {
		return nil, ErrInvalidEnvelope
	}
	return g.Open(nil, e.Nonce, e.Ciphertext, nil)
}

// ReEncrypt decrypts an old envelope and creates a new one, emitting a
// receipt that binds the migration to the original ciphertext.
func ReEncrypt(old Envelope, oldKey, newKey []byte, newAlgorithm, artifactID string, at time.Time) (Envelope, Receipt, error) {
	plain, err := decrypt(oldKey, old)
	if err != nil {
		return Envelope{}, Receipt{}, err
	}
	nonce, ciphertext, err := encrypt(newKey, plain)
	if err != nil {
		return Envelope{}, Receipt{}, err
	}
	receipt := Receipt{ID: artifactID + ":reencrypt:" + at.UTC().Format(time.RFC3339Nano), Kind: "re-encrypt", ArtifactID: artifactID, FromAlgorithm: old.Algorithm, ToAlgorithm: newAlgorithm, At: at, SourceDigest: append([]byte(nil), old.Ciphertext...)}
	return Envelope{ArtifactID: artifactID, Algorithm: newAlgorithm, KeyID: old.KeyID, Nonce: nonce, Ciphertext: ciphertext}, receipt, nil
}

// CompareDigest is constant-time equality for receipt/source binding checks.
func CompareDigest(a, b []byte) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare(a, b) == 1
}
