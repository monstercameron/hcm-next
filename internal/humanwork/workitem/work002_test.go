package workitem_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/humanwork"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
)

// WORK-002's RED clause: an inactive, unauthorized or out-of-scope candidate,
// a stale relationship or an unresolvable fallback must never strand the item
// or hand it to someone who should not hold it. The tests below run the real
// humanwork resolver -- internal/humanwork's own promotion fixture, not a
// stand-in -- and prove [workitem.ResolveAssignment] and [workitem.Store.Route]
// record its answer faithfully: the expression digest, the candidate set, the
// exclusions with their rule ids, the directory and governance-policy
// versions, and the trigger, without ever reimplementing candidate selection.

func promotionRequirement(t *testing.T, requirementID string) (humanwork.ApprovalRequirement, humanwork.PromotionScenario) {
	t.Helper()
	scenario, err := humanwork.NewPromotionScenario(humanwork.PromotionInputExecutive())
	if err != nil {
		t.Fatalf("NewPromotionScenario: %v", err)
	}
	req, ok := scenario.Requirements.Find(requirementID)
	if !ok {
		t.Fatalf("requirement %s not present in the executive-tier scenario", requirementID)
	}
	return req, scenario
}

// TestTodo_WORK_002 is the PRIMARY case: a single-candidate requirement
// assigns directly, a multi-candidate requirement is left AVAILABLE as a
// recorded candidate set, and a requirement whose only candidate has gone
// inactive is escalated -- with the exclusion attached -- rather than
// stranded or handed to someone unauthorized.
func TestTodo_WORK_002(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work002-primary")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	t.Run("a single resolved candidate is assigned directly", func(t *testing.T) {
		req, scenario := promotionRequirement(t, humanwork.RequirementHRBP)
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
		if assignment.ChosenOwner != humanwork.PrincipalHRBP {
			t.Fatalf("chosen owner = %q, want %q", assignment.ChosenOwner, humanwork.PrincipalHRBP)
		}

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, assignment, meta("workitem.routed"))
			return err
		})
		if item.Status != workitem.StatusAssigned {
			t.Fatalf("status = %s, want ASSIGNED", item.Status)
		}
		if item.OwnerKind != workitem.OwnerPrincipal || item.OwnerRef != humanwork.PrincipalHRBP {
			t.Fatalf("owner = %s/%s, want PRINCIPAL/%s", item.OwnerKind, item.OwnerRef, humanwork.PrincipalHRBP)
		}

		// The evidence round-trips through storage exactly: reload and check the
		// resolution expression digest, requirement digest, directory version,
		// governance policy ref and trigger survive the jsonb column.
		var reloaded workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			reloaded, err = store.Load(ctx, tx, tenant, item.WorkItemID)
			return err
		})
		if reloaded.Assignment.Resolution.ExpressionDigest != req.ExpressionDigest {
			t.Errorf("stored expression digest = %q, want %q", reloaded.Assignment.Resolution.ExpressionDigest, req.ExpressionDigest)
		}
		if reloaded.Assignment.Resolution.RequirementDigest != req.Digest() {
			t.Errorf("stored requirement digest = %q, want %q", reloaded.Assignment.Resolution.RequirementDigest, req.Digest())
		}
		if reloaded.Assignment.Resolution.DirectoryVersion != scenario.Directory.Version() {
			t.Errorf("stored directory version = %q, want %q", reloaded.Assignment.Resolution.DirectoryVersion, scenario.Directory.Version())
		}
		if reloaded.Assignment.GovernancePolicyRef != req.Source.GovernancePolicyRef {
			t.Errorf("stored governance policy ref = %q, want %q", reloaded.Assignment.GovernancePolicyRef, req.Source.GovernancePolicyRef)
		}
		if reloaded.Assignment.Trigger != workitem.TriggerInitialRouting {
			t.Errorf("stored trigger = %q, want %q", reloaded.Assignment.Trigger, workitem.TriggerInitialRouting)
		}
		if !reloaded.Assignment.IsCandidate(humanwork.PrincipalHRBP) {
			t.Error("the chosen owner does not round-trip as a recorded candidate")
		}
		if reloaded.Assignment.IsCandidate("principal:nobody") {
			t.Error("a principal never in the candidate set reads back as one")
		}

		// Recording an owner grants nothing: WORK-002's REFACTOR clause. There is
		// no method on Assignment or WorkItem that answers "may X decide this" --
		// only "was X a candidate when this resolved".
	})

	t.Run("more than one surviving candidate is left AVAILABLE, not assigned to one of them", func(t *testing.T) {
		req, scenario := promotionRequirement(t, humanwork.RequirementExecutiveCommittee)
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
		if len(assignment.Resolution.Candidates) < 2 {
			t.Fatalf("the executive committee resolved %d candidates, want at least 2", len(assignment.Resolution.Candidates))
		}
		if assignment.ChosenOwner != "" {
			t.Errorf("a multi-candidate resolution named a chosen owner: %q", assignment.ChosenOwner)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, assignment, meta("workitem.routed"))
			return err
		})
		if item.Status != workitem.StatusAvailable || item.OwnerKind != workitem.OwnerCandidateSet {
			t.Fatalf("status/owner = %s/%s, want AVAILABLE/CANDIDATE_SET", item.Status, item.OwnerKind)
		}
		for _, c := range assignment.Resolution.Candidates {
			if !item.Assignment.IsCandidate(c.PrincipalID) {
				t.Errorf("candidate %s is not recorded on the routed item", c.PrincipalID)
			}
		}
	})

	t.Run("an inactive candidate is excluded and the item escalates rather than stranding", func(t *testing.T) {
		req, scenario := promotionRequirement(t, humanwork.RequirementHRBP)
		scenario.Directory.SetActive(humanwork.PrincipalHRBP, false)
		// The escalation fallback would otherwise reach the deputy group (which
		// holds every authority, HRBP included) and resolve anyway -- correct
		// resolver behaviour, but not the case this test needs. Taking the
		// deputy out too is what actually exhausts the requirement.
		scenario.Directory.SetActive(humanwork.PrincipalDeputy, false)

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
		if assignment.Resolution.Outcome != humanwork.OutcomeNoAuthorizedApprover {
			t.Fatalf("outcome = %s, want NO_AUTHORIZED_APPROVER", assignment.Resolution.Outcome)
		}
		found := false
		for _, e := range assignment.Resolution.Excluded {
			if e.PrincipalID == humanwork.PrincipalHRBP && e.RuleID == humanwork.RulePrincipalInactive {
				found = true
			}
		}
		if !found {
			t.Fatalf("the inactive manager is not recorded as excluded: %+v", assignment.Resolution.Excluded)
		}

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, assignment, meta("workitem.routed"))
			return err
		})
		if item.Status != workitem.StatusEscalated {
			t.Fatalf("status = %s, want ESCALATED -- an unfillable requirement must not strand the item", item.Status)
		}
		if item.OwnerKind != workitem.OwnerPolicyRoute || item.OwnerRef != in.PolicyRouteRef {
			t.Fatalf("owner = %s/%s, want POLICY_ROUTE/%s -- escalation returns to the policy route, not to nobody",
				item.OwnerKind, item.OwnerRef, in.PolicyRouteRef)
		}
		if item.Assignment.IsCandidate(humanwork.PrincipalHRBP) {
			t.Error("an excluded principal reads back as a candidate")
		}
	})
}

