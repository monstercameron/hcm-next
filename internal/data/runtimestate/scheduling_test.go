package runtimestate_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/runtimestate"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// This file is scheduling.go's own per-file suite. db012_test.go proves the
// package's cross-store composition; what is proved here is narrower and
// per-store: for each of the eleven stores migration 00026 backs, that its
// happy path round-trips, that the identity the schema declares makes a repeat
// a typed [runtimestate.ErrDuplicate] rather than a second row, and that its
// compare-and-swap or lifecycle guard refuses the wrong version or the illegal
// next state without writing.
//
// Every store is caller-driven: the tests supply every instant. That is the
// WF-RUN-000 gate holding -- there is no clock, ticker or background claim in
// this package for a test to have to wait on.

// schedulingFixture is one tenant, one persisted instance and an app-role
// connection: everything migration 00026's foreign keys require before any
// scheduling row can exist.
type schedulingFixture struct {
	tenant   uuid.UUID
	conn     *pgxadapter.Conn
	instance runtime.Instance
}

func newSchedulingFixture(t *testing.T, db *pgtest.DB, key string) schedulingFixture {
	t.Helper()
	tenant := insertTenant(t, db, key)
	conn := appConn(t, db)
	inst := createInstance(t, context.Background(), conn, tenant, referencePlan(t))
	return schedulingFixture{tenant: tenant, conn: conn, instance: inst}
}

// do runs one store call in its own committed tenant transaction and fails the
// test if it returns an error.
func (f schedulingFixture) do(t *testing.T, fn func(tx dbport.Tx) error) {
	t.Helper()
	inTenantTx(t, f.conn, f.tenant, fn)
}

// try runs one store call in its own tenant transaction and hands back the
// error, rolling the transaction back so a refused write leaves nothing.
func (f schedulingFixture) try(fn func(tx dbport.Tx) error) error {
	return inTenantTxErr(f.conn, f.tenant, fn)
}

func wantErrIs(t *testing.T, err, target error, what string) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("%s: err = %v, want %v", what, err, target)
	}
}

func TestScheduling_FrontierEntriesAreASetWithADeterministicOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "sched-frontier")
	store := runtimestate.FrontierStore{}

	entry := runtimestate.FrontierEntry{
		TenantID: f.tenant, InstanceID: f.instance.InstanceID, NodeID: "node.alpha",
		State: runtimestate.FrontierReady, Sequence: 1, AdmittedAtVersion: 1,
		EnteredAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return store.Enter(ctx, tx, entry) })

	// Same node again: the frontier is a set.
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Enter(ctx, tx, entry) }),
		runtimestate.ErrDuplicate, "re-admitting a node already on the frontier")

	// A different node reusing sequence 1: an unordered frontier would make a
	// replay non-deterministic, so the sequence is unique too.
	clash := entry
	clash.NodeID = "node.beta"
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Enter(ctx, tx, clash) }),
		runtimestate.ErrDuplicate, "a second entry taking sequence 1")

	second := entry
	second.NodeID = "node.beta"
	second.Sequence = 2
	f.do(t, func(tx dbport.Tx) error { return store.Enter(ctx, tx, second) })

	var open []runtimestate.FrontierEntry
	f.do(t, func(tx dbport.Tx) error {
		var err error
		open, err = store.Open(ctx, tx, f.tenant, f.instance.InstanceID)
		return err
	})
	if len(open) != 2 || open[0].NodeID != "node.alpha" || open[1].NodeID != "node.beta" {
		t.Fatalf("open frontier = %+v, want alpha then beta in sequence order", open)
	}

	// Leave is compare-and-swap: the wrong version writes nothing.
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Leave(ctx, tx, f.tenant, f.instance.InstanceID, "node.alpha", 99, fixedInstant)
	}), runtimestate.ErrVersionConflict, "leaving at a stale entry version")

	f.do(t, func(tx dbport.Tx) error {
		return store.Leave(ctx, tx, f.tenant, f.instance.InstanceID, "node.alpha", 1, fixedInstant)
	})
	f.do(t, func(tx dbport.Tx) error {
		var err error
		open, err = store.Open(ctx, tx, f.tenant, f.instance.InstanceID)
		return err
	})
	if len(open) != 1 || open[0].NodeID != "node.beta" {
		t.Fatalf("after alpha left, open frontier = %+v, want only beta", open)
	}
}

