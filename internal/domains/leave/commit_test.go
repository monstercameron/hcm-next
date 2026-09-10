package leave

import (
	"errors"
	"testing"
)

func commitInput() StartCommitInput {
	return StartCommitInput{
		IdempotencyKey: "commit:leave:w1:start", EmploymentState: "ACTIVE",
		StepReceipts: map[string]string{
			"leave-revision": "receipt:rev", "absence-relationship": "receipt:rel",
			"availability-interval": "receipt:avail", "balance-entries": "receipt:bal",
			"ledger-events": "receipt:ledger", "projections": "receipt:proj", "effect-outbox": "receipt:outbox",
		},
	}
}

func TestTodo_LEAVE_009(t *testing.T) {
	committer := NewCommitter()
	record, err := committer.Commit(commitInput(), nil)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if record.BusinessState != BusinessLeaveActive || len(record.Steps) != 7 {
		t.Fatalf("record=%+v", record)
	}
	if err := record.Verify("commit:leave:w1:start", commitInput().StepReceipts); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	// Duplicate starts return the identical record: never a second start.
	again, err := committer.Commit(commitInput(), nil)
	if err != nil || again.CommitID != record.CommitID || again.Digest != record.Digest {
		t.Fatalf("again=%+v err=%v", again, err)
	}
	// RED: failpoints, missing steps and employment mutation refuse with
	// nothing committed.
	failing := NewCommitter()
	if _, err := failing.Commit(commitInput(), func(step string) error {
		if step == "balance-entries" {
			return errors.New("injected fault at balance entries")
		}
		return nil
	}); err == nil {
		t.Fatal("failpoint committed active leave")
	}
	if _, err := failing.Commit(commitInput(), nil); err != nil {
		t.Fatalf("clean retry after failpoint refused: %v", err)
	}
	partial := commitInput()
	delete(partial.StepReceipts, "availability-interval")
	if _, err := NewCommitter().Commit(partial, nil); err == nil {
		t.Fatal("partial steps committed")
	}
	unbalanced := commitInput()
	delete(unbalanced.StepReceipts, "balance-entries")
	if _, err := NewCommitter().Commit(unbalanced, nil); err == nil {
		t.Fatal("balance debit committed without leave revision context")
	}
	terminated := commitInput()
	terminated.EmploymentState = "TERMINATED"
	if _, err := NewCommitter().Commit(terminated, nil); err == nil {
		t.Fatal("commit mutated employment state")
	}
	var nilCommitter *Committer
	if _, err := nilCommitter.Commit(commitInput(), nil); err == nil {
		t.Fatal("nil committer committed")
	}
}

func TestTodo_LEAVE_009_Property(t *testing.T) {
	committer := NewCommitter()
	first, err := committer.Commit(commitInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Once-or-none across repeats: the registry holds exactly one record
	// with the deterministic seal.
	for i := 0; i < 3; i++ {
		repeat, err := committer.Commit(commitInput(), nil)
		if err != nil || repeat.Digest != first.Digest {
			t.Fatalf("repeat %d diverged", i)
		}
	}
}

func TestTodo_LEAVE_009_Mutation(t *testing.T) {
	committer := NewCommitter()
	base, err := committer.Commit(commitInput(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// A changed step receipt is a different commit with its own seal.
	changed := commitInput()
	changed.IdempotencyKey = "commit:leave:w1:start-2"
	changed.StepReceipts["balance-entries"] = "receipt:bal-2"
	other, err := committer.Commit(changed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if other.Digest == base.Digest {
		t.Fatal("receipt mutation kept the commit seal")
	}
	// Forged records never verify.
	forged := base
	forged.BusinessState = "ON_LEAVE"
	if err := forged.Verify("commit:leave:w1:start", commitInput().StepReceipts); err == nil {
		t.Fatal("forged commit verified")
	}
}
