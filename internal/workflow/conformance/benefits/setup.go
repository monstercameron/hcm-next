package benefits

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// EffectiveDateText is the reference scenario's fixed effective date, in the
// same spirit as the promotion reference's pinned 2026-10-01: a conformance
// fixture never derives its own scenario date from the wall clock.
const EffectiveDateText = "2026-12-01"

// Setup is a ready-to-run simulation of the benefits eligibility/election/
// carrier-reconciliation reference workflow: the compiled plan, the
// declared inputs and the wired zero-effect environment behind every port.
type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  simulate.Inputs
	Options simulate.Options
}

// Params overrides the parts of the workflow's own declared inputs a
// CONF-010 scenario needs to vary. Zero value is the golden scenario: no
// prior election exists.
type Params struct {
	// PriorElectionDigest overrides the workflow's prior_election_digest
	// input, so a caller can prove a second election revision produces a
	// different election digest than the first.
	PriorElectionDigest string
}

// NewSetup wires env against the compiled reference workflow with the
// golden Params (no prior election).
func NewSetup(env *Environment) (*Setup, error) { return newSetup(env, Params{}) }

// NewSetupWithParams wires env with an explicit override of the scenario
// parameters CONF-010's RED/GREEN cases vary.
func NewSetupWithParams(env *Environment, params Params) (*Setup, error) {
	return newSetup(env, params)
}

func newSetup(env *Environment, params Params) (*Setup, error) {
	registry, err := env.Registry()
	if err != nil {
		return nil, err
	}
	plan, err := Compile(registry)
	if err != nil {
		return nil, fmt.Errorf("benefits: reference must compile: %w", err)
	}
	effective, err := values.ParseLocalDate(EffectiveDateText)
	if err != nil {
		return nil, fmt.Errorf("benefits: effective date: %w", err)
	}

	inputs := simulate.Inputs{
		Values: simulate.Bag{
			"worker_id":             simulate.NewBranded("WorkerID", "33333333-3333-4333-8333-333333333333"),
			"life_event_id":         simulate.NewBranded("LifeEventID", "LE-2026-1130"),
			"plan_id":               simulate.NewBranded("BenefitPlanID", "PLAN-MEDICAL-PPO"),
			"prior_election_digest": simulate.NewString(params.PriorElectionDigest),
			"effective_date":        simulate.NewLocalDate(effective),
		},
	}

	return &Setup{
		Plan:   plan,
		Env:    env,
		Inputs: inputs,
		Options: simulate.Options{
			Capabilities: registry,
			SubjectRef:   "principal:benefits-admin-6",
			Decisions:    Decisions{},
			Transforms:   Transforms{},
			Reads:        Reads{Env: env},
			Approvals:    Approvals{},
			Controls: []simulate.ControlVersion{
				{Name: "hcmnext.workflow.conformance.benefits.environment", Version: "v1"},
			},
		},
	}, nil
}
