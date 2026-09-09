package workitem_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

// routedApprovalItem creates and routes an APPROVAL work item for req against
// scenario's directory, at the moment the resolution [ResolveAssignment]
// produces before any later mutation the test makes to the directory.
func routedApprovalItem(
	t *testing.T, ctx context.Context, store workitem.Store, conn *pgxadapter.Conn,
	tenant, instance uuid.UUID, req humanwork.ApprovalRequirement, scenario humanwork.PromotionScenario,
) workitem.WorkItem {
	t.Helper()
	in, err := workitem.NewApprovalTask(newTaskInput(tenant, instance), req.RequirementID)
	if err != nil {
		t.Fatalf("NewApprovalTask: %v", err)
	}
	var item workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Create(ctx, tx, in, meta(workitem.ReasonCreated))
		return err
	})
	assignment, err := workitem.ResolveAssignment(req, scenario.Resolution, scenario.Directory, scenario.Clock, workitem.TriggerInitialRouting)
	if err != nil {
		t.Fatalf("ResolveAssignment: %v", err)
	}
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, assignment, meta("workitem.routed"))
		return err
	})
	return item
}

// TestTodo_WORK_004 is the PRIMARY case: WORK-004's RED clause names a
// departed, suspended or authority-withdrawn owner stranding the task or
// silently transferring it without reason or history. These subtests prove
// [workitem.Store.Reassign] instead re-resolves against the directory as it
// now stands and routes to the next authorized candidate or escalates, with
// an immutable, caller-supplied reason on both new transitions, and that a
// terminal item's history cannot be rewritten by a later relationship
// change.
func TestTodo_WORK_004(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work004-primary")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	reassignMeta := func(reason string) workitem.TransitionMeta {
		return workitem.TransitionMeta{ActorPrincipalID: "system:directory-change", Reason: reason, At: fixedInstant}
	}

	t.Run("a departed sole owner is reassigned to the escalation fallback", func(t *testing.T) {
		// HRBP, unlike the current-manager requirement, has no delegate in
		// the fixture: it resolves to exactly one candidate, which is what
		// this subtest needs to make "the sole owner departed" unambiguous.
		req, scenario := promotionRequirement(t, humanwork.RequirementHRBP)
		item := routedApprovalItem(t, ctx, store, conn, tenant, instance, req, scenario)
		if item.Status != workitem.StatusAssigned || item.OwnerRef != humanwork.PrincipalHRBP {
			t.Fatalf("fixture item = %s/%s, want ASSIGNED/%s", item.Status, item.OwnerRef, humanwork.PrincipalHRBP)
		}

		// The HRBP departs: no longer an active actor at all. The deputy
		// fallback group holds role.hrbp, so the fallback is reachable.
		scenario.Directory.SetActive(humanwork.PrincipalHRBP, false)

		var reassigned workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			reassigned, err = store.Reassign(ctx, tx, workitem.ReassignInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				Requirement: req, Resolution: scenario.Resolution, Directory: scenario.Directory, Clock: scenario.Clock,
				Meta: reassignMeta("work.reassignment.principal_departed"),
			})
			return err
		})
		if reassigned.Status != workitem.StatusAssigned || reassigned.OwnerKind != workitem.OwnerPrincipal {
			t.Fatalf("reassigned = %s/%s, want ASSIGNED/PRINCIPAL (the deputy fallback holds role.hrbp)",
				reassigned.Status, reassigned.OwnerKind)
		}
		if reassigned.OwnerRef != humanwork.PrincipalDeputy {
			t.Fatalf("reassigned owner = %q, want the fallback deputy %q", reassigned.OwnerRef, humanwork.PrincipalDeputy)
		}
		if !reassigned.Assignment.Resolution.FallbackUsed {
			t.Error("reassignment to the deputy did not record FallbackUsed")
		}
		if reassigned.Assignment.Trigger != workitem.TriggerReassignment {
			t.Errorf("recorded trigger = %q, want %q", reassigned.Assignment.Trigger, workitem.TriggerReassignment)
		}

		// Prior history is not rewritten: the original ASSIGNED-to-the-manager
		// transition is still in the chain, and the reassignment adds to it --
		// an ESCALATED row and a new final-target row, both carrying the
		// caller's reason.
		trail, err := loadTransitions(t, conn, tenant, item.WorkItemID)
		if err != nil {
			t.Fatalf("LoadTransitions: %v", err)
		}
		var sawOriginalAssign, sawEscalated, sawReassignedRow bool
		for _, tr := range trail {
			switch {
			case tr.ToStatus == workitem.StatusAssigned && tr.ItemVersion == item.ItemVersion:
				sawOriginalAssign = true
			case tr.ToStatus == workitem.StatusEscalated:
				sawEscalated = true
				if tr.Reason != "work.reassignment.principal_departed" {
					t.Errorf("escalation transition reason = %q, want the caller's own reassignment reason", tr.Reason)
				}
			case tr.ToStatus == workitem.StatusAssigned && tr.ItemVersion == reassigned.ItemVersion:
				sawReassignedRow = true
				if tr.Reason != "work.reassignment.principal_departed" {
					t.Errorf("final routing transition reason = %q, want the caller's own reassignment reason", tr.Reason)
				}
			}
		}
		if !sawOriginalAssign || !sawEscalated || !sawReassignedRow {
			t.Fatalf("transition chain missing an expected row: original=%v escalated=%v reassigned=%v (%d rows)",
				sawOriginalAssign, sawEscalated, sawReassignedRow, len(trail))
		}
	})

	t.Run("no authorized candidate at all escalates rather than stranding", func(t *testing.T) {
		req, scenario := promotionRequirement(t, humanwork.RequirementHRBP)
		item := routedApprovalItem(t, ctx, store, conn, tenant, instance, req, scenario)
		if item.Status != workitem.StatusAssigned || item.OwnerRef != humanwork.PrincipalHRBP {
			t.Fatalf("fixture item = %s/%s, want ASSIGNED/%s", item.Status, item.OwnerRef, humanwork.PrincipalHRBP)
		}

		// The HRBP is suspended, and the fallback deputy is out too, exhausting
		// every path to an authorized approver (the intern in the same
		// fallback group holds no authority at all).
		scenario.Directory.SetActive(humanwork.PrincipalHRBP, false)
		scenario.Directory.SetActive(humanwork.PrincipalDeputy, false)

		var reassigned workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			reassigned, err = store.Reassign(ctx, tx, workitem.ReassignInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				Requirement: req, Resolution: scenario.Resolution, Directory: scenario.Directory, Clock: scenario.Clock,
				Meta: reassignMeta("work.reassignment.principal_suspended"),
			})
			return err
		})
		if reassigned.Status != workitem.StatusEscalated || reassigned.OwnerKind != workitem.OwnerPolicyRoute {
			t.Fatalf("reassigned = %s/%s, want ESCALATED/POLICY_ROUTE -- an exhausted requirement must not strand the item",
				reassigned.Status, reassigned.OwnerKind)
		}
		if reassigned.OwnerRef != item.PolicyRouteRef {
			t.Fatalf("escalated owner ref = %q, want the item's own policy route %q", reassigned.OwnerRef, item.PolicyRouteRef)
		}
		found := false
		for _, e := range reassigned.Assignment.Resolution.Excluded {
			if e.PrincipalID == humanwork.PrincipalHRBP && e.RuleID == humanwork.RulePrincipalInactive {
				found = true
			}
		}
		if !found {
			t.Fatalf("the suspended HRBP is not recorded as excluded: %+v", reassigned.Assignment.Resolution.Excluded)
		}
	})

	t.Run("a completed item's history may not be rewritten by a later relationship change", func(t *testing.T) {
		req, scenario := promotionRequirement(t, humanwork.RequirementCompensationPartner)
		item := routedApprovalItem(t, ctx, store, conn, tenant, instance, req, scenario)

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: humanwork.PrincipalCompensationPartner,
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
				CompletedBy: humanwork.PrincipalCompensationPartner, CompletedOutputDigest: "sha256:" + repeatDigit("7"),
				Now: fixedInstant, Meta: meta("workitem.completed"),
			})
			return err
		})
		if item.Status != workitem.StatusCompleted {
			t.Fatalf("fixture item status = %s, want COMPLETED", item.Status)
		}

		scenario.Directory.SetActive(humanwork.PrincipalCompensationPartner, false)
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Reassign(ctx, tx, workitem.ReassignInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				Requirement: req, Resolution: scenario.Resolution, Directory: scenario.Directory, Clock: scenario.Clock,
				Meta: reassignMeta("work.reassignment.principal_departed"),
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeIllegalTransition {
			t.Fatalf("reassigning a COMPLETED item refusal code = %q, want %q (%v)", code, workitem.CodeIllegalTransition, err)
		}

		var unchanged workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			unchanged, err = store.Load(ctx, tx, tenant, item.WorkItemID)
			return err
		})
		if unchanged.Status != workitem.StatusCompleted || unchanged.CompletedBy != humanwork.PrincipalCompensationPartner ||
			unchanged.ItemVersion != item.ItemVersion {
			t.Fatalf("a refused reassignment changed the completed item: status=%s completed_by=%s version=%d",
				unchanged.Status, unchanged.CompletedBy, unchanged.ItemVersion)
		}
	})
}

