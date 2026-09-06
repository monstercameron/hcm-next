package configbundle

import (
	"crypto/ed25519"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

func cp005Signer(t *testing.T) (*Ed25519ReceiptSigner, ed25519.PublicKey) {
	t.Helper()
	var seed [ed25519.SeedSize]byte
	for i := range seed {
		seed[i] = byte(41 + i)
	}
	private := ed25519.NewKeyFromSeed(seed[:])
	signer, err := NewEd25519ReceiptSigner("cp005-receipts", "v1", private)
	if err != nil {
		t.Fatal(err)
	}
	return signer, private.Public().(ed25519.PublicKey)
}

func cp005Digest(value string) string { return canonicalbytes.Digest([]byte(value)) }

// TestTodo_CP_005 proves signed application evidence, idempotent recording,
// and an adoption watermark that remains pending until all consumers match.
func TestTodo_CP_005(t *testing.T) {
	signer, publicKey := cp005Signer(t)
	at := time.Date(2026, 9, 5, 13, 0, 0, 0, time.UTC)
	desired := cp005Digest("desired-v4")
	store := NewReceiptStore(signer, func() time.Time { return at })
	input := ApplicationReceipt{TenantID: "cp005-tenant", CellID: "cell-a", Service: "api", Build: "build-17", DesiredDigest: desired, AppliedDigest: desired, Epoch: 4, Validation: ValidationValid, AppliedAt: at}
	first, err := store.Record(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Verify(publicKey); err != nil {
		t.Fatalf("receipt Verify: %v", err)
	}
	retry, err := store.Record(input)
	if err != nil || retry.Digest != first.Digest {
		t.Fatalf("idempotent retry = %+v, err=%v", retry, err)
	}
	if _, err := store.Record(ApplicationReceipt{TenantID: input.TenantID, CellID: input.CellID, Service: "worker", Build: "build-17", DesiredDigest: desired, AppliedDigest: desired, Epoch: 4, Validation: ValidationValid, AppliedAt: at}); err != nil {
		t.Fatal(err)
	}
	watermark, err := store.Watermark(input.TenantID, input.CellID, desired, 4, []string{"worker", "api"})
	if err != nil {
		t.Fatal(err)
	}
	if watermark.Status != AdoptionComplete || len(watermark.Adopted) != 2 || watermark.Digest == "" {
		t.Fatalf("complete watermark = %+v", watermark)
	}
	if got := store.List(input.TenantID, input.CellID); len(got) != 2 || got[0].Service != "api" || got[1].Service != "worker" {
		t.Fatalf("deterministic receipt list = %+v", got)
	}
}

func TestTodo_CP_005_Golden(t *testing.T) {
	signer, _ := cp005Signer(t)
	at := time.Date(2026, 9, 5, 13, 0, 0, 0, time.UTC)
	desired := cp005Digest("golden-desired")
	store := NewReceiptStore(signer, func() time.Time { return at })
	receipt, err := store.Record(ApplicationReceipt{TenantID: "golden-tenant", CellID: "cell-a", Service: "api", Build: "build-1", DesiredDigest: desired, AppliedDigest: desired, Epoch: 1, Validation: ValidationValid, AppliedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Digest != "sha256:b2ed30733743e94cc5e60b07b1dab2473ee7135b9dccf499ac116eb9ad29eacc" {
		t.Fatalf("receipt digest = %s, want canonical golden digest", receipt.Digest)
	}
	watermark, err := store.Watermark("golden-tenant", "cell-a", desired, 1, []string{"api", "worker"})
	if err != nil {
		t.Fatal(err)
	}
	// The first receipt is intentionally the only adopted consumer in this
	// golden vector. The explicit pending assertion is the contract.
	if watermark.Status != AdoptionPending {
		t.Fatalf("watermark = %+v", watermark)
	}
	if receipt.Explain() == "" || watermark.Explain() == "" {
		t.Fatal("receipt or watermark explanation is empty")
	}
}
