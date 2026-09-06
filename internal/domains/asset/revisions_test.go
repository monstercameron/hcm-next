package asset

import (
	"strings"
	"testing"
	"time"
)

func TestAssetRevisionsHaveStableDigestAndRedactedExplanation(t *testing.T) {
	at := time.Unix(20, 0).UTC()
	inventory := InventoryRevision{
		InventoryID: assetRef("asset", "digest-laptop"), Owner: assetRef("organization", "owner"),
		Classification: "LAPTOP", SerialNumber: "SERIAL-PRIVATE", Revision: assetRev(t, "inventory-digest", 1),
		EffectiveAt: at, Status: Available,
	}
	digest, err := inventory.Digest()
	if err != nil || digest == "" || inventory.Canonical() == nil {
		t.Fatalf("inventory canonical/digest = %q/%v", digest, err)
	}
	explanation, err := inventory.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if explanation.Digest != digest || strings.Contains(explanation.Digest, inventory.SerialNumber) {
		t.Fatalf("inventory explanation = %+v", explanation)
	}

	custody := CustodyRevision{
		Asset: inventory.InventoryID, Worker: assetRef("worker", "custody-worker"), Location: "HQ-PRIVATE",
		Condition: "private condition narrative", AssigneeReceipt: assetReceipt(t, "assignee", true),
		IssuerReceipt: assetReceipt(t, "issuer", true), Revision: assetRev(t, "custody-digest", 1),
		EffectiveAt: at, Status: Assigned,
	}
	custodyDigest, err := custody.Digest()
	if err != nil || custodyDigest == "" || custody.Canonical() == nil {
		t.Fatalf("custody canonical/digest = %q/%v", custodyDigest, err)
	}
	custodyExplanation, err := custody.Explain()
	if err != nil {
		t.Fatal(err)
	}
	if custodyExplanation.Digest != custodyDigest || !custodyExplanation.AssigneeVerified || !custodyExplanation.IssuerVerified {
		t.Fatalf("custody explanation = %+v", custodyExplanation)
	}
	if strings.Contains(custodyExplanation.Digest, "private condition narrative") || strings.Contains(custodyExplanation.Digest, "HQ-PRIVATE") {
		t.Fatal("custody explanation repeated sensitive values")
	}

	summary, err := Explain(inventory, []CustodyRevision{custody})
	if err != nil || summary.CurrentStatus != Assigned || summary.CustodyEvents != 1 || summary.Digest == "" {
		t.Fatalf("asset explanation = %+v, err=%v", summary, err)
	}
}
