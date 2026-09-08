package jobs_test

// Break attempts against the durable job stores. Each test asserts the
// DESIRED contract; a failure is a confirmed break, not a bad test. Do
// not "fix" these by weakening the assertion: fix the code, or record a
// deliberate design decision in the test.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/jobs"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
)

var (
	breakTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	breakSpanID  = "00f067aa0ba902b7"
)

func breakCausal(expires time.Time) *jobs.CausalMetadata {
	return &jobs.CausalMetadata{
		CorrelationID: "corr-break", CausationID: "cause-break",
		LogicalOperationID: "logical-break", AttemptID: "attempt-break",
		TraceLink: &jobs.TraceLinkMetadata{
			TraceID: breakTraceID, SpanID: breakSpanID,
			TraceFlags: 1, ExpiresAt: expires,
		},
	}
}

func breakRunFixture(t *testing.T) (*pgtest.DB, uuid.UUID, *pgxadapter.Conn, jobs.JobDefinition, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "break")
	conn := appConn(t, db)
	def := publish(t, ctx, conn, tenant, newDefinition(tenant, "job.break", 1))
	runID := uuid.New()
	declareRun(t, ctx, conn, tenant, def, runID)
	return db, tenant, conn, def, runID
}

func breakCreatePartition(t *testing.T, ctx context.Context, conn *pgxadapter.Conn, tenant, runID uuid.UUID, key string, causal *jobs.CausalMetadata) jobs.JobPartition {
	t.Helper()
	var out jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		out, err = (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: tenant, PartitionID: uuid.New(), RunID: runID,
			PartitionKey: key, CreatedAt: fixedInstant, Causal: causal,
		})
		return err
	})
	return out
}

// TestBreak_ExpiryLessTraceLinkEscapesValidation proves Go validation and
// the schema disagree about trace-link expiry: normalizeCausal accepts a
// link with zero ExpiresAt as valid, but no jobs write handles one
// coherently. Run and partition writes surface a raw driver CHECK error
// instead of ErrInvalid (the schema demands trace_link_expires_at IS NOT
// NULL whenever a link is present); the checkpoint write accepts the link
// and stores it, but Latest/List can never return it (their trace-link
// join demands expires_at > now()). This also breaks cross-store
// propagation: the outbox faithfully stores and returns zero-expiry links
// (proven by outbox TestBreak_ZeroExpiryTraceLinkRoundTrip), so copying an
// outbox causal into any jobs write detonates or silently loses the link.
func TestBreak_ExpiryLessTraceLinkEscapesValidation(t *testing.T) {
	_, tenant, conn, def, _ := breakRunFixture(t)
	ctx := context.Background()

	runID := uuid.New()
	runErr := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.RunStore{}).StartRun(ctx, tx, jobs.JobRun{
			TenantID: tenant, RunID: runID, JobID: def.JobID, JobVersion: def.Version,
			DeclaredBy: "workload:break", DeclaredAt: fixedInstant,
			Causal: breakCausal(time.Time{}),
		})
		return err
	})
	if !errors.Is(runErr, jobs.ErrInvalid) {
		t.Errorf("zero-expiry run err = %T %v; want ErrInvalid, not a raw driver error", runErr, runErr)
	}

	// A control run declared without causal metadata works, so partitions
	// below exercise only the link under test.
	runID2 := uuid.New()
	declareRun(t, ctx, conn, tenant, def, runID2)

	partErr := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: tenant, PartitionID: uuid.New(), RunID: runID2,
			PartitionKey: "zero-expiry-part", CreatedAt: fixedInstant,
			Causal: breakCausal(time.Time{}),
		})
		return err
	})
	if !errors.Is(partErr, jobs.ErrInvalid) {
		t.Errorf("zero-expiry partition err = %T %v; want ErrInvalid, not a raw driver error", partErr, partErr)
	}

	plain := breakCreatePartition(t, ctx, conn, tenant, runID2, "expiry-probe-part", nil)
	checkErr := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.CheckpointStore{}).Checkpoint(ctx, tx, jobs.JobCheckpoint{
			TenantID: tenant, PartitionID: plain.PartitionID, Sequence: 1,
			StateDigest: digestOf("zero-expiry-state"), PartitionVersion: plain.Version,
			TakenAt: fixedInstant, Causal: breakCausal(time.Time{}),
		})
		return err
	})
	// Formerly the checkpoint write ACCEPTED the expiry-less link (the
	// link table stored pgx's zero-time encoding, satisfying NOT NULL)
	// while Latest/List could never return it (their trace-link join
	// demands expires_at > now()) — a stored link silently dropped on
	// read. Rejecting the write closes both the raw-error and the
	// silent-drop path: an expiry-less link is now unrepresentable.
	if !errors.Is(checkErr, jobs.ErrInvalid) {
		t.Errorf("zero-expiry checkpoint err = %T %v; want ErrInvalid, not a raw driver error", checkErr, checkErr)
	}
}

