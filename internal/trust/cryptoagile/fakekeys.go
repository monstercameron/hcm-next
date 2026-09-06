package cryptoagile

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
)

// FakeKeySource is the in-memory [SignerPort]/[VerifierPort] test double: it
// holds its own crypto/ed25519 or crypto/hmac key material and never talks
// to a real key custody provider. Production code uses [CustodyKeySource]
// instead; this is what "keys through the custody port or a test fake"
// (the todo's own words) means in this package.
type FakeKeySource struct {
	edPriv map[string]ed25519.PrivateKey
	edPub  map[string]ed25519.PublicKey
	hmac   map[string][]byte
}

// NewFakeKeySource returns a FakeKeySource with no registered suites.
func NewFakeKeySource() *FakeKeySource {
	return &FakeKeySource{
		edPriv: make(map[string]ed25519.PrivateKey),
		edPub:  make(map[string]ed25519.PublicKey),
		hmac:   make(map[string][]byte),
	}
}

// AddEd25519 registers suiteID as an Ed25519 signature suite derived from
// seed, which must be exactly ed25519.SeedSize bytes. Ed25519 signing is
// deterministic given the seed, which is what makes a fixed seed usable in
// a Golden test: the same seed and message always produce the same
// signature bytes.
func (f *FakeKeySource) AddEd25519(suiteID string, seed []byte) error {
	if len(seed) != ed25519.SeedSize {
		return fmt.Errorf("cryptoagile: ed25519 seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	f.edPriv[suiteID] = priv
	f.edPub[suiteID] = priv.Public().(ed25519.PublicKey)
	return nil
}

// AddHMAC registers suiteID as an HMAC-SHA256 MAC suite with key.
func (f *FakeKeySource) AddHMAC(suiteID string, key []byte) error {
	if len(key) == 0 {
		return fmt.Errorf("cryptoagile: hmac key must not be empty")
	}
	cp := make([]byte, len(key))
	copy(cp, key)
	f.hmac[suiteID] = cp
	return nil
}

// Sign implements [SignerPort].
func (f *FakeKeySource) Sign(suiteID string, message []byte) ([]byte, error) {
	if priv, ok := f.edPriv[suiteID]; ok {
		return ed25519.Sign(priv, message), nil
	}
	if key, ok := f.hmac[suiteID]; ok {
		mac := hmac.New(sha256.New, key)
		mac.Write(message)
		return mac.Sum(nil), nil
	}
	return nil, fmt.Errorf("cryptoagile: fake key source has no key for suite %q", suiteID)
}

// Verify implements [VerifierPort].
func (f *FakeKeySource) Verify(suiteID string, message, signature []byte) (bool, error) {
	if pub, ok := f.edPub[suiteID]; ok {
		return ed25519.Verify(pub, message, signature), nil
	}
	if key, ok := f.hmac[suiteID]; ok {
		mac := hmac.New(sha256.New, key)
		mac.Write(message)
		return hmac.Equal(mac.Sum(nil), signature), nil
	}
	return false, fmt.Errorf("cryptoagile: fake key source has no key for suite %q", suiteID)
}
