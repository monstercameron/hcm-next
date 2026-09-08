package commit

import (
	"context"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	transactioncancel "github.com/monstercameron/hcm-next/internal/transaction/cancel"
	transactioncoordinator "github.com/monstercameron/hcm-next/internal/transaction/coordinator"
	"github.com/monstercameron/hcm-next/internal/transaction/plan"
)

// RetryOptions and RetryClosure expose the shared bounded retry mechanics at
// the commit boundary without making workflow execution depend on the
// coordinator's plan-specific API.
type RetryOptions = transactioncoordinator.RetryOptions

var ErrCommitAmbiguous = transactioncoordinator.ErrCommitAmbiguous

func RetryClosure(ctx context.Context, opts RetryOptions, closure func(context.Context) error) error {
	return transactioncoordinator.RetryClosure(ctx, opts, closure)
}

// GovernedResult carries the original commit receipt when this call wins the
// boundary. A pre-commit cancellation has no receipt by definition.
type GovernedResult struct {
	Decision transactioncancel.Result
	Receipt  Receipt
}

// CommitGoverned executes this coordinator under TX-008's cancellation lock.
// Its commit callback uses the same transaction as the boundary decision.
func (c *Committer) CommitGoverned(ctx context.Context, prepared plan.TransactionPlan, req transactioncancel.Request) (GovernedResult, error) {
	var receipt Receipt
	decision, err := (transactioncancel.Coordinator{DB: c.db, Clock: c.opts.Clock}).Commit(ctx, req,
		func(ctx context.Context, tx dbport.Tx) (transactioncancel.CommitRecord, error) {
			var err error
			receipt, err = c.CommitInTx(ctx, tx, prepared)
			if err != nil {
				return transactioncancel.CommitRecord{}, err
			}
			return transactioncancel.CommitRecord{Identity: receipt.ReceiptID.String()}, nil
		})
	if err != nil {
		return GovernedResult{Decision: decision, Receipt: receipt}, err
	}
	return GovernedResult{Decision: decision, Receipt: receipt}, nil
}

// CancelGoverned resolves a cancellation against the same local commit
// boundary and optionally launches the supplied governed TX-007 path.
func (c *Committer) CancelGoverned(ctx context.Context, req transactioncancel.Request, compensator transactioncancel.Compensator) (GovernedResult, error) {
	decision, err := (transactioncancel.Coordinator{DB: c.db, Clock: c.opts.Clock, Compensator: compensator}).Cancel(ctx, req)
	return GovernedResult{Decision: decision}, err
}
