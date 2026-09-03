package ledger_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/engines/wire/digest"
	"github.com/monstercameron/hcm-next/internal/ledger"
)

// TestKernelDigesterComputesAndVerifies proves the item-3 wiring: appending
// through a ledger.KernelDigester produces a digest computed by
// internal/engines/wire/digest under LedgerEventProfileV1, and VerifyEvent
// recomputes that digest from a stored event's schema reference and payload
// rather than trusting the recorded value - so a tampered payload or a
// tampered digest is caught, and an untouched replay verifies clean.
func TestKernelDigesterComputesAndVerifies(t *testing.T) {
	registry, err := ledger.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatalf("NewLedgerEventDigestRegistry: %v", err)
	}
	d := ledger.NewKernelDigester(registry)

	algorithm, digestHex, length, err := d.Digest([]byte("promotion-proposed"), "hcmnext.intents.v1.BusinessIntent@1")
	if err != nil {
		t.Fatalf("Digest: %v", err)
	}
	if algorithm != "sha256" {
		t.Fatalf("algorithm = %s, want sha256", algorithm)
	}
	if digestHex == "" || length == 0 {
		t.Fatalf("digest = %q length = %d, want non-empty/non-zero", digestHex, length)
	}

	// Determinism: the same schema and payload always digest identically.
	algorithm2, digestHex2, length2, err := d.Digest([]byte("promotion-proposed"), "hcmnext.intents.v1.BusinessIntent@1")
	if err != nil {
		t.Fatalf("Digest (repeat): %v", err)
	}
	if algorithm2 != algorithm || digestHex2 != digestHex || length2 != length {
		t.Fatal("Digest is not deterministic for the same schema and payload")
	}

	// A different payload digests differently.
	_, otherDigest, _, err := d.Digest([]byte("something-else"), "hcmnext.intents.v1.BusinessIntent@1")
	if err != nil {
		t.Fatalf("Digest (other payload): %v", err)
	}
	if otherDigest == digestHex {
		t.Fatal("two distinct payloads produced the same digest")
	}

	rec := ledger.EventRecord{
		StreamKey:       "worker:1",
		Sequence:        1,
		EventID:         uuid.New(),
		SchemaRef:       "hcmnext.intents.v1.BusinessIntent@1",
		Payload:         []byte("promotion-proposed"),
		CanonicalLength: length,
		Digest:          digestHex,
		DigestAlgorithm: algorithm,
		RecordedAt:      time.Now().UTC(),
	}
	if err := d.VerifyEvent(rec); err != nil {
		t.Fatalf("VerifyEvent on an untouched record: %v", err)
	}

	tampered := rec
	tampered.Payload = []byte("tampered-payload")
	if err := d.VerifyEvent(tampered); err == nil {
		t.Fatal("expected VerifyEvent to reject a tampered payload")
	}

	tamperedDigest := rec
	tamperedDigest.Digest = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := d.VerifyEvent(tamperedDigest); err == nil {
		t.Fatal("expected VerifyEvent to reject a tampered recorded digest")
	}
}

// TestLedgerEventProfileIsVersionedAndImmutable proves that
// LedgerEventProfileV1 registers exactly once: a second registration attempt
// under the same registry fails, matching every other canonicalization
// profile in the platform (internal/engines/wire/digest: "Published profile
// versions are immutable").
func TestLedgerEventProfileIsVersionedAndImmutable(t *testing.T) {
	registry, err := ledger.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatalf("NewLedgerEventDigestRegistry: %v", err)
	}
	if err := registry.RegisterProfile(ledger.LedgerEventProfileV1(), digest.ScopeSpec{}); err == nil {
		t.Fatal("expected re-registering the same profile version to fail")
	}
}
