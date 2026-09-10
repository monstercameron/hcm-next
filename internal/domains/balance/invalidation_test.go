package balance

import (
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func mustDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	decimal, err := values.NewDecimal(text, 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatalf("NewDecimal(%s): %v", text, err)
	}
	return decimal
}

func invalidationIndex(t *testing.T) *DependencyIndex {
	t.Helper()
	index := NewDependencyIndex()
	for _, component := range []PlanComponent{
		{PlanID: "plan-a", ComponentID: "paid-time", Sources: []string{"bucket:vacation"}, Available: mustDecimal(t, "80.00"), Paid: mustDecimal(t, "32.00"), Unpaid: mustDecimal(t, "8.00"), Revision: "rev-1"},
		{PlanID: "plan-a", ComponentID: "coverage", Sources: []string{"feed:availability"}, Available: mustDecimal(t, "40.00"), Paid: mustDecimal(t, "0.00"), Unpaid: mustDecimal(t, "40.00"), Revision: "rev-1"},
		{PlanID: "plan-b", ComponentID: "paid-time", Sources: []string{"bucket:sick"}, Available: mustDecimal(t, "24.00"), Paid: mustDecimal(t, "16.00"), Unpaid: mustDecimal(t, "0.00"), Revision: "rev-1"},
	} {
		if err := index.Register(component); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}
	return index
}

func TestTodo_BAL_013(t *testing.T) {
	index := invalidationIndex(t)
	// An intervening debit invalidates only the dependent component.
	findings, err := index.Invalidate(AvailabilityEvent{Kind: EventDebit, SourceID: "bucket:vacation", OldRevision: "rev-1", NewRevision: "rev-2", OldAvailable: mustDecimal(t, "80.00"), NewAvailable: mustDecimal(t, "72.00")})
	if err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want exactly the affected component", len(findings))
	}
	finding := findings[0]
	if finding.Finding != "REPLAN_REQUIRED" || finding.PlanID != "plan-a" || finding.ComponentID != "paid-time" {
		t.Fatalf("finding=%+v", finding)
	}
	if finding.OldAvailable != "80.00" || finding.NewAvailable != "72.00" || finding.OldRevision != "rev-1" || finding.NewRevision != "rev-2" {
		t.Fatalf("finding lost its source revisions: %+v", finding)
	}
	// Paid/unpaid allocation travels explicitly: never silently changed.
	if finding.Paid != "32.00" || finding.Unpaid != "8.00" || finding.Digest == "" {
		t.Fatalf("finding=%+v", finding)
	}
	// Unaffected plans remain valid.
	if !index.Valid("plan-a", "coverage", "rev-1", mustDecimal(t, "40.00")) {
		t.Fatal("unrelated component invalidated")
	}
	if !index.Valid("plan-b", "paid-time", "rev-1", mustDecimal(t, "24.00")) {
		t.Fatal("unrelated plan invalidated")
	}
	// A no-move event invalidates nothing.
	quiet, err := index.Invalidate(AvailabilityEvent{Kind: EventCorrection, SourceID: "bucket:sick", OldRevision: "rev-1", NewRevision: "rev-1", OldAvailable: mustDecimal(t, "24.00"), NewAvailable: mustDecimal(t, "24.00")})
	if err != nil || len(quiet) != 0 {
		t.Fatalf("quiet=%v err=%v", quiet, err)
	}
	// RED: unknown kinds, hollow events and hollow registrations refuse.
	if _, err := index.Invalidate(AvailabilityEvent{Kind: "vibes", SourceID: "bucket:vacation"}); err == nil {
		t.Fatal("unknown event kind invalidated")
	}
	if err := index.Register(PlanComponent{PlanID: "plan-a", ComponentID: "paid-time"}); err == nil {
		t.Fatal("duplicate component registered")
	}
	if err := index.Register(PlanComponent{PlanID: "plan-c", ComponentID: "x"}); err == nil {
		t.Fatal("sourceless component registered")
	}
}

