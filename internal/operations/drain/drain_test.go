package drain_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/drain"
)

// TestTodo_OPS_006 is the OPS-006 primary test.
//
// RED: a new lease starts after the fence, an unsafe lease is called
// paused, or resume accepts a stale worker epoch.
// GREEN: the drain rejects new leases, partitions exact safe/ambiguous
// counts, reconciles in-flight effects and resumes only under a new epoch.
func TestTodo_OPS_006(t *testing.T) {
	d := drain.NewDrainer()
	if err := d.Acquire("lease/safe-1", "effect/a"); err != nil {
		t.Fatal(err)
	}
	if err := d.Acquire("lease/unsafe-1", "effect/b", "effect/c"); err != nil {
		t.Fatal(err)
	}
	if err := d.Acquire("lease/unsafe-2"); err != nil {
		t.Fatal(err)
	}
	epoch := d.Epoch()
	if err := d.Heartbeat("lease/safe-1", epoch, drain.RegionSafe); err != nil {
		t.Fatal(err)
	}

	fence, err := d.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if fence != epoch {
		t.Fatalf("fence = %d, want the current epoch %d", fence, epoch)
	}
	// New leases stop at the fence.
	if err := d.Acquire("lease/late", "effect/z"); !errors.Is(err, drain.ErrDrainFenced) {
		t.Fatalf("post-fence acquire = %v, want ErrDrainFenced", err)
	}
	// Safe work completes; unsafe work keeps heartbeating.
	if err := d.Complete("lease/safe-1", epoch); err != nil {
		t.Fatal(err)
	}
	if err := d.Complete("lease/unsafe-1", epoch); !errors.Is(err, drain.ErrUnsafeComplete) {
		t.Fatalf("unsafe complete = %v, want ErrUnsafeComplete", err)
	}

	report, err := d.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Safe) != 0 {
		t.Fatalf("safe = %v, want none (the safe lease completed before finish)", report.Safe)
	}
	if len(report.Ambiguous) != 2 {
		t.Fatalf("ambiguous = %+v, want the two unsafe leases", report.Ambiguous)
	}
	// Unsafe work is ambiguous with reconciled effects, never paused: no
	// ambiguous lease may read safe, and every in-flight effect is named.
	for _, lease := range report.Ambiguous {
		if lease.Obligation == "" || lease.ID == "" {
			t.Fatalf("ambiguous lease %+v carries no obligation", lease)
		}
	}
	if len(report.Ambiguous[0].Effects)+len(report.Ambiguous[1].Effects) != 2 {
		t.Fatalf("ambiguous effects = %+v, want effect/b and effect/c reconciled", report.Ambiguous)
	}

	// Resume requires a new epoch: the fence epoch and anything older is
	// stale, and only then does new work start again.
	if err := d.Resume(fence); !errors.Is(err, drain.ErrStaleEpoch) {
		t.Fatalf("fence-epoch resume = %v, want ErrStaleEpoch", err)
	}
	if err := d.Resume(fence + 1); err != nil {
		t.Fatal(err)
	}
	if d.Epoch() != fence+1 {
		t.Fatalf("epoch = %d, want %d", d.Epoch(), fence+1)
	}
	if err := d.Acquire("lease/next", "effect/n"); err != nil {
		t.Fatalf("post-resume acquire = %v", err)
	}
	// The old world is gone: pre-resume leases are unknown, and their
	// epochs are stale.
	if err := d.Heartbeat("lease/unsafe-1", fence, drain.RegionSafe); !errors.Is(err, drain.ErrUnknownLease) {
		t.Fatalf("retired lease heartbeat = %v, want ErrUnknownLease", err)
	}
	if err := d.Heartbeat("lease/next", fence, drain.RegionSafe); !errors.Is(err, drain.ErrStaleEpoch) {
		t.Fatalf("stale epoch heartbeat = %v, want ErrStaleEpoch", err)
	}
}

func TestTodo_OPS_006_Race(t *testing.T) {
	d := drain.NewDrainer()
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers*3)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("lease/%d", i)
			if err := d.Acquire(id, fmt.Sprintf("effect/%d", i)); err != nil {
				errs <- err
				return
			}
			epoch := d.Epoch()
			if err := d.Heartbeat(id, epoch, drain.RegionSafe); err != nil {
				errs <- err
				return
			}
		}(i)
	}
	wg.Wait()
	if _, err := d.Begin(); err != nil {
		t.Fatal(err)
	}
	epoch := d.Epoch()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := d.Complete(fmt.Sprintf("lease/%d", i), epoch); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent drain = %v", err)
	}
	report, err := d.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Safe) != 0 || len(report.Ambiguous) != 0 {
		t.Fatalf("finish = %+v, want every lease completed first", report)
	}
}