// TestBreak_ClaimedPartitionHasNoRecovery documents the stuck-CLAIMED trap
// (characterization: these assertions describe current behavior, and the
// finding is that no recovery API exists). A partition claimed by a worker
// that then crashes can never be re-claimed — re-claim is
// ErrIllegalTransition, not a lease fence — and the only way out of
// CLAIMED is Cancel, which kills the work instead of redriving it. A
// FAILED partition is worse: nothing leaves FAILED, and re-creating its
// key is ErrDuplicate, so failed work can never be retried under the same
// deterministic key.
func TestBreak_ClaimedPartitionHasNoRecovery(t *testing.T) {
	_, tenant, conn, _, runID := breakRunFixture(t)
	ctx := context.Background()

	part := breakCreatePartition(t, ctx, conn, tenant, runID, "stuck-claim", nil)
	var claimed jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		claimed, err = (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, part.PartitionID, part.Version, "crashed-worker", fixedInstant)
		return err
	})
	if claimed.State != jobs.PartitionClaimed {
		t.Fatalf("state = %q, want CLAIMED", claimed.State)
	}
	// The crashed worker's replacement cannot pick the work back up.
	reclaimErr := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, part.PartitionID, claimed.Version, "replacement-worker", fixedInstant)
		return err
	})
	if !errors.Is(reclaimErr, jobs.ErrIllegalTransition) {
		t.Fatalf("re-claim err = %v, want ErrIllegalTransition (no reclaim path exists)", reclaimErr)
	}
	// The only exit from CLAIMED is Cancel: recovery destroys the work.
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).Cancel(ctx, tx, tenant, part.PartitionID, claimed.Version, fixedInstant)
		return err
	})

	// FAILED is terminal with no redrive: fail a second partition, then
	// show every road out is closed.
	part2 := breakCreatePartition(t, ctx, conn, tenant, runID, "failed-key", nil)
	var claimed2 jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		claimed2, err = (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, part2.PartitionID, part2.Version, "worker-2", fixedInstant)
		return err
	})
	var failed jobs.JobPartition
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		failed, err = (jobs.PartitionStore{}).Fail(ctx, tx, tenant, part2.PartitionID, claimed2.Version, fixedInstant, "boom")
		return err
	})
	if failed.State != jobs.PartitionFailed {
		t.Fatalf("state = %q, want FAILED", failed.State)
	}
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).Cancel(ctx, tx, tenant, part2.PartitionID, failed.Version, fixedInstant)
		return err
	}); !errors.Is(err, jobs.ErrIllegalTransition) {
		t.Fatalf("cancel-after-fail err = %v, want ErrIllegalTransition (FAILED is terminal)", err)
	}
	if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: tenant, PartitionID: uuid.New(), RunID: runID,
			PartitionKey: "failed-key", CreatedAt: fixedInstant,
		})
		return err
	}); !errors.Is(err, jobs.ErrDuplicate) {
		t.Fatalf("re-create err = %v, want ErrDuplicate (key is burned)", err)
	}
}

