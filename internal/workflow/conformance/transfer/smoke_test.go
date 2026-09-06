package transfer

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// TestSmoke_GoldenWalkReachesPendingApprovals is a fast sanity check that the
// reference definition compiles and the golden environment walks to the
// expected terminal, before the full CONF-003 matrix in conformance_test.go
// exercises every RED/GREEN clause in detail.
func TestSmoke_GoldenWalkReachesPendingApprovals(t *testing.T) {
	setup, err := NewSetup(GoldenEnvironment())
	if err != nil {
		t.Fatalf("NewSetup: %v", err)
	}
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if receipt.Terminal.NodeID != NodeEndPendingApprovals {
		t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, NodeEndPendingApprovals, receipt.NodeIDs())
	}
}
