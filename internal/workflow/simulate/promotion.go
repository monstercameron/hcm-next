package simulate

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// PromotionSetup is a ready-to-run simulation of the promote-into-management
// reference workflow: the compiled plan, the declared inputs and the wired
// zero-effect environment behind every port.
//
// It exists so the golden is one call rather than fifty lines of assembly
// repeated in every test and every tool, and so there is exactly one place
// where the reference scenario's numbers live.
type PromotionSetup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  Inputs
	Options Options
}

// Reference proposals for the raise-threshold decision. Jane's pinned baseline
// is 165,000.00 USD annual, so:
const (
	// PromotionWithinThresholdPay is a 9.09% raise, under the reference
	// configuration's 10% finance threshold.
	PromotionWithinThresholdPay = "180000.00"
	// PromotionExceedsThresholdPay is a 15.15% raise, over it.
	PromotionExceedsThresholdPay = "190000.00"
)

// NewPromotionSetup wires the reference run: Jane Doe promoted out of
// ENG-SWE3/P3 into the ENG-MGR1/M1 management band, effective 2026-10-01.
//
// This is a real management promotion, so it carries a grade change, and the
// reference threshold table escalates any grade change to FINANCE_REQUIRED.
// The walk therefore ends at the terminal that reports a pending Finance
// Partner approval - which is exactly what
// planning/reference-workflows/promote-into-management.md describes, and why
// P1A declares that approval as an outstanding obligation rather than
// executing an APPROVAL node that does not exist yet.
func NewPromotionSetup(proposedBasePay string) (*PromotionSetup, error) {
	env, err := NewPromotionEnvironment()
	if err != nil {
		return nil, err
	}
	return newSetup(env, proposedBasePay, true)
}

// NewInGradeSetup wires the same plan around an in-grade increase: Jane stays
// in ENG-SWE3/P3 and only her pay moves, under a policy version that permits a
// same-grade change.
//
// It exists because the management promotion always changes grade, and a grade
// change always escalates. Without this variant the plan's WITHIN_THRESHOLD
// branch - and with it the drift observation and the consistent terminal -
// would never be walked by anything, which would make an OBSERVE
// implementation nobody had executed.
func NewInGradeSetup(proposedBasePay string) (*PromotionSetup, error) {
	env, err := NewPromotionEnvironment()
	if err != nil {
		return nil, err
	}
	env.Target = promotion.TargetPlacement{
		JobCode:    "ENG-SWE3",
		Grade:      "P3",
		OrgUnit:    "eng-platform",
		PositionID: "POS-SWE-118",
		PayZone:    "US-WEST",
	}
	env.Policy.Version = "people.promotion.policy.in_grade/1.0.0"
	env.Policy.AllowSameGrade = true
	env.BusinessReason = "In-grade market adjustment for a senior engineer on Team Phoenix"
	return newSetup(env, proposedBasePay, false)
}

// newSetup compiles the reference workflow against the environment's own
// capability registry and wires every port.
func newSetup(env *Environment, proposedBasePay string, gradeChange bool) (*PromotionSetup, error) {
	registry, err := env.Registry()
	if err != nil {
		return nil, err
	}
	plan, err := workflow.CompilePromotionReference(registry)
	if err != nil {
		return nil, fmt.Errorf("simulate: promotion reference must compile: %w", err)
	}
	base, err := fixtures.Money(proposedBasePay, "USD")
	if err != nil {
		return nil, fmt.Errorf("simulate: proposed base pay: %w", err)
	}

	// The grade change is stated rather than inferred inside a rule: whether a
	// move is a grade change is a fact about the proposal, and the threshold
	// table's job is to price it, not to work out what it is.
	decisions := RulesDecisions{BudgetAuthority: env.budgetAuthority(), GradeChange: gradeChange}

	return &PromotionSetup{
		Plan: plan,
		Env:  env,
		Inputs: Inputs{
			Values: Bag{
				"worker_id":         NewBranded("WorkerID", env.Worker.Id),
				"target_job_id":     NewBranded("JobID", env.Target.JobCode),
				"proposed_base_pay": NewMoney(base),
				"effective_date":    NewLocalDate(env.EffectiveDate),
			},
			Context: map[string]Bag{
				// The snapshot node declares a pinned LegalContext requirement
				// whose declared missing behavior is UNKNOWN. Supplying it is
				// what lets the walk proceed past the snapshot rather than
				// routing to the unknown terminal.
				"LegalContext": {
					"jurisdiction":             NewString("US-CA"),
					"applicable_rule_versions": NewString("legal.promotion.us-ca/2026.1"),
				},
			},
		},
		Options: Options{
			Capabilities: registry,
			SubjectRef:   "principal:hr-partner-7",
			Decisions:    decisions,
			Transforms:   PromotionTransforms{Env: env},
			Reads:        PromotionReads(env),
			Approvals:    HumanWorkApprovals{Decisions: decisions, ProposalNodeID: workflow.PromotionNodeBuildProposal},
			Controls: []ControlVersion{
				{Name: "hcmnext.domains.fixtures.corpus", Version: string(fixtures.Tenant)},
				{Name: "hcmnext.domains.promotion.policy", Version: env.Policy.Version},
				{Name: "hcmnext.domains.rewards.annualization", Version: env.Annualization.Version},
			},
		},
	}, nil
}

// ProposedBasePay returns the money value the setup was built with.
func (s *PromotionSetup) ProposedBasePay() (values.Money, error) {
	v, err := s.Inputs.Values.Get("proposed_base_pay")
	if err != nil {
		return values.Money{}, err
	}
	return v.Money()
}
