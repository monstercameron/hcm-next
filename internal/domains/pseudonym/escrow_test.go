package pseudonym_test

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/domains/pseudonym"
	"github.com/monstercameron/hcm-next/internal/trust/custody"
)

type escrowTestProvider struct {
	mu      sync.Mutex
	decrypt []custody.Handle
}

func (p *escrowTestProvider) Encrypt(ctx custody.Context, object custody.Handle, plaintext []byte) (custody.Ciphertext, custody.Receipt, error) {
	if err := ctx.Validate(); err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	block, err := aes.NewCipher(testKey(object))
	if err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return custody.Ciphertext{}, custody.Receipt{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	for i := range nonce {
		nonce[i] = byte(i + 1)
	}
	return custody.Ciphertext{Handle: object, Algorithm: "test-aes-gcm", Data: append(nonce, gcm.Seal(nil, nonce, plaintext, nil)...)}, custody.Receipt{ID: "encrypt", Handle: object, Operation: custody.Encrypt, At: testNow, ContextDigest: custody.ContextDigest(ctx.RequestContext)}, nil
}

func (p *escrowTestProvider) Decrypt(ctx custody.Context, object custody.Handle, sealed custody.Ciphertext) ([]byte, custody.Receipt, error) {
	if err := ctx.Validate(); err != nil {
		return nil, custody.Receipt{}, err
	}
	p.mu.Lock()
	p.decrypt = append(p.decrypt, object)
	p.mu.Unlock()
	if sealed.Handle != object || len(sealed.Data) < 12 {
		return nil, custody.Receipt{}, errors.New("test provider: key binding failed")
	}
	block, err := aes.NewCipher(testKey(object))
	if err != nil {
		return nil, custody.Receipt{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, custody.Receipt{}, err
	}
	nonce, data := sealed.Data[:gcm.NonceSize()], sealed.Data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, data, nil)
	if err != nil {
		return nil, custody.Receipt{}, err
	}
	return plaintext, custody.Receipt{ID: "decrypt", Handle: object, Operation: custody.Decrypt, At: testNow, ContextDigest: custody.ContextDigest(ctx.RequestContext)}, nil
}

func (p *escrowTestProvider) Sign(custody.Context, custody.Handle, []byte) (custody.Signature, custody.Receipt, error) {
	return custody.Signature{}, custody.Receipt{}, errors.New("test provider: unsupported")
}
func (p *escrowTestProvider) Verify(custody.Context, custody.Handle, []byte, custody.Signature) (bool, custody.Receipt, error) {
	return false, custody.Receipt{}, errors.New("test provider: unsupported")
}
func (p *escrowTestProvider) IssueLease(custody.Context, custody.Handle, custody.Operation, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("test provider: unsupported")
}
func (p *escrowTestProvider) RenewLease(custody.Context, custody.Lease, time.Duration) (custody.Lease, error) {
	return custody.Lease{}, errors.New("test provider: unsupported")
}
func (p *escrowTestProvider) Rotate(custody.Context, custody.Handle) (custody.Handle, custody.Receipt, error) {
	return custody.Handle{}, custody.Receipt{}, errors.New("test provider: unsupported")
}
func (p *escrowTestProvider) Revoke(custody.Context, custody.Handle, string) (custody.Receipt, error) {
	return custody.Receipt{}, errors.New("test provider: unsupported")
}

type recordingDeriver struct {
	inner   custody.KeyDeriver
	allowed custody.Handle
	mu      sync.Mutex
	handles []custody.Handle
}

func (d *recordingDeriver) Derive(ctx custody.Context, handle custody.Handle, label []byte) (custody.DerivedValue, custody.Receipt, error) {
	d.mu.Lock()
	d.handles = append(d.handles, handle)
	d.mu.Unlock()
	if handle != d.allowed {
		return custody.DerivedValue{}, custody.Receipt{}, errors.New("test deriver: handle is not the derivation key")
	}
	return d.inner.Derive(ctx, handle, label)
}

var testNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func testKey(handle custody.Handle) []byte {
	sum := sha256.Sum256([]byte(handle.ID + "\x00" + handle.Version + "\x00" + handle.Tenant + "\x00" + handle.Region))
	return sum[:]
}

func escrowHandles() (custody.Handle, custody.Handle, custody.Handle) {
	return custody.Handle{ID: "derive-key", Kind: custody.Key, Version: "v1", Tenant: "tenant-1", Region: "us-east-1"}, custody.Handle{ID: "escrow-key", Kind: custody.Key, Version: "v1", Tenant: "tenant-1", Region: "us-east-1"}, custody.Handle{ID: "tenant-kek", Kind: custody.Key, Version: "v1", Tenant: "tenant-1", Region: "us-east-1"}
}

func escrowService(t *testing.T) (*pseudonym.EscrowedService, *escrowTestProvider, *recordingDeriver, custody.Handle, custody.Handle) {
	t.Helper()
	derivationKey, escrowKey, tenantKEK := escrowHandles()
	base := custody.NewInMemoryFake(func() time.Time { return testNow })
	deriver := &recordingDeriver{inner: base, allowed: derivationKey}
	provider := &escrowTestProvider{}
	service, err := pseudonym.NewEscrowedService(pseudonym.EscrowConfig{Deriver: deriver, Provider: provider, DerivationKey: derivationKey, EscrowKey: escrowKey, TenantKEKs: []custody.Handle{tenantKEK}, Clock: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	return service, provider, deriver, derivationKey, escrowKey
}

func escrowContext() custody.Context {
	return custody.Context{RequestContext: custody.RequestContext{Workload: "case-worker", Tenant: "tenant-1", Region: "us-east-1", Purpose: "case-intake", Destination: "case"}}
}

// TestTodo_ANON_003 proves separate derivation and escrow custody, dual
// control, bounded purpose/evidence release, and digest-only release events.
func TestTodo_ANON_003(t *testing.T) {
	service, provider, deriver, derivationKey, escrowKey := escrowService(t)
	ctx := escrowContext()
	p, err := service.Generate(ctx, pseudonym.GenerateRequest{Subject: "subject-123", Scope: "program-a", Purpose: "case-intake"})
	if err != nil {
		t.Fatal(err)
	}
	record, ok := service.Escrow().Record(p.ID)
	if !ok || record.Ciphertext.Handle != escrowKey {
		t.Fatalf("record = %+v, ok=%v; want escrow handle", record, ok)
	}
	if record.Ciphertext.Handle == derivationKey || record.Ciphertext.Handle.ID == "tenant-kek" {
		t.Fatalf("mapping is not separately custodied: %+v", record.Ciphertext.Handle)
	}
	if _, _, err := provider.Decrypt(ctx, derivationKey, record.Ciphertext); err == nil {
		t.Fatal("derivation key opened escrow ciphertext")
	}
	for _, handle := range deriver.handles {
		if handle != derivationKey {
			t.Fatalf("deriver saw non-derivation handle: %+v", handle)
		}
	}
	if _, _, err := deriver.Derive(ctx, escrowKey, []byte("escrow-must-not-derive")); err == nil {
		t.Fatal("escrow key derived a pseudonym value")
	}
	if _, _, err := service.Release(ctx, governedRelease(p)); !errors.Is(err, pseudonym.ErrRevelationEvidenceRequired) {
		t.Fatalf("direct Release must refuse without revelation evidence, got %v", err)
	}
	if _, _, err := service.ReleaseWithEvidence(ctx, governedRelease(p), mintRevelationEvidence(t, p, "legal:case-1")); err != nil {
		t.Fatal(err)
	}
	subject, event, err := service.ReleaseWithEvidence(ctx, governedRelease(p), mintRevelationEvidence(t, p, "legal:case-2"))
	if err != nil || subject != "subject-123" || event.Digest == "" || event.Outcome != "released" {
		t.Fatalf("release = %q, %+v, %v", subject, event, err)
	}
	if len(service.Escrow().Events()) != 3 {
		t.Fatalf("events = %+v, want both release attempts", service.Escrow().Events())
	}
	if got := pseudonym.ExplainEscrow(); strings.Contains(got, "subject-123") || strings.Contains(got, "subject") && strings.Contains(got, "mapping") {
		t.Fatalf("ExplainEscrow carries mapping language: %q", got)
	}
}

// TestTodo_ANON_003_Race exercises concurrent release calls against one
// ciphertext record and verifies every successful release is evidenced.
func TestTodo_ANON_003_Race(t *testing.T) {
	service, _, _, _, _ := escrowService(t)
	ctx := escrowContext()
	p, err := service.Generate(ctx, pseudonym.GenerateRequest{Subject: "subject-123", Scope: "program-a", Purpose: "case-intake"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, releaseErr := service.ReleaseWithEvidence(ctx, governedRelease(p), mintRevelationEvidence(t, p, fmt.Sprintf("legal:case-%d", i)))
			errs <- releaseErr
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := len(service.Escrow().Events()); got != 16 {
		t.Fatalf("event count = %d, want 16", got)
	}
}

// TestTodo_ANON_003_Security proves self-approval, missing evidence, excessive
// TTL, same-key construction, and tenant-key reuse are refused.
func TestTodo_ANON_003_Security(t *testing.T) {
	derivationKey, _, tenantKEK := escrowHandles()
	base := custody.NewInMemoryFake(func() time.Time { return testNow })
	provider := &escrowTestProvider{}
	for _, config := range []pseudonym.EscrowConfig{
		{Deriver: base, Provider: provider, DerivationKey: derivationKey, EscrowKey: derivationKey},
		{Deriver: base, Provider: provider, DerivationKey: derivationKey, EscrowKey: tenantKEK, TenantKEKs: []custody.Handle{tenantKEK}},
	} {
		if _, err := pseudonym.NewIdentityEscrow(config); !errors.Is(err, pseudonym.ErrEscrowKeySeparation) {
			t.Fatalf("config = %+v, err = %v", config, err)
		}
	}
	service, _, _, _, _ := escrowService(t)
	ctx := escrowContext()
	p, err := service.Generate(ctx, pseudonym.GenerateRequest{Subject: "subject-123", Scope: "program-a", Purpose: "case-intake"})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []pseudonym.EscrowReleaseRequest{
		{Pseudonym: p, RequestedBy: "alice", EscrowCustodian: "alice", Purpose: "case-intake", EvidenceRef: "ev", TTL: time.Hour},
		{Pseudonym: p, RequestedBy: "alice", EscrowCustodian: "bob", Purpose: "case-intake", TTL: time.Hour},
		{Pseudonym: p, RequestedBy: "alice", EscrowCustodian: "bob", Purpose: "case-intake", EvidenceRef: "ev", TTL: pseudonym.MaxEscrowReleaseTTL + time.Nanosecond},
	} {
		if _, _, err := service.Release(ctx, request); !errors.Is(err, pseudonym.ErrEscrowReleaseDenied) {
			t.Fatalf("request = %+v, err = %v", request, err)
		}
	}
}

// governedRelease is the two-party release request the ANON-004 revelation
// receipt binds to (requester and custodian match revelationRequest).
func governedRelease(p pseudonym.Pseudonym) pseudonym.EscrowReleaseRequest {
	return pseudonym.EscrowReleaseRequest{Pseudonym: p, RequestedBy: "case-worker", EscrowCustodian: "custodian-1", Purpose: "case-intake", TTL: time.Minute}
}

// mintRevelationEvidence authorizes one revelation for p under the test
// policy; distinct legal-basis refs yield distinct single-use receipts.
func mintRevelationEvidence(t testing.TB, p pseudonym.Pseudonym, legalBasis string) pseudonym.RevelationEvidence {
	t.Helper()
	request := revelationRequest(p)
	request.LegalBasisRef = legalBasis
	decision, err := pseudonym.EvaluateRevelation(revelationPolicy(), request)
	if err != nil || !decision.Allowed {
		t.Fatalf("revelation decision = %+v, err = %v", decision, err)
	}
	return decision.Evidence
}
