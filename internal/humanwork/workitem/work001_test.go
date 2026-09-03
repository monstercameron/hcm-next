package workitem_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
)

// WORK-001's RED clause names four defects: an illegal transition, a mutable
// completed output, a record missing correlation/subject/owner/visibility/
// deadline, and a generic status. The tests below are that matrix, walking
// the whole lifecycle diagram doc.go draws and proving every write along it
// appends its own transition row.

// TestTodo_WORK_001 is the PRIMARY case: create -> route -> claimed ->
// in-progress -> completed, with a transition row appended at every step, and
// a routed item that walks to available/escalated/returned/expired/cancelled
// on the other branches.
func TestTodo_WORK_001(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work001-primary")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

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
	if created.ItemVersion != 1 {
		t.Fatalf("a new item is at version %d, want 1", created.ItemVersion)
	}
	if created.Status != workitem.StatusCreated {
		t.Fatalf("a new item is %s, want CREATED", created.Status)
	}
	if created.OwnerKind != workitem.OwnerPolicyRoute || created.OwnerRef != in.PolicyRouteRef {
		t.Fatalf("a new item's owner = %s/%s, want POLICY_ROUTE/%s", created.OwnerKind, created.OwnerRef, in.PolicyRouteRef)
	}

	// Route to a single resolved candidate: ASSIGNED.
	assignment := workitem.Assignment{
		Resolution: singleCandidateResolution("principal:manager-1"),
		Trigger:    workitem.TriggerInitialRouting,
	}
	var routed workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		routed, err = store.Route(ctx, tx, tenant, created.WorkItemID, created.ItemVersion, assignment, meta("workitem.routed"))
		return err
	})
	if routed.Status != workitem.StatusAssigned {
		t.Fatalf("routed status = %s, want ASSIGNED", routed.Status)
	}
	if routed.OwnerKind != workitem.OwnerPrincipal || routed.OwnerRef != "principal:manager-1" {
		t.Fatalf("routed owner = %s/%s, want PRINCIPAL/principal:manager-1", routed.OwnerKind, routed.OwnerRef)
	}
	if routed.AssignmentDigest == "" || !workitem.ValidDigest(routed.AssignmentDigest) {
		t.Errorf("routed assignment digest = %q, want a well-formed digest", routed.AssignmentDigest)
	}
	if !routed.Assignment.IsCandidate("principal:manager-1") {
		t.Errorf("the chosen owner is not recorded as a candidate of its own assignment")
	}

	// Claim.
	var claimed workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		claimed, err = store.Claim(ctx, tx, workitem.ClaimInput{
			TenantID: tenant, WorkItemID: routed.WorkItemID, ExpectedVersion: routed.ItemVersion,
			ClaimantPrincipalID: "principal:manager-1",
			ClaimExpiresAt:      fixedInstant.Add(2 * time.Hour),
			Now:                 fixedInstant,
			Meta:                meta("workitem.claimed"),
		})
		return err
	})
	if claimed.Status != workitem.StatusClaimed || claimed.ClaimedBy != "principal:manager-1" {
		t.Fatalf("claimed = %s/%s, want CLAIMED/principal:manager-1", claimed.Status, claimed.ClaimedBy)
	}

	// Start.
	var started workitem.WorkItem
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		started, err = store.Start(ctx, tx, tenant, claimed.WorkItemID, claimed.ItemVersion, fixedInstant, meta("workitem.started"))
		return err
	})
	if started.Status != workitem.StatusInProgress {
		t.Fatalf("started status = %s, want IN_PROGRESS", started.Status)
	}

	// Complete.
	var completed workitem.WorkItem
	digest := "sha256:" + strings.Repeat("a", 64)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		completed, err = store.Complete(ctx, tx, workitem.CompleteInput{
			TenantID: tenant, WorkItemID: started.WorkItemID, ExpectedVersion: started.ItemVersion,
			CompletedBy: "principal:manager-1", CompletedOutputDigest: digest, Now: fixedInstant,
			Meta: meta("workitem.completed"),
		})
		return err
	})
	if completed.Status != workitem.StatusCompleted || completed.CompletedOutputDigest != digest {
		t.Fatalf("completed = %s/%s, want COMPLETED/%s", completed.Status, completed.CompletedOutputDigest, digest)
	}
	if completed.ClaimID != nil {
		t.Errorf("a completed item still carries a claim")
	}

	// Every write above appended its own transition row: CREATED, ASSIGNED,
	// CLAIMED, IN_PROGRESS, COMPLETED -- five rows, item_version 1 through 5,
	// each with a distinct to_status.
	var trail []workitem.TransitionRecord
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		trail, err = store.LoadTransitions(ctx, tx, tenant, created.WorkItemID)
		return err
	})
	wantStatuses := []workitem.Status{
		workitem.StatusCreated, workitem.StatusRouted, workitem.StatusAssigned, workitem.StatusClaimed,
		workitem.StatusInProgress, workitem.StatusCompleted,
	}
	if len(trail) != len(wantStatuses) {
		t.Fatalf("%d transition rows recorded, want %d: %+v", len(trail), len(wantStatuses), trail)
	}
	for i, tr := range trail {
		if tr.ItemVersion != int64(i+1) {
			t.Errorf("transition %d has item_version %d, want %d", i, tr.ItemVersion, i+1)
		}
		if tr.ToStatus != wantStatuses[i] {
			t.Errorf("transition %d moved to %s, want %s", i, tr.ToStatus, wantStatuses[i])
		}
	}
	if trail[0].FromStatus != "" {
		t.Errorf("the creation row names a predecessor: %q", trail[0].FromStatus)
	}

	// The other branch: ROUTED -> AVAILABLE (multiple candidates) -> CLAIMED ->
	// RETURNED -> ROUTED -> ESCALATED -> CANCELLED, and a fresh item that goes
	// straight to EXPIRED.
	t.Run("available, returned, escalated and cancelled all append evidence", func(t *testing.T) {
		in2, err := workitem.NewWorkItem(newTaskInput(tenant, instance))
		if err != nil {
			t.Fatalf("NewWorkItem: %v", err)
		}
		var item workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Create(ctx, tx, in2, meta(workitem.ReasonCreated))
			return err
		})
		multi := workitem.Assignment{
			Resolution: multiCandidateResolution("principal:a", "principal:b"),
			Trigger:    workitem.TriggerInitialRouting,
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, multi, meta("workitem.routed"))
			return err
		})
		if item.Status != workitem.StatusAvailable || item.OwnerKind != workitem.OwnerCandidateSet {
			t.Fatalf("multi-candidate route = %s/%s, want AVAILABLE/CANDIDATE_SET", item.Status, item.OwnerKind)
		}

		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:a", ClaimExpiresAt: fixedInstant.Add(time.Hour),
				Now: fixedInstant, Meta: meta("workitem.claimed"),
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
			item, err = store.Return(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, fixedInstant, meta("workitem.returned"))
			return err
		})
		if item.Status != workitem.StatusReturned || item.ClaimID != nil {
			t.Fatalf("returned item = %s claim=%v, want RETURNED with no claim", item.Status, item.ClaimID)
		}
		// RETURNED re-routes through ROUTED evidence via a fresh Route call.
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
				workitem.Assignment{Resolution: noAuthorizedApproverResolution(), Trigger: workitem.TriggerReturnedReroute},
				meta("workitem.rerouted"))
			return err
		})
		if item.Status != workitem.StatusEscalated {
			t.Fatalf("re-route with no candidates = %s, want ESCALATED", item.Status)
		}
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Cancel(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, meta("workitem.cancelled"))
			return err
		})
		if item.Status != workitem.StatusCancelled {
			t.Fatalf("cancelled item = %s, want CANCELLED", item.Status)
		}

		var trail2 []workitem.TransitionRecord
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			trail2, err = store.LoadTransitions(ctx, tx, tenant, item.WorkItemID)
			return err
		})
		want := []workitem.Status{
			workitem.StatusCreated, workitem.StatusRouted, workitem.StatusAvailable, workitem.StatusClaimed,
			workitem.StatusInProgress, workitem.StatusReturned, workitem.StatusRouted, workitem.StatusEscalated,
			workitem.StatusCancelled,
		}
		if len(trail2) != len(want) {
			t.Fatalf("%d transitions recorded, want %d: %+v", len(trail2), len(want), trail2)
		}
	})

	t.Run("a fresh item may be routed straight to EXPIRED", func(t *testing.T) {
		in3, err := workitem.NewWorkItem(newTaskInput(tenant, instance))
		if err != nil {
			t.Fatalf("NewWorkItem: %v", err)
		}
		var item workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Create(ctx, tx, in3, meta(workitem.ReasonCreated))
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Route(ctx, tx, tenant, item.WorkItemID, item.ItemVersion,
				workitem.Assignment{Resolution: singleCandidateResolution("principal:x"), Trigger: workitem.TriggerInitialRouting},
				meta("workitem.routed"))
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Expire(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, meta("workitem.expired"))
			return err
		})
		if item.Status != workitem.StatusExpired {
			t.Fatalf("expired item = %s, want EXPIRED", item.Status)
		}
		if !item.Status.Terminal() {
			t.Errorf("EXPIRED is not terminal")
		}
	})
}

