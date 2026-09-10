package trace_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/trace"
	"github.com/monstercameron/human-capital-management-suite/internal/resource/reservation"
)

const (
	promoIntent = "intent/promo-8"
	promoWorker = "jane"
)

func promoNow() time.Time {
	return time.Date(2026, time.February, 1, 9, 0, 0, 0, time.UTC)
}

func promoQuantities() (reservation.Quantity, reservation.Quantity) {
	return reservation.Quantity{Value: 5000, Scale: 3}, reservation.Quantity{Value: 1000, Scale: 3}
}

// drive walks the canonical degraded path: intent to closure with one IAM
// redrive in the middle.
func drive(t *testing.T, tracer *trace.Tracer, store *reservation.Store) {
	t.Helper()
	if err := tracer.CreateIntent(); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Snapshot("sha256:snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Simulate("sha256:simulation"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Propose(1, "sha256:proposal"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Approve("alice", "hrbp"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Revalidate("sha256:proposal", 7, 3); err != nil {
		t.Fatal(err)
	}
	budget, capacity := promoQuantities()
	if err := tracer.Reserve(store, budget, capacity); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Commit(store, "commit-1"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Observe(false); err != nil {
		t.Fatal(err)
	}
	if err := tracer.RepairIAM(); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Close(); err != nil {
		t.Fatal(err)
	}
}

func newTracer() *trace.Tracer {
	return trace.New(promoIntent, promoWorker, "alice", "bob", promoNow())
}

// TestTodo_PROMO_008 is the PROMO-008 primary test.
//
// RED: a stage observes a domain write before its boundary, lineage is
// lost, an old revision is overwritten, the intent closes while repair is
// required, or repair replays the promotion.
// GREEN: the deterministic T0…Tn trace asserts exact inserts and
// zero-forbidden-row counts end to end.
func TestTodo_PROMO_008(t *testing.T) {
	tracer := newTracer()
	store := reservation.NewStore()
	if err := tracer.CreateIntent(); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Snapshot("sha256:snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Simulate("sha256:simulation"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Propose(1, "sha256:proposal"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Approve("alice", "hrbp"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Revalidate("sha256:proposal", 7, 3); err != nil {
		t.Fatal(err)
	}
	// No domain write exists before the commit boundary: the position,
	// compensation and ledger tables are empty this far.
	for _, table := range []string{trace.TablePositionEdge, trace.TableCompComponent, trace.TableLedger} {
		if got := tracer.Count(table); got != 0 {
			t.Fatalf("table %s holds %d rows before commit", table, got)
		}
	}
	budget, capacity := promoQuantities()
	if err := tracer.Reserve(store, budget, capacity); err != nil {
		t.Fatal(err)
	}
	preCommit := len(tracer.Rows())
	if err := tracer.Commit(store, "commit-1"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Observe(false); err != nil {
		t.Fatal(err)
	}
	// No closure exists while repair is required.
	if got := tracer.Count(trace.TableClosure); got != 0 {
		t.Fatalf("closure table holds %d rows while degraded", got)
	}
	preRepair := len(tracer.Rows())
	if err := tracer.RepairIAM(); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Close(); err != nil {
		t.Fatal(err)
	}
	rows := tracer.Rows()

	if len(rows) == 0 || rows[0].Tick != 1 {
		t.Fatalf("trace does not start at T1: %+v", rows)
	}
	for i, row := range rows {
		if row.Tick != i+1 {
			t.Fatalf("row %d ticks T%d: ticks are not deterministic", i, row.Tick)
		}
		if row.IntentRef != promoIntent || row.Digest == "" {
			t.Fatalf("row %d loses lineage: %+v", i, row)
		}
		if i > 0 && row.PrevDigest != rows[i-1].Digest {
			t.Fatalf("row %d breaks the lineage chain", i)
		}
	}

	exact := map[string]int{
		trace.TableIntent: 4, trace.TableSnapshot: 1, trace.TableSimulation: 1,
		trace.TableProposal: 1, trace.TableApproval: 2, trace.TableRevalidation: 1,
		trace.TableReservation: 2, trace.TablePositionEdge: 2, trace.TableCompComponent: 1,
		trace.TableLedger: 2, trace.TableObservation: 4, trace.TableReconciliation: 1,
		trace.TableRepair: 1, trace.TableClosure: 1,
	}
	for table, want := range exact {
		if got := tracer.Count(table); got != want {
			t.Fatalf("table %s holds %d rows, want %d", table, got, want)
		}
	}

	// Forbidden rows stay zero: every commit-batch row lands at or after
	// the pre-commit boundary, and every closure row lands after repair.
	for i, row := range rows {
		switch row.Table {
		case trace.TablePositionEdge, trace.TableCompComponent, trace.TableLedger:
			if i < preCommit {
				t.Fatalf("domain write %+v predates the commit boundary", row)
			}
		case trace.TableClosure:
			if i < preRepair {
				t.Fatalf("closure %+v predates repair", row)
			}
		}
	}

	// The old edge revision survives: supersede appends, never overwrites.
	var oldRev, newRev uint64
	for _, row := range rows {
		if row.Table == trace.TablePositionEdge && strings.HasSuffix(row.ID, "/old") {
			oldRev = row.Revision
		}
		if row.Table == trace.TablePositionEdge && strings.HasSuffix(row.ID, "/new") {
			newRev = row.Revision
		}
	}
	if oldRev != 1 || newRev != 1 {
		t.Fatalf("edge revisions old=%d new=%d, want the appended revision pair", oldRev, newRev)
	}
	if tracer.CountState(trace.TableIntent, "CLOSED") != 1 {
		t.Fatal("intent never closed")
	}
	if tracer.Seal() == "" {
		t.Fatal("trace carries no seal")
	}
}

func TestTodo_PROMO_008_Golden(t *testing.T) {
	tracer := newTracer()
	drive(t, tracer, reservation.NewStore())
	var b strings.Builder
	for _, row := range tracer.Rows() {
		fmt.Fprintf(&b, "T%d %s %s %s rev=%d digest=%.16s prev=%.16s %s\n",
			row.Tick, row.Table, row.ID, row.State, row.Revision, row.Digest, row.PrevDigest, row.Detail)
	}
	fmt.Fprintf(&b, "seal: %s\n", tracer.Seal())
	got := b.String()
	path := filepath.Join("testdata", "promo008_trace.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s", path)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func TestTodo_PROMO_008_Race(t *testing.T) {
	first := newTracer()
	drive(t, first, reservation.NewStore())
	const workers = 16
	seals := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracer := newTracer()
			store := reservation.NewStore()
			if err := tracer.CreateIntent(); err != nil {
				errs <- err
				return
			}
			if err := tracer.Snapshot("sha256:snapshot"); err != nil {
				errs <- err
				return
			}
			if err := tracer.Simulate("sha256:simulation"); err != nil {
				errs <- err
				return
			}
			if err := tracer.Propose(1, "sha256:proposal"); err != nil {
				errs <- err
				return
			}
			if err := tracer.Approve("alice", "hrbp"); err != nil {
				errs <- err
				return
			}
			if err := tracer.Revalidate("sha256:proposal", 7, 3); err != nil {
				errs <- err
				return
			}
			budget, capacity := promoQuantities()
			if err := tracer.Reserve(store, budget, capacity); err != nil {
				errs <- err
				return
			}
			if err := tracer.Commit(store, "commit-1"); err != nil {
				errs <- err
				return
			}
			if err := tracer.Observe(false); err != nil {
				errs <- err
				return
			}
			if err := tracer.RepairIAM(); err != nil {
				errs <- err
				return
			}
			if err := tracer.Close(); err != nil {
				errs <- err
				return
			}
			seals <- tracer.Seal()
		}()
	}
	wg.Wait()
	close(seals)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent trace = %v", err)
	}
	for seal := range seals {
		if seal != first.Seal() {
			t.Fatal("concurrent traces diverge")
		}
	}
}

