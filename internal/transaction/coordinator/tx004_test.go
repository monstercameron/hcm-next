package coordinator_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/coordinator"
)

type fakeTx struct {
	mu                sync.Mutex
	committed, rolled bool
}

func (f *fakeTx) Exec(context.Context, string, ...any) (int64, error)        { return 1, nil }
func (f *fakeTx) Query(context.Context, string, ...any) (dbport.Rows, error) { return nil, nil }
func (f *fakeTx) QueryRow(context.Context, string, ...any) dbport.Row        { return nil }
func (f *fakeTx) Commit(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.committed = true
	return nil
}
func (f *fakeTx) Rollback(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rolled = true
	return nil
}

type fakeDB struct{ tx *fakeTx }

func (d *fakeDB) Begin(context.Context) (dbport.Tx, error) { return d.tx, nil }

func resolution(t *testing.T) transaction.Resolution {
	t.Helper()
	b := transaction.ConsistencyBoundary{BoundaryID: "b", Tenant: values.TenantId("tenant"), CellID: "c", CoordinatorID: "k", Admitted: []transaction.AdmissionSelector{{StorageClass: "LOCAL", StreamPrefix: "s."}}, Isolation: transaction.IsolationSerializable, Protocol: transaction.CommitProtocolSingleDatabaseACID, CoordinatorEpoch: 1, CrossBoundaryDisposition: transaction.CrossBoundaryDispositionSplitIntoEffects}
	p := intent.TransactionPlan{PlanID: "p", Tenant: "tenant", Participants: []intent.PlanParticipant{{ParticipantID: "a", StreamID: "s.a", StorageClass: "LOCAL", Local: true}, {ParticipantID: "b", StreamID: "s.b", StorageClass: "LOCAL", Local: true}}}
	r, e := transaction.ResolveConsistencyBoundary(b, p, 1)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func writes(n *int) []coordinator.Write {
	return []coordinator.Write{{ParticipantID: "a", Apply: func(context.Context, dbport.Tx) error { *n++; return nil }}, {ParticipantID: "b", Apply: func(context.Context, dbport.Tx) error { *n++; return nil }}}
}

// TestTodo_TX_004 proves every admitted write, and only those writes, shares
// one local commit while publication waits for the commit boundary.
func TestTodo_TX_004(t *testing.T) {
	db := &fakeDB{tx: &fakeTx{}}
	c := coordinator.New(db)
	n := 0
	published := false
	_, e := c.Commit(context.Background(), coordinator.CommitRequest{Plan: resolution(t), Writes: writes(&n), Publish: func(context.Context, coordinator.Receipt) error {
		if !db.tx.committed {
			t.Fatal("published before commit")
		}
		published = true
		return nil
	}})
	if e != nil || n != 2 || !published || !db.tx.committed {
		t.Fatalf("e=%v writes=%d published=%v committed=%v", e, n, published, db.tx.committed)
	}
}

func TestTodo_TX_004_Fault(t *testing.T) {
	db := &fakeDB{tx: &fakeTx{}}
	c := coordinator.New(db)
	want := errors.New("injected")
	n := 0
	ws := writes(&n)
	ws[1].Apply = func(context.Context, dbport.Tx) error { return want }
	_, e := c.Commit(context.Background(), coordinator.CommitRequest{Plan: resolution(t), Writes: ws, Publish: func(context.Context, coordinator.Receipt) error { t.Fatal("published after fault"); return nil }})
	if !errors.Is(e, want) || db.tx.committed || !db.tx.rolled {
		t.Fatalf("e=%v commit=%v rollback=%v", e, db.tx.committed, db.tx.rolled)
	}
}

func TestTodo_TX_004_Mutation(t *testing.T) {
	db := &fakeDB{tx: &fakeTx{}}
	c := coordinator.New(db)
	n := 0
	ws := writes(&n)
	ws = ws[:1]
	_, e := c.Commit(context.Background(), coordinator.CommitRequest{Plan: resolution(t), Writes: ws})
	if !errors.Is(e, coordinator.ErrParticipantMismatch) {
		t.Fatalf("e=%v", e)
	}
}

func TestTodo_TX_004_Race(t *testing.T) {
	var mu sync.Mutex
	var wg sync.WaitGroup
	count := 0
	plan := resolution(t)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db := &fakeDB{tx: &fakeTx{}}
			c := coordinator.New(db)
			n := 0
			_, e := c.Commit(context.Background(), coordinator.CommitRequest{Plan: plan, Writes: writes(&n)})
			if e == nil {
				mu.Lock()
				count++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if count != 8 {
		t.Fatalf("successful concurrent commits=%d", count)
	}
}
