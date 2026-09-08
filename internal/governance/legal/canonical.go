package legal

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"reflect"
)

// Canonical-encoding and signature errors. All are matchable with errors.Is.
var (
	ErrSignerKey           = errors.New("legal: signer needs an ed25519 private key of the standard size")
	ErrDigestMismatch      = errors.New("legal: recomputed digest does not match the recorded digest")
	ErrSignatureInvalid    = errors.New("legal: ed25519 signature does not verify")
	ErrSignatureKeyMissing = errors.New("legal: no public key supplied to verify against")
	ErrSignerPort          = errors.New("legal: signing port refused the digest")
)

// appendField appends a length-prefixed (label, value) pair to dst so that
// two adjacent fields can never be reparsed as a different split, and a field
// that is empty still contributes its label and a zero length rather than
// vanishing from the encoding. This mirrors the framing internal/trust uses
// for Principal.Fingerprint and internal/kernel/values uses for its
// canonical byte encodings.
func appendField(dst []byte, label, value string) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(label)))
	dst = append(dst, label...)
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(value)))
	return append(dst, value...)
}

// appendUint32Field appends a length-prefixed label followed by a big-endian
// uint32 value.
func appendUint32Field(dst []byte, label string, v uint32) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(label)))
	dst = append(dst, label...)
	return binary.BigEndian.AppendUint32(dst, v)
}

// appendFieldBool appends a single-byte boolean field.
func appendFieldBool(dst []byte, label string, value bool) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(label)))
	dst = append(dst, label...)
	if value {
		return append(dst, 0x01)
	}
	return append(dst, 0x00)
}

// appendStringSlice appends a length-prefixed count followed by every element
// as its own labelled field, so that ["a","b"] and ["ab"] can never collide
// and an empty slice still contributes its label and a zero count.
func appendStringSlice(dst []byte, label string, values []string) []byte {
	dst = appendUint32Field(dst, label, uint32(len(values)))
	for _, v := range values {
		dst = appendField(dst, label+"[]", v)
	}
	return dst
}

// Signer holds an ed25519 key pair used to sign resolved [LegalContext]
// values. It is a fixture-grade key holder: this package does not source,
// rotate, or escrow keys, and production key custody is out of scope for
// LEGAL-001.
type Signer struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
	port SignerPort
}

// SignerPort signs the raw SHA-256 digest bytes supplied by this package.
// Implementations may keep private key material outside the process (for
// example, in a custody service). The bytes passed to Sign are exactly the
// bytes covered by the existing Signer convention; they are not hex text.
type SignerPort interface {
	Sign([]byte) ([]byte, error)
}

// SignerFunc adapts a function to SignerPort.
type SignerFunc func([]byte) ([]byte, error)

func (f SignerFunc) Sign(digest []byte) ([]byte, error) {
	if f == nil {
		return nil, ErrSignerPort
	}
	return f(digest)
}

// NewSigner wraps an existing ed25519 private key.
func NewSigner(priv ed25519.PrivateKey) (*Signer, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrSignerKey, len(priv))
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return nil, ErrSignerKey
	}
	return &Signer{priv: append(ed25519.PrivateKey(nil), priv...), pub: append(ed25519.PublicKey(nil), pub...)}, nil
}

// NewPortSigner creates a signer backed by an external Ed25519 signing port.
// publicKey is trusted configuration and is embedded in every returned
// signature. The port's output is verified before it is returned, so a
// misconfigured or compromised port cannot emit evidence under another key.
func NewPortSigner(publicKey ed25519.PublicKey, port SignerPort) (*Signer, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: trusted public key has %d bytes", ErrSignerKey, len(publicKey))
	}
	if port == nil || isNilSignerPort(port) {
		return nil, fmt.Errorf("%w: nil signing port", ErrSignerPort)
	}
	return &Signer{pub: append(ed25519.PublicKey(nil), publicKey...), port: port}, nil
}

func isNilSignerPort(port SignerPort) bool {
	v := reflect.ValueOf(port)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

// GenerateSigner creates a fresh ed25519 key pair. src may be nil, which uses
// crypto/rand; tests pass a deterministic reader for reproducible fixtures.
func GenerateSigner(src io.Reader) (*Signer, error) {
	if src == nil {
		src = rand.Reader
	}
	pub, priv, err := ed25519.GenerateKey(src)
	if err != nil {
		return nil, fmt.Errorf("legal: generating signer key: %w", err)
	}
	return &Signer{priv: priv, pub: pub}, nil
}

// PublicKey returns a copy of the signer's public key.
func (s *Signer) PublicKey() ed25519.PublicKey {
	if s == nil {
		return nil
	}
	out := make(ed25519.PublicKey, len(s.pub))
	copy(out, s.pub)
	return out
}

// Signature is the detached ed25519 evidence over a [LegalContext]'s
// canonical digest: which key signed, and the signature bytes. It travels
// with the context so that a downstream simulation can verify, offline, which
// exact law set governed it without recontacting this package's registry.
type Signature struct {
	// PublicKey is the ed25519 public key whose private half produced Bytes.
	PublicKey ed25519.PublicKey
	// Bytes is the raw ed25519 signature over the sha256 digest of the
	// signed value's canonical encoding.
	Bytes []byte
}

// verifyDigest checks that sig is a valid ed25519 signature, by the embedded
// public key, over the sha256 digest of canonicalBytes, and that digest
// equals the recorded digest hex string. Both checks fail closed: a
// digest that matches under a forged signature, or a signature that verifies
// over the wrong bytes, are both rejected.
func verifyDigest(canonicalBytes []byte, digest string, sig Signature) error {
	if len(sig.PublicKey) != ed25519.PublicKeySize {
		return ErrSignatureKeyMissing
	}
	sum := sha256.Sum256(canonicalBytes)
	gotDigest := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(gotDigest), []byte(digest)) != 1 {
		return fmt.Errorf("%w: recomputed %s, recorded %s", ErrDigestMismatch, gotDigest, digest)
	}
	if !ed25519.Verify(sig.PublicKey, sum[:], sig.Bytes) {
		return ErrSignatureInvalid
	}
	return nil
}