// TestTodo_WORK_004_Race drives concurrent reassignments of the same item at
// the same starting version. Exactly one may win the escalate-then-route
// pair; every loser must be refused a stale-item code and leave the item
// exactly as the winner left it.
func TestTodo_WORK_004_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work004-race")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	setup := appConn(t, db)

	req, scenario := promotionRequirement(t, humanwork.RequirementHRBP)
	item := routedApprovalItem(t, ctx, store, setup, tenant, instance, req, scenario)
	scenario.Directory.SetActive(humanwork.PrincipalHRBP, false)

	const writers = 5
	results := make([]error, writers)
	conns := make([]*pgxadapter.Conn, writers)
	for i := range writers {
		conns[i] = appConn(t, db)
	}

	var start, done sync.WaitGroup
	start.Add(1)
	for i := range writers {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			results[i] = inTenantTxErr(conns[i], tenant, func(tx dbport.Tx) error {
				_, err := store.Reassign(ctx, tx, workitem.ReassignInput{
					TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
					Requirement: req, Resolution: scenario.Resolution, Directory: scenario.Directory, Clock: scenario.Clock,
					Meta: workitem.TransitionMeta{ActorPrincipalID: "system:directory-change", Reason: "work.reassignment.race", At: fixedInstant},
				})
				return err
			})
		}(i)
	}
	start.Done()
	done.Wait()

	winners := 0
	for i, err := range results {
		switch {
		case err == nil:
			winners++
		case workitem.CodeOf(err) == workitem.CodeStaleItem:
			// The expected loss.
		default:
			t.Errorf("writer %d failed for an unexpected reason: %v", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d of %d concurrent reassignments committed; exactly one may", winners, writers)
	}

	var final workitem.WorkItem
	inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
		var err error
		final, err = store.Load(ctx, tx, tenant, item.WorkItemID)
		return err
	})
	if final.Status != workitem.StatusAssigned || final.OwnerRef != humanwork.PrincipalDeputy {
		t.Fatalf("final item = %s/%s, want ASSIGNED/%s", final.Status, final.OwnerRef, humanwork.PrincipalDeputy)
	}
}

