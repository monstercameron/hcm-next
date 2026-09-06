package benefits

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// TestSmoke_Scenarios is a fast sanity check across every scenario variant,
// before the full CONF-010 matrix in conformance_test.go exercises each in
// detail.
func TestSmoke_Scenarios(t *testing.T) {
	cases := []struct {
		name string
		env  *Environment
		want string
	}{
		{"golden", GoldenEnvironment(), NodeEndPendingObligations},
		{"disputed_fact", DisputedFactEnvironment(), NodeEndDisputedFactBlocked},
		{"expired_window", ExpiredWindowEnvironment(), NodeEndExpiredWindowBlocked},
		{"duplicate_life_event", DuplicateLifeEventEnvironment(), NodeEndDuplicateLifeEventBlocked},
		{"overlapping_election", OverlappingElectionEnvironment(), NodeEndOverlappingElectionBlocked},
		{"degraded_carrier", DegradedCarrierEnvironment(), NodeEndCarrierDegradedRepair},
		{"degraded_deduction", DegradedDeductionEnvironment(), NodeEndDeductionDegradedRepair},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setup, err := NewSetup(c.env)
			if err != nil {
				t.Fatalf("NewSetup: %v", err)
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
