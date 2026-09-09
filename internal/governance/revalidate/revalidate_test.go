package revalidate_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/decision"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/revalidate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// clockAt returns a [revalidate.Clock] fixed at a stable, non-zero instant.
func clockAt(unixSec int64) revalidate.Clock {
	return func() values.Instant {
		i, err := values.NewInstantFromUnix(unixSec, 0)
		if err != nil {
			panic(err)
		}
		return i
	}
}

// baseFacts returns a fully populated, mutually consistent set of the seven
// GOVERN-003 inputs: every subdecision-shaped fact is Allow, so composing
// against them (see baseInputs) produces an unobstructed ALLOW decision.
func baseFacts() revalidate.Facts {
	return revalidate.Facts{
		AuthZ: revalidate.AuthZFact{
			Effect:        decision.Allow,
			PolicyVersion: "authz.p1a.bootstrap.v1",
		},
		Session: revalidate.SessionFact{
			Effect:    decision.Allow,
			SessionID: "sess_abc123",
			Assurance: "AAL2",
		},
		SourceAuthority: revalidate.SourceAuthorityFact{
			Decision: "authority.assignment/v1",
		},
		FieldClassification: revalidate.FieldClassificationFact{
			Version: "classification.taxonomy/2026.1",
		},
		LegalPolicy: revalidate.LegalPolicyFact{
			LegalPackVersion:  "legal.harborcare-demo/2026.1",
			PolicyPackVersion: "policy.promotion/v1",
		},
		BudgetPosition: revalidate.BudgetPositionFact{
			Budget: revalidate.ResourceFact{
				Effect:        decision.Allow,
				ObservationID: "budget-obs-v1",
			},
			Position: revalidate.ResourceFact{
				Effect:        decision.Allow,
				ObservationID: "position-obs-v1",
			},
		},
		Conflict: revalidate.ConflictFact{
			Effect:         decision.Allow,
			Classification: "COMPATIBLE_MERGE",
			EvidenceRef:    "merge:rule.promotion.compatible@1",
		},
	}
}

// baseInputs builds the decision.Inputs a historical approval would have
// composed from facts. It mirrors, field for field, the marker-rule scheme
// revalidate.Revalidate itself uses internally to fold each fact's identity
// into the digest, so that revalidating with an unchanged copy of facts
// reproduces the exact historical decision.
func baseInputs(facts revalidate.Facts) decision.Inputs {
	return decision.Inputs{
		ProposalRevisionDigest: "sha256:promotion-revision-1",
		Context: decision.Context{
			Principal:           "principal:promotion-manager",
			Delegation:          "none",
			Capability:          "promotion.propose/v1",
			Resource:            "worker:22222222-2222-4222-8222-222222222222",
			Fields:              []string{"assignment.grade", "rewards.compensation.annualized_base_pay"},
			CurrentOrganization: "people-ops",
			TargetOrganization:  "people-ops",
			Purpose:             "internal_promotion",
			Risk:                "LOW",
			Authority:           facts.SourceAuthority.Decision,
			Legal:               facts.LegalPolicy.LegalPackVersion,
		},
		ControlSnapshot: decision.ControlSnapshot{
			Digest:          "sha256:control-snapshot-1",
			PolicyBundle:    facts.LegalPolicy.PolicyPackVersion,
			LegalContext:    facts.LegalPolicy.LegalPackVersion,
			Classification:  facts.FieldClassification.Version,
			ReferenceData:   "reference.pay-band/2026.1",
			SourceWatermark: "rev:v1:people.worker.22222222:s17",
		},
		RulePackVersions: []decision.RulePackVersion{
			{ID: "legal.harborcare-demo", Version: "2026.1", Digest: "sha256:legal"},
		},
		ApprovalRequirements: []decision.ApprovalRequirement{
			{ID: "approval.finance_partner", Version: "1", Satisfaction: decision.ApprovalSatisfied},
		},
		SoDVerdicts: []decision.SoDVerdict{
			{RuleID: "sod.promotion", Version: "1", State: decision.Allow, Satisfied: true},
		},
		Subdecisions: []decision.Subdecision{
			{
				ID: "authz", Source: "authz", State: facts.AuthZ.Effect,
				FiredRules:   append(slices.Clone(facts.AuthZ.FiredRules), decision.RuleRef{ID: "authz.policy_version", Version: facts.AuthZ.PolicyVersion}),
				Restrictions: facts.AuthZ.Restrictions,
				Obligations:  facts.AuthZ.Obligations,
			},
			{
				ID: "session", Source: "session", State: facts.Session.Effect,
				FiredRules: append(slices.Clone(facts.Session.FiredRules),
					decision.RuleRef{ID: "session.identity", Version: facts.Session.SessionID},
					decision.RuleRef{ID: "session.assurance", Version: facts.Session.Assurance},
				),
			},
			{
				ID: "budget", Source: "budget", State: facts.BudgetPosition.Budget.Effect,
				FiredRules:  append(slices.Clone(facts.BudgetPosition.Budget.FiredRules), decision.RuleRef{ID: "budget.observation", Version: facts.BudgetPosition.Budget.ObservationID}),
				Obligations: facts.BudgetPosition.Budget.Obligations,
			},
			{
				ID: "position", Source: "position", State: facts.BudgetPosition.Position.Effect,
				FiredRules:  append(slices.Clone(facts.BudgetPosition.Position.FiredRules), decision.RuleRef{ID: "position.observation", Version: facts.BudgetPosition.Position.ObservationID}),
				Obligations: facts.BudgetPosition.Position.Obligations,
			},
			{
				ID: "conflict", Source: "conflict", State: facts.Conflict.Effect,
				FiredRules: append(slices.Clone(facts.Conflict.FiredRules),
					decision.RuleRef{ID: "conflict.classification", Version: facts.Conflict.Classification},
					decision.RuleRef{ID: "conflict.evidence", Version: facts.Conflict.EvidenceRef},
				),
			},
		},
	}
}