// TestTodo_WORK_002_Race drives two concurrent re-resolutions against the same
// item version. Exactly one may commit its assignment: there is no lease
// here either, only the item_version compare-and-set every write in this
// package goes through.
func TestTodo_WORK_002_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work002-race")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}

	req, scenario := promotionRequirement(t, humanwork.RequirementHRBP)
	assignment, err := workitem.ResolveAssignment(req, scenario.Resolution, scenario.Directory, scenario.Clock, workitem.TriggerInitialRouting)
	if err != nil {
		t.Fatalf("ResolveAssignment: %v", err)
	}

	in, err := workitem.NewApprovalTask(newTaskInput(tenant, instance), req.RequirementID)
	if err != nil {
		t.Fatalf("NewApprovalTask: %v", err)
	}
	setup := appConn(t, db)
	var item workitem.WorkItem
	inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Create(ctx, tx, in, meta(workitem.ReasonCreated))
		return err
	})

	const writers = 5
	type result struct {
		version int64
		err     error
	}
	results := make([]result, writers)
	conns := make([]*pgxadapter.Conn, writers)
	for i := range writers {
		conns[i] = appConn(t, db)
	}

	var start, done sync.WaitGroup
	start.Add(1)
	for i := range writers {
		done.Add(1)
		go func() {
			defer done.Done()
			start.Wait()
			err := inTenantTxErr(conns[i], tenant, func(tx dbport.Tx) error {
				updated, err := store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, assignment, meta("workitem.routed"))
				if err != nil {
					return err
				}
				results[i] = result{version: updated.ItemVersion}
				return nil
			})
			if err != nil {
				results[i] = result{err: err}
			}
		}()
	}
	start.Done()
	done.Wait()

	winners := 0
	for i, r := range results {
		switch {
		case r.err == nil:
			winners++
		case workitem.CodeOf(r.err) == workitem.CodeStaleItem:
			// The expected loss: the ROUTED->ASSIGNED step lost the version race.
		default:
			t.Errorf("writer %d failed for an unexpected reason: %v", i, r.err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d of %d concurrent routings committed; exactly one may", winners, writers)
	}
}

// TestTodo_WORK_002_Security proves a tenant cannot route, or even learn
// about, another tenant's work item.
func TestTodo_WORK_002_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	alice := insertTenant(t, db, "work002-security-a")
	bob := insertTenant(t, db, "work002-security-b")
	instance := uuid.New()
	insertInstance(t, db, alice, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	req, scenario := promotionRequirement(t, humanwork.RequirementHRBP)
	assignment, err := workitem.ResolveAssignment(req, scenario.Resolution, scenario.Directory, scenario.Clock, workitem.TriggerInitialRouting)
	if err != nil {
		t.Fatalf("ResolveAssignment: %v", err)
	}
	in, err := workitem.NewApprovalTask(newTaskInput(alice, instance), req.RequirementID)
	if err != nil {
		t.Fatalf("NewApprovalTask: %v", err)
	}
	var item workitem.WorkItem
	inTenantTx(t, conn, alice, func(tx dbport.Tx) error {
		var err error
		item, err = store.Create(ctx, tx, in, meta(workitem.ReasonCreated))
		return err
	})

	err = inTenantTxErr(conn, bob, func(tx dbport.Tx) error {
		_, txErr := store.Route(ctx, tx, alice, item.WorkItemID, item.ItemVersion, assignment, meta("workitem.routed"))
		return txErr
	})
	if code := workitem.CodeOf(err); code != workitem.CodeWorkItemNotFound {
		t.Fatalf("cross-tenant route code = %q, want %q (%v)", code, workitem.CodeWorkItemNotFound, err)
	}

	var unchanged workitem.WorkItem
	inTenantTx(t, conn, alice, func(tx dbport.Tx) error {
		var err error
		unchanged, err = store.Load(ctx, tx, alice, item.WorkItemID)
		return err
	})
	if unchanged.Status != workitem.StatusCreated {
		t.Fatalf("a cross-tenant route attempt changed the item to %s", unchanged.Status)
	}
}

// TestTodo_WORK_002_Mutation kills the mutants that would make "assignment
// evidence, not authority" unfalsifiable: a malformed assignment digest the
// database still refuses, and a route that reuses the assignment schema to
// smuggle in an owner reference the assignment did not actually resolve.
func TestTodo_WORK_002_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work002-mutation")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	req, scenario := promotionRequirement(t, humanwork.RequirementHRBP)
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
	if !workitem.ValidDigest(item.AssignmentDigest) {
		t.Fatalf("stored assignment digest %q is not well-formed", item.AssignmentDigest)
	}

	t.Run("a malformed assignment digest is refused by the database", func(t *testing.T) {
		if err := db.ExecErr(`UPDATE work_item SET assignment_digest = 'not-a-digest', item_version = item_version + 1
			WHERE tenant_id = $1 AND work_item_id = $2`, tenant, item.WorkItemID); err == nil {
			t.Fatal("the database accepted a malformed assignment digest")
		}
	})

	t.Run("an unrecognized re-resolution trigger is refused before any write", func(t *testing.T) {
		_, err := workitem.ResolveAssignment(req, scenario.Resolution, scenario.Directory, scenario.Clock, "NOT_A_TRIGGER")
		if code := workitem.CodeOf(err); code != workitem.CodeInvalidRecord {
			t.Fatalf("refusal code = %q, want %q (%v)", code, workitem.CodeInvalidRecord, err)
		}
	})

	t.Run("two different resolutions produce two different digests", func(t *testing.T) {
		committeeReq, committeeScenario := promotionRequirement(t, humanwork.RequirementExecutiveCommittee)
		other, err := workitem.ResolveAssignment(committeeReq, committeeScenario.Resolution, committeeScenario.Directory, committeeScenario.Clock, workitem.TriggerInitialRouting)
		if err != nil {
			t.Fatalf("ResolveAssignment: %v", err)
		}
		d1, err := assignment.Digest()
		if err != nil {
			t.Fatalf("Digest: %v", err)
		}
		d2, err := other.Digest()
		if err != nil {
			t.Fatalf("Digest: %v", err)
		}
		if d1 == d2 {
			t.Fatal("two different resolutions produced the same assignment digest")
		}
	})

	t.Run("a work item may not route to a candidate set that was never resolved", func(t *testing.T) {
		// Route is only ever driven by RouteFromAssignment's own derivation --
		// there is no parameter through which a caller can independently name an
		// owner_kind/owner_ref pair. This is asserted directly against the pure
		// function so a future refactor that added one would fail here first.
		ownerKind, ownerRef, status := workitem.RouteFromAssignment(assignment, in.PolicyRouteRef)
		if ownerKind != workitem.OwnerPrincipal || ownerRef != humanwork.PrincipalHRBP || status != workitem.StatusAssigned {
			t.Fatalf("RouteFromAssignment = %s/%s/%s, want PRINCIPAL/%s/ASSIGNED",
				ownerKind, ownerRef, status, humanwork.PrincipalHRBP)
		}
	})
}

