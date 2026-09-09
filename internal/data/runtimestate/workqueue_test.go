package runtimestate_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
)

// This file is workqueue.go's own per-file suite: the four human-work stores
// migration 00026 backs -- queues and queue membership, claim history, SLA and
// approval requirement slots -- each proved on its happy path, on the identity
// the schema makes unrepeatable, and on the guard that refuses a wrong version
// or an illegal next state.
//
// Every one of them is caller-driven. Nothing here sweeps a deadline, expires a
// claim or dispatches a queue on a timer, which is the WF-RUN-000 gate holding:
// the state is durable, the scheduler that would read it is not written.

// queuedItem creates one real work item against the fixture's instance, so the
// foreign keys work_queue_item, work_item_claim and work_item_sla all declare
// have something to point at.
func (f schedulingFixture) workItem(t *testing.T, nodeID, owner string) workitem.WorkItem {
	t.Helper()
	return availableItem(t, context.Background(), workitem.Store{}, f.conn,
		f.tenant, f.instance.InstanceID, nodeID, owner)
}

func TestWorkQueue_AnItemWaitsInExactlyOneQueue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "wq-membership")
	store := runtimestate.QueueStore{}

	triage := runtimestate.Queue{
		TenantID: f.tenant, QueueID: uuid.New(), Key: "queue.triage",
		DisplayName: "Triage", RoutingPolicyRef: "route.triage/v1", CreatedAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return store.Create(ctx, tx, triage) })

	// A queue key is how routing names its destination, so two queues under one
	// key would make routing ambiguous.
	clash := triage
	clash.QueueID = uuid.New()
	clash.DisplayName = "Triage (copy)"
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Create(ctx, tx, clash) }),
		runtimestate.ErrDuplicate, "a second queue under one key")

	escalations := runtimestate.Queue{
		TenantID: f.tenant, QueueID: uuid.New(), Key: "queue.escalations",
		DisplayName: "Escalations", RoutingPolicyRef: "route.escalations/v1", CreatedAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return store.Create(ctx, tx, escalations) })

	item := f.workItem(t, "approval_node", "principal:reviewer-1")
	member := runtimestate.QueueItem{
		TenantID: f.tenant, WorkItemID: item.WorkItemID, QueueID: triage.QueueID,
		EligibleAt: fixedInstant, EnqueuedAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return store.Enqueue(ctx, tx, member) })

	// Membership is keyed on the work item alone: putting it in a second queue
	// is not an addition, it is a collision.
	second := member
	second.QueueID = escalations.QueueID
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Enqueue(ctx, tx, second) }),
		runtimestate.ErrDuplicate, "enqueuing one item into a second queue")

	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Move(ctx, tx, f.tenant, item.WorkItemID, escalations.QueueID, 6, fixedInstant)
	}), runtimestate.ErrVersionConflict, "moving at a stale membership version")

	f.do(t, func(tx dbport.Tx) error {
		return store.Move(ctx, tx, f.tenant, item.WorkItemID, escalations.QueueID, 1, fixedInstant)
	})

	var membership runtimestate.QueueItem
	f.do(t, func(tx dbport.Tx) error {
		var err error
		membership, err = store.LoadItem(ctx, tx, f.tenant, item.WorkItemID)
		return err
	})
	if membership.QueueID != escalations.QueueID || membership.Version != 2 {
		t.Fatalf("membership after the move = %+v, want the escalations queue at version 2", membership)
	}

	// QUEUED -> REMOVED is an edge; REMOVED is terminal.
	f.do(t, func(tx dbport.Tx) error {
		return store.SetMembership(ctx, tx, f.tenant, item.WorkItemID, 2, runtimestate.MembershipRemoved, fixedInstant)
	})
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.SetMembership(ctx, tx, f.tenant, item.WorkItemID, 3, runtimestate.MembershipQueued, fixedInstant)
	}), runtimestate.ErrIllegalTransition, "re-queuing a removed membership")

	// A retired queue is terminal too.
	f.do(t, func(tx dbport.Tx) error {
		return store.SetState(ctx, tx, f.tenant, triage.QueueID, 1, runtimestate.QueueRetired)
	})
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.SetState(ctx, tx, f.tenant, triage.QueueID, 2, runtimestate.QueueActive)
	}), runtimestate.ErrIllegalTransition, "reviving a retired queue")

	var missing error
	_ = inTenantTxErr(f.conn, f.tenant, func(tx dbport.Tx) error {
		_, missing = store.Load(ctx, tx, f.tenant, uuid.New())
		return nil
	})
	wantErrIs(t, missing, runtimestate.ErrNotFound, "loading a queue that does not exist")
}

