package disposition_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger/disposition"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/recordsmeta"
)

// isRetryableTxError reports a transient PostgreSQL serialization or deadlock
// failure -- the only outcome concurrent, correctly-ordered lock acquisition
// should ever produce under load, and safe to retry as a whole new
// transaction. Mirrors internal/data/recordsmeta's own helper of the same
// name.
func isRetryableTxError(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	return false
}

func inTenantTxRetrying(conn interface {
	Begin(ctx context.Context) (dbport.Tx, error)
}, fn func(tx dbport.Tx) error) error {
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		ctx := context.Background()
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		lastErr = fn(tx)
		if lastErr != nil {
			_ = tx.Rollback(ctx)
			if isRetryableTxError(lastErr) {
				continue
			}
			return lastErr
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			lastErr = commitErr
			if isRetryableTxError(commitErr) {
				continue
			}
			return commitErr
		}
		return nil
	}
	return lastErr
}

// TestTodo_LEDGER_011_Race fires several concurrent Erase attempts at the
// same event. Exactly one may succeed; every other attempt must fail with
// disposition.ErrAlreadyDisposed (never silently succeed, never corrupt the
// row, never produce two disposition rows). This is the TOCTOU shape the
// todo calls out directly: nothing may destroy a payload a hold protects, or
// record two conflicting dispositions for one event, because two callers
// raced the check-then-act gap.
func TestTodo_LEDGER_011_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "ledger011-race")
	registerLedgerSchema(t, db, tenant)
	ensureStream(t, conn, tenant, streamKey)
	chainAppender := newChainAppender(t)
	chainDigester := newChainDigester(t)

	receipt := appendWithChain(t, conn, tenant, chainAppender, appendRequest(tenant, streamKey, 0, []byte("race-target"), ""))

	declaration := newDeclaration(tenant)
	var link recordsmeta.CopyLink
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := recordsmeta.InsertRecordDeclaration(ctx, tx, declaration); err != nil {
			return err
		}
		var err error
		link, err = recordsmeta.RegisterCopy(ctx, tx, recordsmeta.CopyLink{
			TenantID: tenant, DeclarationID: declaration.DeclarationID, CopyType: "CANONICAL",
			StoreRef: "ledger:" + streamKey, LedgerStream: streamKey, LedgerSequence: receipt.Sequence,
		})
		return err
	})

	beforeChainHead := verifyChain(t, conn, tenant, streamKey, chainDigester)

	const goroutines = 8
	var wg sync.WaitGroup
	results := make([]error, goroutines)
	states := make([]disposition.PayloadState, goroutines)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			raceConn := db.NewConn(t)
			if _, err := raceConn.Exec(ctx, "SET ROLE hcmnext_app"); err != nil {
				results[i] = err
				return
			}
			results[i] = inTenantTxRetrying(raceConn, func(tx dbport.Tx) error {
				view, err := disposition.Erase(ctx, tx, disposition.EraseRequest{
					Tenant: tenant, StreamKey: streamKey, Sequence: receipt.Sequence,
					DeclarationID: declaration.DeclarationID, LinkID: link.LinkID,
					Classification: disposition.ClassificationStandard,
					Reason:         "race", Actor: "principal:records",
					At: fixedAt.Add(time.Duration(i) * time.Second),
				})
				if err == nil {
					states[i] = view.State
				}
				return err
			})
		}(i)
	}
	wg.Wait()

	succeeded, alreadyDisposed, other := 0, 0, 0
	for i, err := range results {
		switch {
		case err == nil:
			succeeded++
			if states[i] != disposition.StatePayloadErased {
				t.Errorf("goroutine %d succeeded with state %q, want StatePayloadErased", i, states[i])
			}
		case errors.Is(err, disposition.ErrAlreadyDisposed):
			alreadyDisposed++
		default:
			other++
			t.Errorf("goroutine %d returned an unexpected error: %v", i, err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("succeeded = %d of %d concurrent erasures, want exactly 1", succeeded, goroutines)
	}
	if alreadyDisposed != goroutines-1 {
		t.Fatalf("ErrAlreadyDisposed = %d of %d losers, want %d", alreadyDisposed, goroutines-1, goroutines-1)
	}
	if other != 0 {
		t.Fatalf("%d goroutine(s) returned neither success nor ErrAlreadyDisposed", other)
	}

	// Exactly one disposition row exists for the event -- the PK enforces
	// it, and this confirms no goroutine found a way around that.
	if !dispositionRowExists(t, db, tenant, streamKey, receipt.Sequence) {
		t.Fatal("no disposition row exists after a successful concurrent erase")
	}

	view := readView(t, conn, tenant, streamKey, receipt.Sequence)
	if view.State != disposition.StatePayloadErased {
		t.Fatalf("final view = %+v, want StatePayloadErased", view)
	}

	afterChainHead := verifyChain(t, conn, tenant, streamKey, chainDigester)
	if afterChainHead != beforeChainHead {
		t.Fatalf("concurrent erasure attempts changed the chain head: before=%+v after=%+v", beforeChainHead, afterChainHead)
	}
}
