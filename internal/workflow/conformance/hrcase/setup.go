package hrcase

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// EffectiveDateText is the reference scenario's fixed effective date, in the
// same spirit as the promotion reference's pinned 2026-10-01: a conformance
// fixture never derives its own scenario date from the wall clock.
const EffectiveDateText = "2026-11-23"

// defaultSubjectWorkerID and defaultInvestigatorID are distinct principals,
// so the golden walk does not itself trip the investigator-conflict check.
const (
	defaultCaseID          = "CASE-7000"
	defaultSubjectWorkerID = "33333333-3333-4333-8333-333333333333"
	defaultInvestigatorID  = "44444444-4444-4444-8444-444444444444"
	defaultRequesterID     = "principal:er-partner-7"
	defaultFindingSummary  = "investigation substantiated a policy violation; corrective action recommended"
)

// Setup is a ready-to-run simulation of the HR case investigation-and-
// disposition reference workflow: the compiled plan, the declared inputs and
// the wired zero-effect environment behind every port.
type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  simulate.Inputs
	Options simulate.Options
}

// Params overrides the parts of the workflow's own declared inputs a
// CONF-014 scenario needs to vary. Zero value is the golden scenario: legal
// context supplied, an investigator distinct from the subject, the default
// requester and no appeal or already-disposed flag.
type Params struct {
	// OmitLegalContext leaves out the LegalContext snapshot the
	// matter-wall-and-hold resolution node's declared requirement expects -
	// CONF-014's "omitted section" RED case.
	OmitLegalContext bool
	// RequesterID overrides the workflow's requester_id input. Empty means
	// [defaultRequesterID].
	RequesterID string
	// InvestigatorID overrides the workflow's investigator_id input. Empty
	// means [defaultInvestigatorID]. Setting it equal to the subject worker
	// id is CONF-014's "investigator conflict" RED case.
	InvestigatorID string
	// AppealRequested overrides the workflow's appeal_requested input.
	AppealRequested bool
	// CaseAlreadyDisposed overrides the workflow's case_already_disposed
	// input.
	CaseAlreadyDisposed bool
}

// NewSetup wires env against the compiled reference workflow with the
// golden Params (legal context supplied, no appeal, no already-disposed
// flag, an investigator distinct from the subject).
func NewSetup(env *Environment) (*Setup, error) { return newSetup(env, Params{}) }

// NewSetupWithParams wires env with an explicit override of the scenario
// parameters CONF-014's RED/GREEN cases vary.
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
		return nil, fmt.Errorf("hrcase: reference must compile: %w", err)
	}
	effective, err := values.ParseLocalDate(EffectiveDateText)
	if err != nil {
		return nil, fmt.Errorf("hrcase: effective date: %w", err)
	}
	requester := params.RequesterID
	if requester == "" {
		requester = defaultRequesterID
	}
	investigator := params.InvestigatorID
	if investigator == "" {
		investigator = defaultInvestigatorID
	}

	inputs := simulate.Inputs{
		Values: simulate.Bag{
			"case_id":               simulate.NewBranded("CaseID", defaultCaseID),
			"subject_worker_id":     simulate.NewBranded("WorkerID", defaultSubjectWorkerID),
			"investigator_id":       simulate.NewBranded("WorkerID", investigator),
			"requester_id":          simulate.NewBranded("WorkerID", requester),
			"effective_date":        simulate.NewLocalDate(effective),
			"finding_summary":       simulate.NewString(defaultFindingSummary),
			"appeal_requested":      simulate.NewBool(params.AppealRequested),
			"case_already_disposed": simulate.NewBool(params.CaseAlreadyDisposed),
		},
	}
	if !params.OmitLegalContext {
		inputs.Context = map[string]simulate.Bag{
			"LegalContext": {
				"jurisdiction":             simulate.NewString("US-CA"),
				"applicable_rule_versions": simulate.NewString("legal.hrcase.us-ca/2026.1"),
			},
		}
	}

	return &Setup{
		Plan:   plan,
		Env:    env,
		Inputs: inputs,
		Options: simulate.Options{
			Capabilities: registry,
			SubjectRef:   "principal:er-partner-7",
			Decisions:    Decisions{},
			Transforms:   Transforms{},
			Reads:        Reads{Env: env},
			Approvals:    Approvals{},
			Controls: []simulate.ControlVersion{
				{Name: "hcmnext.workflow.conformance.hrcase.environment", Version: "v1"},
			},
		},
	}, nil
}
