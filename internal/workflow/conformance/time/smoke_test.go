package time

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

func TestSmoke_Scenarios(t *testing.T) {
	cases := []struct {
		name string
		env  *Environment
		want string
	}{
		{"accepted", GoldenEnvironment(), NodeEndAcceptedBridgeConfirmed},
		{"duplicate", DuplicatePunchEnvironment(), NodeEndDuplicatePunch},
		{"post_lock", PostLockEditEnvironment(), NodeEndRejectedPostLockEdit},
		{"stale_reopen", StaleReopenEnvironment(), NodeEndRejectedStaleReopen},
		{"spoof", SpoofSuspectedEnvironment(), NodeEndReviewSpoofSuspected},
		{"offline_replay", OfflineReplayEnvironment(), NodeEndReviewOfflineReplay},
		{"dst_fold", DSTFoldEnvironment(), NodeEndReviewDSTFold},
		{"clock_skew", ClockSkewEnvironment(), NodeEndReviewClockSkew},
		{"bridge_degraded", DegradedBridgeEnvironment(), NodeEndBridgeDegradedRepair},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setup, err := NewSetup(tc.env)
			if err != nil {
				t.Fatalf("NewSetup: %v", err)
			}
			receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if receipt.Terminal.NodeID != tc.want {
				t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, tc.want, receipt.NodeIDs())
			}
		})
	}
}