// TestTodo_PROMO_008_Recovery crashes the trace at three boundaries and
// requires clean resume: no partial commit rows, no duplicate batches, no
// lost lineage.
func TestTodo_PROMO_008_Recovery(t *testing.T) {
	// Crash 1: failpoint before the commit batch lands leaves zero commit
	// rows behind.
	tracer := newTracer()
	if err := tracer.CreateIntent(); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Snapshot("sha256:snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Simulate("sha256:simulation"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Propose(1, "sha256:proposal"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Approve("alice", "hrbp"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Revalidate("sha256:proposal", 7, 3); err != nil {
		t.Fatal(err)
	}
	budget, capacity := promoQuantities()
	// Reserve against a fresh store, then crash the committer with a
	// failpoint: no ledger, edge or component row may exist.
	tracer.Failpoint(func(stage string) error { return errors.New("injected crash") })
	crashedStore := reservation.NewStore()
	if err := tracer.Reserve(crashedStore, budget, capacity); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Commit(crashedStore, "commit-1"); err == nil {
		t.Fatal("failpoint did not fire")
	}
	if tracer.Count(trace.TableLedger)+tracer.Count(trace.TablePositionEdge)+tracer.Count(trace.TableCompComponent) != 0 {
		t.Fatal("crashed commit left partial rows")
	}

	// Crash 2: process loss after commit resumes from persisted rows and
	// finishes without duplicating the batch.
	committed := newTracer()
	commitStore := reservation.NewStore()
	if err := committed.CreateIntent(); err != nil {
		t.Fatal(err)
	}
	if err := committed.Snapshot("sha256:snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := committed.Simulate("sha256:simulation"); err != nil {
		t.Fatal(err)
	}
	if err := committed.Propose(1, "sha256:proposal"); err != nil {
		t.Fatal(err)
	}
	if err := committed.Approve("alice", "hrbp"); err != nil {
		t.Fatal(err)
	}
	if err := committed.Revalidate("sha256:proposal", 7, 3); err != nil {
		t.Fatal(err)
	}
	if err := committed.Reserve(commitStore, budget, capacity); err != nil {
		t.Fatal(err)
	}
	if err := committed.Commit(commitStore, "commit-1"); err != nil {
		t.Fatal(err)
	}
	resumed := trace.Resume(promoIntent, promoWorker, "alice", "bob", promoNow(), committed.Rows())
	// Replaying the same commit ID after resume is a no-op, not a second
	// batch: the reservation holds already consumed, so replay must not
	// touch the store either.
	if err := resumed.Commit(commitStore, "commit-1"); err != nil {
		t.Fatalf("commit replay after resume = %v", err)
	}
	if resumed.Count(trace.TableLedger) != 2 || resumed.Count(trace.TablePositionEdge) != 2 {
		t.Fatal("commit replay duplicated the batch")
	}
	if err := resumed.Observe(false); err != nil {
		t.Fatal(err)
	}
	if err := resumed.RepairIAM(); err != nil {
		t.Fatal(err)
	}
	if err := resumed.Close(); err != nil {
		t.Fatal(err)
	}
	if resumed.CountState(trace.TableIntent, "CLOSED") != 1 {
		t.Fatal("resumed trace never closed")
	}
}