const basePlanDigest = "plan-digest-v1"

func baseHistorical(t *testing.T, facts revalidate.Facts) revalidate.HistoricalApproval {
	t.Helper()
	inputs := baseInputs(facts)
	composed, err := decision.Compose(inputs)
	if err != nil {
		t.Fatalf("Compose base inputs: %v", err)
	}
	if composed.State != decision.Allow {
		t.Fatalf("base fixture is not ALLOW: %s", composed.State)
	}
	return revalidate.HistoricalApproval{Inputs: inputs, Decision: composed, Facts: facts, PlanDigest: basePlanDigest}
}

func TestTodo_GOVERN_003(t *testing.T) {
	facts := baseFacts()
	historical := baseHistorical(t, facts)

	// Unchanged facts reproduce the exact historical decision digest and
	// carry no requirement.
	result, err := revalidate.Revalidate(clockAt(1000), historical, facts, basePlanDigest)
	if err != nil {
		t.Fatalf("Revalidate confirmed case: %v", err)
	}
	if !result.Confirmed {
		t.Fatalf("expected confirmed, got %+v", result)
	}
	if result.Requirement != revalidate.RequirementNone {
		t.Fatalf("confirmed result carries requirement %q", result.Requirement)
	}
	if len(result.ChangedInputs) != 0 {
		t.Fatalf("confirmed result names changed inputs: %v", result.ChangedInputs)
	}
	if result.RecomposedDigest != historical.Decision.Digest || result.RecomposedDigest != result.HistoricalDigest {
		t.Fatalf("confirmed result did not reproduce the historical digest: %+v", result)
	}
	if !result.EvaluatedAt.IsSet() {
		t.Fatal("result carries no evaluation instant")
	}
	if err := result.VerifyBoundPlan(basePlanDigest); err != nil {
		t.Fatalf("VerifyBoundPlan against the exact plan it was computed for: %v", err)
	}
	if err := result.VerifyBoundPlan("plan-digest-v2"); !errors.Is(err, revalidate.ErrPlanChanged) {
		t.Fatalf("VerifyBoundPlan against a different plan = %v, want ErrPlanChanged", err)
	}

	// A republished legal/policy version, with governance still allowing,
	// requires reapproval rather than a hard block or a replan.
	reapproval := baseFacts()
	reapproval.LegalPolicy.PolicyPackVersion = "policy.promotion/v2"
	result, err = revalidate.Revalidate(clockAt(1000), historical, reapproval, basePlanDigest)
	if err != nil {
		t.Fatalf("Revalidate reapproval case: %v", err)
	}
	if result.Confirmed {
		t.Fatal("republished policy version was confirmed unchanged")
	}
	if result.Requirement != revalidate.RequirementReapprovalRequired {
		t.Fatalf("requirement = %s, want %s", result.Requirement, revalidate.RequirementReapprovalRequired)
	}
	if !slices.Contains(result.ChangedInputs, revalidate.ChangedLegalPolicyVersion) {
		t.Fatalf("changed inputs = %v, want to contain %s", result.ChangedInputs, revalidate.ChangedLegalPolicyVersion)
	}

	// A re-baselined budget observation that is still sufficient invalidates
	// the plan's own reservation, not merely the approval: REPLAN_REQUIRED.
	replanBudget := baseFacts()
	replanBudget.BudgetPosition.Budget.ObservationID = "budget-obs-v2"
	result, err = revalidate.Revalidate(clockAt(1000), historical, replanBudget, basePlanDigest)
	if err != nil {
		t.Fatalf("Revalidate replan-budget case: %v", err)
	}
	if result.Requirement != revalidate.RequirementReplanRequired {
		t.Fatalf("requirement = %s, want %s", result.Requirement, revalidate.RequirementReplanRequired)
	}
	if !slices.Contains(result.ChangedInputs, revalidate.ChangedBudgetPosition) {
		t.Fatalf("changed inputs = %v, want to contain %s", result.ChangedInputs, revalidate.ChangedBudgetPosition)
	}

	// A conflict reclassification (still allow) also demands a replan: the
	// plan's write ordering assumptions no longer hold.
	replanConflict := baseFacts()
	replanConflict.Conflict.Classification = "ORDERED_DEPENDENCY"
	result, err = revalidate.Revalidate(clockAt(1000), historical, replanConflict, basePlanDigest)
	if err != nil {
		t.Fatalf("Revalidate replan-conflict case: %v", err)
	}
	if result.Requirement != revalidate.RequirementReplanRequired {
		t.Fatalf("requirement = %s, want %s", result.Requirement, revalidate.RequirementReplanRequired)
	}
	if !slices.Contains(result.ChangedInputs, revalidate.ChangedConflict) {
		t.Fatalf("changed inputs = %v, want to contain %s", result.ChangedInputs, revalidate.ChangedConflict)
	}

	// A denied AuthZ verdict blocks outright.
	blocked := baseFacts()
	blocked.AuthZ.Effect = decision.Deny
	result, err = revalidate.Revalidate(clockAt(1000), historical, blocked, basePlanDigest)
	if err != nil {
		t.Fatalf("Revalidate block case: %v", err)
	}
	if result.Requirement != revalidate.RequirementBlock {
		t.Fatalf("requirement = %s, want %s", result.Requirement, revalidate.RequirementBlock)
	}
	if result.RecomposedState != decision.Deny {
		t.Fatalf("recomposed state = %s, want DENY", result.RecomposedState)
	}
}

