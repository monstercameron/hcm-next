package application

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

type legalSignerProvider struct {
	key       ed25519.PrivateKey
	returned  custody.Handle
	err       error
	lastCtx   custody.Context
	lastBytes []byte
	algorithm string
	data      []byte
	mutate    bool
}

func (p *legalSignerProvider) Sign(ctx custody.Context, handle custody.Handle, message []byte) (custody.Signature, custody.Receipt, error) {
	p.lastCtx, p.lastBytes = ctx, append([]byte(nil), message...)
	if p.err != nil {
		return custody.Signature{}, custody.Receipt{}, p.err
	}
	toSign := message
	if p.mutate {
		toSign = append(append([]byte(nil), message...), 0)
	}
	data := p.data
	if data == nil {
		data = ed25519.Sign(p.key, toSign)
	}
	algorithm := p.algorithm
	if algorithm == "" {
		algorithm = "Ed25519"
	}
	return custody.Signature{Handle: p.returned, Algorithm: algorithm, Data: data}, custody.Receipt{}, nil
}
func (*legalSignerProvider) Encrypt(custody.Context, custody.Handle, []byte) (custody.Ciphertext, custody.Receipt, error) {
	panic("unused")
}
func (*legalSignerProvider) Decrypt(custody.Context, custody.Handle, custody.Ciphertext) ([]byte, custody.Receipt, error) {
	panic("unused")
}
func (*legalSignerProvider) Verify(custody.Context, custody.Handle, []byte, custody.Signature) (bool, custody.Receipt, error) {
	panic("unused")
}
func (*legalSignerProvider) IssueLease(custody.Context, custody.Handle, custody.Operation, time.Duration) (custody.Lease, error) {
	panic("unused")
}
func (*legalSignerProvider) RenewLease(custody.Context, custody.Lease, time.Duration) (custody.Lease, error) {
	panic("unused")
}
func (*legalSignerProvider) Rotate(custody.Context, custody.Handle) (custody.Handle, custody.Receipt, error) {
	panic("unused")
}
func (*legalSignerProvider) Revoke(custody.Context, custody.Handle, string) (custody.Receipt, error) {
	panic("unused")
}

func legalSignerFixture(t *testing.T) (custody.RequestContext, custody.Handle, ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = 0x6c
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	return custody.RequestContext{Workload: "legal-worker", Tenant: "tenant-a", Region: "us-east-1", Purpose: "legal-evaluation", Destination: "legal-evidence"}, custody.Handle{ID: "legal-key", Kind: custody.Key, Version: "v1", Tenant: "tenant-a", Region: "us-east-1"}, priv, pub
}

func TestTodo_LEGAL_014(t *testing.T) {
	ctx, handle, priv, pub := legalSignerFixture(t)
	p := &legalSignerProvider{key: priv, returned: handle}
	signer, err := NewLegalCustodySigner(p, ctx, handle, pub)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("exact legal receipt bytes")
	digest, sig, err := signer.SignDigestChecked(data)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data)
	if string(p.lastBytes) != string(want[:]) {
		t.Fatalf("provider received %x, want raw sha256 %x", p.lastBytes, want)
	}
	if err := legal.VerifySignature(data, digest, sig); err != nil {
		t.Fatal(err)
	}
	if p.lastCtx.RequestContext != ctx {
		t.Fatalf("context = %+v, want %+v", p.lastCtx, ctx)
	}
}

