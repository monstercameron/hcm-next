package transfer

import (
	"fmt"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// EffectiveDateText is the reference scenario's fixed effective date, in the
// same spirit as the promotion reference's pinned 2026-10-01: a conformance
// fixture never derives its own scenario date from the wall clock.
const EffectiveDateText = "2026-11-02"

// Setup is a ready-to-run simulation of the cross-company transfer reference
// workflow: the compiled plan, the declared inputs and the wired zero-effect
// environment behind every port.
type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  simulate.Inputs
	Options simulate.Options
}

// NewSetup wires env against the compiled reference workflow, with the
// LegalContext the jurisdiction-resolution node's declared requirement
// expects.
func NewSetup(env *Environment) (*Setup, error) { return newSetup(env, true) }

// NewSetupMissingLegalContext wires env without the LegalContext snapshot,
// which is CONF-003's RED case: jurisdiction is an omitted section, and the
// declared MissingBehavior is UNKNOWN, not a defaulted "no jurisdiction
// applies".
func NewSetupMissingLegalContext(env *Environment) (*Setup, error) { return newSetup(env, false) }

func newSetup(env *Environment, withLegalContext bool) (*Setup, error) {
	registry, err := env.Registry()
	if err != nil {
		return nil, err
	}
	plan, err := Compile(registry)
	if err != nil {
		return nil, fmt.Errorf("transfer: reference must compile: %w", err)
	}
	effective, err := values.ParseLocalDate(EffectiveDateText)
	if err != nil {
		return nil, fmt.Errorf("transfer: effective date: %w", err)
	}

	inputs := simulate.Inputs{
		Values: simulate.Bag{
			"worker_id":              simulate.NewBranded("WorkerID", "11111111-1111-4111-8111-111111111111"),
			"source_company_id":      simulate.NewBranded("CompanyID", "company-acme-labs"),
			"destination_company_id": simulate.NewBranded("CompanyID", "company-acme-cloud"),
			"target_position_id":     simulate.NewBranded("PositionID", "POS-DST-900"),
			"effective_date":         simulate.NewLocalDate(effective),
		},
	}
	if withLegalContext {
		inputs.Context = map[string]simulate.Bag{
			"LegalContext": {
				"jurisdiction":             simulate.NewString("US-CA"),
				"applicable_rule_versions": simulate.NewString("legal.transfer.us-ca/2026.1"),
			},
		}
	}

	return &Setup{
		Plan:   plan,
		Env:    env,
		Inputs: inputs,
		Options: simulate.Options{
			Capabilities: registry,
			SubjectRef:   "principal:hr-partner-9",
			Decisions:    Decisions{},
			Transforms:   Transforms{},
			Reads:        Reads{Env: env},
			Approvals:    Approvals{},
			Controls: []simulate.ControlVersion{
				{Name: "hcmnext.workflow.conformance.transfer.environment", Version: "v1"},
			},
		},
	}, nil
}
