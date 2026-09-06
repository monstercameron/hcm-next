package subscriptionstore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/subscriptionstore"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/subscription"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
        VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func draft(t *testing.T, tenant uuid.UUID, id string, revision uint64) subscription.EventSubscription {
	t.Helper()
	value, err := subscription.NewDraft(subscription.RevisionRequest{
		SubscriptionID: id, Requester: "requester", Subscriber: subscription.Subscriber{PrincipalRef: "principal-1"},
		EventKinds:          []subscription.EventKind{subscription.EventWorkerChanged},
		DeclaredFields:      map[subscription.EventKind][]string{subscription.EventWorkerChanged: {"worker.status"}},
		Filter:              subscription.Filter{Predicates: []subscription.Predicate{{Field: "worker.status", Operator: subscription.OperatorEquals, Value: "ACTIVE"}}},
		DeliveryEndpointRef: "endpoint-1", DeliveryGuarantee: subscription.GuaranteeAtLeastOnce,
		TenantScope: tenant.String(), OrganizationScopeRef: "org-1", PopulationScopeRef: "population-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision != 1 {
		value, err = subscription.NewRevision(value, subscription.RevisionRequest{
			SubscriptionID: id, Requester: "requester", Subscriber: value.Subscriber,
			EventKinds: value.EventKinds, DeclaredFields: value.DeclaredFields, Filter: value.Filter,
			DeliveryEndpointRef: value.DeliveryEndpointRef, DeliveryGuarantee: value.DeliveryGuarantee,
			TenantScope: value.TenantScope, OrganizationScopeRef: value.OrganizationScopeRef, PopulationScopeRef: value.PopulationScopeRef,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return value
}

func active(t *testing.T, revision subscription.EventSubscription) subscription.EventSubscription {
	t.Helper()
	result, err := revision.Activate("requester", "approver")
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func authEvent(t *testing.T, revision subscription.EventSubscription, allowed bool) subscription.AuthorizationEvent {
	t.Helper()
	grant := subscription.ScopeGrant{PrincipalRef: "principal-1", TenantScope: revision.TenantScope, Purpose: "delivery",
		EventKinds: revision.EventKinds, Resources: []string{revision.TenantScope, "org-1", "population-1", "worker.status"},
		Fields: map[subscription.EventKind][]string{subscription.EventWorkerChanged: {"worker.status"}}}
	if !allowed {
		grant.Resources = []string{revision.TenantScope, "org-1", "population-1"}
	}
	decision, err := subscription.Authorize(revision, grant)
	if err != nil && allowed {
		t.Fatal(err)
	}
	if !allowed && err == nil {
		t.Fatal("expected authorization denial")
	}
	event := decision.Event(grant)
	event.SubscriptionID = revision.SubscriptionID
	event.Revision = revision.Revision
	event.Rule = "subscription.scope.exact-declared-event-resource-field"
	event.GrantDigest = fixtureDigest(event.GrantDigest)
	event.DecisionDigest = fixtureDigest(event.DecisionDigest)
	event.Digest = fixtureDigest(event.Digest)
	return event
}

func fixtureDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestTodo_PERSIST_SUBSCRIPTION_001(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "persist-subscription-primary")
	store := subscriptionstore.New(appConn(t, db), tenant)
	one := draft(t, tenant, "sub-1", 1)
	if err := store.AppendRevision(context.Background(), tenant, one); err != nil {
		t.Fatal(err)
	}
	two := active(t, one)
	if err := store.AppendRevision(context.Background(), tenant, two); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendAuthorizationEvent(context.Background(), tenant, authEvent(t, two, true), 1); err != nil {
		t.Fatal(err)
	}
	history, err := store.LoadRevisions(context.Background(), tenant, one.SubscriptionID)
	if err != nil || len(history) != 2 || history[1].State != subscription.StateActive {
		t.Fatalf("history = %#v, err=%v", history, err)
	}
	events, err := store.LoadAuthorizationEvents(context.Background(), tenant, one.SubscriptionID)
	if err != nil || len(events) != 1 || !events[0].Allowed {
		t.Fatalf("authorization events = %#v, err=%v", events, err)
	}
}

func TestTodo_PERSIST_SUBSCRIPTION_001_Fault(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "persist-subscription-fault")
	store := subscriptionstore.New(appConn(t, db), tenant)
	one := draft(t, tenant, "sub-fault", 1)
	if err := store.AppendRevision(context.Background(), tenant, one); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendRevision(context.Background(), tenant, one); subscriptionstore.CodeOf(err) != subscriptionstore.CodeDuplicate {
		t.Fatalf("duplicate revision = %v, code=%q", err, subscriptionstore.CodeOf(err))
	}
	three := one
	three.Revision = 3
	if err := store.AppendRevision(context.Background(), tenant, three); subscriptionstore.CodeOf(err) != subscriptionstore.CodeStaleCAS {
		t.Fatalf("stale revision = %v, code=%q", err, subscriptionstore.CodeOf(err))
	}
}

func TestTodo_PERSIST_SUBSCRIPTION_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "persist-subscription-integration")
	store := subscriptionstore.New(appConn(t, db), tenant)
	one := draft(t, tenant, "sub-integration", 1)
	two := active(t, one)
	if err := store.AppendRevision(context.Background(), tenant, one); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendRevision(context.Background(), tenant, two); err != nil {
		t.Fatal(err)
	}
	event := authEvent(t, two, true)
	if err := store.AppendAuthorizationEvent(context.Background(), tenant, event, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendAuthorizationEvent(context.Background(), tenant, authEvent(t, two, false), 2); err != nil {
		t.Fatal(err)
	}
	activeRows, err := store.LoadActive(context.Background(), tenant)
	if err != nil || len(activeRows) != 1 || activeRows[0].SubscriptionID != one.SubscriptionID {
		t.Fatalf("active rows = %#v, err=%v", activeRows, err)
	}
	events, err := store.LoadAuthorizationEvents(context.Background(), tenant, one.SubscriptionID)
	if err != nil || len(events) != 2 || events[0].Allowed == events[1].Allowed {
		t.Fatalf("events = %#v, err=%v", events, err)
	}
}

func TestTodo_PERSIST_SUBSCRIPTION_001_Security(t *testing.T) {
	db := pgtest.New(t)
	first := insertTenant(t, db, "persist-subscription-security-a")
	second := insertTenant(t, db, "persist-subscription-security-b")
	store := subscriptionstore.New(appConn(t, db), first)
	one := draft(t, first, "sub-security", 1)
	if err := store.AppendRevision(context.Background(), first, one); err != nil {
		t.Fatal(err)
	}
	foreign := subscriptionstore.New(appConn(t, db), second)
	if rows, err := foreign.LoadRevisions(context.Background(), second, one.SubscriptionID); !errors.Is(err, subscriptionstore.ErrNotFound) || rows != nil {
		t.Fatalf("foreign revision read = %#v, err=%v", rows, err)
	}
	var visible int
	conn := appConn(t, db)
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(context.Background(), tx, second); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM event_subscription`).Scan(&visible); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("foreign tenant saw %d subscription rows", visible)
	}
}

func TestTodo_PERSIST_SUBSCRIPTION_001_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "persist-subscription-recovery")
	one := draft(t, tenant, "sub-recovery", 1)
	first := subscriptionstore.New(appConn(t, db), tenant)
	if err := first.AppendRevision(context.Background(), tenant, one); err != nil {
		t.Fatal(err)
	}
	if err := first.AppendAuthorizationEvent(context.Background(), tenant, authEvent(t, one, true), 1); err != nil {
		t.Fatal(err)
	}
	fresh := subscriptionstore.New(appConn(t, db), tenant)
	history, err := fresh.LoadRevisions(context.Background(), tenant, one.SubscriptionID)
	if err != nil || len(history) != 1 || history[0].Digest != one.Digest {
		t.Fatalf("fresh history = %#v, err=%v", history, err)
	}
	events, err := fresh.LoadAuthorizationEvents(context.Background(), tenant, one.SubscriptionID)
	if err != nil || len(events) != 1 || events[0].Digest == "" {
		t.Fatalf("fresh events = %#v, err=%v", events, err)
	}
}

func TestTodo_PERSIST_SUBSCRIPTION_001_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "persist-subscription-mutation")
	store := subscriptionstore.New(appConn(t, db), tenant)
	one := draft(t, tenant, "sub-mutation", 1)
	if err := store.AppendRevision(context.Background(), tenant, one); err != nil {
		t.Fatal(err)
	}
	event := authEvent(t, one, true)
	if err := store.AppendAuthorizationEvent(context.Background(), tenant, event, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE event_subscription SET state='REVOKED' WHERE tenant_id=$1 AND subscription_id=$2 AND revision=1`, tenant, one.SubscriptionID); err == nil {
		t.Fatal("event_subscription accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM event_subscription WHERE tenant_id=$1 AND subscription_id=$2`, tenant, one.SubscriptionID); err == nil {
		t.Fatal("event_subscription accepted DELETE")
	}
	if err := db.ExecErr(`UPDATE subscription_authorization_event SET allowed=false WHERE tenant_id=$1 AND subscription_id=$2`, tenant, one.SubscriptionID); err == nil {
		t.Fatal("authorization event accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM subscription_authorization_event WHERE tenant_id=$1 AND subscription_id=$2`, tenant, one.SubscriptionID); err == nil {
		t.Fatal("authorization event accepted DELETE")
	}
}
