package conformance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	transactioncommit "github.com/monstercameron/hcm-next/internal/transaction/commit"
	"github.com/monstercameron/hcm-next/internal/transaction/plan"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var conformanceAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

type fixture struct {
	db     *pgtest.DB
	tenant uuid.UUID
	plan   plan.TransactionPlan
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'TX-009 conformance', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "tx009-"+tenant.String())
	db.Exec(t, `SELECT set_config('app.tenant_id', $1, false)`, tenant.String())

	p := plan.TransactionPlan{
		PlanID: "tx009-plan-" + tenant.String(), Tenant: values.TenantId(tenant.String()),
		ProposalRevisionID: "proposal:" + tenant.String(), ProposalDigest: "sha256:" + strings.Repeat("a", 64),
		IdempotencyKey: "tx009:" + tenant.String(), ExpiresAt: values.NewInstant(conformanceAt.Add(time.Hour)),
		Streams: []plan.StreamPlan{
			{StreamKey: "stream:b", ExpectedSequence: 0},
			{StreamKey: "stream:a", ExpectedSequence: 0},
		},
		Events: []plan.PlannedEvent{
			{StreamKey: "stream:b", Sequence: 1, EventType: "B_FACT", SchemaRef: "hcmnext.tx009.b/v1", Digest: strings.Repeat("b", 64)},
			{StreamKey: "stream:a", Sequence: 1, EventType: "A_FACT", SchemaRef: "hcmnext.tx009.a/v1", Digest: strings.Repeat("a", 64)},
		},
		OutboxEffects: []plan.OutboxEffect{{
			EffectID: "effect:tx009:" + tenant.String(), DestinationRef: "promotion-provider",
			SchemaRef: "hcmnext.tx009.effect/v1", PayloadDigest: "sha256:" + strings.Repeat("c", 64),
			IdempotencyKey: "effect:" + tenant.String(),
		}},
	}
	digest := sha256.Sum256(p.CanonicalBytes())
	p.Digest = "sha256:" + hex.EncodeToString(digest[:])
	return fixture{db: db, tenant: tenant, plan: p}
}

func (f fixture) committer(t *testing.T, fail Boundary) *transactioncommit.Committer {
	t.Helper()
	return transactioncommit.New(f.db.Conn, transactioncommit.Options{
		Clock: func() time.Time { return conformanceAt },
		Failpoint: func(stage string) error {
			if stage == string(fail) {
				return errors.New("tx009 injected crash at " + stage)
			}
			return nil
		},
	})
}

func rowCounts(t *testing.T, f fixture) map[string]int {
	t.Helper()
	counts := make(map[string]int)
	for _, table := range []string{"ledger_event", "projection_checkpoint", "outbox", "idempotency_record"} {
		var count int
		if err := f.db.Conn.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id = $1", f.tenant).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = count
	}
	return counts
}

func assertDurableSet(t *testing.T, f fixture) {
	t.Helper()
	counts := rowCounts(t, f)
	want := map[string]int{"ledger_event": 2, "projection_checkpoint": 2, "outbox": 1, "idempotency_record": 1}
	for table, expected := range want {
		if counts[table] != expected {
			t.Errorf("%s rows = %d, want %d", table, counts[table], expected)
		}
	}
}

