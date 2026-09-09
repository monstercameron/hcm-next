package mobility

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

const EffectiveDateText = "2026-12-01"
const (
	defaultWorkerID   = "55555555-5555-4555-8555-555555555555"
	defaultMobilityID = "MOB-2026-0042"
)

type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  simulate.Inputs
	Options simulate.Options
}
type Params struct {
	OmitLegalContext   bool
	OmitPrivacyContext bool
}

func NewSetup(env *Environment) (*Setup, error) { return NewSetupWithParams(env, Params{}) }
func NewSetupWithParams(env *Environment, params Params) (*Setup, error) {
	registry, err := env.Registry()
	if err != nil {
		return nil, err
	}
	plan, err := Compile(registry)
	if err != nil {
		return nil, fmt.Errorf("mobility: reference must compile: %w", err)
	}
	effective, err := values.ParseLocalDate(EffectiveDateText)
	if err != nil {
		return nil, fmt.Errorf("mobility: effective date: %w", err)
	}
	inputs := simulate.Inputs{Values: simulate.Bag{"worker_id": simulate.NewBranded("WorkerID", defaultWorkerID), "mobility_id": simulate.NewBranded("MobilityID", defaultMobilityID), "effective_date": simulate.NewLocalDate(effective)}}
	inputs.Context = make(map[string]simulate.Bag)
	if !params.OmitLegalContext {
		inputs.Context["LegalContext"] = simulate.Bag{"jurisdiction": simulate.NewString("US-CA/DE-BE"), "applicable_rule_versions": simulate.NewString("mobility.tax_pe/2026.1")}
	}
	if !params.OmitPrivacyContext {
		inputs.Context["PrivacyContext"] = simulate.Bag{"transfer_mechanism": simulate.NewString("SCC+encryption"), "data_categories": simulate.NewString("HR_CONFIDENTIAL"), "policy_version": simulate.NewString("privacy.cross_border/2026.1")}
	}
	return &Setup{Plan: plan, Env: env, Inputs: inputs, Options: simulate.Options{Capabilities: registry, SubjectRef: "principal:mobility-partner-3", Decisions: Decisions{}, Transforms: Transforms{}, Reads: Reads{Env: env}, Approvals: Approvals{}, Controls: []simulate.ControlVersion{{Name: "hcmnext.workflow.conformance.mobility.environment", Version: "v1"}}}}, nil
}

func parseDate(text string) (values.LocalDate, error) { return values.ParseLocalDate(text) }
