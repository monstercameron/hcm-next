package subprocessor

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func testInventory(revision uint64, region string) Inventory {
	return Inventory{
		Revision: revision,
		Processors: []Processor{{
			ID: "processor-a", Name: "Processor A", Regions: []string{region}, Purposes: []string{"promotion"}, DataCategories: []string{"employee"}, Retention: "7y", ContractVersion: "2026-01", ExitPlan: "export-revoke-confirm",
		}},
		Flows: []Flow{{ID: "flow-a", TenantID: "tenant-a", IntentID: "promotion.execute", ProcessorID: "processor-a", Region: region, Purpose: "promotion", DataCategories: []string{"employee"}, Retention: "7y"}},
	}
}

func reviewedChange(t *testing.T) Change {
	t.Helper()
	change, err := Diff(testInventory(1, "us"), testInventory(2, "eu"))
	if err != nil {
		t.Fatal(err)
	}
	change.Reviews = Review{Security: true, Privacy: true, Residency: true, Contract: true, Evidence: "evidence:review-2"}
	change.ContractApproved = true
	change.ExitFallback = true
	change.Notice = Notice{ID: "notice-2", IssuedAt: testNow, ObjectionOpens: testNow, ObjectionCloses: testNow.Add(24 * time.Hour), TenantIDs: []string{"tenant-a"}}
	return change
}

func TestSubprocessorChangeRequiresImpactNoticeObjectionWindowAndActivationFence(t *testing.T) {
	change, err := Diff(testInventory(1, "us"), testInventory(2, "eu"))
	if err != nil {
		t.Fatal(err)
	}
	if !change.Material || len(change.Differences) == 0 || len(change.AffectedTenants) != 1 || len(change.AffectedFlows) != 1 || len(change.AffectedIntents) != 1 {
		t.Fatalf("impact graph=%+v", change)
	}
	if _, err := Activate(change, testNow.Add(48*time.Hour)); !errors.Is(err, ErrReviewRequired) {
		t.Fatalf("unreviewed change activated: %v", err)
	}
	change = reviewedChange(t)
	if _, err := Activate(change, testNow.Add(time.Hour)); !errors.Is(err, ErrObjectionWindow) {
		t.Fatalf("open objection window=%v", err)
	}
	got, err := Activate(change, testNow.Add(25*time.Hour))
	if err != nil || !got.Allowed || got.Status != StatusActive {
		t.Fatalf("activation=%+v err=%v", got, err)
	}
}

