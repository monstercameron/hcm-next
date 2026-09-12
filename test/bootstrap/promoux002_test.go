package bootstrap_test

import (
	"context"
	"errors"
	"regexp"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
)

// promux002MutatingStatement mirrors internal/humanwork/workitem's own
// countingExecutor pattern exactly (see decide_approval_test.go): the leading
// keyword and target table of a mutating SQL statement, tolerant of this
// package's own formatting.
var promux002MutatingStatement = regexp.MustCompile(`(?is)^\s*(INSERT INTO|UPDATE|DELETE FROM)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)

// countingBeginner wraps the real dbport.Beginner the journey engine reads
// and writes through (CellConfig.ExecutionDB) and counts every mutating
// statement any transaction it opens issues, by the table it targets.
//
// This is the journey engine's own database handle -- the one
// admitPromotionWindow's tenant-scoped transaction opens through -- so
// wrapping it here observes PROMOUX-002's admission guard exactly where it
// runs, end to end, with no UI or transport in the way. Domain-owned writes
// that go through a different handle (CreateIntent's own ledger store,
// reached through IntentService's own pool) are not this handle's, and are
// checked independently below by row count, which is why they must never
// happen at all when the guard refuses -- not merely "not observed here".
type countingBeginner struct {
	dbport.Beginner
	mu     sync.Mutex
	writes map[string]int
}

func (b *countingBeginner) snapshot() map[string]int {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]int, len(b.writes))
	for k, v := range b.writes {
		out[k] = v
	}
	return out
}

func (b *countingBeginner) record(sql string) {
	m := promux002MutatingStatement.FindStringSubmatch(sql)
	if m == nil {
		return
	}
	b.mu.Lock()
	if b.writes == nil {
		b.writes = map[string]int{}
	}
	b.writes[m[2]]++
	b.mu.Unlock()
}

func (b *countingBeginner) Begin(ctx context.Context) (dbport.Tx, error) {
	tx, err := b.Beginner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &countingTx{Tx: tx, parent: b}, nil
}

type countingTx struct {
	dbport.Tx
	parent *countingBeginner
}

func (t *countingTx) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	t.parent.record(sql)
	return t.Tx.Exec(ctx, sql, args...)
}

func (t *countingTx) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	t.parent.record(sql)
	return t.Tx.QueryRow(ctx, sql, args...)
}

// promux002DomainTables are the tables a promotion's own domain-owned write
// path (CreateIntent, its ledger, and anything a routed WorkItem would raise)
// touches. None of them may change when a conflicting proposal is refused.
var promux002DomainTables = []string{
	"intent_instance", "proposal_revision", "ledger_event",
	"worker", "employment", "assignment", "job_position", "position_occupancy",
	"compensation_package", "compensation_component",
	"workflow_instance", "work_item", "work_item_transition",
}

func promux002TableCounts(t *testing.T, h *journeyHarness) map[string]int {
	t.Helper()
	out := make(map[string]int, len(promux002DomainTables))
	for _, table := range promux002DomainTables {
		out[table] = queryOne[int](t, h.cell, `SELECT count(*) FROM `+table)
	}
	return out
}

// TestTodo_PROMOUX_002_Integration reaches a real PostgreSQL server and the
// real composed journey engine (no fake, no mock workflow) and proves GREEN's
// admission claims against it: a worker's second, differently-keyed proposal
// for an overlapping effective window is refused; a genuinely non-overlapping
// window for the same worker is not a conflict; and the refusal left exactly
// the one intent the first proposal recorded, not two.
func TestTodo_PROMOUX_002_Integration(t *testing.T) {
	h := newJourneyHarness(t)
	ctx := h.operatorCtx(t)

	first, err := h.engine.Propose(ctx, journeyProposal())
	if err != nil {
		t.Fatalf("Propose(first): %v", err)
	}

	// journeyProposal() always mints its own fresh idempotency key inside
	// Propose (there is no caller-supplied client_request_id on this legacy
	// form), so a second call with the same input is, from the guard's point
	// of view, a genuinely different request for the same worker and the
	// same effective date -- exactly the concurrent-start defect PROMOUX-002
	// closes.
	if _, err := h.engine.Propose(ctx, journeyProposal()); !errors.Is(err, workspace.ErrJourneyActiveConflict) {
		t.Fatalf("Propose(conflicting second) = %v, want ErrJourneyActiveConflict", err)
	}

	// GREEN's explicit carve-out: a genuinely non-overlapping window for the
	// very same worker is not a conflict.
	nonOverlapping := journeyProposal()
	nonOverlapping.EffectiveDate = "2027-01-15"
	second, err := h.engine.Propose(ctx, nonOverlapping)
	if err != nil {
		t.Fatalf("Propose(non-overlapping date) = %v, want nil (not a conflict)", err)
	}
	if second.IntentID == first.IntentID {
		t.Fatal("a genuinely new proposal must be its own intent")
	}

	tenantID := pgstore.TenantID(testTenant)
	if n := queryOne[int](t, h.cell,
		`SELECT count(*) FROM intent_instance WHERE tenant_id = $1 AND idempotency_key LIKE 'journey:propose:%'`,
		tenantID); n != 2 {
		t.Fatalf("journey:propose intents recorded = %d, want exactly 2 (first and the non-overlapping second; the refused conflict recorded none)", n)
	}
}

// TestTodo_PROMOUX_002_Security calls the admission path directly -- a plain
// Go method call on the composed journey engine, no HTTP, no gRPC, no
// software router, no rendered page anywhere in the call stack -- and proves
// it refuses a conflicting second start on its own. This is what makes
// REFACTOR's claim ("the guard belongs to promotion admission ... UI
// suppression is not the integrity boundary") true rather than asserted: if
// the only thing standing between a worker and a second promotion were the
// People or Person page declining to render a Start button, this test would
// still succeed in creating one, because nothing here ever asks a page for
// permission.
//
// Point 4's absence claim -- "zero new proposal, work item or domain
// mutation" -- is proved by counting twice, independently:
//  1. internal/data/promotionguard/guard_test.go's countingExecutor pattern,
//     replicated here as [countingBeginner], wraps the journey engine's own
//     database handle and shows the refused call's only mutating statement
//     targeted promotion_active_intent_guard -- the guard's own bookkeeping,
//     never a domain table -- through the exact handle the guard's
//     transaction runs on.
//  2. A before/after row count across every table CreateIntent's ledger and
//     a routed WorkItem could have touched (a different pool than #1 wraps)
//     shows none of them moved at all, which is what makes the refusal a
//     genuine no-op rather than merely unobserved by the counting wrapper.
func TestTodo_PROMOUX_002_Security(t *testing.T) {
	counting := &countingBeginner{}
	h := newJourneyHarness(t, func(cfg *app.CellConfig) {
		counting.Beginner = cfg.ExecutionDB
		cfg.ExecutionDB = counting
	})
	ctx := h.operatorCtx(t)

	first, err := h.engine.Propose(ctx, journeyProposal())
	if err != nil {
		t.Fatalf("Propose(first): %v", err)
	}
	_ = first

	before := promux002TableCounts(t, h)
	writesBefore := counting.snapshot()

	// The direct admission-path call: no UI, no transport, a bare Go method
	// on the composed engine, refused on its own.
	if _, err := h.engine.Propose(ctx, journeyProposal()); !errors.Is(err, workspace.ErrJourneyActiveConflict) {
		t.Fatalf("Propose(conflicting, direct call) = %v, want ErrJourneyActiveConflict", err)
	}

	after := promux002TableCounts(t, h)
	for table, want := range before {
		if got := after[table]; got != want {
			t.Errorf("refused Propose changed %s: %d rows before, %d after", table, want, got)
		}
	}

	writesAfter := counting.snapshot()
	for table, count := range writesAfter {
		if table != "promotion_active_intent_guard" {
			if writesBefore[table] != count {
				t.Fatalf("refused Propose issued a mutating statement against unexpected table %q (%d statements)", table, count)
			}
		}
	}
	if writesAfter["promotion_active_intent_guard"] <= writesBefore["promotion_active_intent_guard"] {
		t.Fatal("the refused call's own admission attempt against promotion_active_intent_guard was not observed; the test fixture is not wired to the engine's real database handle")
	}
}
