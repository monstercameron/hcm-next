package commit_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	transactioncommit "github.com/monstercameron/hcm-next/internal/transaction/commit"
	"github.com/monstercameron/hcm-next/internal/transaction/plan"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var commitAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func commitFixture(t *testing.T) (*pgtest.DB, plan.TransactionPlan, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Commit test', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant, "commit-"+tenant.String())
	db.Exec(t, `SELECT set_config('app.tenant_id', $1, false)`, tenant.String())

	p := plan.TransactionPlan{
		PlanID: "commit-plan-" + tenant.String(), Tenant: values.TenantId(tenant.String()),
		ProposalRevisionID: "proposal:" + tenant.String(), ProposalDigest: "sha256:" + strings.Repeat("a", 64),
		IdempotencyKey: "commit:" + tenant.String(), ExpiresAt: values.NewInstant(commitAt.Add(time.Hour)),
		// Deliberately not in stream-key order: the ledger adapter owns the
		// canonical order, while plan order remains material to this digest.
		Streams: []plan.StreamPlan{{StreamKey: "stream:b", ExpectedSequence: 0}, {StreamKey: "stream:a", ExpectedSequence: 0}},
		Events: []plan.PlannedEvent{
			{StreamKey: "stream:b", Sequence: 1, EventType: "B_FACT", SchemaRef: "hcmnext.commit.b/v1", Digest: strings.Repeat("b", 64)},
			{StreamKey: "stream:a", Sequence: 1, EventType: "A_FACT", SchemaRef: "hcmnext.commit.a/v1", Digest: strings.Repeat("a", 64)},
		},
		OutboxEffects: []plan.OutboxEffect{{EffectID: "effect:payroll:" + tenant.String(), DestinationRef: "payroll", SchemaRef: "hcmnext.commit.payroll/v1", PayloadDigest: "sha256:" + strings.Repeat("c", 64), IdempotencyKey: "effect:" + tenant.String()}},
	}
	canonical := sha256.Sum256(p.CanonicalBytes())
	p.Digest = "sha256:" + hex.EncodeToString(canonical[:])
	return db, p, tenant
}

func committer(t *testing.T, db *pgtest.DB) *transactioncommit.Committer {
	t.Helper()
	return transactioncommit.New(db.Conn, transactioncommit.Options{Clock: func() time.Time { return commitAt }})
}

// TestTodo_TX_004 proves ledger, critical checkpoints, outbox and the durable
// replay record are produced by one local commit and a replay returns the same
// receipt without creating a second fact.
func TestTodo_TX_004(t *testing.T) {
	db, prepared, tenant := commitFixture(t)
	c := committer(t, db)
	first, err := c.Commit(context.Background(), prepared)
	if err != nil {
		t.Fatalf("first commit: %v", err)
	}
	if first.Replayed || len(first.Events) != 2 || len(first.Projections) != 2 || len(first.Outbox) != 1 {
		t.Fatalf("first receipt = %+v, want 2 events/2 projections/1 outbox", first)
	}
	second, err := c.Commit(context.Background(), prepared)
	if err != nil {
		t.Fatalf("replay commit: %v", err)
	}
	if !second.Replayed || second.ReceiptID != first.ReceiptID {
		t.Fatalf("replay receipt = %+v, want same receipt marked replayed", second)
	}
	ctx := context.Background()
	for table, want := range map[string]int{"ledger_event": 2, "outbox": 1, "projection_checkpoint": 2, "idempotency_record": 1} {
		var got int
		if err := db.Conn.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id = $1", tenant).Scan(&got); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if got != want {
			t.Errorf("%s rows = %d, want %d", table, got, want)
		}
	}
	var firstStream, secondStream string
	if err := db.Conn.QueryRow(ctx, `SELECT stream_key FROM ledger_event WHERE tenant_id = $1 ORDER BY sequence, stream_key LIMIT 1`, tenant).Scan(&firstStream); err != nil {
		t.Fatal(err)
	}
	if err := db.Conn.QueryRow(ctx, `SELECT stream_key FROM ledger_event WHERE tenant_id = $1 ORDER BY sequence, stream_key OFFSET 1 LIMIT 1`, tenant).Scan(&secondStream); err != nil {
		t.Fatal(err)
	}
	if firstStream != "stream:a" || secondStream != "stream:b" {
		t.Fatalf("ledger order = %s, %s; want stream:a, stream:b", firstStream, secondStream)
	}
}

