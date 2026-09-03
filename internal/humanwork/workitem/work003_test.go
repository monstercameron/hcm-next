package workitem_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
)

// WORK-003's RED clause: a concurrent claim test must never let two owners
// hold the item at once, and an expired or stale item_version must never
// complete an operation. There is no lease, fence token or sweeper anywhere
// below -- definitions/runtime/durable-runtime-decision.yaml gates all three
// behind the P1B re-evaluation -- so every guarantee here is carried by the
// item_version compare-and-swap and by the caller's own supplied Now.

// availableItem creates and routes a work item to ASSIGNED for a single
// named principal, ready to be claimed.
func availableItem(t *testing.T, ctx context.Context, store workitem.Store, conn *pgxadapter.Conn, tenant, instance uuid.UUID, owner string) workitem.WorkItem {
	t.Helper()
	in, err := workitem.NewWorkItem(newTaskInput(tenant, instance))
	if err != nil {
		t.Fatalf("NewWorkItem: %v", err)
	}
	var item workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Create(ctx, tx, in, meta(workitem.ReasonCreated))
		return err
	})
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
			workitem.Assignment{Resolution: singleCandidateResolution(owner), Trigger: workitem.TriggerInitialRouting},
			meta("workitem.routed"))
		return err
	})
	if item.Status != workitem.StatusAssigned {
		t.Fatalf("fixture item status = %s, want ASSIGNED", item.Status)
	}
	return item
}

