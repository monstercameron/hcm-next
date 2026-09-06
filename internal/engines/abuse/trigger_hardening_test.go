package abuse

import (
	"errors"
	"testing"
	"time"
)

func hardeningAssessment(t *testing.T, score, confidence int) RiskAssessment {
	t.Helper()
	table, err := NewWeightingTable("trigger", "1", []FindingWeight{{Category: FindingPrivilegeBurst, Weight: score, Confidence: confidence}}, 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	scorer, err := NewRiskScorer(table)
	if err != nil {
		t.Fatal(err)
	}
	finding := RiskFinding{DetectorID: "detector", DetectorSemver: "1.0.0", DetectorDigest: "sha256:d", SignalID: "signal", Kind: SignalKindAccessGrant, Category: FindingPrivilegeBurst, Severity: SeverityCritical, Evidence: RiskEvidence{Principal: "principal", Scope: FindingScopePrincipal}}
	assessment, err := scorer.Score("principal", RiskWindow{Start: time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC), End: time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC)}, []RiskFinding{finding})
	if err != nil {
		t.Fatal(err)
	}
	// The constructor's weight is the score; confidence is carried by its weight.
	if assessment.Score != score || assessment.Confidence != confidence {
		t.Fatalf("assessment=%+v", assessment)
	}
	return assessment
}

func hardeningRoute() ReviewRoute {
	return ReviewRoute{Queue: "queue", CaseType: "case", SLA: time.Hour, ReviewerSoD: true}
}