func TestScheduling_NodeOutputIsTypedAndWrittenOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "sched-output")
	store := runtimestate.OutputStore{}

	plan := referencePlan(t)
	execID := runtime.NodeExecutionID(f.tenant, f.instance.InstanceID, plan.StartNodeID, 1)
	out := runtimestate.NodeOutput{
		TenantID: f.tenant, NodeExecutionID: execID, InstanceID: f.instance.InstanceID,
		Kind: runtimestate.OutputProposal, SchemaRef: "schema.promotion.proposal/v1",
		ArtifactRef: "artifact://proposal/1", OutputDigest: repeatHex("a"),
		ProducedAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return store.Record(ctx, tx, out) })

	var loaded runtimestate.NodeOutput
	f.do(t, func(tx dbport.Tx) error {
		var err error
		loaded, err = store.Load(ctx, tx, f.tenant, execID)
		return err
	})
	if loaded.Kind != runtimestate.OutputProposal || loaded.OutputDigest != repeatHex("a") {
		t.Fatalf("loaded output = %+v, want the recorded PROPOSAL and its digest", loaded)
	}

	// A completed node's result is a fact: a second output for the same node
	// execution is a duplicate, never an overwrite.
	rewrite := out
	rewrite.OutputDigest = repeatHex("b")
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Record(ctx, tx, rewrite) }),
		runtimestate.ErrDuplicate, "recording a second output for one node execution")

	// The type is closed and the digest shape is checked before any statement.
	bad := out
	bad.Kind = "GUESS"
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Record(ctx, tx, bad) }),
		runtimestate.ErrInvalid, "an undeclared output kind")
	short := out
	short.OutputDigest = "abc"
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Record(ctx, tx, short) }),
		runtimestate.ErrInvalid, "a digest that is not 64 hex characters")

	var missing error
	_ = inTenantTxErr(f.conn, f.tenant, func(tx dbport.Tx) error {
		_, missing = store.Load(ctx, tx, f.tenant, uuid.New())
		return nil
	})
	wantErrIs(t, missing, runtimestate.ErrNotFound, "loading an output that does not exist")
}

func TestScheduling_ReadyWorkIsOneUnitPerAttempt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "sched-ready")
	store := runtimestate.ReadyWorkStore{}

	unit := runtimestate.ReadyWork{
		TenantID: f.tenant, ReadyWorkID: uuid.New(), InstanceID: f.instance.InstanceID,
		NodeID: "node.alpha", Attempt: 1,
		EligibleAt: fixedInstant, EnqueuedAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return store.Enqueue(ctx, tx, unit) })

	// A replayed advancement produces one unit of work, not two claims on one.
	again := unit
	again.ReadyWorkID = uuid.New()
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Enqueue(ctx, tx, again) }),
		runtimestate.ErrDuplicate, "enqueuing the same node attempt twice")

	var stored runtimestate.ReadyWork
	f.do(t, func(tx dbport.Tx) error {
		var err error
		stored, err = store.Load(ctx, tx, f.tenant, unit.ReadyWorkID)
		return err
	})
	if stored.State != runtimestate.ReadyReady || stored.Priority != 100 || stored.Version != 1 {
		t.Fatalf("enqueued unit = %+v, want READY at the default priority and version 1", stored)
	}

	// READY -> DONE is not an edge; READY -> DISPATCHED -> DONE is.
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Transition(ctx, tx, f.tenant, unit.ReadyWorkID, 1, runtimestate.ReadyDone, fixedInstant)
	}), runtimestate.ErrIllegalTransition, "READY straight to DONE")

	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Transition(ctx, tx, f.tenant, unit.ReadyWorkID, 7, runtimestate.ReadyDispatched, fixedInstant)
	}), runtimestate.ErrVersionConflict, "dispatching at a stale version")

	f.do(t, func(tx dbport.Tx) error {
		return store.Transition(ctx, tx, f.tenant, unit.ReadyWorkID, 1, runtimestate.ReadyDispatched, fixedInstant)
	})
	f.do(t, func(tx dbport.Tx) error {
		return store.Transition(ctx, tx, f.tenant, unit.ReadyWorkID, 2, runtimestate.ReadyDone, fixedInstant)
	})
	f.do(t, func(tx dbport.Tx) error {
		var err error
		stored, err = store.Load(ctx, tx, f.tenant, unit.ReadyWorkID)
		return err
	})
	if stored.State != runtimestate.ReadyDone || stored.CompletedAt.IsZero() {
		t.Fatalf("settled unit = %+v, want DONE carrying a completion instant", stored)
	}
}

