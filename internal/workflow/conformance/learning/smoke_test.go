package learning

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// TestSmoke_Scenarios is a fast sanity check across every scenario variant,
// before the full CONF-013 matrix in conformance_test.go exercises each in
// detail.
func TestSmoke_Scenarios(t *testing.T) {
	cases := []struct {
		name   string
		env    *Environment
		params Params
		want   string
	}{
		{"golden", GoldenEnvironment(), Params{}, NodeEndSatisfied},
		{"expiring", ExpiringEnvironment(), Params{}, NodeEndExpiringSatisfaction},
		{"valid_waiver", ValidWaiverEnvironment(), Params{}, NodeEndExemptValidWaiver},
		{"no_evidence", NoEvidenceEnvironment(), Params{}, NodeEndUnsatisfiedNoEvidence},
		{"expired", ExpiredEnvironment(), Params{}, NodeEndUnsatisfiedExpired},
		{"self_claim", SelfClaimEnvironment(), Params{}, NodeEndUnsatisfiedUnverified},
		{"invalid_waiver", InvalidWaiverEnvironment(), Params{}, NodeEndUnsatisfiedInvalidWaiver},
		{"duplicate_enrollment", DuplicateEnrollmentEnvironment(), Params{}, NodeEndDuplicateEnrollment},
		{"degraded_observation", DegradedObservationEnvironment(), Params{}, NodeEndDegradedRepair},
		{"ambiguous_observation", AmbiguousObservationEnvironment(), Params{}, NodeEndUnknown},
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