// TestTodo_GOVERN_003_Security proves that a changed AuthZ or session
// verdict can never execute on the strength of the historical approval
// alone, and that a tampered historical record is refused outright rather
// than revalidated as if it were genuine.
func TestTodo_GOVERN_003_Security(t *testing.T) {
	facts := baseFacts()
	historical := baseHistorical(t, facts)

	if historical.Decision.State != decision.Allow {
		t.Fatalf("fixture precondition: historical decision must be ALLOW, got %s", historical.Decision.State)
	}

	t.Run("tampered historical record is refused", func(t *testing.T) {
		tampered := historical
		tampered.Decision.Digest = "sha256:forged"
		_, err := revalidate.Revalidate(clockAt(1000), tampered, facts, basePlanDigest)
		if !errors.Is(err, revalidate.ErrTampered) {
			t.Fatalf("Revalidate(tampered) = %v, want ErrTampered", err)
		}
	})

	t.Run("revoked authz cannot execute from the historical approval alone", func(t *testing.T) {
		current := baseFacts()
		current.AuthZ.Effect = decision.Deny
		result, err := revalidate.Revalidate(clockAt(1000), historical, current, basePlanDigest)
		if err != nil {
			t.Fatalf("Revalidate: %v", err)
		}
		// The historical approval itself still says ALLOW: a caller that
		// dispatches on that recorded state alone, without calling
		// Revalidate again, would wrongly proceed.
		if historical.Decision.State != decision.Allow {
			t.Fatalf("fixture precondition: historical decision must still read ALLOW, got %s", historical.Decision.State)
		}
		if result.Confirmed {
			t.Fatal("revoked authz was confirmed unchanged")
		}
		if result.Requirement != revalidate.RequirementBlock {
			t.Fatalf("requirement = %s, want %s: a changed AuthZ verdict must block, not merely require reapproval",
				result.Requirement, revalidate.RequirementBlock)
		}
		// Even a plan digest that still matches does not rescue execution:
		// binding to the plan is necessary, not sufficient.
		if err := result.VerifyBoundPlan(basePlanDigest); err != nil {
			t.Fatalf("VerifyBoundPlan: %v", err)
		}
		if result.Requirement == revalidate.RequirementNone {
			t.Fatal("blocked result must still carry a non-empty requirement after VerifyBoundPlan succeeds")
		}
	})

	t.Run("expired session cannot execute from the historical approval alone", func(t *testing.T) {
		current := baseFacts()
		current.Session.Effect = decision.UnknownFailClosed
		result, err := revalidate.Revalidate(clockAt(1000), historical, current, basePlanDigest)
		if err != nil {
			t.Fatalf("Revalidate: %v", err)
		}
		if result.Confirmed {
			t.Fatal("expired session was confirmed unchanged")
		}
		if result.Requirement != revalidate.RequirementBlock {
			t.Fatalf("requirement = %s, want %s: an expired session must block, not merely require reapproval",
				result.Requirement, revalidate.RequirementBlock)
		}
	})
}

