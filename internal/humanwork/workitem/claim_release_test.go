package workitem_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// EP-WORK-002's domain half: [workitem.Store.ClaimCurrent] and
// [workitem.Store.Release]. The exclusive item_version compare-and-swap
// itself is WORK-003's own contract, proven exhaustively (including under
// real concurrency) by TestTodo_WORK_003 and TestTodo_WORK_003_Race; these
// tests cover only what EP-WORK-002 adds on top: current-authority checking
// before a claim, authorized voluntary release, the expired-lease guard on
// release, and the append-only evidence trail across a full claim/release
// cycle.

// TestWorkItemClaimCurrentAndReleaseEnforceCurrentAuthority is the domain
// PRIMARY-adjacent case: an authorized assignee claims, an authorized
// candidate claims, an unauthorized caller is refused before any write, the
// current claimant releases successfully, a different principal may not
// release someone else's claim, and an expired lease is never released as if
// it were current.
func TestWorkItemClaimCurrentAndReleaseEnforceCurrentAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "ep-work-002-authority")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	t.Run("an authorized assignee claims through ClaimCurrent", func(t *testing.T) {
		item := availableItem(t, ctx, store, conn, tenant, instance, "principal:amy")
		var claimed workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			claimed, err = store.ClaimCurrent(ctx, tx, workitem.ClaimCurrentInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:amy",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return err
		})
		if claimed.Status != workitem.StatusClaimed || claimed.ClaimedBy != "principal:amy" {
			t.Fatalf("ClaimCurrent(assignee) = %s/%s, want CLAIMED/principal:amy", claimed.Status, claimed.ClaimedBy)
		}
	})

	t.Run("an unauthorized caller is refused before any write", func(t *testing.T) {
		item := availableItem(t, ctx, store, conn, tenant, instance, "principal:bob")
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.ClaimCurrent(ctx, tx, workitem.ClaimCurrentInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:eve", // never resolved as owner or candidate
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeUnauthorizedClaimant {
			t.Fatalf("unauthorized ClaimCurrent code = %q, want %q (%v)", code, workitem.CodeUnauthorizedClaimant, err)
		}
		var unchanged workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			unchanged, err = store.Load(ctx, tx, tenant, item.WorkItemID)
			return err
		})
		if unchanged.Status != workitem.StatusAssigned || unchanged.ClaimID != nil || unchanged.ItemVersion != item.ItemVersion {
			t.Fatalf("a refused ClaimCurrent still mutated the item: status=%s claim=%v version=%d",
				unchanged.Status, unchanged.ClaimID, unchanged.ItemVersion)
		}
	})

	t.Run("a candidate whose delegation has since expired may not claim", func(t *testing.T) {
		// A CANDIDATE_SET item whose only candidate is a delegate whose
		// delegation window has already closed: EP-WORK-002's SECURITY clause
		// literally, checked against the item's own recorded assignment at
		// Now, not against whatever the caller asserts about their authority.
		in, err := workitem.NewWorkItem(newTaskInput(tenant, instance))
		if err != nil {
			t.Fatalf("NewWorkItem: %v", err)
		}
		var created workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			created, err = store.Create(ctx, tx, in, meta(workitem.ReasonCreated))
			return err
		})
		// Two candidates so resolution produces a CANDIDATE_SET (AVAILABLE)
		// item rather than routing straight to a single named assignee: a
		// solitary candidate would be routed as OwnerPrincipal, whose
		// [MembershipOf] path never re-checks a delegation window at all.
		staleDelegateResolution := humanwork.Resolution{
			RequirementID: "req.test/v1", Outcome: humanwork.OutcomeResolved,
			Candidates: []humanwork.Candidate{
				{
					PrincipalID: "principal:stale-delegate", Via: humanwork.SourceDelegated,
					DelegatedFrom: "principal:carol", TermRef: "term.test",
					DelegationExpiry: values.NewInstant(fixedInstant.Add(-time.Hour)),
				},
				{PrincipalID: "principal:carol", Via: humanwork.SourceDirect, TermRef: "term.test"},
			},
			ResolvedAt: values.NewInstant(fixedInstant), EffectiveAt: values.NewInstant(fixedInstant),
			DirectoryVersion: "directory.test/1", ExpressionDigest: "sha256:" + repeatHex("7"),
			RequirementDigest: "sha256:" + repeatHex("8"), QuorumRequired: 1,
		}
		var item workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Route(ctx, tx, tenant, created.WorkItemID, created.ItemVersion,
				workitem.Assignment{Resolution: staleDelegateResolution, Trigger: workitem.TriggerInitialRouting},
				meta("workitem.routed"))
			return err
		})
		if item.Status != workitem.StatusAvailable {
			t.Fatalf("fixture item status = %s, want AVAILABLE", item.Status)
		}
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.ClaimCurrent(ctx, tx, workitem.ClaimCurrentInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:stale-delegate",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeUnauthorizedClaimant {
			t.Fatalf("stale-delegate ClaimCurrent code = %q, want %q (%v)", code, workitem.CodeUnauthorizedClaimant, err)
		}
	})

	t.Run("the current claimant releases successfully", func(t *testing.T) {
		item := availableItem(t, ctx, store, conn, tenant, instance, "principal:dana")
		var claimed workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			claimed, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:dana",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return err
		})
		var released workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			released, err = store.Release(ctx, tx, workitem.ReleaseInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: claimed.ItemVersion,
				ReleasingPrincipalID: "principal:dana", Now: fixedInstant,
				Meta: meta("workitem.released"),
			})
			return err
		})
		if released.Status != workitem.StatusAssigned || released.ClaimID != nil || released.ClaimedBy != "" {
			t.Fatalf("released item = %s claim=%v claimedBy=%q, want ASSIGNED with no claim",
				released.Status, released.ClaimID, released.ClaimedBy)
		}
		if released.ItemVersion != claimed.ItemVersion+1 {
			t.Fatalf("released item version = %d, want %d", released.ItemVersion, claimed.ItemVersion+1)
		}
	})

	t.Run("a different principal may not release someone else's claim", func(t *testing.T) {
		item := availableItem(t, ctx, store, conn, tenant, instance, "principal:frank")
		var claimed workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			claimed, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:frank",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return err
		})
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Release(ctx, tx, workitem.ReleaseInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: claimed.ItemVersion,
				ReleasingPrincipalID: "principal:mallory", Now: fixedInstant,
				Meta: meta("workitem.released"),
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeUnauthorizedClaimant {
			t.Fatalf("release-by-stranger code = %q, want %q (%v)", code, workitem.CodeUnauthorizedClaimant, err)
		}
		var unchanged workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			unchanged, err = store.Load(ctx, tx, tenant, item.WorkItemID)
			return err
		})
		if unchanged.ClaimedBy != "principal:frank" || unchanged.ItemVersion != claimed.ItemVersion {
			t.Fatalf("a refused release still mutated the item: claimedBy=%s version=%d", unchanged.ClaimedBy, unchanged.ItemVersion)
		}
	})

	t.Run("an expired lease is not released as if it were current", func(t *testing.T) {
		item := availableItem(t, ctx, store, conn, tenant, instance, "principal:grace")
		claimExpiry := fixedInstant.Add(time.Hour)
		var claimed workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			claimed, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:grace",
				ClaimExpiresAt:      claimExpiry, Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return err
		})
		after := claimExpiry.Add(time.Minute)
		var released workitem.WorkItem
		err := commitDespiteError(t, conn, tenant, func(tx dbport.Tx) error {
			var txErr error
			released, txErr = store.Release(ctx, tx, workitem.ReleaseInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: claimed.ItemVersion,
				ReleasingPrincipalID: "principal:grace", Now: after,
				Meta: meta("workitem.released"),
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeClaimExpired {
			t.Fatalf("release-of-expired-lease code = %q, want %q (%v)", code, workitem.CodeClaimExpired, err)
		}
		if released.Status != workitem.StatusAssigned || released.ClaimID != nil {
			t.Fatalf("expired-lease release result = %s claim=%v, want ASSIGNED with no claim", released.Status, released.ClaimID)
		}
		// The release trail must show a system expiry, not grace's own release:
		// an expired lease completing "as current" would instead record grace
		// as the actor of a normal release transition.
		trail, err2 := loadTransitions(t, conn, tenant, item.WorkItemID)
		if err2 != nil {
			t.Fatalf("LoadTransitions: %v", err2)
		}
		found := false
		for _, tr := range trail {
			if tr.ToStatus == workitem.StatusAssigned && tr.ItemVersion == released.ItemVersion {
				found = true
				if tr.ActorPrincipalID != workitem.ActorSystemClaimExpiry {
					t.Fatalf("release-of-expired-lease actor = %q, want %q (an expiry, not grace's own release)",
						tr.ActorPrincipalID, workitem.ActorSystemClaimExpiry)
				}
				if tr.Reason != workitem.ReasonClaimExpired {
					t.Fatalf("release-of-expired-lease reason = %q, want %q", tr.Reason, workitem.ReasonClaimExpired)
				}
			}
		}
		if !found {
			t.Fatal("no transition row records the expiry that released grace's lapsed claim")
		}
	})
}

