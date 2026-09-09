package termination

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// EffectiveDateText is the reference scenario's fixed effective date, in the
// same spirit as the promotion reference's pinned 2026-10-01: a conformance
// fixture never derives its own scenario date from the wall clock.
const EffectiveDateText = "2026-11-16"

// defaultRequesterID is a principal distinct from GoldenEnvironment's
// CurrentManagerID, so the golden walk does not itself trip the
// separation-of-duties check.
const defaultRequesterID = "principal:hr-partner-4"

// Setup is a ready-to-run simulation of the termination-and-offboarding
// reference workflow: the compiled plan, the declared inputs and the wired
// zero-effect environment behind every port.
type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  simulate.Inputs
	Options simulate.Options
}

// Params overrides the parts of the workflow's own declared inputs a
// CONF-005 scenario needs to vary. Zero value is the golden scenario: legal
// context supplied, the default (non-manager) requester and no cancellation.
type Params struct {
	// OmitLegalContext leaves out the LegalContext snapshot the
	// jurisdiction/legal-hold resolution node's declared requirement expects
	// - CONF-005's "omitted section" RED case.
	OmitLegalContext bool
	// RequesterID overrides the workflow's requester_id input. Empty means
	// [defaultRequesterID].
	RequesterID string
	// CancelRequested overrides the workflow's cancel_requested input.
	CancelRequested bool
}

// NewSetup wires env against the compiled reference workflow with the
// golden Params (legal context supplied, no cancellation, a requester
// distinct from the current manager).
func NewSetup(env *Environment) (*Setup, error) { return newSetup(env, Params{}) }

// NewSetupWithParams wires env with an explicit override of the scenario
// parameters CONF-005's RED/GREEN cases vary.
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
		return nil, fmt.Errorf("termination: reference must compile: %w", err)
	}
	effective, err := values.ParseLocalDate(EffectiveDateText)
	if err != nil {
		return nil, fmt.Errorf("termination: effective date: %w", err)
	}
	requester := params.RequesterID
	if requester == "" {
		requester = defaultRequesterID
	}

	inputs := simulate.Inputs{
		Values: simulate.Bag{
			"worker_id":        simulate.NewBranded("WorkerID", "22222222-2222-4222-8222-222222222222"),
			"employment_id":    simulate.NewBranded("EmploymentID", "EMP-9001"),
			"requester_id":     simulate.NewBranded("WorkerID", requester),
			"termination_type": simulate.NewString("INVOLUNTARY_PERFORMANCE"),
			"effective_date":   simulate.NewLocalDate(effective),
			"cancel_requested": simulate.NewBool(params.CancelRequested),
		},
	}
	if !params.OmitLegalContext {
		inputs.Context = map[string]simulate.Bag{
			"LegalContext": {
				"jurisdiction":             simulate.NewString("US-CA"),
				"applicable_rule_versions": simulate.NewString("legal.termination.us-ca/2026.1"),
			},
		}
	}

	return &Setup{
		Plan:   plan,
		Env:    env,
		Inputs: inputs,
		Options: simulate.Options{
			Capabilities: registry,
			SubjectRef:   "principal:hr-partner-4",
			Decisions:    Decisions{},
			Transforms:   Transforms{},
			Reads:        Reads{Env: env},
			Approvals:    Approvals{},
			Controls: []simulate.ControlVersion{
				{Name: "hcmnext.workflow.conformance.termination.environment", Version: "v1"},
			},
		},
	}, nil
}
