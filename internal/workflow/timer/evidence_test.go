package timer

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

var (
	evTenant   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	evTimer    = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	evInstance = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	evReady    = uuid.MustParse("44444444-4444-4444-8444-444444444444")
	evFires    = time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	evAt       = time.Date(2026, 9, 6, 9, 0, 5, 0, time.UTC)
)

func evRow() Timer {
	return Timer{
		TenantID: evTenant, TimerID: evTimer, InstanceID: evInstance, NodeID: "wait.effective_date",
		Key: "digest-of-the-wake-requirement", Kind: "DELAY", State: "PENDING",
		FiresAt: evFires, Version: 1, CreatedAt: evFires.Add(-time.Hour),
	}
}

func TestEvidence_DigestIsStableAcrossIdenticalRecords(t *testing.T) {
	a := newEvidence(EventFired, evRow(), evAt, string(DecisionOnTime), 7, evReady, "")
	b := newEvidence(EventFired, evRow(), evAt, string(DecisionOnTime), 7, evReady, "")
	if a.Digest() == "" {
		t.Fatal("evidence carries no digest")
	}
	if a.Digest() != b.Digest() {
		t.Fatalf("identical evidence digested differently: %s vs %s", a.Digest(), b.Digest())
	}
}

func TestEvidence_EveryFieldMovesTheDigest(t *testing.T) {
	base := newEvidence(EventFired, evRow(), evAt, string(DecisionOnTime), 7, evReady, "")

	otherRow := func(mutate func(*Timer)) Timer {
		row := evRow()
		mutate(&row)
		return row
	}
	variants := map[string]Evidence{
		"kind":     newEvidence(EventSkipped, evRow(), evAt, string(DecisionOnTime), 7, evReady, ""),
		"tenant":   newEvidence(EventFired, otherRow(func(r *Timer) { r.TenantID = evReady }), evAt, string(DecisionOnTime), 7, evReady, ""),
		"timer":    newEvidence(EventFired, otherRow(func(r *Timer) { r.TimerID = evReady }), evAt, string(DecisionOnTime), 7, evReady, ""),
		"instance": newEvidence(EventFired, otherRow(func(r *Timer) { r.InstanceID = evReady }), evAt, string(DecisionOnTime), 7, evReady, ""),
		"node":     newEvidence(EventFired, otherRow(func(r *Timer) { r.NodeID = "wait.other" }), evAt, string(DecisionOnTime), 7, evReady, ""),
		"requirement digest": newEvidence(EventFired, otherRow(func(r *Timer) { r.Key = "another-digest" }),
			evAt, string(DecisionOnTime), 7, evReady, ""),
		"fires at": newEvidence(EventFired, otherRow(func(r *Timer) { r.FiresAt = evFires.Add(time.Minute) }),
			evAt, string(DecisionOnTime), 7, evReady, ""),
		"at":          newEvidence(EventFired, evRow(), evAt.Add(time.Second), string(DecisionOnTime), 7, evReady, ""),
		"decision":    newEvidence(EventFired, evRow(), evAt, string(DecisionCatchUp), 7, evReady, ""),
		"fence token": newEvidence(EventFired, evRow(), evAt, string(DecisionOnTime), 8, evReady, ""),
		"ready work":  newEvidence(EventFired, evRow(), evAt, string(DecisionOnTime), 7, evTimer, ""),
		"reason":      newEvidence(EventFired, evRow(), evAt, string(DecisionOnTime), 7, evReady, "late"),
		"no ready work": newEvidence(EventDeferred, evRow(), evAt, string(DecisionReview), 7, uuid.Nil,
			"needs review"),
	}
	for field, variant := range variants {
		if variant.Digest() == base.Digest() {
			t.Fatalf("changing %s did not move the evidence digest", field)
		}
	}
}

func TestEvidence_InstantsAreNormalizedToUTC(t *testing.T) {
	zone := time.FixedZone("UTC-5", -5*3600)
	row := evRow()
	row.FiresAt = evFires.In(zone)
	shifted := newEvidence(EventFired, row, evAt.In(zone), string(DecisionOnTime), 7, evReady, "")
	base := newEvidence(EventFired, evRow(), evAt, string(DecisionOnTime), 7, evReady, "")
	if shifted.Digest() != base.Digest() {
		t.Fatal("the same instants in another zone digested differently")
	}
}

func TestEvidence_EveryDeclaredEventIsDistinct(t *testing.T) {
	seen := map[EventKind]bool{}
	for _, kind := range []EventKind{
		EventScheduled, EventReplayed, EventFired, EventSkipped, EventCancelled, EventDeferred,
	} {
		if kind == "" {
			t.Fatal("a declared event kind is empty")
		}
		if seen[kind] {
			t.Fatalf("event kind %q is declared twice", kind)
		}
		seen[kind] = true
	}
	if len(seen) != 6 {
		t.Fatalf("expected six declared event kinds, got %d", len(seen))
	}
}