// TestTodo_TX_004_Race proves the same plan submitted on independent database
// sessions produces one idempotency winner and one durable event set.
func TestTodo_TX_004_Race(t *testing.T) {
	db, prepared, tenant := commitFixture(t)
	connections := []*pgxadapter.Conn{db.NewConn(t), db.NewConn(t)}
	start := make(chan struct{})
	results := make(chan error, len(connections))
	var wg sync.WaitGroup
	for _, conn := range connections {
		wg.Add(1)
		go func(conn *pgxadapter.Conn) {
			defer wg.Done()
			<-start
			c := transactioncommit.New(conn, transactioncommit.Options{Clock: func() time.Time { return commitAt }})
			_, err := c.Commit(context.Background(), prepared)
			results <- err
		}(conn)
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("racing commit: %v", err)
		}
	}
	var events, effects int
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenant).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM outbox WHERE tenant_id = $1`, tenant).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if events != 2 || effects != 1 {
		t.Fatalf("racing commit rows = events %d/outbox %d, want 2/1", events, effects)
	}
}

// TestTodo_TX_004_Fault injects a failure after the ledger phase and proves
// the caller's transaction rolls back ledger, projections, outbox and receipt.
func TestTodo_TX_004_Fault(t *testing.T) {
	t.Run("before commit rolls back", func(t *testing.T) {
		db, prepared, tenant := commitFixture(t)
		injected := errors.New("injected after ledger")
		c := transactioncommit.New(db.Conn, transactioncommit.Options{
			Clock: commitClock,
			Failpoint: func(stage string) error {
				if stage == "after-append" {
					return injected
				}
				return nil
			},
		})
		if _, err := c.Commit(context.Background(), prepared); !errors.Is(err, injected) {
			t.Fatalf("fault error = %v, want injected error", err)
		}
		for _, table := range []string{"ledger_event", "projection_checkpoint", "outbox", "idempotency_record"} {
			var count int
			if err := db.Conn.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id = $1", tenant).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Errorf("%s rows after rollback = %d, want 0", table, count)
			}
		}
	})

	t.Run("after commit is durable", func(t *testing.T) {
		db, prepared, tenant := commitFixture(t)
		injected := errors.New("injected after commit")
		c := transactioncommit.New(db.Conn, transactioncommit.Options{
			Clock: commitClock,
			Failpoint: func(stage string) error {
				if stage == "after-commit" {
					return injected
				}
				return nil
			},
		})
		if _, err := c.Commit(context.Background(), prepared); !errors.Is(err, injected) {
			t.Fatalf("post-commit fault error = %v, want injected error", err)
		}
		var events int
		if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenant).Scan(&events); err != nil {
			t.Fatal(err)
		}
		if events != 2 {
			t.Fatalf("ledger rows after post-commit fault = %d, want 2", events)
		}
		replay, err := committer(t, db).Commit(context.Background(), prepared)
		if err != nil || !replay.Replayed {
			t.Fatalf("replay after post-commit fault = %+v, %v; want durable replay", replay, err)
		}
	})
}

// TestTodo_TX_004_Mutation proves a plan digest mutation is refused before
// any transaction-local row can be created.
func TestTodo_TX_004_Mutation(t *testing.T) {
	db, prepared, tenant := commitFixture(t)
	prepared.Events[0].Digest = strings.Repeat("d", 64)
	c := committer(t, db)
	if _, err := c.Commit(context.Background(), prepared); err == nil {
		t.Fatal("tampered plan committed")
	}
	var count int
	if err := db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, tenant).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("ledger rows after invalid plan = %d, want 0", count)
	}
}

func commitClock() time.Time { return commitAt }
