package abuse_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/abuse"
)

func triggerAssessment(t *testing.T, score, confidence int) abuse.RiskAssessment {
	t.Helper()
	table, err := abuse.NewWeightingTable("trigger-table", "2026.09", []abuse.FindingWeight{
		{Category: abuse.FindingPrivilegeBurst, Weight: score, Confidence: confidence},
	}, 100, 50)
	if err != nil {
		t.Fatalf("NewWeightingTable: %v", err)
	}
	scorer, err := abuse.NewRiskScorer(table)
	if err != nil {
		t.Fatalf("NewRiskScorer: %v", err)
	}
	assessment, err := scorer.Score("principal-1", abuse.RiskWindow{
		Start: time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
	}, []abuse.RiskFinding{{
		DetectorID: "detector-1", DetectorSemver: "1.0.0", DetectorDigest: "sha256:detector-1",
		SignalID: "signal-1", Kind: abuse.SignalKindPrivilegedChange, Category: abuse.FindingPrivilegeBurst,
		Severity: abuse.SeverityCritical, Evidence: abuse.RiskEvidence{Scope: abuse.FindingScopePrincipal, Principal: "principal-1"},
	}})
	if err != nil {
		t.Fatalf("Score: %v", err)
	}
	return assessment
}

func triggerRequest(assessment abuse.RiskAssessment, authority abuse.TriggerAuthority) abuse.TriggerRequest {
	return abuse.TriggerRequest{
		Assessment: assessment,
		At:         time.Date(2026, 9, 6, 10, 1, 0, 0, time.UTC),
		Authority:  authority,
		Target:     abuse.ContainmentTarget{TenantID: "tenant-1", Principal: assessment.Principal, Capability: "access.export"},
	}
}

func TestTodo_ABUSE_005(t *testing.T) {
	assessment := triggerAssessment(t, 60, 90)
	policy := abuse.DefaultTriggerPolicy()
	decision, err := abuse.EvaluateTrigger(policy, triggerRequest(assessment, abuse.TriggerAuthority{
		Holder: "operator-2", MayStepUp: true, MayReview: true,
	}))
	if err != nil {
		t.Fatalf("EvaluateTrigger: %v", err)
	}
	if decision.Action != abuse.TriggerStepUp || decision.StepUp == nil || decision.Containment != nil {
		t.Fatalf("decision = %+v, want a step-up only", decision)
	}
	if decision.Review.Route.Queue == "" || decision.StepUp.ExpiresAt.Sub(decision.At) != 10*time.Minute {
		t.Fatalf("decision lost bounded review/expiry contract: %+v", decision)
	}
	if !decision.StepUp.ExpiresAt.After(decision.At) || decision.Digest == "" {
		t.Fatalf("step-up is not time-bounded and digested: %+v", decision)
	}
	if _, err := abuse.ExplainTrigger(decision); err != nil {
		t.Fatalf("ExplainTrigger: %v", err)
	}
}

func TestTodo_ABUSE_005_Security(t *testing.T) {
	assessment := triggerAssessment(t, 95, 40)
	policy := abuse.DefaultTriggerPolicy()
	decision, err := abuse.EvaluateTrigger(policy, triggerRequest(assessment, abuse.TriggerAuthority{
		Holder: "operator-2", MayContain: true, MayReview: true,
	}))
	if err != nil {
		t.Fatalf("low-confidence evaluation: %v", err)
	}
	if decision.Action != abuse.TriggerHumanReview || decision.Review.Route.Queue == "" || decision.Containment != nil {
		t.Fatalf("low-confidence result = %+v, want human review route", decision)
	}
	request := triggerRequest(triggerAssessment(t, 95, 95), abuse.TriggerAuthority{Holder: "operator-2", MayContain: true, MayReview: true})
	request.Target.Capability = "employment.termination"
	if _, err := abuse.EvaluateTrigger(policy, request); !errors.Is(err, abuse.ErrTerminationTarget) {
		t.Fatalf("termination-shaped containment was accepted: %v", err)
	}
	if _, err := abuse.EvaluateTrigger(policy, triggerRequest(triggerAssessment(t, 95, 40), abuse.TriggerAuthority{Holder: "operator-2", MayContain: true})); !errors.Is(err, abuse.ErrTriggerAuthority) {
		t.Fatalf("missing human-review authority was not refused: %v", err)
	}
}

func TestTodo_ABUSE_005_Mutation(t *testing.T) {
	assessment := triggerAssessment(t, 95, 95)
	policy := abuse.DefaultTriggerPolicy()
	decision, err := abuse.EvaluateTrigger(policy, triggerRequest(assessment, abuse.TriggerAuthority{
		Holder: "operator-2", MayContain: true, MayReview: true,
	}))
	if err != nil {
		t.Fatalf("EvaluateTrigger: %v", err)
	}
	if err := decision.Validate(); err != nil {
		t.Fatalf("fresh decision invalid: %v", err)
	}
	decision.ExpiresAt = decision.ExpiresAt.Add(time.Minute)
	if err := decision.Validate(); !errors.Is(err, abuse.ErrInvalidTriggerRequest) {
		t.Fatalf("mutated expiry was accepted: %v", err)
	}
	policy.Digest = "sha256:mutated"
	if _, err := abuse.EvaluateTrigger(policy, triggerRequest(assessment, abuse.TriggerAuthority{
		Holder: "operator-2", MayStepUp: true, MayReview: true,
	})); !errors.Is(err, abuse.ErrInvalidTriggerPolicy) {
		t.Fatalf("mutated policy digest was accepted: %v", err)
	}
}
