package cycle

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func transitionRevision(t *testing.T) Revision {
	t.Helper()
	r, err := NewRevision(validCycle(), "cycle-rev-1", "v1", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func transitionPhase(ops ...TransitionOperation) CompiledPhase {
	allowed := make([]string, len(ops))
	for i, op := range ops {
		allowed[i] = string(op)
	}
	return CompiledPhase{ID: "governed", AllowedOperations: allowed}
}

func request(op TransitionOperation, phase CompiledPhase) TransitionRequest {
	return TransitionRequest{Operation: op, Requester: "operator-a", Reason: "scheduled lifecycle action", At: time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), ActivePhase: phase, Counts: map[string]int{"members": 10}, Exceptions: []string{"none"}, SnapshotVersions: map[string]string{"population": "v1"}}
}

func TestTodo_CYCLE_004(t *testing.T) {
	ledger, err := NewLedger(transitionRevision(t))
	if err != nil {
		t.Fatal(err)
	}
	openReq := request(OperationOpen, transitionPhase(OperationOpen))
	open, openEvent, err := ledger.Transition(openReq)
	if err != nil {
		t.Fatal(err)
	}
	if open.State != StateOpen || openEvent.ToState != StateOpen || openEvent.Digest == "" {
		t.Fatalf("open = %+v, event=%+v", open, openEvent)
	}
	closeReq := request(OperationClose, transitionPhase(OperationClose))
	closeReq.ExpectedSequence = open.Sequence
	closeReq.ExpectedDigest = open.CanonicalDigest()
	closed, closeEvent, err := ledger.Transition(closeReq)
	if err != nil {
		t.Fatal(err)
	}
	if closed.State != StateClosed || closeEvent.BeforeRevisionDigest != open.Revision.Digest {
		t.Fatalf("close evidence = %+v", closeEvent)
	}
	lockReq := request(OperationLock, transitionPhase(OperationLock))
	lockReq.ExpectedSequence = closed.Sequence
	lockReq.ExpectedDigest = closed.CanonicalDigest()
	locked, lockEvent, err := ledger.Transition(lockReq)
	if err != nil {
		t.Fatal(err)
	}
	if locked.State != StateLocked || lockEvent.Sequence != 3 || len(ledger.Events()) != 3 {
		t.Fatalf("lock = %+v, event=%+v", locked, lockEvent)
	}
}

func TestTodo_CYCLE_004_Property(t *testing.T) {
	original := transitionRevision(t)
	cycle, err := NewOpenCycle(original)
	if err != nil {
		t.Fatal(err)
	}
	req := request(OperationClose, transitionPhase(OperationClose))
	next, event, err := cycle.Transition(req)
	if err != nil {
		t.Fatal(err)
	}
	if cycle.State != StateOpen || next.State != StateClosed || event.CanonicalDigest() == "" {
		t.Fatalf("transition mutated receiver: before=%+v after=%+v", cycle, next)
	}
	req.Counts["members"] = 99
	if event.Counts["members"] != 10 {
		t.Fatal("event metadata aliases the request map")
	}
}

func TestTodo_CYCLE_004_Golden(t *testing.T) {
	r := transitionRevision(t)
	first, err := NewOpenCycle(r)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewOpenCycle(r)
	if err != nil {
		t.Fatal(err)
	}
	a, ea, err := first.Transition(request(OperationClose, transitionPhase(OperationClose)))
	if err != nil {
		t.Fatal(err)
	}
	b, eb, err := second.Transition(request(OperationClose, transitionPhase(OperationClose)))
	if err != nil {
		t.Fatal(err)
	}
	if a.State != b.State || ea.Digest != eb.Digest {
		t.Fatalf("same transition is not golden-stable: %q != %q", ea.Digest, eb.Digest)
	}
	if !errors.Is(ea.Validate(), nil) {
		t.Fatalf("event validation failed: %v", ea.Validate())
	}
	ea.Reason = "tampered"
	if ea.Validate() == nil {
		t.Fatal("tampered event passed digest validation")
	}
}

func TestTodo_CYCLE_004_Race(t *testing.T) {
	ledger, err := NewOpenLedger(transitionRevision(t))
	if err != nil {
		t.Fatal(err)
	}
	const workers = 24
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			req := request(OperationClose, transitionPhase(OperationClose))
			req.ExpectedSequence = 0
			if _, _, err := ledger.Transition(req); err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 || ledger.Current().State != StateClosed || len(ledger.Events()) != 1 {
		t.Fatalf("CAS race successes=%d current=%+v events=%d", successes, ledger.Current(), len(ledger.Events()))
	}
}

func TestTodo_CYCLE_004_Security(t *testing.T) {
	cycle, err := NewOpenCycle(transitionRevision(t))
	if err != nil {
		t.Fatal(err)
	}
	closed, _, err := cycle.Transition(request(OperationClose, transitionPhase(OperationClose)))
	if err != nil {
		t.Fatal(err)
	}
	reopen := request(OperationReopen, transitionPhase(OperationReopen))
	reopen.Approver = reopen.Requester
	if _, _, err := closed.Transition(reopen); !errors.Is(err, ErrTransitionApprover) {
		t.Fatalf("self-approved reopen = %v", err)
	}
	reopen.Approver = "reviewer-b"
	reopen.Reason = ""
	if _, _, err := closed.Transition(reopen); !errors.Is(err, ErrTransitionReason) {
		t.Fatalf("unreasoned reopen = %v", err)
	}
	forbidden := request(OperationClose, CompiledPhase{ID: "forbidden"})
	if _, _, err := cycle.Transition(forbidden); !errors.Is(err, ErrTransitionPhase) {
		t.Fatalf("forbidden close = %v", err)
	}
}

func TestTodo_CYCLE_004_Mutation(t *testing.T) {
	r := transitionRevision(t)
	cycle, err := NewOpenCycle(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := cycle.Transition(request(OperationLock, transitionPhase(OperationLock))); !errors.Is(err, ErrTransitionState) {
		t.Fatalf("lock without close = %v", err)
	}
	closed, _, err := cycle.Transition(request(OperationClose, transitionPhase(OperationClose)))
	if err != nil {
		t.Fatal(err)
	}
	reopen := request(OperationReopen, transitionPhase(OperationReopen))
	reopen.Approver = "reviewer-b"
	newCycle, event, err := closed.Transition(reopen)
	if err != nil {
		t.Fatal(err)
	}
	if newCycle.State != StateOpen || newCycle.Revision.Digest == closed.Revision.Digest || event.BeforeRevisionDigest != closed.Revision.Digest || event.AfterRevisionDigest == event.BeforeRevisionDigest {
		t.Fatalf("reopen did not create a new revision: cycle=%+v event=%+v", newCycle, event)
	}
	if closed.State != StateClosed || closed.Revision.Digest != r.Digest {
		t.Fatal("reopen rewrote the closed revision")
	}
	reopenedClosed, _, err := newCycle.Transition(request(OperationClose, transitionPhase(OperationClose)))
	if err != nil {
		t.Fatal(err)
	}
	locked, _, err := reopenedClosed.Transition(request(OperationLock, transitionPhase(OperationLock)))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := locked.Transition(reopen); !errors.Is(err, ErrTransitionState) {
		t.Fatalf("reopen from locked = %v", err)
	}
	stale := request(OperationClose, transitionPhase(OperationClose))
	stale.ExpectedSequence = 99
	if _, _, err := cycle.Transition(stale); !errors.Is(err, ErrStaleTransition) {
		t.Fatalf("stale transition = %v", err)
	}
}