func TestTodo_BAL_013_Property(t *testing.T) {
	index := invalidationIndex(t)
	event := AvailabilityEvent{Kind: EventExpiry, SourceID: "bucket:vacation", OldRevision: "rev-1", NewRevision: "rev-2", OldAvailable: mustDecimal(t, "80.00"), NewAvailable: mustDecimal(t, "70.00")}
	first, err := index.Invalidate(event)
	if err != nil || len(first) != 1 {
		t.Fatalf("first=%v err=%v", first, err)
	}
	second, err := index.Invalidate(event)
	if err != nil || len(second) != 1 || second[0].Digest != first[0].Digest {
		t.Fatal("invalidation is not deterministic")
	}
	// Rollover moves availability the same typed way.
	rollover, err := index.Invalidate(AvailabilityEvent{Kind: EventRollover, SourceID: "bucket:sick", OldRevision: "rev-1", NewRevision: "rev-2", OldAvailable: mustDecimal(t, "24.00"), NewAvailable: mustDecimal(t, "30.00")})
	if err != nil || len(rollover) != 1 || rollover[0].NewAvailable != "30.00" {
		t.Fatalf("rollover=%v err=%v", rollover, err)
	}
}

func TestTodo_BAL_013_Race(t *testing.T) {
	ten := mustDecimal(t, "10.00")
	five := mustDecimal(t, "5.00")
	nine := mustDecimal(t, "9.00")
	index := NewDependencyIndex()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			plan := PlanComponent{PlanID: "plan-race", ComponentID: "c" + string(rune('a'+i)), Sources: []string{"bucket:shared"}, Available: ten, Paid: five, Unpaid: five, Revision: "rev-1"}
			_ = index.Register(plan)
		}(i)
	}
	wg.Wait()
	const readers = 8
	counts := make([]int, readers)
	errs := make([]error, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			findings, err := index.Invalidate(AvailabilityEvent{Kind: EventDebit, SourceID: "bucket:shared", OldRevision: "rev-1", NewRevision: "rev-2", OldAvailable: ten, NewAvailable: nine})
			if err != nil {
				errs[i] = err
				return
			}
			counts[i] = len(findings)
		}(i)
	}
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if counts[i] != 8 {
			t.Fatalf("worker %d findings = %d, want 8", i, counts[i])
		}
	}
}

func TestTodo_BAL_013_Mutation(t *testing.T) {
	index := invalidationIndex(t)
	base, err := index.Invalidate(AvailabilityEvent{Kind: EventDebit, SourceID: "bucket:vacation", OldRevision: "rev-1", NewRevision: "rev-2", OldAvailable: mustDecimal(t, "80.00"), NewAvailable: mustDecimal(t, "72.00")})
	if err != nil || len(base) != 1 {
		t.Fatalf("base=%v err=%v", base, err)
	}
	// A deeper debit re-identifies the finding.
	deeper, err := index.Invalidate(AvailabilityEvent{Kind: EventDebit, SourceID: "bucket:vacation", OldRevision: "rev-1", NewRevision: "rev-3", OldAvailable: mustDecimal(t, "80.00"), NewAvailable: mustDecimal(t, "60.00")})
	if err != nil || len(deeper) != 1 || deeper[0].Digest == base[0].Digest {
		t.Fatalf("deeper=%v err=%v", deeper, err)
	}
	// An event on an unindexed source affects nothing.
	foreign, err := index.Invalidate(AvailabilityEvent{Kind: EventDebit, SourceID: "bucket:foreign", OldRevision: "rev-1", NewRevision: "rev-2", OldAvailable: mustDecimal(t, "1.00"), NewAvailable: mustDecimal(t, "0.00")})
	if err != nil || len(foreign) != 0 {
		t.Fatalf("foreign=%v err=%v", foreign, err)
	}
}
