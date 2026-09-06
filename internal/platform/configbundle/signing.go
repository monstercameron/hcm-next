package configbundle

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

// BundleSignature is detached Ed25519 evidence over a bundle's digest. The
// key handle and version are part of the evidence so verification cannot
// silently substitute a rotated key for the key that signed the bundle.
type BundleSignature struct {
	Algorithm  string `json:"algorithm"`
	KeyHandle  string `json:"key_handle"`
	KeyVersion string `json:"key_version"`
	// KeyID is a compatibility spelling for KeyHandle.
	KeyID string `json:"key_id,omitempty"`
	Value string `json:"value"`
}

// Signature is the concise spelling used by callers.
type Signature = BundleSignature

// SignedBundle is a bundle together with public signing evidence. It never
// carries private key material.
type SignedBundle struct {
	Bundle    Bundle          `json:"bundle"`
	Digest    string          `json:"digest"`
	Signature BundleSignature `json:"signature"`
	// These aliases make the signer identity available without decoding the
	// nested signature. SignBundle always sets both forms.
	SignerKeyHandle  string `json:"signer_key_handle,omitempty"`
	SignerKeyVersion string `json:"signer_key_version,omitempty"`
}

// BundleKey is the public trust-directory record for one bundle-signing key.
// Validity is evaluated at the activation request's issued time. A revoked
// key is never accepted for a new activation, even when its signature is
// otherwise mathematically valid.
type BundleKey struct {
	Handle       string
	Version      string
	PublicKey    ed25519.PublicKey
	TrustProfile string
	NotBefore    time.Time
	NotAfter     time.Time
	Revoked      bool
}

// Key is an alias for BundleKey.
type Key = BundleKey

func (k BundleKey) validAt(at time.Time) bool {
	if k.Revoked {
		return false
	}
	if !k.NotBefore.IsZero() && at.Before(k.NotBefore) {
		return false
	}
	if !k.NotAfter.IsZero() && !at.Before(k.NotAfter) {
		return false
	}
	return true
}

// KeyResolver is the provider-neutral trust-directory port used by
// activation. Production code can adapt KMS or a trust service without
// changing this pure package.
type KeyResolver interface {
	ResolveBundleKey(handle, version string) (BundleKey, bool, error)
}

// InMemoryKeyring is a concurrency-safe fake KeyResolver for tests and
// hermetic local control-plane use.
type InMemoryKeyring struct {
	mu   sync.RWMutex
	keys map[string]BundleKey
}

// NewKeyring returns an empty in-memory bundle-signing keyring.
func NewKeyring() *InMemoryKeyring {
	return &InMemoryKeyring{keys: make(map[string]BundleKey)}
}

func keyringKey(handle, version string) string { return handle + "\x00" + version }

// Put records one public key version. Replacing a key version is refused so
// a resolver cannot change the meaning of historical signing evidence.
func (r *InMemoryKeyring) Put(key BundleKey) error {
	if r == nil || strings.TrimSpace(key.Handle) == "" || strings.TrimSpace(key.Version) == "" || len(key.PublicKey) != ed25519.PublicKeySize {
		return &Error{Code: "INVALID_SIGNING_KEY", Cause: ErrSigningRefused, Detail: "key handle, key version, and a standard Ed25519 public key are required"}
	}
	key.Handle = strings.TrimSpace(key.Handle)
	key.Version = strings.TrimSpace(key.Version)
	key.PublicKey = append(ed25519.PublicKey(nil), key.PublicKey...)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.keys == nil {
		r.keys = make(map[string]BundleKey)
	}
	k := keyringKey(key.Handle, key.Version)
	if prior, ok := r.keys[k]; ok {
		if string(prior.PublicKey) != string(key.PublicKey) || prior.TrustProfile != key.TrustProfile || !prior.NotBefore.Equal(key.NotBefore) || !prior.NotAfter.Equal(key.NotAfter) || prior.Revoked != key.Revoked {
			return &Error{Code: "SIGNING_KEY_CONFLICT", Cause: ErrSigningRefused, Detail: "a key version is immutable once recorded"}
		}
		return nil
	}
	r.keys[k] = key
	return nil
}