// TestTodo_WORK_004_Mutation kills the mutants that would make "invalidates
// stale claim authority" and "only a live routed owner may be reassigned"
// unfalsifiable.
func TestTodo_WORK_004_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work004-mutation")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	rMeta := workitem.TransitionMeta{ActorPrincipalID: "system:directory-change", Reason: "work.reassignment.test", At: fixedInstant}

	t.Run("reassigning a claimed item invalidates the live claim", func(t *testing.T) {
		req, scenario := promotionRequirement(t, humanwork.RequirementCurrentManager)
		item := routedApprovalItem(t, ctx, store, conn, tenant, instance, req, scenario)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: humanwork.PrincipalManager,
				ClaimExpiresAt:      fixedInstant.Add(time.Hour), Now: fixedInstant,
				Meta: meta("workitem.claimed"),
			})
			return err
		})
		if item.ClaimID == nil {
			t.Fatal("fixture item carries no claim to invalidate")
		}

		scenario.Directory.SetActive(humanwork.PrincipalManager, false)
		var reassigned workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			reassigned, err = store.Reassign(ctx, tx, workitem.ReassignInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				Requirement: req, Resolution: scenario.Resolution, Directory: scenario.Directory, Clock: scenario.Clock,
				Meta: rMeta,
			})
			return err
		})
		if reassigned.ClaimID != nil || reassigned.ClaimedBy != "" {
			t.Fatalf("reassignment left a stale claim behind: claim_id=%v claimed_by=%q", reassigned.ClaimID, reassigned.ClaimedBy)
		}
	})

	t.Run("an item with no live routed owner cannot be reassigned", func(t *testing.T) {
		req, scenario := promotionRequirement(t, humanwork.RequirementCurrentManager)
		in, err := workitem.NewApprovalTask(newTaskInput(tenant, instance), req.RequirementID)
		if err != nil {
			t.Fatalf("NewApprovalTask: %v", err)
		}
		var created workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			created, err = store.Create(ctx, tx, in, meta(workitem.ReasonCreated))
			return err
		})
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Reassign(ctx, tx, workitem.ReassignInput{
				TenantID: tenant, WorkItemID: created.WorkItemID, ExpectedVersion: created.ItemVersion,
				Requirement: req, Resolution: scenario.Resolution, Directory: scenario.Directory, Clock: scenario.Clock,
				Meta: rMeta,
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeIllegalTransition {
			t.Fatalf("reassigning a CREATED item refusal code = %q, want %q (%v)", code, workitem.CodeIllegalTransition, err)
		}
	})

	t.Run("a requirement that does not match the item's own is refused", func(t *testing.T) {
		req, scenario := promotionRequirement(t, humanwork.RequirementCurrentManager)
		item := routedApprovalItem(t, ctx, store, conn, tenant, instance, req, scenario)
		otherReq, _ := promotionRequirement(t, humanwork.RequirementHRBP)

		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Reassign(ctx, tx, workitem.ReassignInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				Requirement: otherReq, Resolution: scenario.Resolution, Directory: scenario.Directory, Clock: scenario.Clock,
				Meta: rMeta,
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeInvalidRecord {
			t.Fatalf("mismatched-requirement reassignment refusal code = %q, want %q (%v)", code, workitem.CodeInvalidRecord, err)
		}
	})

	t.Run("a reassignment that resolves to the same owner still records new evidence, never a silent no-op", func(t *testing.T) {
		req, scenario := promotionRequirement(t, humanwork.RequirementHRBP)
		item := routedApprovalItem(t, ctx, store, conn, tenant, instance, req, scenario)

		before, err := loadTransitions(t, conn, tenant, item.WorkItemID)
		if err != nil {
			t.Fatalf("LoadTransitions: %v", err)
		}

		var reassigned workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			reassigned, err = store.Reassign(ctx, tx, workitem.ReassignInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				Requirement: req, Resolution: scenario.Resolution, Directory: scenario.Directory, Clock: scenario.Clock,
				Meta: rMeta,
			})
			return err
		})
		if reassigned.OwnerRef != humanwork.PrincipalHRBP {
			t.Fatalf("owner changed to %q despite nothing invalidating the HRBP; want %q unchanged",
				reassigned.OwnerRef, humanwork.PrincipalHRBP)
		}

		after, err := loadTransitions(t, conn, tenant, item.WorkItemID)
		if err != nil {
			t.Fatalf("LoadTransitions: %v", err)
		}
		if len(after) <= len(before) {
			t.Fatalf("a no-op reassignment added no new transition rows: before=%d after=%d", len(before), len(after))
		}
	})
}

