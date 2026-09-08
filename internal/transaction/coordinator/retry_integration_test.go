package coordinator_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/transaction"
	"github.com/monstercameron/hcm-next/internal/transaction/coordinator"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_DB_EDGE_003_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	db.Exec(t, `CREATE TABLE retry_probe (id integer PRIMARY KEY, value integer NOT NULL)`)
	db.Exec(t, `INSERT INTO retry_probe (id, value) VALUES (1, 0)`)
	second := db.NewConn(t)
	c1, c2 := coordinator.New(db.Conn), coordinator.New(second)
	var prepared atomic.Int32
	var seenMu sync.Mutex
	var seen []int
	barrier := make(chan struct{})
	var ready atomic.Int32
	prepare := func(ctx context.Context, tx dbport.Tx) (coordinator.CommitRequest, error) {
		var isolation string
		if err := tx.QueryRow(ctx, `SELECT current_setting('transaction_isolation')`).Scan(&isolation); err != nil {
			return coordinator.CommitRequest{}, err
		}
		if isolation != "serializable" {
			return coordinator.CommitRequest{}, fmt.Errorf("isolation=%q, want serializable", isolation)
		}
		var value int
		if err := tx.QueryRow(ctx, `SELECT value FROM retry_probe WHERE id=1`).Scan(&value); err != nil {
			return coordinator.CommitRequest{}, err
		}
		seenMu.Lock()
		seen = append(seen, value)
		seenMu.Unlock()
		if ready.Add(1) == 2 {
			close(barrier)
		}
		select {
		case <-barrier:
		case <-ctx.Done():
			return coordinator.CommitRequest{}, ctx.Err()
		}
		prepared.Add(1)
		boundary := transaction.ConsistencyBoundary{BoundaryID: "b", Tenant: values.TenantId("tenant"), CellID: "c", CoordinatorID: "k", Admitted: []transaction.AdmissionSelector{{StorageClass: "LOCAL", StreamPrefix: "s."}}, Isolation: transaction.IsolationSerializable, Protocol: transaction.CommitProtocolSingleDatabaseACID, CoordinatorEpoch: 1, CrossBoundaryDisposition: transaction.CrossBoundaryDispositionSplitIntoEffects}
		plan, err := transaction.ResolveConsistencyBoundary(boundary, intent.TransactionPlan{PlanID: fmt.Sprintf("snapshot-%d", value), Tenant: "tenant", Participants: []intent.PlanParticipant{{ParticipantID: "a", StreamID: "s.a", StorageClass: "LOCAL", Local: true}, {ParticipantID: "b", StreamID: "s.b", StorageClass: "LOCAL", Local: true}}}, 1)
		if err != nil {
			return coordinator.CommitRequest{}, err
		}
		apply := func(ctx context.Context, tx dbport.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE retry_probe SET value=$1 WHERE id=1`, value+1)
			return err
		}
		return coordinator.CommitRequest{Plan: plan, Writes: []coordinator.Write{{ParticipantID: "a", Apply: apply}, {ParticipantID: "b", Apply: apply}}}, nil
	}
	opts := coordinator.RetryOptions{MaxAttempts: 3, Admit: func(context.Context) error { return nil }, Prepare: prepare, Sleep: func(context.Context, time.Duration) error { return nil }}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	var errs [2]error
	var receipts [2]coordinator.Receipt
	go func() {
		defer wg.Done()
		receipts[0], errs[0] = c1.CommitWithRetry(ctx, coordinator.CommitRequest{}, opts)
	}()
	go func() {
		defer wg.Done()
		receipts[1], errs[1] = c2.CommitWithRetry(ctx, coordinator.CommitRequest{}, opts)
	}()
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("concurrent commits: %v / %v", errs[0], errs[1])
	}
	var value int
	if err := db.QueryRow(context.Background(), `SELECT value FROM retry_probe WHERE id=1`).Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != 2 || prepared.Load() < 3 {
		t.Fatalf("final value=%d prepared=%d, want 2 and a fresh retry snapshot", value, prepared.Load())
	}
	if receipts[0].PlanID == receipts[1].PlanID {
		t.Fatalf("receipts reused stale plan: %q", receipts[0].PlanID)
	}
	seenMu.Lock()
	defer seenMu.Unlock()
	if len(seen) < 3 || seen[len(seen)-1] == 0 {
		t.Fatalf("snapshots=%v, retry did not observe committed value", seen)
	}
}
