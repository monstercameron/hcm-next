package hrcase

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

func TestDigest_IsDeterministic(t *testing.T) {
	if digest("profile", "x", "y") != digest("profile", "x", "y") {
		t.Fatal("digest is not deterministic")
	}
	if digest("profile", "x", "y") == digest("profile", "x", "z") {
		t.Fatal("digest ignored changed input")
	}
}

func TestApprovals_WouldAwaitOneWorkItemPerRequirement(t *testing.T) {
	items, err := Approvals{}.WouldAwait(context.Background(), simulate.ApprovalRequest{NodeID: NodeEndPendingDisposition, RequirementRefs: []string{ApprovalLegal, ApprovalHRCaseManager}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("work items = %d, want 2", len(items))
	}
}
