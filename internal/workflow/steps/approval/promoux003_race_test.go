package approval_test

// TestTodo_PROMOUX_003_Race proves PROMOUX-003's core guarantee under real
// concurrency: two goroutines, each on its own connection and its own
// transaction, race to complete the two different approval requirements on
// one proposal as the identical principal. At most one may succeed.
//
// The guarantee is database-level row locking, not an in-process mutex or a
// unique constraint: [workitem.Store.LockApprovalSiblings] issues
// `SELECT ... FOR UPDATE` over every APPROVAL work item sharing
// (tenant_id, proposal_ref) before [stepapproval.Complete] inspects or
// writes anything. Both goroutines here name the same row set -- the
// finance and manager items share one proposal_ref -- so PostgreSQL's own
// lock manager serializes them: whichever transaction's SELECT ... FOR
// UPDATE is granted first holds both rows until it commits or rolls back,
// and the second transaction's SELECT ... FOR UPDATE blocks until then. By
// the time the second transaction proceeds, it reads the first
// transaction's already-committed completion and refuses.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	stepapproval "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/approval"
)

func TestTodo_PROMOUX_003_Race(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "promoux003-race")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance, "approve_finance")
	ctx := context.Background()

	prop := promoProposal(strings.Repeat("7", 64))
	financeReq := promoRequirement("approval.promoux003.finance.race", "finance_partner")
	managerReq := promoRequirement("approval.promoux003.manager.race", "current_manager")
	const conflicted = "principal:race-conflicted"

	// Every routing write below (Open/Route/Claim/Start) runs on its own
	// short-lived connection, exactly as promoOpenRouteStart already does,
	// so the two items are fully IN_PROGRESS and durable before either race
	// goroutine begins.
	setupConn := appConn(t, db)
	financeItem := promoOpenRouteStart(t, ctx, setupConn, tenant, instance, "approve_finance", financeReq, prop, conflicted)
	managerItem := promoOpenRouteStart(t, ctx, setupConn, tenant, instance, "approve_manager", managerReq, prop, conflicted)

	// Two independent connections, each opening its own transaction, so the
	// race is between two genuinely separate database sessions -- not two
	// goroutines sharing one connection's transaction.
	connA := appConn(t, db)
	connB := appConn(t, db)
	store := workitem.Store{}

	var wg sync.WaitGroup
	var barrier sync.WaitGroup
	barrier.Add(2)
	results := make(chan error, 2)

	race := func(conn *pgxadapter.Conn, item workitem.WorkItem, req humanwork.ApprovalRequirement, decisionID string) {
		defer wg.Done()
		tx, err := conn.Begin(ctx)
		if err != nil {
			results <- err
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
			results <- err
			return
		}
		barrier.Done()
		barrier.Wait()
		d := promoDecision(req, prop, conflicted, decisionID, intentapproval.OutcomeApproved)
		if _, err := stepapproval.Complete(ctx, tx, store, item, d, promoux003At,
			workitem.TransitionMeta{ActorPrincipalID: conflicted, Reason: "workitem.completed", At: promoux003At}); err != nil {
			results <- err
			return
		}
		results <- tx.Commit(ctx)
	}

	wg.Add(2)
	go race(connA, financeItem, financeReq, "decision:race-finance")
	go race(connB, managerItem, managerReq, "decision:race-manager")
	wg.Wait()
	close(results)

	var succeeded, refused int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, stepapproval.ErrSeparationConflict):
			refused++
		default:
			t.Fatalf("race attempt ended with an unexpected error: %v", err)
		}
	}
	if succeeded != 1 || refused != 1 {
		t.Fatalf("concurrent completions by one principal: succeeded=%d refused=%d, want exactly one of each", succeeded, refused)
	}

	// Durable proof, not just the two goroutines' own return values: exactly
	// one of the two items is COMPLETED and the other is unchanged.
	verifyConn := appConn(t, db)
	inTenantTx(t, verifyConn, tenant, func(tx dbport.Tx) error {
		f, err := store.Load(ctx, tx, tenant, financeItem.WorkItemID)
		if err != nil {
			return err
		}
		m, err := store.Load(ctx, tx, tenant, managerItem.WorkItemID)
		if err != nil {
			return err
		}
		completedCount := 0
		for _, it := range []workitem.WorkItem{f, m} {
			if it.Status == workitem.StatusCompleted {
				completedCount++
			}
		}
		if completedCount != 1 {
			t.Fatalf("durable state after the race: finance=%s manager=%s, want exactly one COMPLETED", f.Status, m.Status)
		}
		return nil
	})
}
