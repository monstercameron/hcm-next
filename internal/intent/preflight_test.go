package intent_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/governance"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

func validPreflightPlanRequest() intent.PreflightPlanRequest {
	return intent.PreflightPlanRequest{
		Definition:       intent.Ref{TypeID: "payroll.run", Version: 2},
		ProposalRevision: "proposal/payroll-run-88/r9",
		Snapshot:         snapshot.ReadSnapshot{Digest: "sha256:snap"},
		Governance: governance.ComposeRequest{
			AuthZ:   governance.AuthZInput{Effect: "ALLOW", Digest: "sha256:authz"},
			Legal:   governance.LegalInput{Effect: "ALLOW", Digest: "sha256:legal"},
			Purpose: governance.PurposeInput{Allowed: true},
			Risk:    governance.RiskInput{Level: "MEDIUM"},
			Now:     time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC),
		},
		Assumptions: []intent.Assumption{{Name: "pay-calendar", Value: "monthly"}},
		Cost:        intent.CostBreakdown{Units: 42, Basis: "price-list@v7"},
		Risk:        intent.RiskAssessment{Level: intent.RiskMedium, Factors: []string{"overtime-spike"}},
		Reservations: []intent.ConflictReservation{
			{IntentID: "intent/payroll-run-87", Fence: 9},
		},
		Approvals:   []intent.ApprovalTask{{Kind: intent.KindApproval, Ref: "approval/captains-1"}},
		Obligations: []string{"retain-7y"},
		Writes: []intent.IntendedWrite{
			{Target: "ledger/payroll", Effect: "POST"},
			{Target: "notify/send", Effect: "SEND"},
		},
		EffectEdges:          []intent.EffectEdge{{From: "ledger/payroll", To: "notify/send"}},
		Observations:         []string{"timesheet-approved"},
		RevalidationTriggers: []string{"calendar-revision"},
		Completion:           intent.WaitForChild,
		RepairExpectation:    "retry-once-then-escalate",
		EngineVersions:       map[string]string{"payroll-core": "v1.4.0"},
		ControlVersions:      map[string]string{"admission": "v3"},
	}
}

// TestIntentPreflightPlanBindsSimulationRiskCostApprovalsObligationsAndEffects
// proves one side-effect-free preflight plan binds its whole evidence
// bundle under a canonical digest, or fails with an exact cause.
func TestIntentPreflightPlanBindsSimulationRiskCostApprovalsObligationsAndEffects(t *testing.T) {
	plan, err := intent.CompilePreflightPlan(validPreflightPlanRequest())
	if err != nil {
		t.Fatalf("valid preflight rejected: %v", err)
	}
	if plan.Digest == "" {
		t.Fatal("preflight plan carries no digest")
	}
	again, err := intent.CompilePreflightPlan(validPreflightPlanRequest())
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != plan.Digest {
		t.Fatal("duplicate compilation created a different identity")
	}
	if plan.Governance.Decision == "" {
		t.Fatal("governance composition missing from the plan")
	}

	adversaries := []struct {
		name   string
		mutate func(*intent.PreflightPlanRequest)
		cause  error
	}{
		{"missing proposal", func(r *intent.PreflightPlanRequest) { r.ProposalRevision = "" }, intent.ErrInvalidPreflight},
		{"mismatched snapshot", func(r *intent.PreflightPlanRequest) { r.Snapshot.Digest = "" }, intent.ErrMismatchedSnapshot},
		{"missing control version", func(r *intent.PreflightPlanRequest) { r.ControlVersions["admission"] = "" }, intent.ErrMismatchedSnapshot},
		{"missing cost basis", func(r *intent.PreflightPlanRequest) { r.Cost.Basis = "" }, intent.ErrMissingEstimate},
		{"missing risk level", func(r *intent.PreflightPlanRequest) { r.Risk.Level = "" }, intent.ErrMissingEstimate},
		{"hidden effect branch", func(r *intent.PreflightPlanRequest) {
			r.EffectEdges = append(r.EffectEdges, intent.EffectEdge{From: "ledger/payroll", To: "shadow/ledger"})
		}, intent.ErrHiddenEffect},
		{"guaranteed external outcome", func(r *intent.PreflightPlanRequest) { r.Writes[0].Guaranteed = true }, intent.ErrGuaranteedOutcome},
		{"redacted affirmation", func(r *intent.PreflightPlanRequest) {
			r.Assumptions = append(r.Assumptions, intent.Assumption{Name: "salary", Unknown: true, ResolvedValue: "90000"})
		}, intent.ErrUnsafeAffirmation},
		{"unfenced reservation", func(r *intent.PreflightPlanRequest) { r.Reservations[0].Fence = 0 }, intent.ErrUnfencedReservation},
		{"missing repair policy", func(r *intent.PreflightPlanRequest) { r.RepairExpectation = "" }, intent.ErrMissingRepairPolicy},
		{"invalid completion", func(r *intent.PreflightPlanRequest) { r.Completion = "HOPE" }, intent.ErrMissingRepairPolicy},
	}
	for _, tc := range adversaries {
		t.Run(tc.name, func(t *testing.T) {
			req := validPreflightPlanRequest()
			tc.mutate(&req)
			if _, err := intent.CompilePreflightPlan(req); !errors.Is(err, tc.cause) {
				t.Fatalf("want %v, got %v", tc.cause, err)
			}
		})
	}
}