// TestTodo_WORK_003 is the PRIMARY case: a claim wins, a second claim on the
// live claim is refused ALREADY_CLAIMED and mutates nothing, an in-progress
// item cannot be claimed either, and a claim discovered expired on a touch
// releases the item to its policy route and refuses the operation that
// touched it.
func TestTodo_WORK_003(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work003-primary")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	t.Run("a claim wins, and a second claim on it is refused and mutates nothing", func(t *testing.T) {
		item := availableItem(t, ctx, store, conn, tenant, instance, "principal:owner-1")
		var claimed workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			claimed, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:owner-1",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return err
		})
		if claimed.Status != workitem.StatusClaimed || claimed.ClaimedBy != "principal:owner-1" {
			t.Fatalf("claim = %s/%s, want CLAIMED/principal:owner-1", claimed.Status, claimed.ClaimedBy)
		}
		if claimed.ClaimID == nil {
			t.Fatal("a live claim carries no claim id")
		}

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: claimed.ItemVersion,
				ClaimantPrincipalID: "principal:owner-2",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeAlreadyClaimed {
			t.Fatalf("second claim refusal code = %q, want %q (%v)", code, workitem.CodeAlreadyClaimed, err)
		}
		var unchanged workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			unchanged, err = store.Load(ctx, tx, tenant, item.WorkItemID)
			return err
		})
		if unchanged.ClaimedBy != "principal:owner-1" || unchanged.ItemVersion != claimed.ItemVersion {
			t.Fatalf("a refused second claim changed the item to claimed_by=%s version=%d",
				unchanged.ClaimedBy, unchanged.ItemVersion)
		}
	})

	t.Run("an in-progress item cannot be claimed by anyone", func(t *testing.T) {
		item := availableItem(t, ctx, store, conn, tenant, instance, "principal:owner-3")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:owner-3",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Start(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, fixedInstant, meta("workitem.started"))
			return err
		})
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:owner-4",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeAlreadyClaimed {
			t.Fatalf("claim-of-in-progress refusal code = %q, want %q (%v)", code, workitem.CodeAlreadyClaimed, err)
		}
	})

	t.Run("a stale item_version never completes a claim", func(t *testing.T) {
		item := availableItem(t, ctx, store, conn, tenant, instance, "principal:owner-5")
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion + 41,
				ClaimantPrincipalID: "principal:owner-5",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeAlreadyClaimed {
			t.Fatalf("stale-version claim refusal code = %q, want %q (%v)", code, workitem.CodeAlreadyClaimed, err)
		}
		var unchanged workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			unchanged, err = store.Load(ctx, tx, tenant, item.WorkItemID)
			return err
		})
		if unchanged.Status != workitem.StatusAssigned || unchanged.ClaimID != nil {
			t.Fatalf("a stale-version claim attempt still mutated the item: %s claim=%v", unchanged.Status, unchanged.ClaimID)
		}
	})

	t.Run("a touch that finds an expired claim releases it to the policy route and refuses the operation", func(t *testing.T) {
		item := availableItem(t, ctx, store, conn, tenant, instance, "principal:owner-6")
		claimExpiry := fixedInstant.Add(time.Hour)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:owner-6",
				ClaimExpiresAt:      claimExpiry, Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return err
		})
		after := claimExpiry.Add(time.Minute) // strictly after the claim's own expiry

		// doc.go is explicit that this refusal is not like the others: "A
		// caller that commits keeps the release. Rolling back instead loses
		// nothing." This test wants the release kept, so -- unlike every other
		// refusal in this suite, which this package's inTenantTxErr rolls back
		// -- the transaction is committed despite Start's own error.
		var released workitem.WorkItem
		err := commitDespiteError(t, conn, tenant, func(tx dbport.Tx) error {
			var txErr error
			released, txErr = store.Start(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, after, meta("workitem.started"))
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeClaimExpired {
			t.Fatalf("Start on an expired claim refusal code = %q, want %q (%v)", code, workitem.CodeClaimExpired, err)
		}
		if released.Status != workitem.StatusAssigned || released.ClaimID != nil {
			t.Fatalf("the released item = %s claim=%v, want ASSIGNED with no claim -- doc.go: "+
				"'the returned item is the released one'", released.Status, released.ClaimID)
		}
		if released.ItemVersion != item.ItemVersion+1 {
			t.Fatalf("released item version = %d, want %d (one bump for the release)", released.ItemVersion, item.ItemVersion+1)
		}

		// The released item is open for claim again by anyone, including a
		// different principal, and the release itself is durable once committed.
		var reclaimed workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			reclaimed, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: released.ItemVersion,
				ClaimantPrincipalID: "principal:owner-7",
				ClaimExpiresAt:      after.Add(time.Hour), Now: after,
				Meta: meta("workitem.claimed"),
			})
			return err
		})
		if reclaimed.ClaimedBy != "principal:owner-7" {
			t.Fatalf("reclaimed by = %q, want principal:owner-7", reclaimed.ClaimedBy)
		}

		trail, err := loadTransitions(t, conn, tenant, item.WorkItemID)
		if err != nil {
			t.Fatalf("LoadTransitions: %v", err)
		}
		foundExpiry := false
		for _, tr := range trail {
			if tr.Reason == workitem.ReasonClaimExpired {
				foundExpiry = true
				if tr.ActorPrincipalID != workitem.ActorSystemClaimExpiry {
					t.Errorf("claim-expiry transition actor = %q, want %q", tr.ActorPrincipalID, workitem.ActorSystemClaimExpiry)
				}
			}
		}
		if !foundExpiry {
			t.Fatal("no transition row records the claim expiry release")
		}
	})

	t.Run("a completed item's output digest cannot be reused as a fresh claim target", func(t *testing.T) {
		item := availableItem(t, ctx, store, conn, tenant, instance, "principal:owner-8")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:owner-8",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Start(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, fixedInstant, meta("workitem.started"))
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Complete(ctx, tx, workitem.CompleteInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				CompletedBy: "principal:owner-8", CompletedOutputDigest: "sha256:" + repeatDigit("d"),
				Now: fixedInstant, Meta: meta("workitem.completed"),
			})
			return err
		})
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:owner-9",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeIllegalTransition {
			t.Fatalf("claiming a completed item refusal code = %q, want %q (%v)", code, workitem.CodeIllegalTransition, err)
		}
	})
}