func TestTodo_SUBPROCESSOR_001_Property(t *testing.T) {
	before := testInventory(1, "us")
	after := testInventory(2, "us")
	if _, err := Diff(before, before); !errors.Is(err, ErrRevision) {
		t.Fatalf("same revision=%v", err)
	}
	change, err := Diff(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if change.Material || len(change.Differences) != 0 {
		t.Fatalf("unchanged inventory became material=%+v", change)
	}
	got, err := Activate(change, testNow)
	if err != nil || !got.Allowed || got.Reason != "NO_MATERIAL_CHANGE" {
		t.Fatalf("immaterial activation=%+v err=%v", got, err)
	}
}

func TestTodo_SUBPROCESSOR_001_Golden(t *testing.T) {
	change := reviewedChange(t)
	if got := Explain(change); !strings.Contains(got, "revision=2") || !strings.Contains(got, "status=MATERIAL") || !strings.Contains(got, "differences=2") {
		t.Fatalf("explanation=%q", got)
	}
	if Version() != 1 || Explain(Change{}) == "" {
		t.Fatal("contract metadata missing")
	}
}

func TestTodo_SUBPROCESSOR_001_Integration(t *testing.T) {
	change := reviewedChange(t)
	change, err := RecordObjection(change, Objection{TenantID: "tenant-a", Reason: "review required"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Activate(change, testNow.Add(25*time.Hour)); !errors.Is(err, ErrObjectionPending) {
		t.Fatalf("pending objection was ignored: %v", err)
	}
	change, err = ResolveObjection(change, "tenant-a", "accepted-with-commitment", testNow.Add(25*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Activate(change, testNow.Add(25*time.Hour)); err != nil || !got.Allowed {
		t.Fatalf("resolved activation=%+v err=%v", got, err)
	}
}

func TestTodo_SUBPROCESSOR_001_Fault(t *testing.T) {
	change := reviewedChange(t)
	change.ExitFallback = false
	if _, err := Activate(change, testNow.Add(25*time.Hour)); !errors.Is(err, ErrExitFallback) {
		t.Fatalf("missing exit fallback=%v", err)
	}
	change = reviewedChange(t)
	change.Notice.ObjectionCloses = change.Notice.ObjectionOpens.Add(-time.Hour)
	if _, err := Activate(change, testNow.Add(25*time.Hour)); !errors.Is(err, ErrNoticeRequired) {
		t.Fatalf("invalid notice=%v", err)
	}
}

func TestTodo_SUBPROCESSOR_001_Security(t *testing.T) {
	bad := testInventory(1, "us")
	bad.Processors[0].ContractVersion = ""
	if err := ValidateInventory(bad); !errors.Is(err, ErrInvalidInventory) {
		t.Fatalf("incomplete processor=%v", err)
	}
	bad = testInventory(1, "us")
	bad.Flows[0].Region = "unknown"
	if err := ValidateInventory(bad); !errors.Is(err, ErrInvalidInventory) {
		t.Fatalf("out-of-contract flow=%v", err)
	}
	change := reviewedChange(t)
	change.Emergency = &EmergencyReplacement{IncidentRef: "incident-1", RequestedBy: "operator-a", ApprovedBy: "operator-a", CompensatingControls: []string{"read-only"}, ExpiresAt: testNow.Add(time.Hour)}
	if _, err := Activate(change, testNow); !errors.Is(err, ErrEmergency) {
		t.Fatalf("self-approved emergency=%v", err)
	}
}

func TestTodo_SUBPROCESSOR_001_Conformance(t *testing.T) {
	if Digest(testInventory(1, "us")) != Digest(testInventory(1, "us")) {
		t.Fatal("digest is not deterministic")
	}
	shuffled := testInventory(1, "us")
	shuffled.Processors[0].Regions = []string{"us"}
	if Digest(shuffled) != Digest(testInventory(1, "us")) {
		t.Fatal("canonical digest changed for equivalent inventory")
	}
}

func TestTodo_SUBPROCESSOR_001_Recovery(t *testing.T) {
	change := reviewedChange(t)
	change.Emergency = &EmergencyReplacement{IncidentRef: "incident-1", RequestedBy: "operator-a", ApprovedBy: "operator-b", CompensatingControls: []string{"read-only", "reconcile"}, ExpiresAt: testNow.Add(2 * time.Hour)}
	got, err := Activate(change, testNow)
	if err != nil || !got.Allowed || got.Status != StatusEmergency {
		t.Fatalf("emergency activation=%+v err=%v", got, err)
	}
	if _, err := Activate(change, testNow.Add(73*time.Hour)); !errors.Is(err, ErrEmergency) {
		t.Fatalf("expired emergency=%v", err)
	}
}

func TestTodo_SUBPROCESSOR_001_ModelBased(t *testing.T) {
	for _, region := range []string{"eu", "ap"} {
		change, err := Diff(testInventory(1, "us"), testInventory(2, region))
		if err != nil {
			t.Fatal(err)
		}
		if !change.Material || change.AffectedFlows[0] != "flow-a" {
			t.Fatalf("region=%s change=%+v", region, change)
		}
	}
}

func TestTodo_SUBPROCESSOR_001_Mutation(t *testing.T) {
	change := reviewedChange(t)
	original, err := RecordObjection(change, Objection{TenantID: "tenant-a", Reason: "objection"})
	if err != nil {
		t.Fatal(err)
	}
	original.Objections[0].Disposition = "mutated-outside"
	if change.Objections != nil || original.Objections[0].Disposition != "mutated-outside" {
		t.Fatal("change mutation leaked into source")
	}
	if got, err := ResolveObjection(original, "tenant-a", "resolved", testNow); err != nil || !got.Objections[0].Resolved() {
		t.Fatalf("resolution=%+v err=%v", got, err)
	}
}
