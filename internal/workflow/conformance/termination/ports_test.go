package termination

import (
	"context"
	"testing"

	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

// TestDigest_IsDeterministic is a smoke check on the small content-identity
// helper every transform in this package uses.
func TestDigest_IsDeterministic(t *testing.T) {
	a := digest("profile", "x", "y")
	b := digest("profile", "x", "y")
	if a != b {
		t.Fatalf("digest is not deterministic: %q vs %q", a, b)
	}
	if c := digest("profile", "x", "z"); c == a {
		t.Fatalf("digest did not change with a changed input")
	}
}

// TestApprovals_WouldAwaitOneWorkItemPerRequirement is a smoke check that
// every declared requirement ref raises exactly one work item.
func TestApprovals_WouldAwaitOneWorkItemPerRequirement(t *testing.T) {
	items, err := Approvals{}.WouldAwait(context.Background(), simulate.ApprovalRequest{
		NodeID:          NodeEndPendingObligations,
		RequirementRefs: []string{ApprovalHR, ApprovalFinance},
	})
	if err != nil {
		t.Fatalf("WouldAwait: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("work items = %d, want 2", len(items))
	}
	for _, item := range items {
		if item.State != simulate.WouldAwait {
			t.Errorf("item %s state = %q, want %q", item.RequirementID, item.State, simulate.WouldAwait)
		}
	}
}
