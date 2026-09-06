package runtimestate_test

import (
	"context"
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

// The four helpers below were added for WF-RUN-026: internal/workflow/migrate/
// artifacts has to enumerate what an instance still holds before it can move
// any of it onto a new epoch, and it has to be able to retire a subscription
// it re-keyed. Three are SELECTs; one is a compare-and-swap in exactly the
// shape SignalStore.Apply already writes that column.

type wfrun026Env struct {
	db       *pgtest.DB
	conn     *pgxadapter.Conn
	tenant   uuid.UUID
	instance runtime.Instance
	child    runtime.Instance
}

func newWFRUN026Env(t *testing.T) wfrun026Env {
	t.Helper()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun026-store")
	conn := appConn(t, db)
	plan := referencePlan(t)
	return wfrun026Env{
		db: db, conn: conn, tenant: tenant,
		instance: createInstance(t, ctx, conn, tenant, plan),
		child:    createInstance(t, ctx, conn, tenant, plan),
	}
}

func TestSignalStoreOpenForInstanceListsOnlyOpenSubscriptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newWFRUN026Env(t)
	store := runtimestate.SignalStore{}

	open := uuid.New()
	closed := uuid.New()
	other := uuid.New()
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		for _, sub := range []runtimestate.Subscription{
			{SubscriptionID: open, InstanceID: env.instance.InstanceID, NodeID: "node_b",
				SignalName: "sig.b", CorrelationKey: "corr-1"},
			{SubscriptionID: closed, InstanceID: env.instance.InstanceID, NodeID: "node_a",
				SignalName: "sig.a", CorrelationKey: "corr-2"},
			{SubscriptionID: other, InstanceID: env.child.InstanceID, NodeID: "node_a",
				SignalName: "sig.a", CorrelationKey: "corr-3"},
		} {
			sub.TenantID, sub.CreatedAt = env.tenant, fixedInstant
			if err := store.Subscribe(ctx, tx, sub); err != nil {
				return err
			}
		}
		return store.CloseSubscription(ctx, tx, env.tenant, closed, 1, runtimestate.SubscriptionCancelled, fixedInstant)
	})

	var rows []runtimestate.Subscription
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		var err error
		rows, err = store.OpenForInstance(ctx, tx, env.tenant, env.instance.InstanceID)
		return err
	})
	if len(rows) != 1 {
		t.Fatalf("OpenForInstance returned %d rows, want only the still-open one: %+v", len(rows), rows)
	}
	got := rows[0]
	if got.SubscriptionID != open || got.State != runtimestate.SubscriptionOpen {
		t.Fatalf("row = %+v", got)
	}
	if got.NodeID != "node_b" || got.SignalName != "sig.b" || got.CorrelationKey != "corr-1" {
		t.Fatalf("the row lost its node, signal or correlation key: %+v", got)
	}
	if got.Version != 1 || got.CreatedAt.IsZero() {
		t.Fatalf("the row lost its version or creation instant: %+v", got)
	}
}

func TestSignalStoreCloseSubscriptionIsACompareAndSwapOnOpenRowsOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newWFRUN026Env(t)
	store := runtimestate.SignalStore{}
	id := uuid.New()

	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		return store.Subscribe(ctx, tx, runtimestate.Subscription{
			TenantID: env.tenant, SubscriptionID: id, InstanceID: env.instance.InstanceID,
			NodeID: "node_a", SignalName: "sig.a", CorrelationKey: "corr", CreatedAt: fixedInstant,
		})
	})

	// SATISFIED is not settleable here: a subscription is satisfied by a
	// signal actually arriving, which records a receipt.
	err := inTenantTxErr(env.conn, env.tenant, func(tx dbport.Tx) error {
		return store.CloseSubscription(ctx, tx, env.tenant, id, 1, runtimestate.SubscriptionSatisfied, fixedInstant)
	})
	if !errors.Is(err, runtimestate.ErrInvalid) {
		t.Fatalf("closing as SATISFIED = %v, want ErrInvalid", err)
	}
	// A stale expected version changes nothing.
	err = inTenantTxErr(env.conn, env.tenant, func(tx dbport.Tx) error {
		return store.CloseSubscription(ctx, tx, env.tenant, id, 7, runtimestate.SubscriptionCancelled, fixedInstant)
	})
	if !errors.Is(err, runtimestate.ErrVersionConflict) {
		t.Fatalf("closing at the wrong version = %v, want ErrVersionConflict", err)
	}
	// A missing instant is refused before any statement.
	err = inTenantTxErr(env.conn, env.tenant, func(tx dbport.Tx) error {
		return store.CloseSubscription(ctx, tx, env.tenant, id, 1, runtimestate.SubscriptionCancelled, time.Time{})
	})
	if !errors.Is(err, runtimestate.ErrInvalid) {
		t.Fatalf("closing with no instant = %v, want ErrInvalid", err)
	}

	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		return store.CloseSubscription(ctx, tx, env.tenant, id, 1, runtimestate.SubscriptionCancelled, fixedInstant)
	})
	var state string
	var closedAt *time.Time
	var version int64
	env.db.QueryRow(ctx, `SELECT subscription_state, closed_at, subscription_version
		FROM workflow_signal_subscription WHERE tenant_id = $1 AND subscription_id = $2`,
		env.tenant, id).Scan(&state, &closedAt, &version)
	if state != runtimestate.SubscriptionCancelled || closedAt == nil || version != 2 {
		t.Fatalf("after closing: state %s, closed_at %v, version %d", state, closedAt, version)
	}

	// A second close finds no OPEN row and refuses rather than rewriting one.
	err = inTenantTxErr(env.conn, env.tenant, func(tx dbport.Tx) error {
		return store.CloseSubscription(ctx, tx, env.tenant, id, 2, runtimestate.SubscriptionCancelled, fixedInstant)
	})
	if !errors.Is(err, runtimestate.ErrVersionConflict) {
		t.Fatalf("closing an already-closed subscription = %v, want ErrVersionConflict", err)
	}
}