func TestTriggerPolicyAndAuthorityValidationBranches(t *testing.T) {
	for _, action := range []TriggerAction{TriggerNoAction, TriggerStepUp, TriggerHumanReview, TriggerTemporaryContainment} {
		if !action.Valid() {
			t.Errorf("action %q invalid", action)
		}
	}
	if TriggerAction("bad").Valid() {
		t.Fatal("unknown action valid")
	}
	validRule := TriggerRule{ID: "rule", MinimumScore: 50, MinimumConfidence: 50, Action: TriggerStepUp, Duration: time.Minute}
	for _, tc := range []struct {
		name   string
		mutate func(*TriggerRule)
	}{
		{"id", func(r *TriggerRule) { r.ID = "" }}, {"score low", func(r *TriggerRule) { r.MinimumScore = -1 }}, {"score high", func(r *TriggerRule) { r.MinimumScore = 101 }},
		{"confidence low", func(r *TriggerRule) { r.MinimumConfidence = -1 }}, {"confidence high", func(r *TriggerRule) { r.MinimumConfidence = 101 }}, {"action", func(r *TriggerRule) { r.Action = "bad" }},
		{"step duration zero", func(r *TriggerRule) { r.Duration = 0 }}, {"step duration too long", func(r *TriggerRule) { r.Duration = 2 * time.Hour }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := validRule
			tc.mutate(&r)
			if !errors.Is(r.Validate(time.Hour), ErrInvalidTriggerPolicy) {
				t.Fatalf("error=%v", r.Validate(time.Hour))
			}
		})
	}
	noAction := TriggerRule{ID: "none", Action: TriggerNoAction}
	if err := noAction.Validate(time.Hour); err != nil {
		t.Fatal(err)
	}
	noAction.Duration = time.Second
	if err := noAction.Validate(time.Hour); !errors.Is(err, ErrInvalidTriggerPolicy) {
		t.Fatalf("no-action duration error=%v", err)
	}
	noAction = TriggerRule{ID: "review", Action: TriggerHumanReview}
	if err := noAction.Validate(time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := (ReviewRoute{}).Validate(); !errors.Is(err, ErrInvalidTriggerPolicy) {
		t.Fatalf("invalid review route=%v", err)
	}

	baseRules := []TriggerRule{{ID: "none", MinimumScore: 0, Action: TriggerNoAction}, {ID: "step", MinimumScore: 50, Action: TriggerStepUp, Duration: time.Minute}}
	policy, err := NewTriggerPolicy("p", "1", baseRules, hardeningRoute(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*TriggerPolicy)
	}{
		{"identity", func(p *TriggerPolicy) { p.ID = "" }}, {"version", func(p *TriggerPolicy) { p.Version = "" }}, {"duration", func(p *TriggerPolicy) { p.MaxActionDuration = 0 }},
		{"rules", func(p *TriggerPolicy) { p.Rules = nil }}, {"duplicate id", func(p *TriggerPolicy) { p.Rules = append(p.Rules, p.Rules[0]) }},
		{"duplicate score", func(p *TriggerPolicy) {
			p.Rules = append(p.Rules, TriggerRule{ID: "other", MinimumScore: 0, Action: TriggerHumanReview})
		}},
		{"bad digest", func(p *TriggerPolicy) { p.Digest = "sha256:bad" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := policy
			p.Rules = append([]TriggerRule(nil), policy.Rules...)
			tc.mutate(&p)
			if !errors.Is(p.Validate(), ErrInvalidTriggerPolicy) {
				t.Fatalf("error=%v", p.Validate())
			}
		})
	}
	if _, err := NewTriggerPolicy("p", "1", []TriggerRule{{ID: "bad", MinimumScore: 0, Action: TriggerStepUp, Duration: 2 * time.Hour}}, hardeningRoute(), time.Hour); !errors.Is(err, ErrInvalidTriggerPolicy) {
		t.Fatalf("constructor accepted too-long rule: %v", err)
	}
	rules := append([]TriggerRule(nil), baseRules...)
	copyPolicy, err := NewTriggerPolicy("copy", "1", rules, hardeningRoute(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	rules[0].ID = "mutated"
	if copyPolicy.Rules[0].ID == "mutated" {
		t.Fatal("NewTriggerPolicy retained caller-owned rule slice")
	}

	for _, tc := range []struct {
		name      string
		authority TriggerAuthority
		want      error
	}{
		{"blank holder", TriggerAuthority{MayStepUp: true}, ErrInvalidTriggerRequest},
		{"no permissions", TriggerAuthority{Holder: "h"}, ErrInvalidTriggerRequest},
	} {
		t.Run("authority "+tc.name, func(t *testing.T) {
			if !errors.Is(tc.authority.Validate(), tc.want) {
				t.Fatalf("error=%v", tc.authority.Validate())
			}
		})
	}
	if err := (TriggerAuthority{Holder: "h", MayReview: true}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, capability := range []string{"terminate", "employment-termination", "payroll_termination", "fire"} {
		target := ContainmentTarget{TenantID: "tenant", Principal: "principal", Capability: capability}
		if !errors.Is(target.Validate("principal"), ErrTerminationTarget) {
			t.Errorf("capability %q accepted", capability)
		}
	}
	for _, target := range []ContainmentTarget{{}, {TenantID: "tenant", Principal: "other", Capability: "access"}, {TenantID: "tenant", Principal: "principal", Capability: ""}} {
		if err := target.Validate("principal"); !errors.Is(err, ErrInvalidTriggerRequest) {
			t.Errorf("target=%+v error=%v", target, err)
		}
	}
}

func TestTriggerEvaluationDecisionBranchesAndDigestGuards(t *testing.T) {
	policy := DefaultTriggerPolicy()
	at := time.Date(2026, 9, 6, 10, 1, 0, 0, time.UTC)
	noAction, err := EvaluateTrigger(policy, TriggerRequest{Assessment: hardeningAssessment(t, 10, 90), At: at, Authority: TriggerAuthority{Holder: "h", MayReview: true}})
	if err != nil || noAction.Action != TriggerNoAction || noAction.Review.Route.Queue != "" || noAction.ExpiresAt != (time.Time{}) {
		t.Fatalf("no-action decision=%+v err=%v", noAction, err)
	}
	if exp, err := ExplainTrigger(noAction); err != nil || exp.HasHumanRoute || exp.Reversible {
		t.Fatalf("no-action explanation=%+v err=%v", exp, err)
	}

	step, err := EvaluateTrigger(policy, TriggerRequest{Assessment: hardeningAssessment(t, 60, 90), At: at, Authority: TriggerAuthority{Holder: "h", MayStepUp: true, MayReview: true}})
	if err != nil || step.Action != TriggerStepUp || step.StepUp == nil || step.ExpiresAt.Sub(at) != 10*time.Minute {
		t.Fatalf("step decision=%+v err=%v", step, err)
	}
	if err := step.Validate(); err != nil {
		t.Fatal(err)
	}
	if exp, err := ExplainTrigger(step); err != nil || !exp.HasHumanRoute || !exp.Reversible {
		t.Fatalf("step explanation=%+v err=%v", exp, err)
	}
	lowConfidence, err := EvaluateTrigger(policy, TriggerRequest{Assessment: hardeningAssessment(t, 95, 40), At: at, Authority: TriggerAuthority{Holder: "h", MayReview: true}})
	if err != nil || lowConfidence.Action != TriggerHumanReview || lowConfidence.Reason != "CONFIDENCE_REQUIRES_HUMAN_REVIEW" {
		t.Fatalf("confidence decision=%+v err=%v", lowConfidence, err)
	}
	if _, err := EvaluateTrigger(policy, TriggerRequest{Assessment: hardeningAssessment(t, 95, 40), At: at, Authority: TriggerAuthority{Holder: "h", MayStepUp: true}}); !errors.Is(err, ErrTriggerAuthority) {
		t.Fatalf("missing review authority error=%v", err)
	}

	containmentRequest := TriggerRequest{Assessment: hardeningAssessment(t, 95, 95), At: at, Authority: TriggerAuthority{Holder: "h", MayContain: true, MayReview: true}, Target: ContainmentTarget{TenantID: "tenant", Principal: "principal", Capability: "access.export"}}
	containment, err := EvaluateTrigger(policy, containmentRequest)
	if err != nil || containment.Action != TriggerTemporaryContainment || containment.Containment == nil || !containment.Containment.Reversible {
		t.Fatalf("containment decision=%+v err=%v", containment, err)
	}
	if err := containment.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateTrigger(policy, TriggerRequest{Assessment: hardeningAssessment(t, 95, 95), At: at, Authority: TriggerAuthority{Holder: "h", MayStepUp: true}, Target: containmentRequest.Target}); !errors.Is(err, ErrTriggerAuthority) {
		t.Fatalf("missing containment authority error=%v", err)
	}
	badTarget := containmentRequest
	badTarget.Target.Capability = "employment-termination"
	if _, err := EvaluateTrigger(policy, badTarget); !errors.Is(err, ErrTerminationTarget) {
		t.Fatalf("termination target error=%v", err)
	}

	invalidRequest := containmentRequest
	invalidRequest.At = time.Time{}
	if _, err := EvaluateTrigger(policy, invalidRequest); !errors.Is(err, ErrInvalidTriggerRequest) {
		t.Fatalf("invalid time request error=%v", err)
	}
	noDigest := policy
	noDigest.Digest = ""
	if _, err := EvaluateTrigger(noDigest, containmentRequest); !errors.Is(err, ErrInvalidTriggerPolicy) {
		t.Fatalf("missing policy digest error=%v", err)
	}
	mutated := step
	mutated.Reason = "changed"
	if err := mutated.Validate(); !errors.Is(err, ErrInvalidTriggerRequest) {
		t.Fatalf("mutated decision accepted: %v", err)
	}
	badReview := step
	badReview.Review.AssessmentDigest = "other"
	if err := badReview.Validate(); !errors.Is(err, ErrInvalidTriggerRequest) {
		t.Fatalf("unbound review accepted: %v", err)
	}
	badNoAction := noAction
	badNoAction.Review.Route.Queue = "bad"
	if err := badNoAction.Validate(); !errors.Is(err, ErrInvalidTriggerRequest) {
		t.Fatalf("no-action route accepted: %v", err)
	}
	badContainment := containment
	badContainment.Containment.Reversible = false
	if err := badContainment.Validate(); !errors.Is(err, ErrInvalidTriggerRequest) {
		t.Fatalf("irreversible containment accepted: %v", err)
	}
}