func TestScheduling_VariablesAreCompareAndSwapFenced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "sched-variable")
	store := runtimestate.VariableStore{}

	v := runtimestate.Variable{
		TenantID: f.tenant, InstanceID: f.instance.InstanceID, Name: "proposal",
		SchemaRef: "schema.promotion.proposal/v1", Value: json.RawMessage(`{"grade":"L5"}`),
		WrittenAtRevision: 1, WrittenAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return store.Put(ctx, tx, v, 0) })

	// A second first-write is a duplicate, not a silent overwrite.
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Put(ctx, tx, v, 0) }),
		runtimestate.ErrDuplicate, "a second insert of one variable")

	// A value that is not a JSON object never reaches the statement.
	scalar := v
	scalar.Value = json.RawMessage(`"L5"`)
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Put(ctx, tx, scalar, 0) }),
		runtimestate.ErrInvalid, "a scalar variable value")

	next := v
	next.Value = json.RawMessage(`{"grade":"L6"}`)
	next.WrittenAtRevision = 2
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Put(ctx, tx, next, 9) }),
		runtimestate.ErrVersionConflict, "overwriting at a stale version")

	f.do(t, func(tx dbport.Tx) error { return store.Put(ctx, tx, next, 1) })

	var got runtimestate.Variable
	f.do(t, func(tx dbport.Tx) error {
		var err error
		got, err = store.Get(ctx, tx, f.tenant, f.instance.InstanceID, "proposal")
		return err
	})
	if string(got.Value) != `{"grade": "L6"}` && string(got.Value) != `{"grade":"L6"}` {
		t.Fatalf("variable value = %s, want the L6 overwrite", got.Value)
	}
	if got.Version != 2 || got.WrittenAtRevision != 2 {
		t.Fatalf("variable = %+v, want version 2 at revision 2", got)
	}
}

func TestScheduling_TimersArePromisesSettledOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "sched-timer")
	store := runtimestate.TimerStore{}

	timer := runtimestate.Timer{
		TenantID: f.tenant, TimerID: uuid.New(), InstanceID: f.instance.InstanceID,
		NodeID: "node.wait", Key: "escalation",
		Kind: runtimestate.TimerDeadline, FiresAt: fixedInstant.Add(time.Hour), CreatedAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return store.Set(ctx, tx, timer) })

	// Setting the same (instance, node, key) twice is one wakeup, not two.
	dup := timer
	dup.TimerID = uuid.New()
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Set(ctx, tx, dup) }),
		runtimestate.ErrDuplicate, "setting one timer key twice")

	unknown := timer
	unknown.TimerID = uuid.New()
	unknown.Key = "other"
	unknown.Kind = "WHENEVER"
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Set(ctx, tx, unknown) }),
		runtimestate.ErrInvalid, "an undeclared timer kind")

	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Fire(ctx, tx, f.tenant, timer.TimerID, 4, fixedInstant.Add(time.Hour))
	}), runtimestate.ErrVersionConflict, "firing at a stale version")

	f.do(t, func(tx dbport.Tx) error {
		return store.Fire(ctx, tx, f.tenant, timer.TimerID, 1, fixedInstant.Add(time.Hour))
	})

	// Firing twice would wake the node twice, and cancelling a fired timer
	// would unmake a wakeup that already happened.
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Fire(ctx, tx, f.tenant, timer.TimerID, 2, fixedInstant.Add(2*time.Hour))
	}), runtimestate.ErrIllegalTransition, "firing an already fired timer")
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Cancel(ctx, tx, f.tenant, timer.TimerID, 2, fixedInstant.Add(2*time.Hour))
	}), runtimestate.ErrIllegalTransition, "cancelling an already fired timer")

	var loaded runtimestate.Timer
	f.do(t, func(tx dbport.Tx) error {
		var err error
		loaded, err = store.Load(ctx, tx, f.tenant, timer.TimerID)
		return err
	})
	if loaded.State != runtimestate.TimerFired || loaded.Version != 2 {
		t.Fatalf("settled timer = %+v, want FIRED at version 2", loaded)
	}
}

