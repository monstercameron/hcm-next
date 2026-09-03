package federation

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Algorithm is a JOSE signing algorithm this package verifies natively. HCM
// Next does not pin a JOSE dependency (LIB-011): these three algorithms are
// the complete, closed set an enterprise identity provider needs for an
// asymmetric identity assertion, and each maps to exactly one Go standard
// library verification primitive (RSA PKCS#1v1.5, ECDSA on P-256, or
// Ed25519). A caller cannot register a fourth: [SigningKey.Algorithm] and
// the JWS header are both compared against this closed set, so a "none" or
// HMAC-family algorithm — the classic algorithm-confusion attacks — is
// rejected before any key lookup happens.
type Algorithm string

// The three algorithms this package verifies.
const (
	AlgRS256 Algorithm = "RS256"
	AlgES256 Algorithm = "ES256"
	AlgEdDSA Algorithm = "EdDSA"
)

func (a Algorithm) valid() bool {
	return a == AlgRS256 || a == AlgES256 || a == AlgEdDSA
}

// maxAssertionBytesDefault bounds assertion parsing before any allocation
// that depends on the assertion's own length.
const maxAssertionBytesDefault = 16 << 10

// b64 is strict, unpadded base64url: one byte string has exactly one
// spelling, so an assertion cannot be re-encoded into a different-looking
// assertion that still verifies.
var b64 = base64.RawURLEncoding.Strict()

// Assertion parsing and verification errors. All are matchable with
// errors.Is.
var (
	ErrMalformedAssertion   = errors.New("federation: assertion is malformed")
	ErrUnsupportedAlgorithm = errors.New("federation: assertion algorithm is not one this verifier accepts")
	ErrSignatureInvalid     = errors.New("federation: assertion signature does not verify")
	ErrKeyNotFound          = errors.New("federation: no signing key resolves for this issuer and key id")
	ErrKeyExpired           = errors.New("federation: signing key is outside its own validity window")
)

// joseHeader is the JWS protected header this package accepts. Decoding is
// strict and the field set is closed: an assertion carrying a header
// parameter this package does not recognize is rejected rather than
// silently processed as if the parameter were absent.
type joseHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Type      string `json:"typ,omitempty"`
}

// splitJWS splits a compact JWS into its decoded header, the exact
// still-encoded signing input (header.payload, byte for byte what was
// signed), the decoded payload and the decoded signature.
func splitJWS(raw string, maxBytes int) (h joseHeader, signingInput string, payload []byte, sig []byte, err error) {
	if raw == "" {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: empty", ErrMalformedAssertion)
	}
	if len(raw) > maxBytes {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: exceeds %d bytes", ErrMalformedAssertion, maxBytes)
	}
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: expected 3 segments, got %d", ErrMalformedAssertion, len(parts))
	}
	if parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: empty segment", ErrMalformedAssertion)
	}

	headerBytes, err := b64.DecodeString(parts[0])
	if err != nil {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: header encoding: %v", ErrMalformedAssertion, err)
	}
	dec := json.NewDecoder(strings.NewReader(string(headerBytes)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&h); err != nil {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: header: %v", ErrMalformedAssertion, err)
	}
	if dec.More() {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: trailing content after header", ErrMalformedAssertion)
	}

	payload, err = b64.DecodeString(parts[1])
	if err != nil {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: payload encoding: %v", ErrMalformedAssertion, err)
	}
	sig, err = b64.DecodeString(parts[2])
	if err != nil {
		return joseHeader{}, "", nil, nil, fmt.Errorf("%w: signature encoding: %v", ErrMalformedAssertion, err)
	}
	return h, parts[0] + "." + parts[1], payload, sig, nil
}

