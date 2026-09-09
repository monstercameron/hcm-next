package mobility

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

func TestSmoke_GoldenWalkReachesPendingApprovals(t *testing.T) {
	setup, err := NewSetup(GoldenEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Terminal.NodeID != NodeEndPendingApprovals {
		t.Fatalf("terminal = %q, want %q", receipt.Terminal.NodeID, NodeEndPendingApprovals)
	}
}