// TestTodo_WORK_004_Security proves a tenant cannot reassign, or even learn
// about, another tenant's work item.
func TestTodo_WORK_004_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	alice := insertTenant(t, db, "work004-security-a")
	bob := insertTenant(t, db, "work004-security-b")
	instance := uuid.New()
	insertInstance(t, db, alice, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	req, scenario := promotionRequirement(t, humanwork.RequirementHRBP)
	item := routedApprovalItem(t, ctx, store, conn, alice, instance, req, scenario)
	scenario.Directory.SetActive(humanwork.PrincipalHRBP, false)

	err := inTenantTxErr(conn, bob, func(tx dbport.Tx) error {
		_, txErr := store.Reassign(ctx, tx, workitem.ReassignInput{
			TenantID: alice, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
			Requirement: req, Resolution: scenario.Resolution, Directory: scenario.Directory, Clock: scenario.Clock,
			Meta: workitem.TransitionMeta{ActorPrincipalID: "system:directory-change", Reason: "work.reassignment.test", At: fixedInstant},
		})
		return txErr
	})
	if code := workitem.CodeOf(err); code != workitem.CodeWorkItemNotFound {
		t.Fatalf("cross-tenant reassignment code = %q, want %q (%v)", code, workitem.CodeWorkItemNotFound, err)
	}

	var unchanged workitem.WorkItem
	inTenantTx(t, conn, alice, func(tx dbport.Tx) error {
		var err error
		unchanged, err = store.Load(ctx, tx, alice, item.WorkItemID)
		return err
	})
	if unchanged.Status != workitem.StatusAssigned || unchanged.OwnerRef != humanwork.PrincipalHRBP {
		t.Fatalf("a cross-tenant reassignment attempt changed the item to %s/%s", unchanged.Status, unchanged.OwnerRef)
	}
}
