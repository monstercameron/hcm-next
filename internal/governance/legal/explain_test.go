package legal

import (
	"strings"
	"testing"
)

func TestExplainNamesIdentityJurisdictionAndDigest(t *testing.T) {
	ca, err := CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	got := ca.Explain()
	for _, want := range []string{ca.PackID, ca.Jurisdiction.String(), ca.ReviewStatus.String(), "supersedes=none", "superseded_by=none"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Explain() = %q, want substring %q", got, want)
		}
	}
}

func TestRollbackTargetIsAbsentWithoutASupersedes(t *testing.T) {
	ca, err := CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	if _, ok := ca.RollbackTarget(); ok {
		t.Fatal("a first release should have no rollback target")
	}
}

func TestRollbackTargetReportsThePredecessor(t *testing.T) {
	ca, err := CaliforniaPromotionPack()
	if err != nil {
		t.Fatalf("CaliforniaPromotionPack: %v", err)
	}
	predecessor := ca.Release()
	successor := ca
	successor.Version = ca.Version + 1
	successor.Supersedes = &predecessor

	target, ok := successor.RollbackTarget()
	if !ok {
		t.Fatal("expected a rollback target")
	}
	if target != predecessor {
		t.Fatalf("RollbackTarget() = %+v, want %+v", target, predecessor)
	}
	if !strings.Contains(successor.Explain(), "supersedes="+predecessor.PackID) {
		t.Fatalf("Explain() should name the supersedes target: %q", successor.Explain())
	}
}
