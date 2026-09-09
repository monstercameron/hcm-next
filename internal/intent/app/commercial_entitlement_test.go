package app

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

func promotionGateSnapshot(t *testing.T, status commercial.ContractStatus) commercial.EntitlementSnapshot {
	t.Helper()
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	snapshot, err := commercial.NewEntitlementSnapshot(commercial.FixedPricePilotContract{
		TenantID: "tenant-a", ContractID: "pilot-a", Revision: 1,
		EffectiveFrom: from, EffectiveTo: from.Add(90 * 24 * time.Hour), Status: status,
		Capabilities: []string{commercial.PromotionEntitlementCapability},
		Bound:        commercial.EntitlementBound{Seats: 25}, PriceCents: 2500000, Currency: "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPromotionEntitlementGatePinsAllowedExecutionAndRefusesOutOfScope(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	snapshot := promotionGateSnapshot(t, commercial.StatusActive)
	gate, err := NewPromotionEntitlementGate(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := gate.Admit(PromotionEntitlementRequest{
		TenantID: "tenant-a", Capability: commercial.PromotionEntitlementCapability,
		At: from, Channel: commercial.ChannelGRPC, Phase: "submit",
	})
	if err != nil || !accepted.Allowed() || accepted.Fingerprint() != snapshot.Fingerprint() {
		t.Fatalf("accepted binding = %+v, %v", accepted, err)
	}
	refused, err := gate.Admit(PromotionEntitlementRequest{
		TenantID: "tenant-a", Capability: commercial.LeaveEntitlementCapability,
		At: from, Channel: commercial.ChannelHTTP, Phase: "resume",
	})
	if err == nil {
		t.Fatal("out-of-scope capability was admitted")
	}
	owned, ok := err.(*envelope.Error)
	if !ok || owned.ReasonRef() != ReasonPromotionEntitlementRequired || owned.Code() != envelope.CodeFailedPrecondition {
		t.Fatalf("refusal = %T %v, want typed commercial refusal", err, err)
	}
	if refused.Decision.Code != commercial.CodeCapabilityOutOfScope || refused.Fingerprint() != snapshot.Fingerprint() {
		t.Fatalf("refused binding = %+v, want denial fingerprint", refused)
	}
	if owned.EvidenceRef().Digest != snapshot.Fingerprint() {
		t.Fatalf("refusal evidence digest = %q, want %q", owned.EvidenceRef().Digest, snapshot.Fingerprint())
	}
}

func TestPromotionEntitlementGateRefusesSuspendedAndExpiredSnapshots(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		status commercial.ContractStatus
		at     time.Time
		want   commercial.EntitlementCode
	}{
		{name: "suspended", status: commercial.StatusSuspended, at: from, want: commercial.CodeContractSuspended},
		{name: "expired", status: commercial.StatusActive, at: from.Add(90 * 24 * time.Hour), want: commercial.CodeContractExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gate, err := NewPromotionEntitlementGate(promotionGateSnapshot(t, tc.status))
			if err != nil {
				t.Fatal(err)
			}
			decision := gate.Decide(PromotionEntitlementRequest{TenantID: "tenant-a", Capability: commercial.PromotionEntitlementCapability, At: tc.at, Channel: commercial.ChannelUI})
			if decision.Code != tc.want {
				t.Fatalf("decision = %+v, want %q", decision, tc.want)
			}
			if _, err := gate.Admit(PromotionEntitlementRequest{TenantID: "tenant-a", Capability: commercial.PromotionEntitlementCapability, At: tc.at, Channel: commercial.ChannelUI}); err == nil {
				t.Fatal("ineligible snapshot was admitted")
			}
		})
	}
}