// TestTodo_EP_WORK_002_Property is the PROPERTY matrix case: across a full
// claim/release cycle repeated several times, the item version strictly
// increases at every step and never repeats or goes backward -- the
// versioned-and-monotonic half of EP-WORK-002's GREEN clause.
func TestTodo_EP_WORK_002_Property(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "ep-work-002-property")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	item := availableItem(t, ctx, store, conn, tenant, instance, "principal:mono")
	seen := map[int64]bool{seenVersion(item): true}
	last := item.ItemVersion

	for cycle := 0; cycle < 4; cycle++ {
		var claimed workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			claimed, err = store.ClaimCurrent(ctx, tx, workitem.ClaimCurrentInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: last,
				ClaimantPrincipalID: "principal:mono",
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return err
		})
		if claimed.ItemVersion <= last || seen[claimed.ItemVersion] {
			t.Fatalf("cycle %d: claim version = %d, want strictly greater than %d and unseen", cycle, claimed.ItemVersion, last)
		}
		seen[claimed.ItemVersion] = true
		last = claimed.ItemVersion

		var released workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			released, err = store.Release(ctx, tx, workitem.ReleaseInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: last,
				ReleasingPrincipalID: "principal:mono", Now: fixedInstant,
				Meta: meta("workitem.released"),
			})
			return err
		})
		if released.ItemVersion <= last || seen[released.ItemVersion] {
			t.Fatalf("cycle %d: release version = %d, want strictly greater than %d and unseen", cycle, released.ItemVersion, last)
		}
		seen[released.ItemVersion] = true
		last = released.ItemVersion
	}
}

