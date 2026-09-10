package conflict_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

func conflict004Intent(t *testing.T, id string) conflict.WriteIntent {
	t.Helper()
	return conflict.WriteIntent{
		TenantID: "00000000-0000-0000-0000-000000000001", ID: id, ProposalID: "proposal-" + id,
		Footprints:     []conflict.WriteFootprint{baseFootprint(t)},
		SnapshotDigest: "snapshot-42",
	}
}

func conflict004Request(t *testing.T) conflict.ReevaluateRequest {
	t.Helper()
	pinned := []conflict.WriteIntent{conflict004Intent(t, "intent-left"), conflict004Intent(t, "intent-right")}
	current := []conflict.WriteIntent{conflict004Intent(t, "intent-left"), conflict004Intent(t, "intent-right")}
	return conflict.ReevaluateRequest{PolicyVersion: "v2", Pinned: pinned, Current: current}
}

// TestTodo_CONFLICT_004 proves execution preflight re-evaluates the whole
// cross-workflow conflict set: post-approval creations surface, footprint
// and interval drift replan, overlaps block with explanations, and the
// blocked path never mutates anything.
func TestTodo_CONFLICT_004(t *testing.T) {
	preflight, err := conflict.ReevaluatePreflight(conflict004Request(t))
	if err != nil {
		t.Fatalf("identical sets rejected: %v", err)
	}
	if preflight.Verdict != conflict.PreflightClear {
		t.Fatalf("verdict = %s, want CLEAR", preflight.Verdict)
	}
	if preflight.Digest == "" {
		t.Fatal("preflight carries no digest")
	}

	t.Run("post-approval creation needs reapproval", func(t *testing.T) {
		req := conflict004Request(t)
		late := conflict004Intent(t, "intent-late")
		late.Footprints[0].Field = "employment.assignment.manager_ref"
		req.Current = append(req.Current, late)
		preflight, err := conflict.ReevaluatePreflight(req)
		if err != nil {
			t.Fatal(err)
		}
		if preflight.Verdict != conflict.PreflightReapprovalRequired {
			t.Fatalf("verdict = %s, want REAPPROVAL_REQUIRED", preflight.Verdict)
		}
		if len(preflight.New) != 1 || preflight.New[0] != "intent-late" {
			t.Fatalf("new intents = %+v", preflight.New)
		}
	})

	t.Run("withdrawn intent needs replan", func(t *testing.T) {
		req := conflict004Request(t)
		req.Current = req.Current[:1]
		preflight, err := conflict.ReevaluatePreflight(req)
		if err != nil {
			t.Fatal(err)
		}
		if preflight.Verdict != conflict.PreflightReplanRequired {
			t.Fatalf("verdict = %s, want REPLAN_REQUIRED", preflight.Verdict)
		}
	})

	t.Run("overlap blocks with explanation", func(t *testing.T) {
		overlapping := conflict004Request(t)
		overlapping.Pinned = []conflict.WriteIntent{conflict004Intent(t, "intent-left")}
		overlapping.Current = []conflict.WriteIntent{conflict004Intent(t, "intent-left"), conflict004Intent(t, "intent-clash")}
		blocked, err := conflict.ReevaluatePreflight(overlapping)
		if err != nil {
			t.Fatal(err)
		}
		if blocked.Verdict != conflict.PreflightBlocked {
			t.Fatalf("verdict = %s, want BLOCKED", blocked.Verdict)
		}
		if len(blocked.Overlaps) == 0 {
			t.Fatal("blocked verdict explains no overlap")
		}
	})

	t.Run("blocked path mutates nothing", func(t *testing.T) {
		req := conflict004Request(t)
		req.Current = append(req.Current, conflict004Intent(t, "intent-clash"))
		before, err := conflict.ReevaluatePreflight(req)
		if err != nil {
			t.Fatal(err)
		}
		if before.Verdict != conflict.PreflightBlocked {
			t.Fatalf("verdict = %s, want BLOCKED", before.Verdict)
		}
		after, err := conflict.ReevaluatePreflight(req)
		if err != nil {
			t.Fatal(err)
		}
		if after.Digest != before.Digest {
			t.Fatal("blocked preflight is not repeatable")
		}
		if len(req.Current) != 3 || len(req.Pinned) != 2 {
			t.Fatal("preflight mutated its request")
		}
	})
}
