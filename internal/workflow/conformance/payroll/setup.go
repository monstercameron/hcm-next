package payroll

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// CutoffDateText is the reference scenario's fixed cutoff date, in the same
// spirit as the promotion reference's pinned 2026-10-01: a conformance
// fixture never derives its own scenario date from the wall clock.
const CutoffDateText = "2026-11-30"

// GrossPayText is the reference scenario's fixed gross-pay input: an exact
// decimal amount, never a float literal.
const GrossPayText = "4000.00"

// Setup is a ready-to-run simulation of the payroll run/calculation/release/
// settlement reference workflow: the compiled plan, the declared inputs and
// the wired zero-effect environment behind every port.
type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  simulate.Inputs
	Options simulate.Options
}

// Params overrides the parts of the workflow's own declared inputs a
// CONF-009 scenario needs to vary. Zero value is the golden scenario: no
// reversal requested.
type Params struct {
	// ReversalRequested overrides the workflow's reversal_requested input.
	ReversalRequested bool
}

// NewSetup wires env against the compiled reference workflow with the
// golden Params (no reversal requested).
func NewSetup(env *Environment) (*Setup, error) { return newSetup(env, Params{}) }

// NewSetupWithParams wires env with an explicit override of the scenario
// parameters CONF-009's RED/GREEN cases vary.
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
		return nil, fmt.Errorf("payroll: reference must compile: %w", err)
	}
	cutoff, err := values.ParseLocalDate(CutoffDateText)
	if err != nil {
		return nil, fmt.Errorf("payroll: cutoff date: %w", err)
	}
	gross, err := values.NewMoney(GrossPayText, "USD", 2, values.RoundingHalfEven)
	if err != nil {
		return nil, fmt.Errorf("payroll: gross pay: %w", err)
	}

	inputs := simulate.Inputs{
		Values: simulate.Bag{
			"run_id":                 simulate.NewBranded("PayrollRunID", "PR-2026-11-B"),
			"population_snapshot_id": simulate.NewBranded("PopulationSnapshotID", "POP-2026-11-B"),
			"cutoff_date":            simulate.NewLocalDate(cutoff),
			"gross_pay_input":        simulate.NewMoney(gross),
			"reversal_requested":     simulate.NewBool(params.ReversalRequested),
		},
	}

	return &Setup{
		Plan:   plan,
		Env:    env,
		Inputs: inputs,
		Options: simulate.Options{
			Capabilities: registry,
			SubjectRef:   "principal:payroll-controller-2",
			Decisions:    Decisions{},
			Transforms:   Transforms{},
			Reads:        Reads{Env: env},
			Approvals:    Approvals{},
			Controls: []simulate.ControlVersion{
				{Name: "hcmnext.workflow.conformance.payroll.environment", Version: "v1"},
			},
		},
	}, nil
}
