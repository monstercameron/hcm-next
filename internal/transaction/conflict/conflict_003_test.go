package conflict_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transaction/conflict"
)

func registeredConflictIntents(t *testing.T) (*conflict.Registry, conflict.WriteIntent, conflict.WriteIntent) {
	t.Helper()
	r := conflict.NewRegistry()
	base := baseFootprint(t)
	left := conflict.WriteIntent{TenantID: "00000000-0000-0000-0000-000000000001", ID: "intent-left", ProposalID: "proposal-left", Footprints: []conflict.WriteFootprint{base}, SnapshotDigest: "snapshot-42"}
	right := conflict.WriteIntent{TenantID: "00000000-0000-0000-0000-000000000001", ID: "intent-right", ProposalID: "proposal-right", Footprints: []conflict.WriteFootprint{base}, SnapshotDigest: "snapshot-42"}
	var err error
	if left, err = r.Register(left); err != nil {
		t.Fatalf("register left: %v", err)
	}
	if right, err = r.Register(right); err != nil {
		t.Fatalf("register right: %v", err)
	}
	return r, left, right
}

// TestTodo_CONFLICT_003 proves the commit-time fence closes a successful
// preflight race: the first validated writer commits and the other is closed
// with a stable stale-baseline error.
func TestTodo_CONFLICT_003(t *testing.T) {
	t.Run("missing current baseline fails closed", func(t *testing.T) {
		r, left, _ := registeredConflictIntents(t)
		_, err := r.Commit(conflict.CommitRequest{IntentID: left.ID})
		if !errors.Is(err, conflict.ErrStaleBaseline) || conflict.CodeOf(err) != conflict.CodeConflictStaleBaseline {
			t.Fatalf("commit without current baseline = %v (code %q), want stale-baseline refusal", err, conflict.CodeOf(err))
		}
	})

	r, left, right := registeredConflictIntents(t)
	current := []conflict.WriteFootprint{baseFootprint(t)}

	winner, err := r.Commit(conflict.CommitRequest{IntentID: left.ID, Current: current})
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if winner.Intent.Status != conflict.IntentCommitted || winner.Fence == 0 {
		t.Fatalf("winner = %+v, want committed intent with a fence", winner.Intent)
	}
	if _, err := r.Commit(conflict.CommitRequest{IntentID: right.ID, Current: current}); !errors.Is(err, conflict.ErrStaleBaseline) || conflict.CodeOf(err) != conflict.CodeConflictStaleBaseline {
		t.Fatalf("loser error = %v (code %q), want stale-baseline error", err, conflict.CodeOf(err))
	}
	loser, ok := r.Lookup(right.ID)
	if !ok || loser.Status != conflict.IntentConflicted {
		t.Fatalf("loser = %+v (found=%v), want conflicted", loser, ok)
	}

	released, err := r.Release(left.ID, winner.Fence)
	if err != nil || released.Status != conflict.IntentReleased {
		t.Fatalf("release = %+v, %v; want released", released, err)
	}
	replayed, err := r.Release(left.ID, winner.Fence)
	if err != nil || replayed.Status != conflict.IntentReleased {
		t.Fatalf("replayed release = %+v, %v; want idempotent released", replayed, err)
	}
}

// TestTodo_CONFLICT_003_Race uses real goroutines at the production registry
// boundary. Both intents have the same baseline and exactly one can acquire
// the scope fence; the other receives the deterministic typed refusal.
func TestTodo_CONFLICT_003_Race(t *testing.T) {
	r, left, right := registeredConflictIntents(t)
	current := []conflict.WriteFootprint{baseFootprint(t)}
	start := make(chan struct{})
	type outcome struct {
		result conflict.CommitResult
		err    error
	}
	out := make(chan outcome, 2)
	var wg sync.WaitGroup
	for _, id := range []string{left.ID, right.ID} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			<-start
			result, err := r.ValidateAtCommit(conflict.CommitRequest{IntentID: id, Current: current})
			out <- outcome{result: result, err: err}
		}(id)
	}
	close(start)
	wg.Wait()
	close(out)

	commits, stale := 0, 0
	var winner conflict.CommitResult
	for got := range out {
		if got.err == nil {
			commits++
			winner = got.result
			continue
		}
		if errors.Is(got.err, conflict.ErrStaleBaseline) && conflict.CodeOf(got.err) == conflict.CodeConflictStaleBaseline {
			stale++
			continue
		}
		t.Fatalf("unexpected concurrent commit error: %v", got.err)
	}
	if commits != 1 || stale != 1 {
		t.Fatalf("concurrent outcomes = commits %d, stale %d; want exactly one each", commits, stale)
	}
	if winner.Fence == 0 || winner.Intent.Status != conflict.IntentCommitted {
		t.Fatalf("winner = %+v, want fenced committed intent", winner)
	}
	if _, err := r.Release(winner.Intent.ID, winner.Fence); err != nil {
		t.Fatalf("winner release: %v", err)
	}
}