// TestTodo_TX_009 walks every commit boundary and proves that a worker can
// stop there, restart from durable rows, and finish without losing or
// duplicating a governed fact.
func TestTodo_TX_009(t *testing.T) {
	for _, boundary := range Boundaries() {
		boundary := boundary
		t.Run(string(boundary), func(t *testing.T) {
			f := newFixture(t)
			first, err := f.committer(t, boundary).Commit(context.Background(), f.plan)
			if err == nil {
				t.Fatal("crash failpoint did not stop the first commit")
			}

			if boundary == BoundaryAfterCommit {
				if first.Replayed {
					t.Fatal("post-commit crash returned a replay receipt on the first call")
				}
				assertDurableSet(t, f)
			} else if counts := rowCounts(t, f); counts["ledger_event"] != 0 || counts["projection_checkpoint"] != 0 || counts["outbox"] != 0 || counts["idempotency_record"] != 0 {
				t.Fatalf("pre-commit crash left partial rows: %v", counts)
			}

			replay, retryErr := transactioncommit.New(f.db.Conn, transactioncommit.Options{
				Clock: func() time.Time { return conformanceAt },
			}).Commit(context.Background(), f.plan)
			if retryErr != nil {
				t.Fatalf("restart after %s: %v", boundary, retryErr)
			}
			if replay.Replayed != (boundary == BoundaryAfterCommit) {
				t.Fatalf("restart after %s replayed=%t, want %t", boundary, replay.Replayed, boundary == BoundaryAfterCommit)
			}
			assertDurableSet(t, f)
		})
	}
}

// TestTodo_TX_009_Race proves concurrent retries converge on one durable fact
// set when two database sessions cross the same idempotency boundary.
func TestTodo_TX_009_Race(t *testing.T) {
	f := newFixture(t)
	conns := []*pgtest.DB{f.db, f.db}
	start := make(chan struct{})
	errs := make(chan error, len(conns))
	for _, db := range conns {
		go func(db *pgtest.DB) {
			<-start
			_, err := transactioncommit.New(db.NewConn(t), transactioncommit.Options{
				Clock: func() time.Time { return conformanceAt },
			}).Commit(context.Background(), f.plan)
			errs <- err
		}(db)
	}
	close(start)
	for range conns {
		if err := <-errs; err != nil {
			t.Fatalf("racing restart: %v", err)
		}
	}
	assertDurableSet(t, f)
}

// TestTodo_TX_009_Fault states the rollback contract for every injected
// pre-commit fault and the acknowledgement ambiguity for the post-commit
// fault.
func TestTodo_TX_009_Fault(t *testing.T) {
	for _, boundary := range Boundaries() {
		boundary := boundary
		t.Run(string(boundary), func(t *testing.T) {
			f := newFixture(t)
			_, err := f.committer(t, boundary).Commit(context.Background(), f.plan)
			if err == nil || !strings.Contains(err.Error(), string(boundary)) {
				t.Fatalf("fault at %s returned %v", boundary, err)
			}
			if boundary != BoundaryAfterCommit {
				counts := rowCounts(t, f)
				if counts["ledger_event"] != 0 || counts["projection_checkpoint"] != 0 || counts["outbox"] != 0 || counts["idempotency_record"] != 0 {
					t.Fatalf("fault at %s was not atomic: %v", boundary, counts)
				}
			}
		})
	}
}

// TestTodo_TX_009_Conformance pins the boundary vocabulary to the commit
// adapter's documented failpoint seam and ensures it is deterministic.
func TestTodo_TX_009_Conformance(t *testing.T) {
	want := []Boundary{"before-append", "after-append", "after-projection", "after-outbox", "before-receipt", "after-commit"}
	got := Boundaries()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("boundaries = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if Boundaries()[0] != BoundaryBeforeAppend {
		t.Fatal("Boundaries returned aliased storage")
	}
}

// TestTodo_TX_009_Mutation proves a changed plan cannot create a fact set or
// accidentally reuse the idempotency receipt for different content.
func TestTodo_TX_009_Mutation(t *testing.T) {
	f := newFixture(t)
	f.plan.Events[0].Digest = strings.Repeat("d", 64)
	if _, err := transactioncommit.New(f.db.Conn, transactioncommit.Options{
		Clock: func() time.Time { return conformanceAt },
	}).Commit(context.Background(), f.plan); err == nil {
		t.Fatal("mutated transaction plan committed")
	}
	counts := rowCounts(t, f)
	if counts["ledger_event"] != 0 || counts["projection_checkpoint"] != 0 || counts["outbox"] != 0 || counts["idempotency_record"] != 0 {
		t.Fatalf("mutated plan left durable rows: %v", counts)
	}
}