// Revoke marks a known key version unusable for future activations.
func (r *InMemoryKeyring) Revoke(handle, version string) error {
	if r == nil {
		return ErrSigningRefused
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key, ok := r.keys[keyringKey(strings.TrimSpace(handle), strings.TrimSpace(version))]
	if !ok {
		return &Error{Code: "UNKNOWN_SIGNING_KEY", Cause: ErrUnknownSigningKey, Detail: "signing key is not in the trust directory"}
	}
	key.Revoked = true
	r.keys[keyringKey(key.Handle, key.Version)] = key
	return nil
}

// ResolveBundleKey implements KeyResolver and returns defensive key bytes.
func (r *InMemoryKeyring) ResolveBundleKey(handle, version string) (BundleKey, bool, error) {
	if r == nil {
		return BundleKey{}, false, ErrUnknownSigningKey
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	key, ok := r.keys[keyringKey(strings.TrimSpace(handle), strings.TrimSpace(version))]
	if !ok {
		return BundleKey{}, false, nil
	}
	key.PublicKey = append(ed25519.PublicKey(nil), key.PublicKey...)
	return key, true, nil
}

// SignBundle validates bundle identity and signs its recorded digest. The
// variadic form accepts the CP-003 shape (key handle, key version, private
// key) and the older two-argument convenience shape (key handle, private
// key), using key version "v1" for the latter.
func SignBundle(bundle Bundle, keyHandle string, keyVersionAndPrivate ...any) (SignedBundle, error) {
	keyVersion, privateKey, err := signingArguments(keyVersionAndPrivate)
	if err != nil {
		return SignedBundle{}, err
	}
	if strings.TrimSpace(keyHandle) == "" || strings.TrimSpace(keyVersion) == "" {
		return SignedBundle{}, &Error{Code: "INVALID_SIGNING_KEY", Cause: ErrSigningRefused, Detail: "key handle and key version are required"}
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return SignedBundle{}, &Error{Code: "INVALID_SIGNING_KEY", Cause: ErrSigningRefused, Detail: "private key has the wrong size"}
	}
	if err := bundle.Verify(); err != nil {
		return SignedBundle{}, err
	}
	digestBytes, err := digestBytes(bundle.Digest)
	if err != nil {
		return SignedBundle{}, err
	}
	value := ed25519.Sign(privateKey, digestBytes)
	sig := BundleSignature{Algorithm: "ed25519", KeyHandle: strings.TrimSpace(keyHandle), KeyVersion: strings.TrimSpace(keyVersion), KeyID: strings.TrimSpace(keyHandle), Value: hex.EncodeToString(value)}
	return SignedBundle{Bundle: bundle, Digest: bundle.Digest, Signature: sig, SignerKeyHandle: sig.KeyHandle, SignerKeyVersion: sig.KeyVersion}, nil
}

func signingArguments(args []any) (string, ed25519.PrivateKey, error) {
	if len(args) == 1 {
		privateKey, ok := args[0].(ed25519.PrivateKey)
		if !ok {
			return "", nil, &Error{Code: "INVALID_SIGNING_KEY", Cause: ErrSigningRefused, Detail: "signing argument is not an Ed25519 private key"}
		}
		return "v1", privateKey, nil
	}
	if len(args) != 2 {
		return "", nil, &Error{Code: "INVALID_SIGNING_REQUEST", Cause: ErrSigningRefused, Detail: "want key version and Ed25519 private key"}
	}
	version, ok := args[0].(string)
	if !ok {
		return "", nil, &Error{Code: "INVALID_SIGNING_REQUEST", Cause: ErrSigningRefused, Detail: "key version must be a string"}
	}
	privateKey, ok := args[1].(ed25519.PrivateKey)
	if !ok {
		return "", nil, &Error{Code: "INVALID_SIGNING_KEY", Cause: ErrSigningRefused, Detail: "signing argument is not an Ed25519 private key"}
	}
	return version, privateKey, nil
}

// SignBundleWithKey is the typed alternative to SignBundle's compatibility
// form.
func SignBundleWithKey(bundle Bundle, keyHandle, keyVersion string, privateKey ed25519.PrivateKey) (SignedBundle, error) {
	return SignBundle(bundle, keyHandle, keyVersion, privateKey)
}

// VerifyBundleSignature verifies bundle integrity first and then verifies the
// detached signature over the raw digest bytes. The key identity is checked
// by the activation verifier against its KeyResolver.
func VerifyBundleSignature(signed SignedBundle, publicKey ed25519.PublicKey) error {
	if err := signed.Bundle.Verify(); err != nil {
		return err
	}
	if !strings.EqualFold(signed.Digest, signed.Bundle.Digest) {
		return &Error{Code: "BUNDLE_DIGEST_MISMATCH", Cause: ErrInvalidSignature, Detail: "signed digest differs from bundle digest"}
	}
	if signed.Signature.Algorithm != "ed25519" || len(publicKey) != ed25519.PublicKeySize {
		return &Error{Code: "INVALID_SIGNATURE", Cause: ErrInvalidSignature, Detail: "Ed25519 signature and public key are required"}
	}
	sig, err := hex.DecodeString(signed.Signature.Value)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return &Error{Code: "INVALID_SIGNATURE", Cause: ErrInvalidSignature, Detail: "signature is not a valid Ed25519 encoding"}
	}
	digest, err := digestBytes(signed.Digest)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, digest, sig) {
		return &Error{Code: "INVALID_SIGNATURE", Cause: ErrInvalidSignature, Detail: "Ed25519 verification failed"}
	}
	return nil
}

// VerifySignedBundle is an explicit-name alias for VerifyBundleSignature.
func VerifySignedBundle(signed SignedBundle, publicKey ed25519.PublicKey) error {
	return VerifyBundleSignature(signed, publicKey)
}

func digestBytes(digest string) ([]byte, error) {
	algorithm, encoded, ok := strings.Cut(strings.TrimSpace(digest), ":")
	if !ok || algorithm != "sha256" || encoded == "" {
		return nil, &Error{Code: "INVALID_DIGEST", Cause: ErrInvalidSignature, Detail: "digest must be sha256:hex"}
	}
	decoded, err := hex.DecodeString(encoded)
	if err != nil || len(decoded) != 32 {
		return nil, &Error{Code: "INVALID_DIGEST", Cause: ErrInvalidSignature, Detail: "digest is not a SHA-256 value"}
	}
	return decoded, nil
}

var (
	ErrSigningRefused    = errors.New("configbundle: signing refused")
	ErrInvalidSignature  = errors.New("configbundle: signature is invalid")
	ErrUnknownSigningKey = errors.New("configbundle: signing key is unknown")
	ErrRevokedSigningKey = errors.New("configbundle: signing key is revoked")
)

// CodeOf extends the package's typed refusal classifier to signing and
// activation errors. Existing compilation errors continue to classify as
// before.
func CodeOfSigning(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// ExplainSigning returns a non-sensitive description of the signing
// contract. It deliberately excludes signature bytes and private key data.
func ExplainSigning() string {
	return "configbundle signing v1: ed25519 over the sha256 bundle digest, identified by immutable key handle and version"
}
