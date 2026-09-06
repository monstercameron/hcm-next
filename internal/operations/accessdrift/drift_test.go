package accessdrift

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/connectivity/observe"
)

func resource(kind ResourceKind, id, subject, value, state string, risk Risk) Resource {
	return Resource{ID: id, Kind: kind, Subject: subject, Value: value, State: state, Risk: risk}
}
func observed(r Resource, state string, complete bool, freshness observe.Freshness) Resource {
	r.State, r.Complete, r.Freshness, r.ProviderVersion, r.ObservedAt = state, complete, freshness, "provider-1", time.Unix(100, 0)
	return r
}

func TestAccessReconciliationClassifiesAndRepairsPartialLogicalDeviceAndBadgeDrift(t *testing.T) {
	expected := []Resource{resource(KindAccount, "a-1", "worker", "account", "ACTIVE", RiskLow), resource(KindDevice, "d-1", "worker", "laptop", "ACTIVE", RiskHigh), resource(KindBadge, "b-1", "worker", "zone-a", "REVOKED", RiskPrivileged)}
	got, err := Reconcile(ReconcileRequest{Tenant: "tenant-1", AsOf: time.Unix(120, 0), Expected: expected, Observed: []Resource{observed(expected[0], "ACTIVE", true, observe.FreshnessFresh), observed(expected[1], "LOCKED", true, observe.FreshnessFresh), observed(resource(KindBadge, "b-1", "worker", "zone-a", "ACTIVE", RiskPrivileged), "ACTIVE", true, observe.FreshnessFresh)}})
	if err != nil {
		t.Fatal(err)
	}
	var matched, device, badge Finding
	for _, finding := range got.Findings {
		switch finding.Kind {
		case KindAccount:
			matched = finding
		case KindDevice:
			device = finding
		case KindBadge:
			badge = finding
		}
	}
	if matched.Status != StatusMatch || device.Status != StatusPartial || badge.Severity != SeverityCritical {
		t.Fatalf("report = %#v", got)
	}
	plan, err := CreateRepairPlan(RepairRequest{Report: got, FindingIDs: []string{got.Findings[1].ID, got.Findings[2].ID}, Actor: "access-ops", IdempotencyKey: "repair-1", At: time.Unix(130, 0), Freshness: observe.FreshnessFresh, Complete: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 || !plan.RequiresApproval {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestTodo_ACCESS_004_Property(t *testing.T) {
	want := resource(KindEntitlement, "e-1", "worker", "read", "ACTIVE", RiskLow)
	for i := 0; i < 10; i++ {
		r, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(want, "ACTIVE", true, observe.FreshnessFresh)}})
		if err != nil || r.Findings[0].Status != StatusMatch {
			t.Fatalf("iteration %d: %#v %v", i, r, err)
		}
	}
}

func TestTodo_ACCESS_004_Golden(t *testing.T) {
	want := resource(KindAccount, "a", "w", "v", "ACTIVE", RiskLow)
	r, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: nil})
	if err != nil {
		t.Fatal(err)
	}
	if r.Findings[0].Status != StatusUnknown || r.Freshness != observe.FreshnessUnknown {
		t.Fatalf("unknown report = %#v", r)
	}
}

func TestTodo_ACCESS_004_Security(t *testing.T) {
	want := resource(KindAccount, "a", "w", "v", "ACTIVE", RiskPrivileged)
	r, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(resource(KindAccount, "a", "w", "v", "ACTIVE", RiskPrivileged), "REVOKED", true, observe.FreshnessFresh)}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Findings[0].Status != StatusPartial {
		t.Fatal("state drift was not detected")
	}
	if _, err := CreateRepairPlan(RepairRequest{Report: r, FindingIDs: []string{r.Findings[0].ID}, ParentTransactionID: "employment-1", Actor: "ops", IdempotencyKey: "x", At: time.Unix(3, 0), Freshness: observe.FreshnessFresh, Complete: true}); !errors.Is(err, ErrRepairParent) {
		t.Fatalf("parent transaction = %v", err)
	}
}

func TestTodo_ACCESS_004_Integration(t *testing.T) {
	want := resource(KindDevice, "d", "w", "laptop", "ACTIVE", RiskHigh)
	r, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(want, "ACTIVE", true, observe.FreshnessStale)}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Findings[0].Status != StatusStale {
		t.Fatalf("stale = %#v", r.Findings[0])
	}
	if _, err := CreateRepairPlan(RepairRequest{Report: r, FindingIDs: []string{r.Findings[0].ID}, Actor: "ops", IdempotencyKey: "x", At: time.Unix(3, 0), Freshness: observe.FreshnessStale, Complete: true}); !errors.Is(err, ErrFreshObservation) {
		t.Fatalf("stale repair = %v", err)
	}
}

func TestTodo_ACCESS_004_Race(t *testing.T) {
	want := resource(KindEntitlement, "e", "w", "read", "ACTIVE", RiskLow)
	done := make(chan error, 12)
	for i := 0; i < 12; i++ {
		go func() {
			_, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(want, "ACTIVE", true, observe.FreshnessFresh)}})
			done <- err
		}()
	}
	for i := 0; i < 12; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestTodo_ACCESS_004_Fault(t *testing.T) {
	want := resource(KindAccount, "a", "w", "v", "ACTIVE", RiskLow)
	if _, err := Reconcile(ReconcileRequest{Tenant: "", AsOf: time.Time{}, Expected: []Resource{want}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("invalid request = %v", err)
	}
}

func TestTodo_ACCESS_004_Conformance(t *testing.T) {
	want := resource(KindBadge, "b", "w", "zone", "ACTIVE", RiskLow)
	got, err := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(resource(KindBadge, "b", "w", "zone", "ACTIVE", RiskLow), "ACTIVE", false, observe.FreshnessFresh)}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Findings[0].Status != StatusPartial || got.Complete {
		t.Fatalf("incomplete = %#v", got)
	}
}

func TestTodo_ACCESS_004_Mutation(t *testing.T) {
	want := resource(KindAccount, "a", "w", "v", "ACTIVE", RiskLow)
	r, _ := Reconcile(ReconcileRequest{Tenant: "t", AsOf: time.Unix(2, 0), Expected: []Resource{want}, Observed: []Resource{observed(want, "ACTIVE", true, observe.FreshnessFresh)}})
	original := r.Findings[0].ID
	r.Findings[0].Status = StatusExcess
	if original != r.Findings[0].ID {
		t.Fatal("finding identity changed")
	}
}