func TestWorkQueue_ClaimHistoryHoldsAtMostOneOpenHold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "wq-claim")
	store := runtimestate.ClaimStore{}

	item := f.workItem(t, "approval_node", "principal:reviewer-1")
	hold := runtimestate.Claim{
		TenantID: f.tenant, ClaimID: uuid.New(), WorkItemID: item.WorkItemID,
		ClaimedBy: "principal:reviewer-1",
		ClaimedAt: fixedInstant, ExpiresAt: fixedInstant.Add(time.Hour),
	}
	f.do(t, func(tx dbport.Tx) error { return store.Open(ctx, tx, hold) })

	// Claim exclusivity written as history: the partial unique index admits one
	// open hold, so a second claimant collides rather than co-holding.
	rival := hold
	rival.ClaimID = uuid.New()
	rival.ClaimedBy = "principal:reviewer-2"
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Open(ctx, tx, rival) }),
		runtimestate.ErrDuplicate, "a second open hold on one item")

	var live runtimestate.Claim
	f.do(t, func(tx dbport.Tx) error {
		var err error
		live, err = store.OpenClaim(ctx, tx, f.tenant, item.WorkItemID)
		return err
	})
	if live.ClaimID != hold.ClaimID || live.ClaimedBy != "principal:reviewer-1" {
		t.Fatalf("open claim = %+v, want reviewer-1's hold %s", live, hold.ClaimID)
	}

	// A claim expiring before it is taken is refused before any statement runs.
	backwards := hold
	backwards.ClaimID = uuid.New()
	backwards.ExpiresAt = fixedInstant.Add(-time.Hour)
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Open(ctx, tx, backwards) }),
		runtimestate.ErrInvalid, "a hold expiring before it is taken")

	f.do(t, func(tx dbport.Tx) error {
		return store.Close(ctx, tx, f.tenant, hold.ClaimID, runtimestate.ClaimReleased, fixedInstant.Add(time.Minute))
	})
	// A closed hold is immutable: closing it again writes nothing.
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Close(ctx, tx, f.tenant, hold.ClaimID, runtimestate.ClaimCompleted, fixedInstant.Add(2*time.Minute))
	}), runtimestate.ErrIllegalTransition, "closing an already closed hold")

	// Closing freed the item, and the previous hold stayed as history.
	f.do(t, func(tx dbport.Tx) error { return store.Open(ctx, tx, rival) })
	f.do(t, func(tx dbport.Tx) error {
		var err error
		live, err = store.OpenClaim(ctx, tx, f.tenant, item.WorkItemID)
		return err
	})
	if live.ClaimID != rival.ClaimID {
		t.Fatalf("open claim after release = %s, want reviewer-2's hold %s", live.ClaimID, rival.ClaimID)
	}
	var holds int
	f.do(t, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM work_item_claim WHERE tenant_id = $1 AND work_item_id = $2`,
			f.tenant, item.WorkItemID).Scan(&holds)
	})
	if holds != 2 {
		t.Fatalf("claim history holds %d rows, want both the closed and the live hold", holds)
	}
}

func TestWorkQueue_SLABreachStatesAdvanceOnlyForward(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "wq-sla")
	store := runtimestate.SLAStore{}

	item := f.workItem(t, "approval_node", "principal:reviewer-1")
	sla := runtimestate.SLA{
		TenantID: f.tenant, WorkItemID: item.WorkItemID,
		PolicyRef: "sla.approval.standard/v1",
		TargetAt:  fixedInstant.Add(24 * time.Hour), EscalateAt: fixedInstant.Add(48 * time.Hour),
	}
	f.do(t, func(tx dbport.Tx) error { return store.Set(ctx, tx, sla) })

	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Set(ctx, tx, sla) }),
		runtimestate.ErrDuplicate, "setting an SLA on one item twice")

	// Escalation cannot come before the target it escalates past.
	backwards := sla
	backwards.WorkItemID = f.workItem(t, "approval_node_b", "principal:reviewer-1").WorkItemID
	backwards.EscalateAt = fixedInstant
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Set(ctx, tx, backwards) }),
		runtimestate.ErrInvalid, "an escalation instant before the target")

	// WITHIN_TARGET -> ESCALATED is not an edge; the breach comes first.
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Advance(ctx, tx, f.tenant, item.WorkItemID, 1, runtimestate.SLAEscalated,
			fixedInstant.Add(50*time.Hour), "principal:manager")
	}), runtimestate.ErrIllegalTransition, "escalating an SLA that never breached")

	f.do(t, func(tx dbport.Tx) error {
		return store.Advance(ctx, tx, f.tenant, item.WorkItemID, 1, runtimestate.SLABreached,
			fixedInstant.Add(25*time.Hour), "")
	})

	// An escalation names who it escalated to; the schema check says the same.
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Advance(ctx, tx, f.tenant, item.WorkItemID, 2, runtimestate.SLAEscalated,
			fixedInstant.Add(50*time.Hour), "")
	}), runtimestate.ErrInvalid, "escalating without naming a target")

	f.do(t, func(tx dbport.Tx) error {
		return store.Advance(ctx, tx, f.tenant, item.WorkItemID, 2, runtimestate.SLAEscalated,
			fixedInstant.Add(50*time.Hour), "principal:manager")
	})

	// Satisfaction keeps both the breach instant and whoever it was escalated
	// to, so the record of how the item finally got worked survives.
	f.do(t, func(tx dbport.Tx) error {
		return store.Advance(ctx, tx, f.tenant, item.WorkItemID, 3, runtimestate.SLASatisfied,
			fixedInstant.Add(60*time.Hour), "")
	})

	var settled runtimestate.SLA
	f.do(t, func(tx dbport.Tx) error {
		var err error
		settled, err = store.Load(ctx, tx, f.tenant, item.WorkItemID)
		return err
	})
	if settled.BreachState != runtimestate.SLASatisfied {
		t.Fatalf("settled SLA state = %q, want SATISFIED", settled.BreachState)
	}
	if settled.EscalatedTo != "principal:manager" || settled.BreachedAt.IsZero() {
		t.Fatalf("settled SLA = %+v, want the escalation target and breach instant preserved", settled)
	}
	if settled.Version != 4 {
		t.Fatalf("settled SLA version = %d, want 4 after three advances", settled.Version)
	}

	// SATISFIED is terminal.
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Advance(ctx, tx, f.tenant, item.WorkItemID, 4, runtimestate.SLABreached,
			fixedInstant.Add(70*time.Hour), "")
	}), runtimestate.ErrIllegalTransition, "re-breaching a satisfied SLA")
}

func TestWorkQueue_OneApprovalSlotProducesOneWorkItem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "wq-approval")
	store := runtimestate.ApprovalRequirementStore{}

	slot := runtimestate.ApprovalRequirement{
		TenantID: f.tenant, RequirementID: uuid.New(), InstanceID: f.instance.InstanceID,
		NodeID: "approval_node", SlotKey: "manager",
		RequirementRef: "req.promotion.manager/v1", SeparationConstraint: "sep.not_self/v1",
		MaterialityClass: "MATERIAL", CreatedAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return store.Declare(ctx, tx, slot) })

	// The RED clause's duplicate approval slot: one slot produces one work
	// item, however many times an advancement replays.
	replay := slot
	replay.RequirementID = uuid.New()
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Declare(ctx, tx, replay) }),
		runtimestate.ErrDuplicate, "declaring one approval slot twice")

	loose := slot
	loose.RequirementID = uuid.New()
	loose.SlotKey = "hr"
	loose.MaterialityClass = "PROBABLY"
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Declare(ctx, tx, loose) }),
		runtimestate.ErrInvalid, "an undeclared materiality class")

	// A DECLARED slot cannot be satisfied: it has to be routed to an item
	// first, and the item is what carries the decision.
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Settle(ctx, tx, f.tenant, slot.RequirementID, 1, runtimestate.RequirementSatisfied, fixedInstant)
	}), runtimestate.ErrIllegalTransition, "satisfying a slot nothing was routed to")

	item := f.workItem(t, "approval_node", "principal:reviewer-1")
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Route(ctx, tx, f.tenant, slot.RequirementID, item.WorkItemID, 8)
	}), runtimestate.ErrVersionConflict, "routing at a stale version")

	f.do(t, func(tx dbport.Tx) error {
		return store.Route(ctx, tx, f.tenant, slot.RequirementID, item.WorkItemID, 1)
	})
	f.do(t, func(tx dbport.Tx) error {
		return store.Settle(ctx, tx, f.tenant, slot.RequirementID, 2, runtimestate.RequirementSatisfied, fixedInstant)
	})

	var settled runtimestate.ApprovalRequirement
	f.do(t, func(tx dbport.Tx) error {
		var err error
		settled, err = store.Load(ctx, tx, f.tenant, slot.RequirementID)
		return err
	})
	if settled.State != runtimestate.RequirementSatisfied || settled.WorkItemID != item.WorkItemID {
		t.Fatalf("settled slot = %+v, want SATISFIED naming %s", settled, item.WorkItemID)
	}
	if settled.SatisfiedAt.IsZero() || settled.Version != 3 {
		t.Fatalf("settled slot = %+v, want a satisfaction instant at version 3", settled)
	}

	// SATISFIED is terminal: a slot is not reopened, it is re-declared.
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Settle(ctx, tx, f.tenant, slot.RequirementID, 3, runtimestate.RequirementCancelled, fixedInstant)
	}), runtimestate.ErrIllegalTransition, "cancelling a satisfied slot")
}
