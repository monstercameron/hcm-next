package time

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

func TestDigest_IsDeterministic(t *testing.T) {
	if got, want := digest("profile", "x", "y"), digest("profile", "x", "y"); got != want {
		t.Fatalf("digest drifted: %q vs %q", got, want)
	}
	if digest("profile", "x", "z") == digest("profile", "x", "y") {
		t.Fatal("digest ignored an input")
	}
}

func TestReads_ObserveReturnsDeclaredBridgeOutcome(t *testing.T) {
	env := GoldenEnvironment()
	env.ObserveOutcome = "PARTIAL"
	env.ObserveWatermark = ""
	observation, err := (Reads{Env: env}).Observe(context.Background(), simulate.ObservationRequest{NodeID: NodeObservePayrollBridge})
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if observation.Outcome != "PARTIAL" || observation.Watermark != "" {
		t.Fatalf("observation = %+v, want PARTIAL with no watermark", observation)
	}
}

func TestApprovals_WouldAwaitOneWorkItemPerRequirement(t *testing.T) {
	items, err := (Approvals{}).WouldAwait(context.Background(), simulate.ApprovalRequest{
		NodeID:          NodeEndAcceptedBridgeConfirmed,
		RequirementRefs: []string{ApprovalPayrollBridge, ApprovalTimekeepingAdmin},
	})
	if err != nil {
		t.Fatalf("WouldAwait: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("work items = %d, want 2", len(items))
	}
	for _, item := range items {
		if item.State != simulate.WouldAwait || len(item.Candidates) == 0 {
			t.Errorf("invalid approval work item: %+v", item)
		}
	}
}