func TestReadyWorkStorePendingForInstanceIncludesDispatchedWork(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newWFRUN026Env(t)
	store := runtimestate.ReadyWorkStore{}

	ready := uuid.New()
	dispatched := uuid.New()
	done := uuid.New()
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		for i, id := range []uuid.UUID{ready, dispatched, done} {
			if err := store.Enqueue(ctx, tx, runtimestate.ReadyWork{
				TenantID: env.tenant, ReadyWorkID: id, InstanceID: env.instance.InstanceID,
				NodeID: "node_a", Attempt: i + 1, State: runtimestate.ReadyReady,
				EligibleAt: fixedInstant.Add(time.Duration(i) * time.Minute), EnqueuedAt: fixedInstant,
			}); err != nil {
				return err
			}
		}
		if err := store.Transition(ctx, tx, env.tenant, dispatched, 1, runtimestate.ReadyDispatched, fixedInstant); err != nil {
			return err
		}
		if err := store.Transition(ctx, tx, env.tenant, done, 1, runtimestate.ReadyDispatched, fixedInstant); err != nil {
			return err
		}
		return store.Transition(ctx, tx, env.tenant, done, 2, runtimestate.ReadyDone, fixedInstant)
	})

	var rows []runtimestate.ReadyWork
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		var err error
		rows, err = store.PendingForInstance(ctx, tx, env.tenant, env.instance.InstanceID)
		return err
	})
	if len(rows) != 2 {
		t.Fatalf("PendingForInstance returned %d rows, want the READY and the DISPATCHED one: %+v", len(rows), rows)
	}
	// Soonest-eligible first.
	if !rows[0].EligibleAt.Before(rows[1].EligibleAt) {
		t.Fatalf("rows are not ordered by eligibility: %s then %s", rows[0].EligibleAt, rows[1].EligibleAt)
	}
	if rows[0].ReadyWorkID != ready || rows[1].ReadyWorkID != dispatched {
		t.Fatalf("rows = %s, %s", rows[0].ReadyWorkID, rows[1].ReadyWorkID)
	}
	if rows[0].Priority != 100 || rows[0].Version != 1 || rows[0].EnqueuedAt.IsZero() {
		t.Fatalf("the row lost its priority, version or enqueue instant: %+v", rows[0])
	}
	if rows[1].Version != 2 {
		t.Fatalf("the dispatched row's version is %d, want 2", rows[1].Version)
	}
}

func TestChildLinkStoreLinksForParentReturnsWholeRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newWFRUN026Env(t)
	store := runtimestate.ChildLinkStore{}

	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		return store.Link(ctx, tx, runtimestate.ChildLink{
			TenantID: env.tenant, Parent: env.instance.InstanceID, Child: env.child.InstanceID,
			ParentNodeID: "node_a", Ordinal: 1, Mode: runtimestate.ChildAwait,
			InputDigest: repeatHex("a"), CreatedAt: fixedInstant,
		})
	})

	var rows []runtimestate.ChildLink
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		var err error
		rows, err = store.LinksForParent(ctx, tx, env.tenant, env.instance.InstanceID)
		return err
	})
	if len(rows) != 1 {
		t.Fatalf("LinksForParent returned %d rows, want 1", len(rows))
	}
	got := rows[0]
	if got.Child != env.child.InstanceID || got.ParentNodeID != "node_a" ||
		got.Ordinal != 1 || got.Mode != runtimestate.ChildAwait {
		t.Fatalf("row = %+v", got)
	}
	if got.InputDigest != repeatHex("a") || got.CreatedAt.IsZero() {
		t.Fatalf("the row lost its input digest or creation instant: %+v", got)
	}

	// Children and LinksForParent must agree on which children exist and in
	// which order, or a caller reading one and reasoning with the other would
	// silently disagree with itself.
	var ids []uuid.UUID
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		var err error
		ids, err = store.Children(ctx, tx, env.tenant, env.instance.InstanceID)
		return err
	})
	if len(ids) != len(rows) || ids[0] != rows[0].Child {
		t.Fatalf("Children returned %v; LinksForParent returned %s", ids, rows[0].Child)
	}
}

// A parent with no children is an empty list, not an error: an instance that
// spawned nothing is a perfectly ordinary instance.
func TestChildLinkStoreLinksForParentIsEmptyForAChildlessInstance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	env := newWFRUN026Env(t)
	var rows []runtimestate.ChildLink
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		var err error
		rows, err = (runtimestate.ChildLinkStore{}).LinksForParent(ctx, tx, env.tenant, env.instance.InstanceID)
		return err
	})
	if len(rows) != 0 {
		t.Fatalf("a childless instance reported %d links", len(rows))
	}
}