func TestTodo_OPS_006_Fault(t *testing.T) {
	d := drain.NewDrainer()
	if err := d.Heartbeat("lease/ghost", 1, drain.RegionSafe); !errors.Is(err, drain.ErrUnknownLease) {
		t.Fatalf("ghost heartbeat = %v, want ErrUnknownLease", err)
	}
	if _, err := d.Finish(); !errors.Is(err, drain.ErrDrainState) {
		t.Fatalf("finish-before-begin = %v, want ErrDrainState", err)
	}
	if err := d.Resume(2); !errors.Is(err, drain.ErrDrainState) {
		t.Fatalf("resume-before-drain = %v, want ErrDrainState", err)
	}
	if err := d.Acquire("lease/a"); err != nil {
		t.Fatal(err)
	}
	if err := d.Acquire("lease/a"); !errors.Is(err, drain.ErrDuplicateLease) {
		t.Fatalf("duplicate acquire = %v, want ErrDuplicateLease", err)
	}
	if _, err := d.Begin(); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Begin(); !errors.Is(err, drain.ErrDrainState) {
		t.Fatalf("double begin = %v, want ErrDrainState", err)
	}
	if err := d.Complete("lease/a", d.Epoch()); !errors.Is(err, drain.ErrUnsafeComplete) {
		t.Fatalf("unsafe default complete = %v, want ErrUnsafeComplete", err)
	}
	report, err := d.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Ambiguous) != 1 || report.Ambiguous[0].ID != "lease/a" {
		t.Fatalf("finish = %+v, want the unsafe lease ambiguous", report)
	}
	// Completed-then-finished drains report exact zeros, not absences.
	empty := drain.NewDrainer()
	if _, err := empty.Begin(); err != nil {
		t.Fatal(err)
	}
	blank, err := empty.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(blank.Safe) != 0 || len(blank.Ambiguous) != 0 {
		t.Fatalf("empty finish = %+v, want exact zeros", blank)
	}
}

func TestTodo_OPS_006_Security(t *testing.T) {
	d := drain.NewDrainer()
	if err := d.Acquire("lease/a", "effect/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Begin(); err != nil {
		t.Fatal(err)
	}
	epoch := d.Epoch()
	if err := d.Heartbeat("lease/a", epoch, drain.RegionSafe); err != nil {
		t.Fatal(err)
	}
	report, err := d.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Safe) != 1 {
		t.Fatalf("finish = %+v, want the safe lease retired", report)
	}
	// A worker replaying the fence epoch after resume is stale: resume
	// past it, then prove the old epoch is dead for every operation.
	if err := d.Resume(epoch + 1); err != nil {
		t.Fatal(err)
	}
	if err := d.Acquire("lease/b"); err != nil {
		t.Fatal(err)
	}
	if err := d.Heartbeat("lease/b", epoch, drain.RegionSafe); !errors.Is(err, drain.ErrStaleEpoch) {
		t.Fatalf("stale heartbeat = %v, want ErrStaleEpoch", err)
	}
	if err := d.Complete("lease/b", epoch); !errors.Is(err, drain.ErrStaleEpoch) {
		t.Fatalf("stale complete = %v, want ErrStaleEpoch", err)
	}
	// Epochs never move backward: resuming at or below the fence refuses.
	d2 := drain.NewDrainer()
	if err := d2.Acquire("lease/x"); err != nil {
		t.Fatal(err)
	}
	fence, err := d2.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d2.Finish(); err != nil {
		t.Fatal(err)
	}
	for _, stale := range []uint64{0, fence - 1, fence} {
		if err := d2.Resume(stale); !errors.Is(err, drain.ErrStaleEpoch) {
			t.Fatalf("resume(%d) = %v, want ErrStaleEpoch", stale, err)
		}
	}
}

func TestTodo_OPS_006_Mutation(t *testing.T) {
	// Mutant 1: a lease acquired after the fence never exists.
	d := drain.NewDrainer()
	if _, err := d.Begin(); err != nil {
		t.Fatal(err)
	}
	if err := d.Acquire("lease/late"); !errors.Is(err, drain.ErrDrainFenced) {
		t.Fatalf("late acquire = %v, want ErrDrainFenced", err)
	}
	if err := d.Heartbeat("lease/late", d.Epoch(), drain.RegionSafe); !errors.Is(err, drain.ErrUnknownLease) {
		t.Fatalf("late heartbeat = %v, want ErrUnknownLease", err)
	}
	// Mutant 2: an unsafe lease is ambiguous at finish, never safe.
	d2 := drain.NewDrainer()
	if err := d2.Acquire("lease/u", "effect/u"); err != nil {
		t.Fatal(err)
	}
	if _, err := d2.Begin(); err != nil {
		t.Fatal(err)
	}
	report, err := d2.Finish()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range report.Safe {
		if id == "lease/u" {
			t.Fatal("unsafe lease reported safe")
		}
	}
	if len(report.Ambiguous) != 1 || len(report.Ambiguous[0].Effects) != 1 {
		t.Fatalf("finish = %+v, want one ambiguous lease with its effect", report)
	}
	// Mutant 3: resume under the fence epoch is stale, not a reopen.
	if err := d2.Resume(report.Fence); !errors.Is(err, drain.ErrStaleEpoch) {
		t.Fatalf("fence resume = %v, want ErrStaleEpoch", err)
	}
	// Mutant 4: empty and whitespace lease IDs are refused.
	d3 := drain.NewDrainer()
	for _, id := range []string{"", "   "} {
		if err := d3.Acquire(id); !errors.Is(err, drain.ErrUnknownLease) {
			t.Fatalf("acquire(%q) = %v, want refusal", id, err)
		}
	}
}