func seenVersion(w workitem.WorkItem) int64 { return w.ItemVersion }

// TestTodo_EP_WORK_002_Mutation is the MUTATION matrix case: a claim/release
// cycle appends exactly the transition rows the cycle performed, in
// item_version order with no gap and no duplicate, and the database itself
// -- not merely this package's own code path -- refuses any attempt to
// rewrite or remove a prior transition row.
func TestTodo_EP_WORK_002_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "ep-work-002-mutation")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	item := availableItem(t, ctx, store, conn, tenant, instance, "principal:append-only")
	before, err := loadTransitions(t, conn, tenant, item.WorkItemID)
	if err != nil {
		t.Fatalf("LoadTransitions before: %v", err)
	}

	var claimed workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		claimed, err = store.ClaimCurrent(ctx, tx, workitem.ClaimCurrentInput{
			TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			ClaimantPrincipalID: "principal:append-only",
			ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
			Meta: meta("workitem.claimed"),
		})
		return err
	})
	var released workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		released, err = store.Release(ctx, tx, workitem.ReleaseInput{
			TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: claimed.ItemVersion,
			ReleasingPrincipalID: "principal:append-only", Now: fixedInstant,
			Meta: meta("workitem.released"),
		})
		return err
	})

	after, err := loadTransitions(t, conn, tenant, item.WorkItemID)
	if err != nil {
		t.Fatalf("LoadTransitions after: %v", err)
	}
	if len(after) != len(before)+2 {
		t.Fatalf("transition count = %d, want %d (before=%d plus exactly claim+release)", len(after), len(before)+2, len(before))
	}
	// Every prior row is byte-for-byte unchanged: a rewrite would show up here
	// even if the trigger below were somehow bypassed.
	for i, b := range before {
		if after[i] != b {
			t.Fatalf("transition row %d changed after a later write: was %+v, now %+v", i, b, after[i])
		}
	}
	last := after[len(after)-1]
	if last.ToStatus != workitem.StatusAssigned || last.ItemVersion != released.ItemVersion {
		t.Fatalf("final transition = %+v, want the release to ASSIGNED at version %d", last, released.ItemVersion)
	}

	// The database itself forbids rewriting or removing an appended row, not
	// merely this package's own write path: migration 00017's
	// work_item_transition_append_only trigger (forbid_mutation) refuses an
	// UPDATE and a DELETE issued directly, bypassing this package entirely.
	rawConn := appConn(t, db)
	updateErr := inTenantTxErr(rawConn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE work_item_transition SET reason = 'tampered' WHERE tenant_id = $1 AND work_item_id = $2 AND item_version = $3`,
			tenant, item.WorkItemID, before[len(before)-1].ItemVersion)
		return err
	})
	if updateErr == nil {
		t.Fatal("a direct UPDATE on work_item_transition succeeded; append-only evidence is not enforced")
	}
	deleteErr := inTenantTxErr(rawConn, tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM work_item_transition WHERE tenant_id = $1 AND work_item_id = $2 AND item_version = $3`,
			tenant, item.WorkItemID, claimed.ItemVersion)
		return err
	})
	if deleteErr == nil {
		t.Fatal("a direct DELETE on work_item_transition succeeded; append-only evidence is not enforced")
	}

	final, err2 := loadTransitions(t, conn, tenant, item.WorkItemID)
	if err2 != nil {
		t.Fatalf("LoadTransitions final: %v", err2)
	}
	if len(final) != len(after) {
		t.Fatalf("transition count after refused tamper attempts = %d, want unchanged %d", len(final), len(after))
	}
}
