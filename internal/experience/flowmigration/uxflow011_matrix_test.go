package flowmigration

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// UXFLOW-011 is an exact behavioral matrix. These cases deliberately use the
// public registry boundary so the contract remains isolated from persistence
// or transport implementations.
func TestUXFLOW011FlowLifecycleMatrix(t *testing.T) {
	newReview := func(mapping map[string]string) Review {
		return Review{Reviewer: "reviewer", ReviewedAt: time.Unix(1, 0).UTC(), Mapping: mapping, Replan: true}
	}

	t.Run("pinning and reviewed successor replan", func(t *testing.T) {
		r, v1, v2 := published(t)
		if err := r.Activate("leave", v1.Digest, Review{}); err != nil {
			t.Fatal(err)
		}
		// Publishing a successor never moves the active pin or rewrites existing work.
		w := Work{ID: "w-pin", FlowID: "leave", VersionDigest: v1.Digest, Draft: map[string]string{"hours": "8"}, TaskIDs: []string{"task-1"}}
		if err := r.Activate("leave", v2.Digest, newReview(map[string]string{"submit": "submit-v2"})); err != nil {
			t.Fatal(err)
		}
		active, ok := r.Active("leave")
		if !ok || active.Digest != v2.Digest {
			t.Fatalf("active successor = %#v, %v", active, ok)
		}
		if w.VersionDigest != v1.Digest || w.Draft["hours"] != "8" || len(w.TaskIDs) != 1 {
			t.Fatalf("pinned work changed: %#v", w)
		}
		got, err := r.ResolveLink(Link{FlowID: "leave", VersionDigest: v1.Digest, ActionID: "submit"})
		if err != nil || got.Mode != LinkReplan || !got.Replan || got.ReadOnly {
			t.Fatalf("reviewed successor link = %#v, %v", got, err)
		}
	})

	t.Run("retired stale link is read-only and history remains inspectable", func(t *testing.T) {
		r, v1, v2 := published(t)
		if err := r.Activate("leave", v1.Digest, Review{}); err != nil {
			t.Fatal(err)
		}
		if err := r.Activate("leave", v2.Digest, newReview(nil)); err != nil {
			t.Fatal(err)
		}
		got, err := r.ResolveLink(Link{FlowID: "leave", VersionDigest: v1.Digest, ActionID: "submit"})
		if err != nil || got.Mode != LinkReplan {
			t.Fatalf("successor without action map = %#v, %v", got, err)
		}
		// A reviewed successor still makes the old artifact non-executable.
		seen := map[string]ActionReceipt{}
		if _, err := r.Execute(Work{ID: "w-stale", FlowID: "leave", VersionDigest: v1.Digest}, Link{FlowID: "leave", VersionDigest: v1.Digest, ActionID: "submit"}, "once", seen); !errors.Is(err, ErrStaleLink) {
			t.Fatalf("retired action error = %v", err)
		}
		history := r.Versions("leave")
		if len(history) != 2 || history[0].Digest != v1.Digest || history[1].Digest != v2.Digest {
			t.Fatalf("version history = %#v", history)
		}
	})

	t.Run("rollback preserves history and requires review", func(t *testing.T) {
		r, v1, v2 := published(t)
		if err := r.Activate("leave", v1.Digest, Review{}); err != nil {
			t.Fatal(err)
		}
		if err := r.Activate("leave", v2.Digest, newReview(map[string]string{"submit": "submit-v2"})); err != nil {
			t.Fatal(err)
		}
		if err := r.Activate("leave", v1.Digest, Review{}); !errors.Is(err, ErrReviewRequired) {
			t.Fatalf("unreviewed rollback error = %v", err)
		}
		if err := r.Activate("leave", v1.Digest, newReview(map[string]string{"submit-v2": "submit"})); err != nil {
			t.Fatalf("reviewed rollback: %v", err)
		}
		active, _ := r.Active("leave")
		if active.Digest != v1.Digest {
			t.Fatalf("rollback active = %s, want %s", active.Digest, v1.Digest)
		}
		if got, err := r.ResolveLink(Link{FlowID: "leave", VersionDigest: v2.Digest}); err != nil || got.Mode != LinkReplan {
			t.Fatalf("rolled-back successor link = %#v, %v", got, err)
		}
	})

	t.Run("migration keeps all work and deduplicates action", func(t *testing.T) {
		r, v1, v2 := published(t)
		if err := r.Activate("leave", v1.Digest, Review{}); err != nil {
			t.Fatal(err)
		}
		if err := r.Activate("leave", v2.Digest, newReview(map[string]string{"submit": "submit-v2"})); err != nil {
			t.Fatal(err)
		}
		before := Work{ID: "w-migrate", FlowID: "leave", VersionDigest: v1.Digest, Draft: map[string]string{"hours": "8"}, TaskIDs: []string{"task-1"}, ApprovalIDs: []string{"approval-1"}, Completed: map[string]bool{"draft": true}}
		after, receipt, err := r.Migrate(before, v2.Digest, newReview(map[string]string{"submit": "submit-v2"}))
		if err != nil {
			t.Fatal(err)
		}
		if after.VersionDigest != v2.Digest || !reflect.DeepEqual(after.Draft, before.Draft) || !reflect.DeepEqual(after.TaskIDs, before.TaskIDs) || !reflect.DeepEqual(after.ApprovalIDs, before.ApprovalIDs) || !reflect.DeepEqual(after.Completed, before.Completed) {
			t.Fatalf("migration lost work: before=%#v after=%#v", before, after)
		}
		if !receipt.PreservedDraft || !receipt.PreservedTasks || !receipt.PreservedApprovals || receipt.ActionMap["submit"] != "submit-v2" || receipt.ReceiptDigest == "" {
			t.Fatalf("migration receipt = %#v", receipt)
		}
		seen := map[string]ActionReceipt{}
		first, err := r.Execute(after, Link{FlowID: "leave", VersionDigest: v2.Digest, ActionID: "submit-v2"}, "idempotency-1", seen)
		if err != nil {
			t.Fatal(err)
		}
		second, err := r.Execute(after, Link{FlowID: "leave", VersionDigest: v2.Digest, ActionID: "submit-v2"}, "idempotency-1", seen)
		if err != nil || !second.Duplicate || first.ActionDigest != second.ActionDigest {
			t.Fatalf("duplicate receipt: first=%#v second=%#v err=%v", first, second, err)
		}
	})
}
