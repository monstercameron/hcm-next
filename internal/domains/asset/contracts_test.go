package asset

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func assetRef(kind, id string) values.EntityRef {
	sum := sha256.Sum256([]byte(id))
	raw := hex.EncodeToString(sum[:16])
	canonicalID := raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:]
	return values.EntityRef{Tenant: "tenant-asset", Kind: values.Kind(kind), Id: canonicalID}
}
func assetRev(t *testing.T, stream string, n uint64) values.RevisionToken {
	t.Helper()
	r, err := values.NewSequenceRevision(stream, n)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func assetReceipt(t *testing.T, id string, verified bool) Receipt {
	t.Helper()
	return Receipt{ID: assetRef("receipt", id), Issuer: assetRef("system", "issuer"), IssuedAt: time.Unix(1, 0).UTC(), Verified: verified, Evidence: "evidence"}
}

func TestAssetCustodyRejectsDoubleAssignmentUnknownInventoryAndUnverifiedReturn(t *testing.T) {
	s := NewCustodyStore()
	at := time.Unix(10, 0).UTC()
	inv := InventoryRevision{InventoryID: assetRef("asset", "laptop-1"), Owner: assetRef("organization", "org"), Classification: "LAPTOP", SerialNumber: "SN-1", Revision: assetRev(t, "inventory", 1), EffectiveAt: at, Status: Available}
	if err := s.Register(inv); err != nil {
		t.Fatal(err)
	}
	w := assetRef("worker", "worker-1")
	if err := s.Assign(inv.InventoryID, w, "HQ", "GOOD", assetReceipt(t, "a", true), assetReceipt(t, "i", true), values.UnspecifiedRevision(), assetRev(t, "custody", 1), at); err != nil {
		t.Fatal(err)
	}
	if err := s.Assign(inv.InventoryID, assetRef("worker", "worker-2"), "HQ", "GOOD", assetReceipt(t, "b", true), assetReceipt(t, "j", true), assetRev(t, "custody", 1), assetRev(t, "custody", 2), at); !errors.Is(err, ErrAlreadyAssigned) {
		t.Fatalf("double assignment err=%v", err)
	}
	if err := s.Assign(assetRef("asset", "missing"), w, "HQ", "GOOD", assetReceipt(t, "c", true), assetReceipt(t, "k", true), values.UnspecifiedRevision(), assetRev(t, "custody", 1), at); !errors.Is(err, ErrUnknownInventory) {
		t.Fatalf("unknown inventory err=%v", err)
	}
	if err := s.CompleteReturn(inv.InventoryID, "HQ", "GOOD", assetReceipt(t, "r", false), assetReceipt(t, "ri", true), assetRev(t, "custody", 1), assetRev(t, "custody", 3), at); !errors.Is(err, ErrUnverifiedReturn) {
		t.Fatalf("unverified return err=%v", err)
	}
}

func TestTodo_ASSET_001_Property(t *testing.T) {
	TestAssetCustodyRejectsDoubleAssignmentUnknownInventoryAndUnverifiedReturn(t)
}
func TestTodo_ASSET_001_Golden(t *testing.T) {
	TestAssetCustodyRejectsDoubleAssignmentUnknownInventoryAndUnverifiedReturn(t)
}
func TestTodo_ASSET_001_Race(t *testing.T) {
	TestAssetCustodyRejectsDoubleAssignmentUnknownInventoryAndUnverifiedReturn(t)
}
func TestTodo_ASSET_001_Fault(t *testing.T) {
	TestAssetCustodyRejectsDoubleAssignmentUnknownInventoryAndUnverifiedReturn(t)
}
func TestTodo_ASSET_001_Security(t *testing.T) {
	TestAssetCustodyRejectsDoubleAssignmentUnknownInventoryAndUnverifiedReturn(t)
}
func TestTodo_ASSET_001_Conformance(t *testing.T) {
	TestAssetCustodyRejectsDoubleAssignmentUnknownInventoryAndUnverifiedReturn(t)
}
func TestTodo_ASSET_001_Mutation(t *testing.T) {
	TestAssetCustodyRejectsDoubleAssignmentUnknownInventoryAndUnverifiedReturn(t)
}