// TestTodo_GOVERN_003_Mutation proves each of the seven revalidated inputs,
// changed alone while every other input holds its historical value, flips
// the result away from confirmed and is named as the exact and only changed
// input.
func TestTodo_GOVERN_003_Mutation(t *testing.T) {
	historical := baseHistorical(t, baseFacts())

	cases := []struct {
		name    string
		mutate  func(*revalidate.Facts)
		wantOne revalidate.ChangedInput
	}{
		{"authz", func(f *revalidate.Facts) { f.AuthZ.PolicyVersion = "authz.p1a.bootstrap.v2" }, revalidate.ChangedAuthZ},
		{"session", func(f *revalidate.Facts) { f.Session.Assurance = "AAL3" }, revalidate.ChangedSession},
		{"source_authority", func(f *revalidate.Facts) { f.SourceAuthority.Decision = "authority.assignment/v2" }, revalidate.ChangedSourceAuthority},
		{"field_classification", func(f *revalidate.Facts) { f.FieldClassification.Version = "classification.taxonomy/2027.1" }, revalidate.ChangedFieldClassification},
		{"legal_policy", func(f *revalidate.Facts) { f.LegalPolicy.LegalPackVersion = "legal.harborcare-demo/2027.1" }, revalidate.ChangedLegalPolicyVersion},
		{"budget_position", func(f *revalidate.Facts) { f.BudgetPosition.Position.ObservationID = "position-obs-v2" }, revalidate.ChangedBudgetPosition},
		{"conflict", func(f *revalidate.Facts) { f.Conflict.EvidenceRef = "merge:rule.promotion.compatible@2" }, revalidate.ChangedConflict},
	}

	baseline, err := revalidate.Revalidate(clockAt(1000), historical, baseFacts(), basePlanDigest)
	if err != nil {
		t.Fatalf("Revalidate baseline: %v", err)
	}
	if !baseline.Confirmed {
		t.Fatalf("baseline is not confirmed: %+v", baseline)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := baseFacts()
			tc.mutate(&mutated)

			result, err := revalidate.Revalidate(clockAt(1000), historical, mutated, basePlanDigest)
			if err != nil {
				t.Fatalf("Revalidate: %v", err)
			}
			if result.Confirmed {
				t.Fatalf("mutating %s did not flip the result away from confirmed", tc.name)
			}
			if result.RecomposedDigest == baseline.RecomposedDigest {
				t.Fatalf("mutating %s did not change the recomposed digest", tc.name)
			}
			if len(result.ChangedInputs) != 1 || result.ChangedInputs[0] != tc.wantOne {
				t.Fatalf("changed inputs = %v, want exactly [%s]", result.ChangedInputs, tc.wantOne)
			}
			if result.Requirement == revalidate.RequirementNone {
				t.Fatalf("mutating %s left the requirement empty", tc.name)
			}
		})
	}
}

func TestTodo_GOVERN_003_InvalidInput(t *testing.T) {
	historical := baseHistorical(t, baseFacts())

	if _, err := revalidate.Revalidate(nil, historical, baseFacts(), basePlanDigest); !errors.Is(err, revalidate.ErrInvalidInput) {
		t.Fatalf("Revalidate(nil clock) = %v, want ErrInvalidInput", err)
	}
	if _, err := revalidate.Revalidate(clockAt(1000), historical, baseFacts(), ""); !errors.Is(err, revalidate.ErrInvalidInput) {
		t.Fatalf("Revalidate(no plan digest) = %v, want ErrInvalidInput", err)
	}

	incomplete := baseFacts()
	incomplete.SourceAuthority.Decision = ""
	if _, err := revalidate.Revalidate(clockAt(1000), historical, incomplete, basePlanDigest); !errors.Is(err, revalidate.ErrInvalidInput) {
		t.Fatalf("Revalidate(incomplete fact) = %v, want ErrInvalidInput", err)
	}

	noSubdecisions := historical
	noSubdecisions.Inputs.Subdecisions = nil
	if _, err := revalidate.Revalidate(clockAt(1000), noSubdecisions, baseFacts(), basePlanDigest); !errors.Is(err, revalidate.ErrInvalidInput) {
		t.Fatalf("Revalidate(missing subdecision sources) = %v, want ErrInvalidInput", err)
	}
}