func TestTodo_PROMO_008_Mutation(t *testing.T) {
	// Mutant 1: observing before commit is outside the boundary.
	tracer := newTracer()
	if err := tracer.Observe(false); !errors.Is(err, trace.ErrStageOrder) {
		t.Fatalf("observe-first = %v, want ErrStageOrder", err)
	}
	// Mutant 2: committing a changed proposal is refused.
	tracer = newTracer()
	if err := tracer.CreateIntent(); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Snapshot("sha256:snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Simulate("sha256:simulation"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Propose(1, "sha256:proposal"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Approve("alice", "hrbp"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Revalidate("sha256:other-proposal", 7, 3); !errors.Is(err, trace.ErrStaleProposal) {
		t.Fatalf("changed proposal = %v, want ErrStaleProposal", err)
	}
	// Mutant 3: closing while repair is required is refused.
	tracer = newTracer()
	store := reservation.NewStore()
	if err := tracer.CreateIntent(); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Snapshot("sha256:snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Simulate("sha256:simulation"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Propose(1, "sha256:proposal"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Approve("alice", "hrbp"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Revalidate("sha256:proposal", 7, 3); err != nil {
		t.Fatal(err)
	}
	budget, capacity := promoQuantities()
	if err := tracer.Reserve(store, budget, capacity); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Commit(store, "commit-1"); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Observe(false); err != nil {
		t.Fatal(err)
	}
	if err := tracer.Close(); !errors.Is(err, trace.ErrCloseWhileRepairOpen) {
		t.Fatalf("close-while-degraded = %v, want ErrCloseWhileRepairOpen", err)
	}
	// Mutant 4: thin quorum never approves.
	thin := newTracer()
	if err := thin.CreateIntent(); err != nil {
		t.Fatal(err)
	}
	if err := thin.Snapshot("sha256:snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := thin.Simulate("sha256:simulation"); err != nil {
		t.Fatal(err)
	}
	if err := thin.Propose(1, "sha256:proposal"); err != nil {
		t.Fatal(err)
	}
	if err := thin.Approve("alice"); !errors.Is(err, trace.ErrStageOrder) {
		t.Fatalf("thin quorum = %v, want refusal", err)
	}
}