func TestScheduling_SignalDeliveryIsDedupedAndAppliedExactlyOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "sched-signal")
	store := runtimestate.SignalStore{}

	sub := runtimestate.Subscription{
		TenantID: f.tenant, SubscriptionID: uuid.New(), InstanceID: f.instance.InstanceID,
		NodeID: "node.await", SignalName: "promotion.approved", CorrelationKey: "worker:jane",
		CreatedAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return store.Subscribe(ctx, tx, sub) })

	dupSub := sub
	dupSub.SubscriptionID = uuid.New()
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Subscribe(ctx, tx, dupSub) }),
		runtimestate.ErrDuplicate, "one wait subscribed twice")

	sig := runtimestate.Signal{
		TenantID: f.tenant, SignalID: uuid.New(),
		SignalName: "promotion.approved", CorrelationKey: "worker:jane", DedupeToken: "delivery-1",
		SchemaRef: "schema.signal.promotion/v1", Payload: json.RawMessage(`{"decision":"APPROVE"}`),
		PayloadDigest: repeatHex("c"), DeliveredAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return store.Deliver(ctx, tx, sig) })

	// An at-least-once transport redelivers; the dedupe token makes that safe.
	redelivery := sig
	redelivery.SignalID = uuid.New()
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return store.Deliver(ctx, tx, redelivery) }),
		runtimestate.ErrDuplicate, "redelivering the same dedupe token")

	f.do(t, func(tx dbport.Tx) error {
		return store.Apply(ctx, tx, f.tenant, sig.SignalID, sub.SubscriptionID, f.instance.InstanceID, 1, fixedInstant)
	})
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Apply(ctx, tx, f.tenant, sig.SignalID, sub.SubscriptionID, f.instance.InstanceID, 2, fixedInstant)
	}), runtimestate.ErrDuplicate, "applying one delivery to one subscription twice")

	// Applying closed the subscription in the same transaction.
	var state string
	f.do(t, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT subscription_state FROM workflow_signal_subscription WHERE tenant_id = $1 AND subscription_id = $2`,
			f.tenant, sub.SubscriptionID).Scan(&state)
	})
	if state != runtimestate.SubscriptionSatisfied {
		t.Fatalf("subscription state after apply = %q, want SATISFIED", state)
	}
}

func TestScheduling_LeasesAreExclusiveAndFenced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "sched-lease")
	store := runtimestate.LeaseStore{}

	first := runtimestate.Lease{
		TenantID: f.tenant, LeaseID: uuid.New(),
		ResourceKind: runtimestate.LeaseWorkflowInstance, ResourceID: f.instance.InstanceID.String(),
		HolderID:   "worker:alpha",
		AcquiredAt: fixedInstant, ExpiresAt: fixedInstant.Add(30 * time.Second),
	}
	var held runtimestate.Lease
	f.do(t, func(tx dbport.Tx) error {
		var err error
		held, err = store.Acquire(ctx, tx, first, fixedInstant)
		return err
	})
	if held.FenceToken != 1 || held.State != runtimestate.LeaseHeld {
		t.Fatalf("first acquire = %+v, want HELD at fence token 1", held)
	}

	rival := first
	rival.LeaseID = uuid.New()
	rival.HolderID = "worker:beta"
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		_, err := store.Acquire(ctx, tx, rival, fixedInstant.Add(time.Second))
		return err
	}), runtimestate.ErrLeaseHeld, "a rival acquiring a live lease")

	// A holder whose token is not the resource's current one is refusable by
	// comparison rather than by hoping it noticed.
	wantErrIs(t, f.try(func(tx dbport.Tx) error {
		return store.Heartbeat(ctx, tx, f.tenant, held.LeaseID, 99,
			fixedInstant.Add(10*time.Second), fixedInstant.Add(60*time.Second))
	}), runtimestate.ErrFenceStale, "a heartbeat presenting a stale fence token")

	f.do(t, func(tx dbport.Tx) error {
		return store.Heartbeat(ctx, tx, f.tenant, held.LeaseID, 1,
			fixedInstant.Add(10*time.Second), fixedInstant.Add(60*time.Second))
	})

	// Past the extended expiry the lease is takeable, and the new token is the
	// previous one plus one.
	lapsed := rival
	lapsed.AcquiredAt = fixedInstant.Add(90 * time.Second)
	lapsed.ExpiresAt = fixedInstant.Add(120 * time.Second)
	var taken runtimestate.Lease
	f.do(t, func(tx dbport.Tx) error {
		var err error
		taken, err = store.Acquire(ctx, tx, lapsed, fixedInstant.Add(90*time.Second))
		return err
	})
	if taken.FenceToken != 2 || taken.HolderID != "worker:beta" {
		t.Fatalf("takeover = %+v, want worker:beta at fence token 2", taken)
	}

	var current runtimestate.Lease
	f.do(t, func(tx dbport.Tx) error {
		var err error
		current, err = store.Current(ctx, tx, f.tenant, runtimestate.LeaseWorkflowInstance,
			f.instance.InstanceID.String())
		return err
	})
	if current.LeaseID != taken.LeaseID {
		t.Fatalf("current holder = %s, want the takeover lease %s", current.LeaseID, taken.LeaseID)
	}

	f.do(t, func(tx dbport.Tx) error {
		return store.Release(ctx, tx, f.tenant, taken.LeaseID, 2, fixedInstant.Add(100*time.Second))
	})
	var afterRelease error
	_ = inTenantTxErr(f.conn, f.tenant, func(tx dbport.Tx) error {
		_, afterRelease = store.Current(ctx, tx, f.tenant, runtimestate.LeaseWorkflowInstance,
			f.instance.InstanceID.String())
		return nil
	})
	wantErrIs(t, afterRelease, runtimestate.ErrNotFound, "reading the holder of a released resource")
}

func TestScheduling_CheckpointsAndChildLinksAreImmutableFacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	f := newSchedulingFixture(t, db, "sched-checkpoint")
	checkpoints := runtimestate.CheckpointStore{}
	links := runtimestate.ChildLinkStore{}

	cp := runtimestate.Checkpoint{
		TenantID: f.tenant, InstanceID: f.instance.InstanceID, Sequence: 1,
		Kind:        runtimestate.CheckpointSafePoint,
		StateDigest: repeatHex("1"), FrontierDigest: repeatHex("2"), VariableDigest: repeatHex("3"),
		InstanceVersion: 1, TakenAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return checkpoints.Take(ctx, tx, cp) })

	rewrite := cp
	rewrite.StateDigest = repeatHex("4")
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return checkpoints.Take(ctx, tx, rewrite) }),
		runtimestate.ErrDuplicate, "re-taking checkpoint sequence 1")

	second := cp
	second.Sequence = 2
	second.Kind = runtimestate.CheckpointPause
	second.InstanceVersion = 2
	f.do(t, func(tx dbport.Tx) error { return checkpoints.Take(ctx, tx, second) })

	var latest runtimestate.Checkpoint
	f.do(t, func(tx dbport.Tx) error {
		var err error
		latest, err = checkpoints.Latest(ctx, tx, f.tenant, f.instance.InstanceID)
		return err
	})
	if latest.Sequence != 2 || latest.Kind != runtimestate.CheckpointPause {
		t.Fatalf("latest checkpoint = %+v, want sequence 2 (PAUSE)", latest)
	}

	child := createInstance(t, ctx, f.conn, f.tenant, referencePlan(t))
	link := runtimestate.ChildLink{
		TenantID: f.tenant, Parent: f.instance.InstanceID, Child: child.InstanceID,
		ParentNodeID: "node.fanout", Ordinal: 1, Mode: runtimestate.ChildAwait,
		InputDigest: repeatHex("5"), CreatedAt: fixedInstant,
	}
	f.do(t, func(tx dbport.Tx) error { return links.Link(ctx, tx, link) })

	// A child that could be re-parented would make cancellation propagation
	// ambiguous; one that is its own parent would make it non-terminating.
	reparent := link
	reparent.Parent = child.InstanceID
	reparent.Child = f.instance.InstanceID
	reparent.Ordinal = 2
	f.do(t, func(tx dbport.Tx) error { return links.Link(ctx, tx, reparent) })

	again := link
	again.Ordinal = 3
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return links.Link(ctx, tx, again) }),
		runtimestate.ErrDuplicate, "linking a child that already has a parent")

	self := link
	self.Child = f.instance.InstanceID
	wantErrIs(t, f.try(func(tx dbport.Tx) error { return links.Link(ctx, tx, self) }),
		runtimestate.ErrInvalid, "an instance linked as its own child")

	var children []uuid.UUID
	f.do(t, func(tx dbport.Tx) error {
		var err error
		children, err = links.Children(ctx, tx, f.tenant, f.instance.InstanceID)
		return err
	})
	if len(children) != 1 || children[0] != child.InstanceID {
		t.Fatalf("children of the parent = %v, want exactly %s", children, child.InstanceID)
	}
}
