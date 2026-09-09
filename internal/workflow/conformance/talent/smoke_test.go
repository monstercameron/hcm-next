package talent

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// TestSmoke_Scenarios is a fast sanity check across every scenario variant,
// before the full CONF-012 matrix in conformance_test.go exercises each in
// detail.
func TestSmoke_Scenarios(t *testing.T) {
	cases := []struct {
		name   string
		env    *Environment
		params Params
		want   string
	}{
		{"golden", GoldenEnvironment(), Params{}, NodeEndPendingApprovals},
		{"stale_population", StalePopulationEnvironment(), Params{}, NodeEndStalePopulation},
		{"unauthorized_rating", GoldenEnvironment(), Params{RatingAuthorID: "principal:mismatched-author"}, NodeEndUnauthorizedRating},
		{"calibration_conflict", CalibrationConflictEnvironment(), Params{}, NodeEndCalibrationConflict},
		{"correction", GoldenEnvironment(), Params{IsCorrection: true, PriorAssessmentDigest: "sha256:prior-assessment-fixture-0001"}, NodeEndRevisionCreated},
		{"degraded_observation", DegradedObservationEnvironment(), Params{}, NodeEndDegradedRepair},
		{"missing_policy_context", GoldenEnvironment(), Params{OmitPolicyContext: true}, NodeEndUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setup, err := NewSetupWithParams(c.env, c.params)
			if err != nil {
				t.Fatalf("NewSetupWithParams: %v", err)
			}
			receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if receipt.Terminal.NodeID != c.want {
				t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, c.want, receipt.NodeIDs())
			}
		})
	}
}
