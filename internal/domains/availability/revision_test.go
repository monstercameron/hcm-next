package availability

import (
	"errors"
	"sync"
	"testing"
)

func revisionRequest() Revision {
	return Revision{
		WorkerID: "w1", State: Unavailable,
		IntentID: "intent:leave:w1", ProposalDigest: "sha256:proposal",
		Reason: "leave-start", LeaveEventID: "leave:w1:sep",
		EffectiveStart: 10, EffectiveEnd: 17,
	}
}

func appendRevision(t *testing.T, log *RevisionLog, revision Revision) Revision {
	t.Helper()
	token, err := log.Prepare(revision)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	committed, err := log.Commit(token, nil)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return committed
}

func TestTodo_AVAIL_003(t *testing.T) {
	log := NewRevisionLog()
	committed := appendRevision(t, log, revisionRequest())
	if committed.State != Unavailable || len(committed.Reconciliations) != 2 {
		t.Fatalf("committed=%+v", committed)
	}
	if err := committed.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// History grows append-only: the prior state survives.
	if history := log.History("w1"); len(history) != 1 {
		t.Fatalf("history=%v", history)
	}
	// A restored successor returns to available explicitly.
	restored := revisionRequest()
	restored.PriorRevision = committed.RevisionID
	restored.State = Available
	restored.Restored = true
	restored.LeaveEventID = "leave:w1:return"
	restored.EffectiveStart = 18
	restored.EffectiveEnd = 30
	// Overlaps the prior unavailable interval end-to-end (17 vs 18): no
	// overlap, so it appends.
	back := appendRevision(t, log, restored)
	if back.State != Available || !back.Restored {
		t.Fatalf("back=%+v", back)
	}
	if history := log.History("w1"); len(history) != 2 {
		t.Fatal("history overwrote the prior revision")
	}
	// RED: stale heads, overlapping incompatible intervals, duplicates,
	// event-free availability and failpoints refuse.
	stale := revisionRequest()
	stale.LeaveEventID = "leave:w1:other"
	if _, err := log.Prepare(stale); err == nil {
		t.Fatal("stale head prepared")
	}
	clash := revisionRequest()
	clash.PriorRevision = back.RevisionID
	clash.LeaveEventID = "leave:w1:clash"
	clash.State = Restricted
	if _, err := log.Prepare(clash); err == nil {
		t.Fatal("overlapping incompatible interval prepared")
	}
	eventless := revisionRequest()
	eventless.PriorRevision = back.RevisionID
	eventless.LeaveEventID = ""
	if _, err := log.Prepare(eventless); err == nil {
		t.Fatal("event-free availability prepared")
	}
	// Duplicate leave effects return the existing revision, never a second.
	again, err := log.Prepare(revisionRequest())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if again != committed.RevisionID {
		t.Fatalf("duplicate effect prepared %q", again)
	}
	failLog := NewRevisionLog()
	token, err := failLog.Prepare(revisionRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failLog.Commit(token, func(step string) error { return errors.New("injected fault: " + step) }); err == nil {
		t.Fatal("failpoint committed")
	}
	if history := failLog.History("w1"); len(history) != 0 {
		t.Fatal("failpoint produced availability without its leave event")
	}
}

func TestTodo_AVAIL_003_Property(t *testing.T) {
	log := NewRevisionLog()
	first := appendRevision(t, log, revisionRequest())
	// Successors chain on exact priors: the log is a linked history.
	second := revisionRequest()
	second.PriorRevision = first.RevisionID
	second.LeaveEventID = "leave:w1:second"
	second.EffectiveStart = 18
	second.EffectiveEnd = 20
	linked := appendRevision(t, log, second)
	if linked.PriorRevision != first.RevisionID {
		t.Fatalf("linked=%+v", linked)
	}
	// Restricted successors append the same bounded way.
	third := revisionRequest()
	third.PriorRevision = linked.RevisionID
	third.State = Restricted
	third.LeaveEventID = "leave:w1:third"
	third.EffectiveStart = 21
	third.EffectiveEnd = 22
	bounded := appendRevision(t, log, third)
	if bounded.State != Restricted || len(bounded.Reconciliations) != 2 {
		t.Fatalf("bounded=%+v", bounded)
	}
}

func TestTodo_AVAIL_003_Race(t *testing.T) {
	log := NewRevisionLog()
	const workers = 8
	var wg sync.WaitGroup
	succeeded := make([]bool, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			request := revisionRequest()
			request.LeaveEventID = "leave:w1:race"
			token, err := log.Prepare(request)
			if err != nil {
				return
			}
			if _, err := log.Commit(token, nil); err == nil {
				succeeded[i] = true
			}
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, ok := range succeeded {
		if ok {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("wins = %d, want exactly one commit", wins)
	}
	// One event appends exactly once across all racers.
	if history := log.History("w1"); len(history) != 1 {
		t.Fatalf("history=%d revisions", len(history))
	}
}

func TestTodo_AVAIL_003_Recovery(t *testing.T) {
	log := NewRevisionLog()
	// Crash between prepare and commit: recovery appends exactly once.
	token, err := log.Prepare(revisionRequest())
	if err != nil {
		t.Fatal(err)
	}
	_ = token
	recovered, err := log.Recover()
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(recovered) != 1 || recovered[0].State != Unavailable {
		t.Fatalf("recovered=%v", recovered)
	}
	// Recovery is idempotent: nothing commits twice.
	again, err := log.Recover()
	if err != nil || len(again) != 0 {
		t.Fatalf("again=%v err=%v", again, err)
	}
	if history := log.History("w1"); len(history) != 1 {
		t.Fatal("recovery duplicated the revision")
	}
}

func TestTodo_AVAIL_003_HistoryOrder(t *testing.T) {
	log := NewRevisionLog()
	prior := ""
	// Past the tenth revision the IDs must still sort in append order.
	for i := 0; i < 12; i++ {
		request := revisionRequest()
		request.PriorRevision = prior
		request.LeaveEventID = "leave:w1:seq/" + string(rune('a'+i))
		committed := appendRevision(t, log, request)
		prior = committed.RevisionID
	}
	history := log.History("w1")
	if len(history) != 12 {
		t.Fatalf("history=%d revisions, want 12", len(history))
	}
	for i := 1; i < len(history); i++ {
		if history[i-1].RevisionID >= history[i].RevisionID {
			t.Fatalf("history out of order at %d: %q then %q", i, history[i-1].RevisionID, history[i].RevisionID)
		}
		if history[i].PriorRevision != history[i-1].RevisionID {
			t.Fatalf("history[%d] chains to %q, want %q", i, history[i].PriorRevision, history[i-1].RevisionID)
		}
	}
}

func TestTodo_AVAIL_003_Mutation(t *testing.T) {
	log := NewRevisionLog()
	base := appendRevision(t, log, revisionRequest())
	// Interval change re-identifies the revision.
	moved := revisionRequest()
	moved.PriorRevision = base.RevisionID
	moved.LeaveEventID = "leave:w1:moved"
	moved.EffectiveStart = 30
	moved.EffectiveEnd = 35
	changed, err := log.Prepare(moved)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := log.Commit(changed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Digest == base.Digest {
		t.Fatal("interval mutation kept the revision digest")
	}
	// Forged revisions never verify.
	forged := base
	forged.State = Restricted
	if err := forged.Verify(); err == nil {
		t.Fatal("forged revision verified")
	}
}
