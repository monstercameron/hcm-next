package commercial

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var entitlementAt = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

func fixedPriceRevision(bound EntitlementBound) FixedPricePilotContract {
	return FixedPricePilotContract{
		TenantID:      "tenant-a",
		ContractID:    "pilot-a",
		Revision:      1,
		EffectiveFrom: entitlementAt,
		EffectiveTo:   entitlementAt.Add(90 * 24 * time.Hour),
		Capabilities:  []string{"promotion.simulate", "worker.explain"},
		Bound:         bound,
		PriceCents:    2500000,
		Currency:      "USD",
	}
}

func TestTodo_COMM_001(t *testing.T) {
	snapshot, err := NewEntitlementSnapshot(fixedPriceRevision(EntitlementBound{Seats: 25}))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		in   EntitlementRequest
		want EntitlementCode
	}{
		{"allowed", EntitlementRequest{TenantID: "tenant-a", Capability: "promotion.simulate", At: entitlementAt}, CodeAllowed},
		{"not_yet_effective", EntitlementRequest{TenantID: "tenant-a", Capability: "promotion.simulate", At: entitlementAt.Add(-time.Nanosecond)}, CodeContractNotYetEffective},
		{"expired_at_window_end", EntitlementRequest{TenantID: "tenant-a", Capability: "promotion.simulate", At: entitlementAt.Add(90 * 24 * time.Hour)}, CodeContractExpired},
		{"out_of_scope", EntitlementRequest{TenantID: "tenant-a", Capability: "promotion.execute", At: entitlementAt}, CodeCapabilityOutOfScope},
		{"wrong_tenant_fails_closed", EntitlementRequest{TenantID: "tenant-b", Capability: "promotion.simulate", At: entitlementAt}, CodeCapabilityOutOfScope},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := snapshot.Resolve(tc.in)
			if got.Code != tc.want {
				t.Fatalf("code = %q, want %q", got.Code, tc.want)
			}
			if got.Fingerprint != snapshot.Fingerprint() {
				t.Fatalf("fingerprint = %q, want %q", got.Fingerprint, snapshot.Fingerprint())
			}
		})
	}
	if _, err := NewEntitlementSnapshot(fixedPriceRevision(EntitlementBound{Population: "population:pilot-workers"})); err != nil {
		t.Fatalf("population-bound revision rejected: %v", err)
	}
}

func TestTodo_COMM_001_Golden(t *testing.T) {
	base := fixedPriceRevision(EntitlementBound{Seats: 25})
	first, err := NewEntitlementSnapshot(base)
	if err != nil {
		t.Fatal(err)
	}
	base.Capabilities = []string{"worker.explain", "promotion.simulate"}
	second, err := NewEntitlementSnapshot(base)
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatalf("capability set ordering changed fingerprint: %q != %q", first.Fingerprint(), second.Fingerprint())
	}
	if len(first.Fingerprint()) != 64 {
		t.Fatalf("fingerprint length = %d, want 64", len(first.Fingerprint()))
	}
	if first.Explain() != Explain(first) {
		t.Fatal("package Explain does not delegate to snapshot Explain")
	}
}

func TestTodo_COMM_001_Integration(t *testing.T) {
	snapshot, err := NewEntitlementSnapshot(fixedPriceRevision(EntitlementBound{Seats: 25}))
	if err != nil {
		t.Fatal(err)
	}
	request := EntitlementRequest{TenantID: "tenant-a", Capability: "promotion.execute", At: entitlementAt}
	decisions := []EntitlementDecision{
		NewUIFake(snapshot).Resolve(request),
		NewHTTPFake(snapshot).Resolve(request),
		NewGRPCFake(snapshot).Resolve(request),
		NewWorkflowFake(snapshot).Resolve(request),
		NewConnectorFake(snapshot).Resolve(request),
	}
	for i, got := range decisions[1:] {
		if got.Code != decisions[0].Code || got.Fingerprint != decisions[0].Fingerprint {
			t.Fatalf("channel %d decision = %+v, want %+v", i+1, got, decisions[0])
		}
	}
	if decisions[0].Code != CodeCapabilityOutOfScope {
		t.Fatalf("parity denial code = %q", decisions[0].Code)
	}
}

func TestTodo_COMM_001_Security(t *testing.T) {
	snapshot, err := NewEntitlementSnapshot(fixedPriceRevision(EntitlementBound{Population: "population:pilot-workers"}))
	if err != nil {
		t.Fatal(err)
	}
	copyOfContract := snapshot.Contract()
	copyOfContract.Capabilities[0] = "promotion.execute"
	copyOfContract.Bound.Population = "population:other"
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("defensive contract copy changed snapshot: %v", err)
	}
	snapshot.contract.Capabilities[0] = "promotion.execute"
	if err := snapshot.Validate(); !errors.Is(err, ErrInvalidEntitlementSnapshot) {
		t.Fatalf("tampered snapshot error = %v, want ErrInvalidEntitlementSnapshot", err)
	}
	if strings.Contains(snapshot.Explain(), "population:pilot-workers") {
		t.Fatal("Explain disclosed opaque population reference")
	}
}

func TestTodo_COMM_001_Mutation(t *testing.T) {
	old, err := NewEntitlementSnapshot(fixedPriceRevision(EntitlementBound{Seats: 25}))
	if err != nil {
		t.Fatal(err)
	}
	next := fixedPriceRevision(EntitlementBound{Seats: 50})
	next.Revision = 2
	next.Capabilities = []string{"promotion.execute"}
	amended, err := old.Amend(next)
	if err != nil {
		t.Fatal(err)
	}
	oldDecision := old.Resolve(EntitlementRequest{TenantID: "tenant-a", Capability: "promotion.simulate", At: entitlementAt})
	newDecision := amended.Resolve(EntitlementRequest{TenantID: "tenant-a", Capability: "promotion.simulate", At: entitlementAt})
	if !oldDecision.Allowed() || newDecision.Code != CodeCapabilityOutOfScope {
		t.Fatalf("amendment changed pinned answers: old=%+v new=%+v", oldDecision, newDecision)
	}
	if oldDecision.Fingerprint == newDecision.Fingerprint || amended.Revision() != 2 {
		t.Fatal("amendment did not create a new snapshot revision")
	}
	if _, err := old.Amend(next); err != nil {
		t.Fatalf("reusing a valid amendment should remain independently valid: %v", err)
	}
	badIdentity := next
	badIdentity.TenantID = "tenant-b"
	if _, err := old.Amend(badIdentity); !errors.Is(err, ErrInvalidEntitlementAmendment) {
		t.Fatalf("identity change error = %v, want amendment error", err)
	}
}