// TestTodo_WORK_003_Race drives N genuinely concurrent claimants -- separate
// connections, separate transactions -- at the same AVAILABLE item and the
// same item version. Exactly one may win, matching WORK-003's RED clause
// literally: "concurrent claim test allows two owners ... completes" is the
// defect, and the only thing preventing it is the item_version
// compare-and-swap [Store.Claim] runs its UPDATE through.
func TestTodo_WORK_003_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work003-race")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	setup := appConn(t, db)

	item := availableItem(t, ctx, store, setup, tenant, instance, "principal:owner-race")

	const claimants = 8
	type result struct {
		claimant string
		version  int64
		err      error
	}
	results := make([]result, claimants)
	conns := make([]*pgxadapter.Conn, claimants)
	for i := range claimants {
		conns[i] = appConn(t, db)
	}

	var start, done sync.WaitGroup
	start.Add(1)
	for i := range claimants {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			claimant := fmt.Sprintf("principal:racer-%d", i)
			err := inTenantTxErr(conns[i], tenant, func(tx dbport.Tx) error {
				claimed, err := store.Claim(ctx, tx, workitem.ClaimInput{
					TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
					ClaimantPrincipalID: claimant,
					ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
					Meta: workitem.TransitionMeta{ActorPrincipalID: claimant, Reason: "workitem.claimed", At: fixedInstant},
				})
				if err != nil {
					return err
				}
				results[i] = result{claimant: claimant, version: claimed.ItemVersion}
				return nil
			})
			if err != nil {
				results[i] = result{claimant: claimant, err: err}
			}
		}()
	}
	start.Done()
	done.Wait()

	winners := 0
	var winner string
	for i, r := range results {
		switch {
		case r.err == nil:
			winners++
			winner = r.claimant
			if r.version != item.ItemVersion+1 {
				t.Errorf("winner %d claimed at version %d, want %d", i, r.version, item.ItemVersion+1)
			}
		case workitem.CodeOf(r.err) == workitem.CodeAlreadyClaimed:
			// The expected loss.
		default:
			t.Errorf("claimant %d failed for an unexpected reason: %v", i, r.err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d of %d concurrent claimants won the claim; exactly one may", winners, claimants)
	}

	reader := appConn(t, db)
	var final workitem.WorkItem
	inTenantTx(t, reader, tenant, func(tx dbport.Tx) error {
		var err error
		final, err = store.Load(ctx, tx, tenant, item.WorkItemID)
		return err
	})
	if final.ClaimedBy != winner {
		t.Fatalf("stored claimant = %q, want the sole winner %q", final.ClaimedBy, winner)
	}
	if final.Status != workitem.StatusClaimed {
		t.Fatalf("stored status = %s, want CLAIMED", final.Status)
	}

	// Exactly one CLAIMED transition row exists -- a loser that had already
	// inserted evidence before losing the version race would show up here.
	trail, err := loadTransitions(t, reader, tenant, item.WorkItemID)
	if err != nil {
		t.Fatalf("LoadTransitions: %v", err)
	}
	claimedRows := 0
	for _, tr := range trail {
		if tr.ToStatus == workitem.StatusClaimed {
			claimedRows++
			if tr.ActorPrincipalID != winner {
				t.Errorf("a CLAIMED transition row was recorded for %q, not the winner %q", tr.ActorPrincipalID, winner)
			}
		}
	}
	if claimedRows != 1 {
		t.Fatalf("%d CLAIMED transition rows recorded after the race, want 1", claimedRows)
	}
}

func loadTransitions(t *testing.T, conn *pgxadapter.Conn, tenant, workItemID uuid.UUID) ([]workitem.TransitionRecord, error) {
	t.Helper()
	var (
		trail []workitem.TransitionRecord
		err   error
	)
	e := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		trail, err = workitem.Store{}.LoadTransitions(context.Background(), tx, tenant, workItemID)
		return err
	})
	if e != nil {
		return nil, e
	}
	return trail, err
}

func repeatDigit(d string) string {
	out := ""
	for range 64 {
		out += d
	}
	return out
}

// commitDespiteError runs fn inside a tenant-scoped transaction and commits
// it even when fn returns an error -- the one shape [inTenantTxErr] cannot
// produce, since it always rolls back on error. It exists for exactly one
// case in this suite: [workitem.Store.Start]/Complete/Return legitimately
// return both a released item and a [workitem.CodeClaimExpired] refusal in
// the same call, and doc.go is explicit that keeping that release is the
// caller's choice to make by committing anyway.
func commitDespiteError(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("scope tenant: %v", err)
	}
	fnErr := fn(tx)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit despite error: %v", err)
	}
	return fnErr
}
