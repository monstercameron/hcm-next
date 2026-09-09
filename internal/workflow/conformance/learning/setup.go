package learning

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

// Setup is a ready-to-run simulation of the learning credential-satisfaction
// reference workflow: the compiled plan, the declared inputs and the wired
// zero-effect environment behind every port.
type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  simulate.Inputs
	Options simulate.Options
}

// Params overrides the parts of the workflow's own declared inputs a
// CONF-013 scenario needs to vary. Zero value is the golden scenario: a
// PolicyContext snapshot is supplied.
type Params struct {
	// OmitPolicyContext leaves out the PolicyContext snapshot the waiver
	// read node's declared requirement expects - CONF-013's "omitted
	// section" RED case: a waiver's authority is never resolved from a
	// silently-defaulted "no waiver applies".
	OmitPolicyContext bool
}

// NewSetup wires env against the compiled reference workflow with the
// golden Params (a PolicyContext snapshot supplied).
func NewSetup(env *Environment) (*Setup, error) { return newSetup(env, Params{}) }

// NewSetupWithParams wires env with an explicit override of the scenario
// parameters CONF-013's RED/GREEN cases vary.
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
		return nil, fmt.Errorf("learning: reference must compile: %w", err)
	}
	effective, err := values.ParseLocalDate(EffectiveDateText)
	if err != nil {
		return nil, fmt.Errorf("learning: effective date: %w", err)
	}

	inputs := simulate.Inputs{
		Values: simulate.Bag{
			"worker_id":      simulate.NewBranded("WorkerID", "33333333-3333-4333-8333-333333333333"),
			"requirement_id": simulate.NewBranded("RequirementID", "REQ-LND-4400"),
			"effective_date": simulate.NewLocalDate(effective),
		},
	}
	if !params.OmitPolicyContext {
		inputs.Context = map[string]simulate.Bag{
			"PolicyContext": {
				"waiver_authority_scope": simulate.NewString("scope-authority:learning-waiver"),
				"policy_version":         simulate.NewString("legal.learning.waiver/2026.1"),
			},
		}
	}

	return &Setup{
		Plan:   plan,
		Env:    env,
		Inputs: inputs,
		Options: simulate.Options{
			Capabilities: registry,
			SubjectRef:   "principal:lnd-partner-7",
			Decisions:    Decisions{},
			Transforms:   Transforms{},
			Reads:        Reads{Env: env},
			Approvals:    Approvals{},
			Controls: []simulate.ControlVersion{
				{Name: "hcmnext.workflow.conformance.learning.environment", Version: "v1"},
			},
		},
	}, nil
}
