package configbundle

import (
	"errors"
	"testing"
	"time"
)

func canaryPlan() CanaryPlan {
	from := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	return CanaryPlan{
		Scope: Scope{TenantID: "canary-tenant", CellID: "cell-a"}, BundleDigest: cp005Digest("canary-bundle"), Epoch: 7,
		Cohort: []string{"org-a", "org-b"}, MaxCohort: 10, CohortID: "pilot-1", WindowFrom: from, WindowTo: to,
		Health:         []CanaryObservation{{ID: "availability", Status: CanaryHealthy, ObservedAt: to, WindowFrom: from, WindowTo: to, Good: 100, Total: 100, Complete: true, EvidenceDigest: "availability-evidence"}},
		Reconciliation: CanaryEvidence{Available: true, Passed: true, EvidenceDigest: "reconciliation-evidence"},
		Conformance:    CanaryEvidence{Available: true, Passed: true, EvidenceDigest: "conformance-evidence"},
	}
}

// TestTodo_CP_007 proves bounded canary decisions are signed, deterministic,
// and promote only after every declared evidence source passes.
func TestTodo_CP_007(t *testing.T) {
	signer, publicKey := cp005Signer(t)
	now := time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)
	controller := NewCanaryController(signer, func() time.Time { return now })
	decision, err := controller.EvaluateCanary(canaryPlan())
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != CanaryPromoted || decision.Reason != "HEALTHY_EVIDENCE" {
		t.Fatalf("decision=%+v", decision)
	}
	if err := decision.Verify(publicKey); err != nil {
		t.Fatalf("decision Verify: %v", err)
	}
	retry, err := controller.EvaluateCanary(canaryPlan())
	if err != nil || retry.Digest != decision.Digest {
		t.Fatalf("repeat decision=%+v err=%v", retry, err)
	}
}

func TestTodo_CP_007_Golden(t *testing.T) {
	signer, _ := cp005Signer(t)
	now := time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC)
	first, err := EvaluateCanary(canaryPlan(), signer, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EvaluateCanary(canaryPlan(), signer, now)
	if err != nil || first.Digest != second.Digest || first.Explain() == "" {
		t.Fatalf("deterministic canary first=%+v second=%+v err=%v", first, second, err)
	}
}

func TestTodo_CP_007_Conformance(t *testing.T) {
	signer, _ := cp005Signer(t)
	plan := canaryPlan()
	plan.Health[0].Status = CanaryUnknown
	decision, err := EvaluateCanary(plan, signer, plan.WindowTo.Add(time.Hour))
	if err != nil || decision.Outcome != CanaryPaused || decision.Reason != "HEALTH_UNKNOWN" {
		t.Fatalf("unknown health decision=%+v err=%v", decision, err)
	}
	plan = canaryPlan()
	plan.Reconciliation.Passed = false
	decision, err = EvaluateCanary(plan, signer, plan.WindowTo.Add(time.Hour))
	if err != nil || decision.Outcome != CanaryPaused || decision.Reason != "RECONCILIATION_NOT_CONFORMANT" {
		t.Fatalf("reconciliation decision=%+v err=%v", decision, err)
	}
	plan = canaryPlan()
	plan.WindowTo = plan.WindowFrom.Add(time.Hour)
	decision, err = EvaluateCanary(plan, signer, plan.WindowFrom.Add(30*time.Minute))
	if err != nil || decision.Outcome != CanaryPaused || decision.Reason != "EVIDENCE_WINDOW_OPEN" {
		t.Fatalf("open window decision=%+v err=%v", decision, err)
	}
	bad := canaryPlan()
	bad.Cohort = nil
	if _, err := EvaluateCanary(bad, signer, nowForCanary()); !errors.Is(err, ErrCanaryInvalid) {
		t.Fatalf("invalid plan error=%v", err)
	}
}

func nowForCanary() time.Time { return time.Date(2026, 9, 5, 14, 0, 0, 0, time.UTC) }
