package artifacts_test

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/artifacts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

const sweepJurisdiction = "US-FL"

func sweepRetentionClasses() map[string]model.RetentionClass {
	return map[string]model.RetentionClass{
		retentionClass: {
			ClassRef:          retentionClass,
			DefaultPeriodDays: 1,
			TriggerEvent:      "RECORD_CREATED",
			DispositionOwner:  "records-management",
			AuthorityRef:      "authority:workday",
		},
	}
}

// sweepCandidateIDs runs SweepCandidates in its own transaction and returns
// the candidate content ids.
func sweepCandidateIDs(t *testing.T, f fixture, asOf time.Time) []string {
	t.Helper()
	classes := sweepRetentionClasses()
	var ids []string
	err := f.inTx(func(tx dbport.Tx) error {
		candidates, err := artifacts.SweepCandidates(context.Background(), tx, f.schema, f.tenant, sweepJurisdiction, classes, asOf)
		if err != nil {
			return err
		}
		for _, c := range candidates {
			ids = append(ids, c.ContentID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("sweep candidates: %v", err)
	}
	return ids
}

func containsID(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

// TestTodo_DATA_016_Recovery proves that a currently referenced artifact
// never appears in the sweep listing, that removing its last reference makes
// it eligible once its retention window has elapsed, and that adding a
// reference back recovers its protection -- exactly the same reference state
// [Retrieve]'s subject scope check reads, now read by [SweepCandidates]
// instead.
func TestTodo_DATA_016_Recovery(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	rec := f.mustPut(t, f.putRequest([]byte("retained then referenced record")))
	owner := artifacts.OwnerRef{Kind: artifacts.OwnerProposalRevision, ID: "intent-1:rev-1"}

	// Far enough past created_at that the one-day retention window in
	// sweepRetentionClasses has unambiguously elapsed.
	longAfter := rec.CreatedAt.AddDate(0, 0, 30)

	t.Run("unreferenced and past its window: eligible", func(t *testing.T) {
		ids := sweepCandidateIDs(t, f, longAfter)
		if !containsID(ids, rec.ContentID) {
			t.Fatalf("unreferenced, retention-elapsed artifact %s did not appear in the sweep listing", rec.ContentID)
		}
	})

	t.Run("unreferenced but still inside its window: not eligible", func(t *testing.T) {
		soonAfter := rec.CreatedAt.Add(time.Hour)
		ids := sweepCandidateIDs(t, f, soonAfter)
		if containsID(ids, rec.ContentID) {
			t.Fatalf("artifact %s appeared in the sweep listing before its retention window elapsed", rec.ContentID)
		}
	})

	t.Run("referenced: never eligible, no matter how long past its window", func(t *testing.T) {
		f.mustAddReference(t, rec.ContentID, owner)
		ids := sweepCandidateIDs(t, f, longAfter)
		if containsID(ids, rec.ContentID) {
			t.Fatalf("referenced artifact %s appeared in the sweep listing", rec.ContentID)
		}
	})

	t.Run("reference removed: eligible again", func(t *testing.T) {
		if err := f.removeReference(rec.ContentID, owner); err != nil {
			t.Fatalf("remove reference: %v", err)
		}
		ids := sweepCandidateIDs(t, f, longAfter)
		if !containsID(ids, rec.ContentID) {
			t.Fatalf("artifact %s did not become eligible again after its only reference was removed", rec.ContentID)
		}
	})

	t.Run("reference recovered: protected again", func(t *testing.T) {
		f.mustAddReference(t, rec.ContentID, owner)
		ids := sweepCandidateIDs(t, f, longAfter)
		if containsID(ids, rec.ContentID) {
			t.Fatalf("artifact %s appeared in the sweep listing after its reference was recovered", rec.ContentID)
		}
	})
}