// verifySignature checks sig over signingInput under alg using pub. It fails
// closed: a key of the wrong concrete type for alg, or a signature of the
// wrong length, is rejected the same as a cryptographically wrong signature.
func verifySignature(alg Algorithm, pub crypto.PublicKey, signingInput string, sig []byte) error {
	switch alg {
	case AlgRS256:
		key, ok := pub.(*rsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: RS256 needs an RSA public key", ErrUnsupportedAlgorithm)
		}
		sum := sha256.Sum256([]byte(signingInput))
		if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
			return ErrSignatureInvalid
		}
		return nil
	case AlgES256:
		key, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return fmt.Errorf("%w: ES256 needs an ECDSA public key", ErrUnsupportedAlgorithm)
		}
		if key.Curve != elliptic.P256() {
			return fmt.Errorf("%w: ES256 needs a P-256 key", ErrUnsupportedAlgorithm)
		}
		// JWS ES256 signatures are the raw concatenation of R and S
		// (RFC 7518 3.4), each 32 bytes for P-256 -- not an ASN.1 DER
		// sequence. A DER-encoded signature is the wrong length here and
		// is rejected rather than reparsed.
		if len(sig) != 64 {
			return fmt.Errorf("%w: ES256 signature must be 64 bytes, got %d", ErrSignatureInvalid, len(sig))
		}
		r := new(big.Int).SetBytes(sig[:32])
		s := new(big.Int).SetBytes(sig[32:])
		sum := sha256.Sum256([]byte(signingInput))
		if !ecdsa.Verify(key, sum[:], r, s) {
			return ErrSignatureInvalid
		}
		return nil
	case AlgEdDSA:
		key, ok := pub.(ed25519.PublicKey)
		if !ok {
			return fmt.Errorf("%w: EdDSA needs an Ed25519 public key", ErrUnsupportedAlgorithm)
		}
		if len(sig) != ed25519.SignatureSize {
			return fmt.Errorf("%w: EdDSA signature must be %d bytes, got %d", ErrSignatureInvalid, ed25519.SignatureSize, len(sig))
		}
		if !ed25519.Verify(key, []byte(signingInput), sig) {
			return ErrSignatureInvalid
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedAlgorithm, alg)
	}
}

// KeyID identifies one verification key within an issuer's key set.
type KeyID string

// SigningKey is one verification key a [KeySource] resolves for an issuer.
type SigningKey struct {
	ID        KeyID
	Algorithm Algorithm
	Public    crypto.PublicKey
	// NotBefore and NotAfter bound the key's own rotation window. A zero
	// value leaves that side unbounded. A key presented outside its window
	// is rejected as stale signing metadata even though its bytes still
	// verify a signature -- the point of publishing a rotation window at
	// all is that a retired key stops being trusted the instant its
	// replacement is announced, not only once the old key is deleted.
	NotBefore time.Time
	NotAfter  time.Time
}

// validAt reports whether k is inside its own declared validity window.
func (k SigningKey) validAt(at time.Time) bool {
	if !k.NotBefore.IsZero() && at.Before(k.NotBefore) {
		return false
	}
	if !k.NotAfter.IsZero() && !at.Before(k.NotAfter) {
		return false
	}
	return true
}

// KeySource resolves the verification key an issuer signed an assertion
// with. It is the only door through which a raw key ever reaches a
// [Validator]; the wire format itself is untrusted until a key resolved
// here verifies it.
//
// A production implementation resolves this from a tenant's configured
// JWKS endpoint with caching and rotation once LIB-010 is qualified.
// [StaticKeySource] is the deterministic development and test double used
// until then: it never performs network I/O, so a hermetic test can present
// a stale or unknown key on demand instead of depending on live JWKS
// content.
type KeySource interface {
	ResolveKey(ctx context.Context, issuer string, keyID KeyID) (SigningKey, error)
}

// StaticKeySource is a fixed, compiled-in [KeySource]: a map from issuer to
// key set. It is not safe for concurrent writes after construction; build it
// once with [NewStaticKeySource] and [StaticKeySource.WithKey] and then only
// read it.
type StaticKeySource struct {
	keys map[string]map[KeyID]SigningKey
}

// NewStaticKeySource returns an empty static key source.
func NewStaticKeySource() *StaticKeySource {
	return &StaticKeySource{keys: make(map[string]map[KeyID]SigningKey)}
}

// WithKey registers key under issuer and returns the receiver, so that
// several keys can be chained onto one source.
func (s *StaticKeySource) WithKey(issuer string, key SigningKey) *StaticKeySource {
	if s.keys[issuer] == nil {
		s.keys[issuer] = make(map[KeyID]SigningKey)
	}
	s.keys[issuer][key.ID] = key
	return s
}

// ResolveKey implements [KeySource].
func (s *StaticKeySource) ResolveKey(_ context.Context, issuer string, keyID KeyID) (SigningKey, error) {
	set, ok := s.keys[issuer]
	if !ok {
		return SigningKey{}, fmt.Errorf("%w: issuer %q", ErrKeyNotFound, issuer)
	}
	key, ok := set[keyID]
	if !ok {
		return SigningKey{}, fmt.Errorf("%w: issuer %q key %q", ErrKeyNotFound, issuer, keyID)
	}
	return key, nil
}
