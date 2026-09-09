package provenance

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

// KeySource resolves the Ed25519 signing operation used by provenance.
// Implementations return only a public trust anchor and a signature; private
// key material is intentionally absent from this port.
type KeySource interface {
	PublicKey() string
	SignDigest(digestHex string) (string, error)
}

// FixtureKeySource is the development-only KeySource backed by an explicit
// fixture private key. It preserves the existing fixture signing path while
// making the source of the signing operation explicit to callers.
type FixtureKeySource struct {
	private ed25519.PrivateKey
	label   string
}

// NewFixtureKeySource creates a fixture KeySource from an already validated
// Ed25519 private key. The private key is copied so later caller mutation
// cannot alter this source.
func NewFixtureKeySource(private ed25519.PrivateKey, label string) (*FixtureKeySource, error) {
	if len(private) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("provenance: fixture key has %d bytes, want %d", len(private), ed25519.PrivateKeySize)
	}
	copyKey := append(ed25519.PrivateKey(nil), private...)
	return &FixtureKeySource{private: copyKey, label: strings.TrimSpace(label)}, nil
}

// LoadFixtureKeySource loads the checked-in development fixture into a
// KeySource. The fixture remains explicitly non-operational test material.
func LoadFixtureKeySource(path string) (KeySource, error) {
	private, err := LoadSigningKeyFixture(path)
	if err != nil {
		return nil, err
	}
	return NewFixtureKeySource(private, path)
}

func (s *FixtureKeySource) PublicKey() string {
	if s == nil || len(s.private) != ed25519.PrivateKeySize {
		return ""
	}
	return hex.EncodeToString(s.private[ed25519.SeedSize:])
}

func (s *FixtureKeySource) SignDigest(digestHex string) (string, error) {
	if s == nil || len(s.private) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("provenance: fixture key source is not initialized")
	}
	return SignDigest(s.private, digestHex)
}

// CustodyKeySource adapts the provider-neutral custody signing port to the
// provenance KeySource. PublicKey is supplied by the custody ceremony and is
// safe to persist; the provider owns all private key material.
type CustodyKeySource struct {
	provider custody.Provider
	context  custody.Context
	handle   custody.Handle
	public   string
}

// NewCustodyKeySource constructs a custody-backed source for one versioned
// key handle. publicKeyHex is the custody-issued Ed25519 public key pinned by
// release admission; it is not private key material.
func NewCustodyKeySource(provider custody.Provider, context custody.Context, handle custody.Handle, publicKeyHex string) (*CustodyKeySource, error) {
	if provider == nil {
		return nil, fmt.Errorf("provenance: custody key source requires a provider")
	}
	if err := context.Validate(); err != nil {
		return nil, fmt.Errorf("provenance: custody key source context: %w", err)
	}
	if err := handle.Validate(); err != nil {
		return nil, fmt.Errorf("provenance: custody key source handle: %w", err)
	}
	if handle.Kind != custody.Key {
		return nil, fmt.Errorf("provenance: custody key source handle.kind: want KEY")
	}
	if handle.Tenant != context.Tenant || handle.Region != context.Region {
		return nil, fmt.Errorf("provenance: custody key source handle.tenant/region: scope mismatch")
	}
	publicKeyHex = strings.ToLower(strings.TrimSpace(publicKeyHex))
	publicKey, err := hex.DecodeString(publicKeyHex)
	if err != nil {
		return nil, fmt.Errorf("provenance: custody key source public_key: %w", err)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("provenance: custody key source public_key: got %d bytes, want %d", len(publicKey), ed25519.PublicKeySize)
	}
	return &CustodyKeySource{provider: provider, context: context, handle: handle, public: publicKeyHex}, nil
}

func (s *CustodyKeySource) PublicKey() string {
	if s == nil {
		return ""
	}
	return s.public
}

func (s *CustodyKeySource) SignDigest(digestHex string) (string, error) {
	if s == nil || s.provider == nil {
		return "", fmt.Errorf("provenance: custody key source is not initialized")
	}
	digest, err := hex.DecodeString(digestHex)
	if err != nil {
		return "", fmt.Errorf("provenance: custody digest: %w", err)
	}
	if len(digest) != sha256DigestSize {
		return "", fmt.Errorf("provenance: custody digest: got %d bytes, want %d", len(digest), sha256DigestSize)
	}
	signature, _, err := s.provider.Sign(s.context, s.handle, digest)
	if err != nil {
		return "", fmt.Errorf("provenance: custody sign: %w", err)
	}
	if signature.Handle != s.handle {
		return "", fmt.Errorf("provenance: custody signature.handle: returned handle does not match")
	}
	if signature.Algorithm != AlgorithmEd25519 {
		return "", fmt.Errorf("provenance: custody signature.algorithm: got %q, want %q", signature.Algorithm, AlgorithmEd25519)
	}
	if len(signature.Data) != ed25519.SignatureSize {
		return "", fmt.Errorf("provenance: custody signature.value: got %d bytes, want %d", len(signature.Data), ed25519.SignatureSize)
	}
	return hex.EncodeToString(signature.Data), nil
}

const sha256DigestSize = 32

// SignStatementWithKeySource signs a statement through a resolved KeySource.
// The optional label is descriptive evidence only and never contains key
// material.
func SignStatementWithKeySource(source KeySource, statement Statement, label string) (Statement, error) {
	if source == nil {
		return Statement{}, fmt.Errorf("provenance: key source is required")
	}
	publicKey := strings.ToLower(strings.TrimSpace(source.PublicKey()))
	publicBytes, err := hex.DecodeString(publicKey)
	if err != nil || len(publicBytes) != ed25519.PublicKeySize {
		return Statement{}, fmt.Errorf("provenance: key source public_key: invalid Ed25519 public key")
	}
	digest, err := statement.CanonicalDigest()
	if err != nil {
		return Statement{}, err
	}
	signature, err := source.SignDigest(digest)
	if err != nil {
		return Statement{}, err
	}
	if decoded, err := hex.DecodeString(signature); err != nil || len(decoded) != ed25519.SignatureSize {
		return Statement{}, fmt.Errorf("provenance: key source signature.value: invalid Ed25519 signature")
	}
	signed := statement
	signed.Signature = &Signature{Algorithm: AlgorithmEd25519, PublicKey: publicKey, Value: signature, KeyFixture: strings.TrimSpace(label)}
	return signed, nil
}

// Explain describes the key-source boundary without exposing private key
// material or account, tenant, or provider identifiers.
func (s *CustodyKeySource) Explain() string {
	return "provenance signing resolves an Ed25519 key through custody and emits only a pinned public key and signature"
}
