package asset

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestCustodyStoreSatisfiesRepository(t *testing.T) {
	var repository Repository = NewCustodyStore()
	if repository == nil {
		t.Fatal("repository is nil")
	}
	at := time.Unix(50, 0).UTC()
	inventory := InventoryRevision{InventoryID: assetRef("asset", "repository"), Owner: assetRef("organization", "repository-owner"), Classification: "LAPTOP", SerialNumber: "REPO-1", Revision: assetRev(t, "inventory", 1), EffectiveAt: at, Status: Available}
	if err := repository.RegisterInventory(inventory); err != nil {
		t.Fatal(err)
	}
	event := CustodyRevision{Asset: inventory.InventoryID, Worker: assetRef("worker", "repository-worker"), Location: "HQ", Condition: "GOOD", AssigneeReceipt: assetReceipt(t, "repo-a", true), IssuerReceipt: assetReceipt(t, "repo-i", true), Revision: assetRev(t, "custody", 1), EffectiveAt: at.Add(time.Minute), Status: Assigned}
	if err := repository.AppendCustody(inventory.InventoryID, values.UnspecifiedRevision(), event); err != nil {
		t.Fatal(err)
	}
	current, ok, err := repository.CurrentCustody(inventory.InventoryID)
	if err != nil || !ok || current != event {
		t.Fatalf("current=%+v ok=%v err=%v", current, ok, err)
	}
	history, err := repository.HistoryCustody(inventory.InventoryID)
	if err != nil || len(history) != 1 || history[0] != event {
		t.Fatalf("history=%+v err=%v", history, err)
	}
}
