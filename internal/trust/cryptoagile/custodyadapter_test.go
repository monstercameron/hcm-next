package cryptoagile

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

// fakeCustodyProvider is a minimal custody.Provider: it implements Sign and
// Verify with real crypto/ed25519 keyed by Handle.ID, and every other
// operation returns an error, since CustodyKeySource never calls them. It
// exists only to prove CustodyKeySource wires SignerPort/VerifierPort
// through the custody port shape correctly - it is not a general-purpose
// custody test double.
type fakeCustodyProvider struct {
	keys map[string]ed25519.PrivateKey
}

func newFakeCustodyProvider() *fakeCustodyProvider {
	return &fakeCustodyProvider{keys: make(map[string]ed25519.PrivateKey)}
}

func (p *fakeCustodyProvider) addKey(handleID string, seed byte) {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = seed
	}
	p.keys[handleID] = ed25519.NewKeyFromSeed(s)
}

var errFakeCustodyUnimplemented = errors.New("fakeCustodyProvider: operation not implemented")

func (p *fakeCustodyProvider) Encrypt(custody.Context, custody.Handle, []byte) (custody.Ciphertext, custody.Receipt, error) {
	return custody.Ciphertext{}, custody.Receipt{}, errFakeCustodyUnimplemented
}

func (p *fakeCustodyProvider) Decrypt(custody.Context, custody.Handle, custody.Ciphertext) ([]byte, custody.Receipt, error) {
	return nil, custody.Receipt{}, errFakeCustodyUnimplemented
}

func (p *fakeCustodyProvider) Sign(ctx custody.Context, object custody.Handle, message []byte) (custody.Signature, custody.Receipt, error) {
	if err := ctx.Validate(); err != nil {
		return custody.Signature{}, custody.Receipt{}, err
	}
	priv, ok := p.keys[object.ID]
	if !ok {
		return custody.Signature{}, custody.Receipt{}, fmt.Errorf("fakeCustodyProvider: no key for handle %q", object.ID)
	}
	sig := ed25519.Sign(priv, message)
	return custody.Signature{Handle: object, Algorithm: "ed25519", Data: sig},
		custody.Receipt{ID: "receipt-sign", Handle: object, Operation: custody.Sign, ContextDigest: custody.ContextDigest(ctx.RequestContext), At: time.Now()},
		nil
}

func (p *fakeCustodyProvider) Verify(ctx custody.Context, object custody.Handle, message []byte, signature custody.Signature) (bool, custody.Receipt, error) {
	if err := ctx.Validate(); err != nil {
		return false, custody.Receipt{}, err
	}
	priv, ok := p.keys[object.ID]
	if !ok {
		return false, custody.Receipt{}, fmt.Errorf("fakeCustodyProvider: no key for handle %q", object.ID)
	}
	pub := priv.Public().(ed25519.PublicKey)
	ok = ed25519.Verify(pub, message, signature.Data)
	return ok, custody.Receipt{ID: "receipt-verify", Handle: object, Operation: custody.Verify, ContextDigest: custody.ContextDigest(ctx.RequestContext), At: time.Now()}, nil
}

func (p *fakeCustodyProvider) IssueLease(custody.Context, custody.Handle, custody.Operation, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errFakeCustodyUnimplemented
}

func (p *fakeCustodyProvider) RenewLease(custody.Context, custody.Lease, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errFakeCustodyUnimplemented
}

func (p *fakeCustodyProvider) Rotate(custody.Context, custody.Handle) (custody.Handle, custody.Receipt, error) {
	return custody.Handle{}, custody.Receipt{}, errFakeCustodyUnimplemented
}

func (p *fakeCustodyProvider) Revoke(custody.Context, custody.Handle, string) (custody.Receipt, error) {
	return custody.Receipt{}, errFakeCustodyUnimplemented
}

var _ custody.Provider = (*fakeCustodyProvider)(nil)

func testRequestContext() custody.RequestContext {
	return custody.RequestContext{
		Workload:    "cryptoagile-test",
		Tenant:      "tenant-1",
		Region:      "us-east-1",
		Purpose:     "sign-envelope",
		Destination: "ledger",
	}
}

func testHandle(id string) custody.Handle {
	return custody.Handle{ID: id, Kind: custody.Key, Version: "v1", Tenant: "tenant-1", Region: "us-east-1"}
}

func TestCustodyKeySource_SignVerify(t *testing.T) {
	provider := newFakeCustodyProvider()
	provider.addKey("suite-a", 7)

	ks, err := NewCustodyKeySource(provider, testRequestContext(), map[string]custody.Handle{
		"suite-a": testHandle("suite-a"),
	})
	if err != nil {
		t.Fatalf("NewCustodyKeySource: %v", err)
	}

	sig, err := ks.Sign("suite-a", []byte("payload"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	ok, err := ks.Verify("suite-a", []byte("payload"), sig)
	if err != nil || !ok {
		t.Fatalf("Verify = %v, %v, want true, nil", ok, err)
	}
	ok, err = ks.Verify("suite-a", []byte("tampered"), sig)
	if err != nil || ok {
		t.Fatalf("Verify(tampered) = %v, %v, want false, nil", ok, err)
	}
}

func TestCustodyKeySource_EndToEndThroughEnvelope(t *testing.T) {
	provider := newFakeCustodyProvider()
	provider.addKey("suite-a", 9)
	ks, err := NewCustodyKeySource(provider, testRequestContext(), map[string]custody.Handle{
		"suite-a": testHandle("suite-a"),
	})
	if err != nil {
		t.Fatalf("NewCustodyKeySource: %v", err)
	}

	registry := NewRegistry()
	if err := registry.Register(AlgorithmSuite{ID: "suite-a", Kind: KindSignature, Status: StatusActive, ActivatedAt: day(0)}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	env, err := NewEnvelopeSigner(ks).Sign("suite-a", []byte("m"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := NewEnvelopeVerifier(registry, ks).Verify([]byte("m"), env); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestNewCustodyKeySource_RefusesUnmappedSuite(t *testing.T) {
	provider := newFakeCustodyProvider()
	ks, err := NewCustodyKeySource(provider, testRequestContext(), map[string]custody.Handle{})
	if err != nil {
		t.Fatalf("NewCustodyKeySource: %v", err)
	}
	if _, err := ks.Sign("unmapped", []byte("m")); err == nil {
		t.Fatalf("Sign(unmapped suite) = nil, want error")
	}
}

func TestNewCustodyKeySource_RefusesInvalidInputs(t *testing.T) {
	if _, err := NewCustodyKeySource(nil, testRequestContext(), nil); err == nil {
		t.Fatalf("NewCustodyKeySource(nil provider) = nil, want error")
	}
	provider := newFakeCustodyProvider()
	if _, err := NewCustodyKeySource(provider, custody.RequestContext{}, nil); err == nil {
		t.Fatalf("NewCustodyKeySource(invalid request context) = nil, want error")
	}
	if _, err := NewCustodyKeySource(provider, testRequestContext(), map[string]custody.Handle{"x": {}}); err == nil {
		t.Fatalf("NewCustodyKeySource(invalid handle) = nil, want error")
	}
}
