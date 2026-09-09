package performance

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type storeTenant string

func (t storeTenant) String() string { return string(t) }

func storeDigest(ch byte) string {
	return "sha256:" + string(make([]byte, 64))[:0] + string(repeatByte(ch, 64))
}

func repeatByte(ch byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = ch
	}
	return out
}

func storeInstant() values.Instant {
	return values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
}

func TestTodo_PERSIST_PERFORMANCE_001_Memory(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	tenant := storeTenant("tenant-a")
	cycle := CycleRevision{CycleID: "cycle-1", Revision: 1, State: PerformanceCyclePlanned, CanonicalDigest: storeDigest('a')}
	if err := store.SaveCycle(ctx, tenant, cycle); err != nil {
		t.Fatalf("SaveCycle: %v", err)
	}
	if got, err := store.LoadCycle(ctx, tenant, cycle.CycleID, 1); err != nil || got != cycle {
		t.Fatalf("LoadCycle = %+v, %v", got, err)
	}
	if err := store.SaveCycle(ctx, tenant, cycle); !errors.Is(err, ErrDuplicateRevision) {
		t.Fatalf("duplicate cycle = %v", err)
	}
	if err := store.SaveCycle(ctx, tenant, CycleRevision{CycleID: cycle.CycleID, Revision: 3, State: PerformanceCycleOpen, SupersedesRevision: 2, CanonicalDigest: storeDigest('b')}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale cycle = %v", err)
	}

	caseRow := RatingCaseRecord{CaseID: "case-1", ParticipantID: "person-1", CanonicalDigest: storeDigest('c')}
	if err := store.SaveRatingCase(ctx, tenant, caseRow); err != nil {
		t.Fatalf("SaveRatingCase: %v", err)
	}
	event := RatingEventRecord{Kind: RatingEventContestRaised, ActorID: "actor-1", At: storeInstant(), Digest: storeDigest('d')}
	if err := store.AppendRatingEvent(ctx, tenant, caseRow.CaseID, 1, event); err != nil {
		t.Fatalf("AppendRatingEvent: %v", err)
	}
	if err := store.AppendRatingEvent(ctx, tenant, caseRow.CaseID, 1, event); !errors.Is(err, ErrDuplicateEvent) {
		t.Fatalf("duplicate event = %v", err)
	}
	if err := store.FinalizeRatingCase(ctx, tenant, caseRow.CaseID, storeDigest('e')); err != nil {
		t.Fatalf("FinalizeRatingCase: %v", err)
	}
	if err := store.FinalizeRatingCase(ctx, tenant, caseRow.CaseID, storeDigest('f')); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale finalization = %v", err)
	}
}
