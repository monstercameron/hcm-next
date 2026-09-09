package cancel_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	transactioncancel "github.com/monstercameron/human-capital-management-suite/internal/transaction/cancel"
	transactioncommit "github.com/monstercameron/human-capital-management-suite/internal/transaction/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/plan"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var tx008At = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

type fixture struct {
	db     *pgtest.DB
	plan   plan.TransactionPlan
	tenant uuid.UUID
}

func newFixture(t *testing.T, suffix string) fixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1, $2, 'cell-local', 'TX-008 Promotion', 'ACTIVE', $3)`, tenant, "tx008-"+suffix+"-"+tenant.String(), tx008At)
	p := plan.TransactionPlan{
		PlanID: uuid.New().String(), Tenant: values.TenantId(tenant.String()), ProposalRevisionID: "promotion:" + suffix,
		ProposalDigest: "sha256:" + strings.Repeat("a", 64), IdempotencyKey: "tx008:" + suffix + ":" + tenant.String(),
		ExpiresAt:     values.NewInstant(tx008At.Add(time.Hour)),
		Streams:       []plan.StreamPlan{{StreamKey: "promotion:worker", ExpectedSequence: 0}},
		Events:        []plan.PlannedEvent{{StreamKey: "promotion:worker", Sequence: 1, EventType: "PROMOTION_COMMITTED", SchemaRef: "hcmnext.promotion/v1", Digest: strings.Repeat("b", 64)}},
		OutboxEffects: []plan.OutboxEffect{{EffectID: "promotion:payroll", DestinationRef: "payroll", SchemaRef: "hcmnext.payroll/v1", PayloadDigest: "sha256:" + strings.Repeat("c", 64), IdempotencyKey: "effect:" + tenant.String()}},
	}
	digest := sha256.Sum256(p.CanonicalBytes())
	p.Digest = "sha256:" + hex.EncodeToString(digest[:])
	return fixture{db: db, plan: p, tenant: tenant}
}

func request(f fixture) transactioncancel.Request {
	return transactioncancel.NewRequest(f.plan, "principal:promotion", "PROMOTION_WITHDRAWN", tx008At)
}

func commitFunc(f fixture, conn *pgxadapter.Conn) transactioncancel.CommitFunc {
	committer := transactioncommit.New(conn, transactioncommit.Options{Clock: func() time.Time { return tx008At }})
	return func(ctx context.Context, tx dbport.Tx) (transactioncancel.CommitRecord, error) {
		receipt, err := committer.CommitInTx(ctx, tx, f.plan)
		if err != nil {
			return transactioncancel.CommitRecord{}, err
		}
		return transactioncancel.CommitRecord{Identity: receipt.ReceiptID.String()}, nil
	}
}

func count(t *testing.T, f fixture, table string) int {
	t.Helper()
	var n int
	if err := f.db.Conn.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id = $1", f.tenant).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestTodo_TX_008 proves both sides of the boundary against the real
// Promotion-shaped ledger/outbox commit.
func TestTodo_TX_008(t *testing.T) {
	t.Run("before commit", func(t *testing.T) {
		f := newFixture(t, "before")
		coord := transactioncancel.Coordinator{DB: f.db.Conn}
		if _, err := coord.Cancel(context.Background(), request(f)); err != nil {
			t.Fatalf("cancel before commit: %v", err)
		}
		result, err := coord.Commit(context.Background(), request(f), commitFunc(f, f.db.Conn))
		if err != nil {
			t.Fatalf("commit after cancellation: %v", err)
		}
		if result.Outcome != transactioncancel.OutcomeCancelled || result.Boundary != transactioncancel.BoundaryBeforeCommit {
			t.Fatalf("result = %+v, want CANCELLED/BEFORE_COMMIT", result)
		}
		if got := count(t, f, "ledger_event"); got != 0 {
			t.Fatalf("ledger rows = %d, want 0", got)
		}
		if got := count(t, f, "outbox"); got != 0 {
			t.Fatalf("outbox rows = %d, want 0", got)
		}
		var boundary, identity string
		if err := f.db.Conn.QueryRow(context.Background(), `SELECT scope->>'boundary', scope->>'commit_identity' FROM control_evidence WHERE tenant_id = $1 AND control_key = 'transaction.commit.cancellation'`, f.tenant).Scan(&boundary, &identity); err != nil {
			t.Fatal(err)
		}
		if boundary != transactioncancel.BoundaryBeforeCommit || identity == "" {
			t.Fatalf("evidence = %s/%s", boundary, identity)
		}
	})

	t.Run("after commit", func(t *testing.T) {
		f := newFixture(t, "after")
		coord := transactioncancel.Coordinator{DB: f.db.Conn}
		committed, err := coord.Commit(context.Background(), request(f), commitFunc(f, f.db.Conn))
		if err != nil || committed.Outcome != transactioncancel.OutcomeCommitted {
			t.Fatalf("commit = %+v, %v", committed, err)
		}
		var launches atomic.Int32
		req := request(f)
		req.CompensationPath = "correction:promotion-withdrawal"
		cancelled, err := (transactioncancel.Coordinator{DB: f.db.Conn, Compensator: func(context.Context, transactioncancel.CompensationRequest) error { launches.Add(1); return nil }}).Cancel(context.Background(), req)
		if err != nil {
			t.Fatalf("cancel after commit: %v", err)
		}
		if cancelled.Outcome != transactioncancel.OutcomeCommitted || cancelled.Boundary != transactioncancel.BoundaryAfterCommit || !cancelled.CompensationLaunched {
			t.Fatalf("result = %+v", cancelled)
		}
		if launches.Load() != 1 {
			t.Fatalf("compensation launches = %d, want 1", launches.Load())
		}
		if got := count(t, f, "ledger_event"); got != 1 {
			t.Fatalf("ledger rows = %d, want 1", got)
		}
		if got := count(t, f, "outbox"); got != 1 {
			t.Fatalf("outbox rows = %d, want 1", got)
		}
		var boundary, identity string
		if err := f.db.Conn.QueryRow(context.Background(), `SELECT scope->>'boundary', scope->>'commit_identity' FROM control_evidence WHERE tenant_id = $1 AND control_key = 'transaction.commit.cancellation'`, f.tenant).Scan(&boundary, &identity); err != nil {
			t.Fatal(err)
		}
		if boundary != transactioncancel.BoundaryAfterCommit || identity == "" {
			t.Fatalf("evidence = %s/%s", boundary, identity)
		}
	})
}

// TestTodo_TX_008_Race proves independent connections resolve to one durable
// boundary and never expose a half-cancelled ledger/outbox result.
func TestTodo_TX_008_Race(t *testing.T) {
	f := newFixture(t, "race")
	other := f.db.NewConn(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var commitResult, cancelResult transactioncancel.Result
	var commitErr, cancelErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		commitResult, commitErr = (transactioncancel.Coordinator{DB: f.db.Conn}).Commit(context.Background(), request(f), commitFunc(f, f.db.Conn))
	}()
	go func() {
		defer wg.Done()
		<-start
		cancelResult, cancelErr = (transactioncancel.Coordinator{DB: other}).Cancel(context.Background(), request(f))
	}()
	close(start)
	wg.Wait()
	if commitErr != nil {
		t.Fatalf("commit race: %v", commitErr)
	}
	if cancelErr != nil {
		t.Fatalf("cancel race: %v", cancelErr)
	}
	if commitResult.Outcome != transactioncancel.OutcomeCommitted && commitResult.Outcome != transactioncancel.OutcomeCancelled {
		t.Fatalf("commit outcome = %+v", commitResult)
	}
	if cancelResult.Outcome != transactioncancel.OutcomeCommitted && cancelResult.Outcome != transactioncancel.OutcomeCancelled {
		t.Fatalf("cancel outcome = %+v", cancelResult)
	}
	if commitResult.Outcome == transactioncancel.OutcomeCancelled && cancelResult.Boundary != transactioncancel.BoundaryBeforeCommit {
		t.Fatalf("cancel won without BEFORE_COMMIT evidence: %+v", cancelResult)
	}
	if commitResult.Outcome == transactioncancel.OutcomeCommitted && cancelResult.Boundary != transactioncancel.BoundaryAfterCommit {
		t.Fatalf("commit won without AFTER_COMMIT evidence: %+v", cancelResult)
	}
	ledger, effects := count(t, f, "ledger_event"), count(t, f, "outbox")
	if ledger != effects || (ledger != 0 && ledger != 1) {
		t.Fatalf("ledger/outbox = %d/%d, want 0/0 or 1/1", ledger, effects)
	}
	var evidenceCount int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM control_evidence WHERE tenant_id = $1 AND control_key = 'transaction.commit.cancellation'`, f.tenant).Scan(&evidenceCount); err != nil {
		t.Fatal(err)
	}
	if evidenceCount != 1 {
		t.Fatalf("boundary evidence rows = %d, want 1", evidenceCount)
	}
}

// TestTodo_TX_008_Mutation proves an after-commit cancellation cannot rewrite
// the committed outcome and that its append-only evidence names the boundary.
func TestTodo_TX_008_Mutation(t *testing.T) {
	f := newFixture(t, "mutation")
	coord := transactioncancel.Coordinator{DB: f.db.Conn}
	if _, err := coord.Commit(context.Background(), request(f), commitFunc(f, f.db.Conn)); err != nil {
		t.Fatal(err)
	}
	result, err := coord.Cancel(context.Background(), request(f))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != transactioncancel.OutcomeCommitted || result.Boundary != transactioncancel.BoundaryAfterCommit {
		t.Fatalf("result = %+v", result)
	}
	var boundary string
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT scope->>'boundary' FROM control_evidence WHERE tenant_id = $1 AND evidence_id = $2`, f.tenant, result.CancellationEvidence).Scan(&boundary); err != nil {
		t.Fatal(err)
	}
	if boundary != transactioncancel.BoundaryAfterCommit {
		t.Fatalf("boundary = %s", boundary)
	}
	if transactioncancel.Version() != 1 || transactioncancel.Explain() == "" {
		t.Fatal("missing package contract")
	}
}
