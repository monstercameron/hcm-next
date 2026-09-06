package commercial

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestContractRevisionStoreMemoryPreservesCASAndSnapshotIdentity(t *testing.T) {
	store := NewMemoryStore()
	contract := fixedPriceTestContract("tenant-memory", 1)
	if err := store.PutContractRevision(context.Background(), contract); err != nil {
		t.Fatal(err)
	}
	if err := store.PutContractRevision(context.Background(), contract); !errors.Is(err, ErrStoreDuplicateRevision) {
		t.Fatalf("duplicate error = %v", err)
	}
	next := contract
	next.Revision = 3
	if err := store.PutContractRevision(context.Background(), next, 1); !errors.Is(err, ErrStoreStaleCAS) {
		t.Fatalf("gap error = %v", err)
	}
	snapshot, err := NewEntitlementSnapshot(contract)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutEntitlementSnapshot(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetEntitlementSnapshot(context.Background(), contract.TenantID, contract.ContractID, contract.Revision)
	if err != nil || got.Fingerprint() != snapshot.Fingerprint() {
		t.Fatalf("snapshot = %v, %v", got, err)
	}
}

func fixedPriceTestContract(tenant string, revision uint64) FixedPricePilotContract {
	return FixedPricePilotContract{
		TenantID: tenant, ContractID: "contract-1", Revision: revision,
		EffectiveFrom: testTime(), EffectiveTo: testTime().Add(24 * 60 * 60 * 1e9),
		Capabilities: []string{"promotion.simulate"}, Bound: EntitlementBound{Seats: 5}, PriceCents: 1500000, Currency: "USD",
	}
}

func testTime() (t time.Time) { return time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC) }
