package commercial_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/commercial"
)

func TestEntitlementGateReturnsTypedRefusalWithPinnedFingerprint(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	snapshot, err := commercial.NewEntitlementSnapshot(commercial.FixedPricePilotContract{
		TenantID: "tenant-a", ContractID: "pilot-a", Revision: 1,
		EffectiveFrom: from, EffectiveTo: from.Add(90 * 24 * time.Hour),
		Capabilities: []string{commercial.PromotionEntitlementCapability},
		Bound:        commercial.EntitlementBound{Seats: 25}, PriceCents: 2500000, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	gate, err := commercial.NewGate(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := gate.Admit(commercial.NewEntitlementRequest(
		"tenant-a", commercial.LeaveEntitlementCapability, from, commercial.ChannelWorkflow))
	if err == nil {
		t.Fatal("out-of-scope Leave capability was admitted")
	}
	var refusal *commercial.EntitlementRefusal
	if !errors.As(err, &refusal) || !errors.Is(err, commercial.ErrEntitlementDenied) {
		t.Fatalf("error = %T %v, want typed entitlement refusal", err, err)
	}
	if decision.Code != commercial.CodeCapabilityOutOfScope || decision.Fingerprint != snapshot.Fingerprint() {
		t.Fatalf("decision = %+v, want out-of-scope with snapshot fingerprint", decision)
	}
	if refusal.Decision() != decision {
		t.Fatalf("refusal decision = %+v, want %+v", refusal.Decision(), decision)
	}
	if err.Error() != "commercial: entitlement denied" {
		t.Fatalf("refusal leaked decision details: %q", err.Error())
	}
}

func TestSuspendedAndAmendedSnapshotsDoNotRewriteAcceptedDecision(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	base := commercial.FixedPricePilotContract{
		TenantID: "tenant-a", ContractID: "pilot-a", Revision: 1,
		EffectiveFrom: from, EffectiveTo: from.Add(90 * 24 * time.Hour),
		Capabilities: []string{commercial.PromotionEntitlementCapability},
		Bound:        commercial.EntitlementBound{Seats: 25}, PriceCents: 2500000, Currency: "USD",
	}
	old, err := commercial.NewEntitlementSnapshot(base)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := commercial.NewGate(old)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := gate.Admit(commercial.NewEntitlementRequest("tenant-a", commercial.PromotionEntitlementCapability, from, commercial.ChannelUI))
	if err != nil {
		t.Fatal(err)
	}
	next := base
	next.Revision = 2
	next.Status = commercial.StatusSuspended
	amended, err := old.Amend(next)
	if err != nil {
		t.Fatal(err)
	}
	if got := amended.Resolve(commercial.EntitlementRequest{TenantID: "tenant-a", Capability: commercial.PromotionEntitlementCapability, At: from}).Code; got != commercial.CodeContractSuspended {
		t.Fatalf("amended status code = %q, want %q", got, commercial.CodeContractSuspended)
	}
	if accepted.Code != commercial.CodeAllowed || accepted.Fingerprint != old.Fingerprint() || amended.Fingerprint() == old.Fingerprint() {
		t.Fatalf("accepted=%+v old=%s amended=%s", accepted, old.Fingerprint(), amended.Fingerprint())
	}
}
