package approval_test

// TestTodo_PROMOUX_003_Mutation proves the separation-of-duties refusal
// writes nothing: not the CAS UPDATE on work_item, not the append-only
// INSERT into work_item_transition, nothing at all beyond the read-only
// SELECT ... FOR UPDATE [workitem.Store.LockApprovalSiblings] issues to
// establish the conflict in the first place. It reuses the exact
// countingExecutor pattern internal/humanwork/workitem/decide_approval_test.go
// established for EP-WORK-003's own zero-domain-mutation clause: a wrapper
// around the real [dbport.Tx] that parses every mutating statement's target
// table and lets a test assert on exactly which tables, and how many times,
// a call wrote to.

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

// mutatingStatementPromoux003 matches the leading keyword and target table of
// a mutating SQL statement, tolerant of this package's own formatting.
var mutatingStatementPromoux003 = regexp.MustCompile(`(?is)^\s*(INSERT INTO|UPDATE|DELETE FROM)\s+([a-zA-Z_][a-zA-Z0-9_]*)`)

// countingExecutor wraps a real [dbport.Tx] and counts every mutating
// statement by the table it targets, leaving every read untouched. See this
// file's own doc comment.
type countingExecutor struct {
	dbport.Tx
	mu     sync.Mutex
	writes map[string]int
}

func (c *countingExecutor) record(sql string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writes == nil {
		c.writes = map[string]int{}
	}
	if m := mutatingStatementPromoux003.FindStringSubmatch(sql); m != nil {
		c.writes[m[2]]++
	}
}

func (c *countingExecutor) Exec(ctx context.Context, sql string, args ...any) (int64, error) {
	c.record(sql)
	return c.Tx.Exec(ctx, sql, args...)
}

func (c *countingExecutor) QueryRow(ctx context.Context, sql string, args ...any) dbport.Row {
	c.record(sql)
	return c.Tx.QueryRow(ctx, sql, args...)
}

func TestTodo_PROMOUX_003_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "promoux003-mutation")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance, "approve_finance")
	conn := appConn(t, db)
	ctx := context.Background()

	prop := promoProposal(strings.Repeat("8", 64))
	financeReq := promoRequirement("approval.promoux003.finance.mutation", "finance_partner")
	managerReq := promoRequirement("approval.promoux003.manager.mutation", "current_manager")
	const conflicted = "principal:mutation-conflicted"

	financeItem := promoOpenRouteStart(t, ctx, conn, tenant, instance, "approve_finance", financeReq, prop, conflicted)
	managerItem := promoOpenRouteStart(t, ctx, conn, tenant, instance, "approve_manager", managerReq, prop, conflicted)

	store := workitem.Store{}
	var completedFinance workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		d := promoDecision(financeReq, prop, conflicted, "decision:mutation-finance", intentapproval.OutcomeApproved)
		var err error
		completedFinance, err = stepapproval.Complete(ctx, tx, store, financeItem, d, promoux003At,
			workitem.TransitionMeta{ActorPrincipalID: conflicted, Reason: "workitem.completed", At: promoux003At})
		return err
	})
	if completedFinance.Status != workitem.StatusCompleted {
		t.Fatalf("finance item status = %s, want COMPLETED", completedFinance.Status)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("scope tenant: %v", err)
	}
	counting := &countingExecutor{Tx: tx}
	d := promoDecision(managerReq, prop, conflicted, "decision:mutation-manager", intentapproval.OutcomeApproved)
	_, completeErr := stepapproval.Complete(ctx, counting, store, managerItem, d, promoux003At,
		workitem.TransitionMeta{ActorPrincipalID: conflicted, Reason: "workitem.completed", At: promoux003At})
	if !errors.Is(completeErr, stepapproval.ErrSeparationConflict) {
		t.Fatalf("Complete = %v, want ErrSeparationConflict", completeErr)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit the refused attempt's (empty) transaction: %v", err)
	}

	if len(counting.writes) != 0 {
		t.Fatalf("a separation-of-duties refusal wrote to %v; it must write nothing at all", counting.writes)
	}
}