// FuzzTodo_WORK_002 fuzzes [workitem.RouteFromAssignment] over arbitrary
// candidate-set shapes: it must never panic, must always name a non-blank
// owner reference (WORK-001's RED clause forbids a blank owner even under
// escalation), must always return a declared owner kind and status, and must
// route to exactly the status the candidate count implies.
func FuzzTodo_WORK_002(f *testing.F) {
	f.Add(0, false)
	f.Add(1, false)
	f.Add(2, true)
	f.Add(5, false)
	f.Fuzz(func(t *testing.T, n int, fallbackUsed bool) {
		if n < 0 || n > 200 {
			t.Skip()
		}
		candidates := make([]humanwork.Candidate, n)
		for i := range candidates {
			candidates[i] = humanwork.Candidate{
				PrincipalID: fmt.Sprintf("principal:fuzz-%d", i),
				Via:         humanwork.SourceDirect,
				TermRef:     "term.fuzz",
			}
		}
		outcome := humanwork.OutcomeResolved
		if n == 0 {
			outcome = humanwork.OutcomeNoAuthorizedApprover
		}
		res := humanwork.Resolution{
			RequirementID:    "req.fuzz/v1",
			Outcome:          outcome,
			Candidates:       candidates,
			FallbackUsed:     fallbackUsed,
			ExpressionDigest: "digest.fuzz",
		}
		a := workitem.Assignment{Resolution: res, Trigger: workitem.TriggerInitialRouting}

		ownerKind, ownerRef, status := workitem.RouteFromAssignment(a, "route.fuzz/v1")
		if ownerRef == "" {
			t.Fatalf("n=%d: owner ref is blank", n)
		}
		if !ownerKind.Valid() {
			t.Fatalf("n=%d: owner kind %q is not declared", n, ownerKind)
		}
		if !status.Valid() {
			t.Fatalf("n=%d: status %q is not declared", n, status)
		}
		switch {
		case n == 0:
			if status != workitem.StatusEscalated || ownerKind != workitem.OwnerPolicyRoute || ownerRef != "route.fuzz/v1" {
				t.Fatalf("n=0: routed to %s/%s/%s, want ESCALATED/POLICY_ROUTE/route.fuzz/v1", ownerKind, ownerRef, status)
			}
		case n == 1:
			if status != workitem.StatusAssigned || ownerKind != workitem.OwnerPrincipal {
				t.Fatalf("n=1: routed to %s/%s, want ASSIGNED/PRINCIPAL", ownerKind, status)
			}
		default:
			if status != workitem.StatusAvailable || ownerKind != workitem.OwnerCandidateSet {
				t.Fatalf("n=%d: routed to %s/%s, want AVAILABLE/CANDIDATE_SET", n, ownerKind, status)
			}
		}

		// Digest must be computable without panic or error for every shape.
		if _, err := a.Digest(); err != nil {
			t.Fatalf("n=%d: Digest: %v", n, err)
		}
	})
}
