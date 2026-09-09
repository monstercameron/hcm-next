package talent

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

// Setup is a ready-to-run simulation of the talent performance calibration
// reference workflow: the compiled plan, the declared inputs and the wired
// zero-effect environment behind every port.
type Setup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  simulate.Inputs
	Options simulate.Options
}

// Params overrides the parts of the workflow's own declared inputs a
// CONF-012 scenario needs to vary. Zero value is the golden scenario: policy
// context supplied, a rating author equal to the environment's rater of
// record (so the golden walk never accidentally trips the authority-mismatch
// check) and no correction.
type Params struct {
	// RatingAuthorID overrides the workflow's rating_author_id input. Empty
	// means the environment's own RaterID - the golden, non-mismatched
	// scenario. Set it to any other value to reach CONF-012's "unauthorized
	// rating" RED case.
	RatingAuthorID string
	// OmitPolicyContext leaves out the PolicyContext snapshot the
	// calibration-committee resolution node's declared requirement expects -
	// CONF-012's "omitted section" RED case.
	OmitPolicyContext bool
	// IsCorrection overrides the workflow's is_correction input.
	IsCorrection bool
	// PriorAssessmentDigest overrides the workflow's prior_assessment_digest
	// input. A correction only takes the new-revision route when this is
	// paired with IsCorrection=true and a non-empty digest.
	PriorAssessmentDigest string
}

// NewSetup wires env against the compiled reference workflow with the golden
// Params (policy context supplied, no mismatch, no correction).
func NewSetup(env *Environment) (*Setup, error) { return newSetup(env, Params{}) }

// NewSetupWithParams wires env with an explicit override of the scenario
// parameters CONF-012's RED/GREEN cases vary.
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
		return nil, fmt.Errorf("talent: reference must compile: %w", err)
	}
	effective, err := values.ParseLocalDate(EffectiveDateText)
	if err != nil {
		return nil, fmt.Errorf("talent: effective date: %w", err)
	}
	ratingAuthor := params.RatingAuthorID
	if ratingAuthor == "" {
		ratingAuthor = env.RaterID
	}

	inputs := simulate.Inputs{
		Values: simulate.Bag{
			"worker_id":               simulate.NewBranded("WorkerID", "33333333-3333-4333-8333-333333333333"),
			"review_cycle_id":         simulate.NewBranded("ReviewCycleID", "RC-2026-H2"),
			"rating_author_id":        simulate.NewBranded("WorkerID", ratingAuthor),
			"effective_date":          simulate.NewLocalDate(effective),
			"is_correction":           simulate.NewBool(params.IsCorrection),
			"prior_assessment_digest": simulate.NewString(params.PriorAssessmentDigest),
		},
	}
	if !params.OmitPolicyContext {
		inputs.Context = map[string]simulate.Bag{
			"PolicyContext": {
				"calibration_policy_version": simulate.NewString("policy.talent.calibration/2026.2"),
				"committee_roster_version":   simulate.NewString("roster.talent.calibration_committee/2026.2"),
			},
		}
	}

	return &Setup{
		Plan:   plan,
		Env:    env,
		Inputs: inputs,
		Options: simulate.Options{
			Capabilities: registry,
			SubjectRef:   "principal:hr-partner-talent-1",
			Decisions:    Decisions{},
			Transforms:   Transforms{},
			Reads:        Reads{Env: env},
			Approvals:    Approvals{},
			Controls: []simulate.ControlVersion{
				{Name: "hcmnext.workflow.conformance.talent.environment", Version: "v1"},
			},
		},
	}, nil
}