func TestTodo_LEGAL_014_Golden(t *testing.T) {
	ctx, handle, priv, pub := legalSignerFixture(t)
	p := &legalSignerProvider{key: priv, returned: handle}
	signer, err := NewLegalCustodySigner(p, ctx, handle, pub)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("golden receipt")
	digest, _, err := signer.SignDigestChecked(data)
	if err != nil {
		t.Fatal(err)
	}
	const wantDigest = "f9ee387d99b2cfa0f7309811227a092aada653e7bf4e022e4a9191049013dfea"
	const wantSignature = "a4a3b6fc6ee685d31a9fff58ea2909861c42eef0736445ad5186bd9e97d49610b05826fb520b84daeac62c709119226581caad7f37b80c995849b9aeeb70a006"
	if digest != wantDigest {
		t.Fatalf("digest = %q, want %q", digest, wantDigest)
	}
	_, sig, err := signer.SignDigestChecked(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(sig.Bytes); got != wantSignature {
		t.Fatalf("signature = %q, want %q", got, wantSignature)
	}
}

func TestTodo_LEGAL_014_Security(t *testing.T) {
	ctx, handle, priv, pub := legalSignerFixture(t)
	p := &legalSignerProvider{key: priv, returned: handle}
	if _, err := newLegalCustodySignerPort(p, ctx, custody.Handle{ID: "x", Kind: custody.Secret, Version: "v1", Tenant: ctx.Tenant, Region: ctx.Region}, pub); !errors.Is(err, ErrLegalCustodySignerKind) {
		t.Fatal(err)
	}
	wrong := handle
	wrong.Tenant = "tenant-b"
	if _, err := newLegalCustodySignerPort(p, ctx, wrong, pub); !errors.Is(err, ErrLegalCustodySignerScope) {
		t.Fatal(err)
	}
	p.returned = wrong
	signer, err := NewLegalCustodySigner(p, ctx, handle, pub)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = signer.SignDigestChecked([]byte("x")); !errors.Is(err, ErrLegalCustodySignerHandle) {
		t.Fatal(err)
	}
	p.returned = handle
	p.algorithm = "ECDSA-P256-SHA256"
	if _, _, err = signer.SignDigestChecked([]byte("x")); !errors.Is(err, ErrLegalCustodySignerAlgorithm) {
		t.Fatalf("mislabelled algorithm = %v", err)
	}
	p.algorithm = "Ed25519"
	p.data = make([]byte, ed25519.SignatureSize-1)
	if _, _, err = signer.SignDigestChecked([]byte("x")); !errors.Is(err, ErrLegalCustodySignerResult) {
		t.Fatalf("malformed signature length = %v", err)
	}
	p.data = nil
	_, attacker, _ := ed25519.GenerateKey(nil)
	p.key = attacker
	if _, _, err = signer.SignDigestChecked([]byte("x")); !errors.Is(err, legal.ErrSignatureInvalid) {
		t.Fatalf("wrong-key signature = %v", err)
	}
	p.key = priv
	p.mutate = true
	if _, _, err = signer.SignDigestChecked([]byte("x")); !errors.Is(err, legal.ErrSignatureInvalid) {
		t.Fatalf("mutated digest bytes = %v", err)
	}
	var nilProvider *legalSignerProvider
	if _, err := NewLegalCustodySigner(nilProvider, ctx, handle, pub); !errors.Is(err, ErrLegalCustodySignerProvider) {
		t.Fatalf("typed-nil provider = %v", err)
	}
}

func TestTodo_LEGAL_014_Fault(t *testing.T) {
	ctx, handle, priv, pub := legalSignerFixture(t)
	want := errors.New("provider unavailable at kms://private-locator/legal-key")
	p := &legalSignerProvider{key: priv, returned: handle, err: want}
	signer, err := NewLegalCustodySigner(p, ctx, handle, pub)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = signer.SignDigestChecked([]byte("x")); !errors.Is(err, want) {
		t.Fatal(err)
	} else if strings.Contains(err.Error(), "private-locator") || strings.Contains(err.Error(), handle.ID) {
		t.Fatalf("provider error exposed custody locator or handle: %q", err)
	}
}

func TestTodo_LEGAL_014_Recovery(t *testing.T) {
	ctx, handle, priv, pub := legalSignerFixture(t)
	provider := &legalSignerProvider{key: priv, returned: handle}
	first, err := NewLegalCustodySigner(provider, ctx, handle, pub)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("receipt retained across signer reconstruction")
	digest, signature, err := first.SignDigestChecked(data)
	if err != nil {
		t.Fatal(err)
	}
	first = nil
	if _, err := NewLegalCustodySigner(provider, ctx, handle, pub); err != nil {
		t.Fatalf("reconstruct signer bridge: %v", err)
	}
	if err := legal.VerifySignature(data, digest, signature); err != nil {
		t.Fatalf("offline verification after bridge reconstruction: %v", err)
	}
}