// TestBreak_StaleVersionAfterTerminalReportsIllegalTransition proves the
// run-level classifier is CAS-second, unlike the partition CAS-first
// classifier: a caller presenting a STALE version against a terminal row
// is told ErrIllegalTransition, masking the version conflict. Whether the
// loser of a race sees VersionConflict or IllegalTransition therefore
// depends on read timing, not on what actually happened.
func TestBreak_StaleVersionAfterTerminalReportsIllegalTransition(t *testing.T) {
	_, tenant, conn, _, runID := breakRunFixture(t)
	ctx := context.Background()

	var begun jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		begun, err = (jobs.RunStore{}).Begin(ctx, tx, tenant, runID, 1, fixedInstant)
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.RunStore{}).Complete(ctx, tx, tenant, runID, begun.Version, fixedInstant)
		return err
	})
	// begun.Version is now stale AND the state moved: the stale caller is
	// told about the state, never about its stale version.
	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.RunStore{}).Cancel(ctx, tx, tenant, runID, begun.Version, fixedInstant)
		return err
	})
	if !errors.Is(err, jobs.ErrVersionConflict) {
		t.Fatalf("stale cancel err = %v; want ErrVersionConflict (version %d is stale regardless of state)", err, begun.Version)
	}
}

// TestBreak_ConcurrentDoubleCompleteElectsOneWinner characterizes the
// same-operation race: two concurrent Completes at the same version elect
// exactly one winner. The loser's error depends on interleaving
// (VersionConflict if it loses the CAS, IllegalTransition if it reads
// after the win), which TestBreak_StaleVersionAfterTerminal... covers
// deterministically.
func TestBreak_ConcurrentDoubleCompleteElectsOneWinner(t *testing.T) {
	db, tenant, _, _, runID := breakRunFixture(t)
	ctx := context.Background()

	conn := appConn(t, db)
	var begun jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		begun, err = (jobs.RunStore{}).Begin(ctx, tx, tenant, runID, 1, fixedInstant)
		return err
	})

	const racers = 2
	start := make(chan struct{})
	errs := make(chan error, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			c := appConn(t, db)
			errs <- inTenantTxErr(c, tenant, func(tx dbport.Tx) error {
				_, err := (jobs.RunStore{}).Complete(ctx, tx, tenant, runID, begun.Version, fixedInstant)
				return err
			})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	wins := 0
	for err := range errs {
		if err == nil {
			wins++
		} else if !errors.Is(err, jobs.ErrVersionConflict) && !errors.Is(err, jobs.ErrIllegalTransition) {
			t.Fatalf("loser err = %v, want VersionConflict or IllegalTransition", err)
		}
	}
	if wins != 1 {
		t.Fatalf("winners = %d, want exactly 1", wins)
	}
	var final jobs.JobRun
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		final, err = (jobs.RunStore{}).Load(ctx, tx, tenant, runID)
		return err
	})
	if final.State != jobs.RunCompleted {
		t.Fatalf("final state = %q, want COMPLETED", final.State)
	}
}

// TestBreak_WhitespaceIdentifiersEscapeValidation proves Go-side validation
// accepts identifiers the database domain refuses: semantic_key demands
// trimmed values, but Create/Claim check only =="". A padded key reaches
// the database and comes back as a raw driver error instead of ErrInvalid,
// so callers cannot programmatically distinguish "bad input" from
// "database is broken".
func TestBreak_WhitespaceIdentifiersEscapeValidation(t *testing.T) {
	_, tenant, conn, _, runID := breakRunFixture(t)
	ctx := context.Background()

	err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).Create(ctx, tx, jobs.JobPartition{
			TenantID: tenant, PartitionID: uuid.New(), RunID: runID,
			PartitionKey: " padded-key ", CreatedAt: fixedInstant,
		})
		return err
	})
	if !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("padded key err = %T %v; want ErrInvalid, not a raw driver error", err, err)
	}

	part := breakCreatePartition(t, ctx, conn, tenant, runID, "holder-key", nil)
	err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := (jobs.PartitionStore{}).ClaimPartition(ctx, tx, tenant, part.PartitionID, part.Version, " padded-holder ", fixedInstant)
		return err
	})
	if !errors.Is(err, jobs.ErrInvalid) {
		t.Fatalf("padded holder err = %T %v; want ErrInvalid, not a raw driver error", err, err)
	}
}