// TestTodo_WORK_001_Race drives several genuinely concurrent writers against
// the same item version, on separate connections and separate transactions.
// Exactly one may win: there is no lease here, only the item_version
// compare-and-set.
func TestTodo_WORK_001_Race(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work001-race")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}

	in, err := workitem.NewWorkItem(newTaskInput(tenant, instance))
	if err != nil {
		t.Fatalf("NewWorkItem: %v", err)
	}
	setup := appConn(t, db)
	var item workitem.WorkItem
	inTenantTx(t, setup, tenant, func(tx dbport.Tx) error {
		var err error
		item, err = store.Create(ctx, tx, in, meta(workitem.ReasonCreated))
		return err
	})

	const writers = 6
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
				updated, err := store.Escalate(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, meta("workitem.escalated"))
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
			if r.version != 2 {
				t.Errorf("writer %d won with version %d, want 2", i, r.version)
			}
		case workitem.CodeOf(r.err) == workitem.CodeStaleItem:
			// The expected loss.
		default:
			t.Errorf("writer %d failed for an unexpected reason: %v", i, r.err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d of %d writers committed from the same item version; exactly one may", winners, writers)
	}
}

// TestTodo_WORK_001_Security proves the tenant boundary is physical: another
// tenant's transaction cannot read, write or even learn that a work item
// exists, and an unscoped transaction sees nothing.
func TestTodo_WORK_001_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	alice := insertTenant(t, db, "work001-security-a")
	bob := insertTenant(t, db, "work001-security-b")
	instance := uuid.New()
	insertInstance(t, db, alice, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	in, err := workitem.NewWorkItem(newTaskInput(alice, instance))
	if err != nil {
		t.Fatalf("NewWorkItem: %v", err)
	}
	var item workitem.WorkItem
	inTenantTx(t, conn, alice, func(tx dbport.Tx) error {
		var err error
		item, err = store.Create(ctx, tx, in, meta(workitem.ReasonCreated))
		return err
	})

	t.Run("another tenant cannot read the item", func(t *testing.T) {
		err := inTenantTxErr(conn, bob, func(tx dbport.Tx) error {
			_, txErr := store.Load(ctx, tx, alice, item.WorkItemID)
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeWorkItemNotFound {
			t.Fatalf("cross-tenant read code = %q, want %q (%v)", code, workitem.CodeWorkItemNotFound, err)
		}
	})

	t.Run("an unscoped transaction sees nothing", func(t *testing.T) {
		err := inTxErr(conn, func(tx dbport.Tx) error {
			_, txErr := store.Load(ctx, tx, alice, item.WorkItemID)
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeWorkItemNotFound {
			t.Fatalf("unscoped read code = %q, want %q (%v)", code, workitem.CodeWorkItemNotFound, err)
		}
	})

	t.Run("another tenant cannot escalate the item either", func(t *testing.T) {
		err := inTenantTxErr(conn, bob, func(tx dbport.Tx) error {
			_, txErr := store.Escalate(ctx, tx, alice, item.WorkItemID, item.ItemVersion, meta("workitem.escalated"))
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeWorkItemNotFound {
			t.Fatalf("cross-tenant write code = %q, want %q (%v)", code, workitem.CodeWorkItemNotFound, err)
		}
	})

	t.Run("another tenant's transitions are invisible", func(t *testing.T) {
		var seen []workitem.TransitionRecord
		inTenantTx(t, conn, bob, func(tx dbport.Tx) error {
			var err error
			seen, err = store.LoadTransitions(ctx, tx, alice, item.WorkItemID)
			return err
		})
		if len(seen) != 0 {
			t.Fatalf("another tenant read %d transition rows", len(seen))
		}
	})
}

// TestTodo_WORK_001_Mutation kills the mutants that would make "legal state,
// immutable evidence" unfalsifiable: a generic status, a record missing what
// WORK-001 names, an illegal transition, and a completed output digest
// rewritten underneath this package's own store.
func TestTodo_WORK_001_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "work001-mutation")
	instance := uuid.New()
	insertInstance(t, db, tenant, instance)
	store := workitem.Store{}
	conn := appConn(t, db)

	t.Run("a record missing what WORK-001 names is refused", func(t *testing.T) {
		cases := []struct {
			name   string
			mutate func(in *workitem.NewWorkItemInput)
		}{
			{"no correlation id", func(in *workitem.NewWorkItemInput) { in.CorrelationID = "" }},
			{"no subject refs", func(in *workitem.NewWorkItemInput) { in.SubjectRefs = nil }},
			{"no policy route (owner)", func(in *workitem.NewWorkItemInput) { in.PolicyRouteRef = "" }},
			{"no visibility", func(in *workitem.NewWorkItemInput) { in.Visibility = "" }},
			{"no deadline", func(in *workitem.NewWorkItemInput) { in.DeadlineAt = time.Time{} }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				broken := newTaskInput(tenant, instance)
				tc.mutate(&broken)
				_, err := workitem.NewWorkItem(broken)
				if code := workitem.CodeOf(err); code != workitem.CodeInvalidRecord {
					t.Fatalf("refusal code = %q, want %q (%v)", code, workitem.CodeInvalidRecord, err)
				}
			})
		}
	})

	t.Run("a generic status is refused", func(t *testing.T) {
		in, err := workitem.NewWorkItem(newTaskInput(tenant, instance))
		if err != nil {
			t.Fatalf("NewWorkItem: %v", err)
		}
		in.Status = "OPEN"
		if err := in.Validate(); workitem.CodeOf(err) != workitem.CodeInvalidRecord {
			t.Fatalf("a generic status validated cleanly: %v", err)
		}
	})

	t.Run("an illegal transition is refused and mutates nothing", func(t *testing.T) {
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
		// CREATED may not jump straight to CLAIMED.
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, txErr := store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:x", ClaimExpiresAt: fixedInstant.Add(time.Hour),
				Now: fixedInstant, Meta: meta("workitem.claimed"),
			})
			return txErr
		})
		if code := workitem.CodeOf(err); code != workitem.CodeIllegalTransition {
			t.Fatalf("refusal code = %q, want %q (%v)", code, workitem.CodeIllegalTransition, err)
		}
		var unchanged workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			unchanged, err = store.Load(ctx, tx, tenant, item.WorkItemID)
			return err
		})
		if unchanged.ItemVersion != item.ItemVersion || unchanged.Status != workitem.StatusCreated {
			t.Errorf("a refused illegal transition changed the item to %s/%d", unchanged.Status, unchanged.ItemVersion)
		}
	})

	t.Run("a rolled back transaction persists neither the item change nor a transition", func(t *testing.T) {
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
		wantErr := errors.New("caller aborted after recording")
		err = inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			if _, txErr := store.Escalate(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, meta("workitem.escalated")); txErr != nil {
				return txErr
			}
			return wantErr
		})
		if !errors.Is(err, wantErr) {
			t.Fatalf("transaction error = %v, want the caller's own", err)
		}
		var after workitem.WorkItem
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			after, err = store.Load(ctx, tx, tenant, item.WorkItemID)
			return err
		})
		if after.ItemVersion != item.ItemVersion || after.Status != workitem.StatusCreated {
			t.Errorf("a rolled-back write still moved the item to %s/%d", after.Status, after.ItemVersion)
		}
	})

	t.Run("the completed output digest is immutable even against a raw UPDATE", func(t *testing.T) {
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
				workitem.Assignment{Resolution: singleCandidateResolution("principal:x"), Trigger: workitem.TriggerInitialRouting},
				meta("workitem.routed"))
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Claim(ctx, tx, workitem.ClaimInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				ClaimantPrincipalID: "principal:x", ClaimExpiresAt: fixedInstant.Add(time.Hour),
				Now: fixedInstant, Meta: meta("workitem.claimed"),
			})
			return err
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Start(ctx, tx, tenant, item.WorkItemID, item.ItemVersion, fixedInstant, meta("workitem.started"))
			return err
		})
		digest := "sha256:" + strings.Repeat("b", 64)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			var err error
			item, err = store.Complete(ctx, tx, workitem.CompleteInput{
				TenantID: tenant, WorkItemID: item.WorkItemID, ExpectedVersion: item.ItemVersion,
				CompletedBy: "principal:x", CompletedOutputDigest: digest, Now: fixedInstant,
				Meta: meta("workitem.completed"),
			})
			return err
		})

		other := "sha256:" + strings.Repeat("c", 64)
		if err := db.ExecErr(`UPDATE work_item SET completed_output_digest = $1, item_version = item_version + 1
			WHERE tenant_id = $2 AND work_item_id = $3`, other, tenant, item.WorkItemID); err == nil {
			t.Fatal("the database accepted a rewrite of a set completed output digest")
		}
		if err := db.ExecErr(`UPDATE work_item SET work_item_id = $1
			WHERE tenant_id = $2 AND work_item_id = $3`, uuid.New(), tenant, item.WorkItemID); err == nil {
			t.Fatal("the database accepted an identity rewrite")
		}
	})

	t.Run("the declared status set matches the migration", func(t *testing.T) {
		def := constraintDef(t, db, "work_item_status_allowed")
		for _, s := range workitem.Statuses() {
			if !strings.Contains(def, "'"+string(s)+"'") {
				t.Errorf("status %q is declared in Go but not in the migration CHECK", s)
			}
		}
		if got := strings.Count(def, "'"); got != 2*len(workitem.Statuses()) {
			t.Errorf("the migration CHECK names %d literals, Go declares %d statuses", got/2, len(workitem.Statuses()))
		}
	})
}

func constraintDef(t *testing.T, db *pgtest.DB, name string) string {
	t.Helper()
	var def string
	err := db.QueryRow(context.Background(), `
		SELECT pg_get_constraintdef(c.oid)
		FROM pg_constraint c
		JOIN pg_namespace n ON n.oid = c.connamespace
		WHERE c.conname = $1 AND n.nspname = current_schema()`, name).Scan(&def)
	if err != nil {
		t.Fatalf("read constraint %s: %v", name, err)
	}
	return def
}
