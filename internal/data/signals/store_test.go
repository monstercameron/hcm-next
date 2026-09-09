package signals_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/signals"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	stepSignal "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/signal"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var signalAt = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

type acceptingVerifier struct{}

func (acceptingVerifier) Verify(stepSignal.Signal) error { return nil }

func TestTodo_WF_RUN_005(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "wf-run-005-primary")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	subscriptionID := uuid.New()
	request := signalRequest(tenant, "attempt-primary", []byte(`{"approved":true}`))

	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		if err := store.Subscribe(context.Background(), tx, signals.Subscription{
			TenantID: tenant, SubscriptionID: subscriptionID, InstanceID: instance,
			NodeID: "await-approval", NodeAttempt: 1, EventType: "approval.completed",
			CorrelationKey: "proposal", CorrelationValue: "proposal-1", ExpectedSchemaRef: "approval/v1",
			AcceptedSources: []string{"approvals"}, Ordering: stepSignal.OrderingNone, CreatedAt: signalAt,
		}); err != nil {
			return err
		}
		got, err := store.Receive(context.Background(), tx, request, acceptingVerifier{})
		if err != nil {
			return err
		}
		if len(got.Dispositions) != 1 || got.Dispositions[0].Status != stepSignal.StatusAccepted {
			t.Fatalf("first receive = %+v, want one ACCEPTED disposition", got)
		}
		return nil
	})

	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		got, err := store.Receive(context.Background(), tx, request, acceptingVerifier{})
		if err != nil {
			return err
		}
		if len(got.Dispositions) != 1 || got.Dispositions[0].Status != stepSignal.StatusDuplicateSameBytes {
			t.Fatalf("redelivery = %+v, want DUPLICATE_SAME_BYTES", got)
		}
		return nil
	})

	assertCounts(t, conn, tenant, instance, subscriptionID, 2, 1, 1, "accepted plus duplicate are inspectable without a second wakeup")
}

func TestTodo_WF_RUN_005_Race(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "wf-run-005-race")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	subscriptionID := uuid.New()
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		return store.Subscribe(context.Background(), tx, signals.Subscription{TenantID: tenant, SubscriptionID: subscriptionID,
			InstanceID: instance, NodeID: "await", NodeAttempt: 1, EventType: "event.ready", CorrelationKey: "subject",
			CorrelationValue: "subject-1", ExpectedSchemaRef: "event/v1", AcceptedSources: []string{"source"},
			Ordering: stepSignal.OrderingNone, CreatedAt: signalAt})
	})
	conn2 := appConn(t, db)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, c := range []*pgxadapter.Conn{conn, conn2} {
		go func(c *pgxadapter.Conn) {
			<-start
			results <- transactionErr(c, tenant, func(tx dbport.Tx) error {
				_, err := store.Receive(context.Background(), tx, signalRequest(tenant, "race", []byte(`{"ok":true}`)), acceptingVerifier{})
				return err
			})
		}(c)
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent receive: %v", err)
		}
	}
	assertCounts(t, conn, tenant, instance, subscriptionID, 2, 1, 1, "concurrent delivery remains one durable wakeup")
}

func TestTodo_WF_RUN_005_Fault(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "wf-run-005-fault")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	subscriptionID := uuid.New()
	err := transactionErr(conn, tenant, func(tx dbport.Tx) error {
		if err := store.Subscribe(context.Background(), tx, signals.Subscription{TenantID: tenant, SubscriptionID: subscriptionID,
			InstanceID: instance, NodeID: "await", NodeAttempt: 1, EventType: "event.fault", CorrelationKey: "subject",
			CorrelationValue: "subject-1", ExpectedSchemaRef: "event/v1", AcceptedSources: []string{"source"},
			Ordering: stepSignal.OrderingNone, CreatedAt: signalAt}); err != nil {
			return err
		}
		if _, err := store.Receive(context.Background(), tx, signalRequest(tenant, "fault", []byte(`{"ok":true}`)), acceptingVerifier{}); err != nil {
			return err
		}
		return errors.New("inject rollback after receive")
	})
	if err == nil {
		t.Fatal("fault transaction unexpectedly committed")
	}
	var dispositions int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM workflow_signal_disposition`).Scan(&dispositions); err != nil {
		t.Fatal(err)
	}
	if dispositions != 0 {
		t.Fatalf("rolled-back disposition count = %d, want 0", dispositions)
	}
}

func TestTodo_WF_RUN_005_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "wf-run-005-mutation")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		return store.Subscribe(context.Background(), tx, signals.Subscription{TenantID: tenant, SubscriptionID: uuid.New(),
			InstanceID: instance, NodeID: "await", NodeAttempt: 1, EventType: "event.mutation", CorrelationKey: "subject",
			CorrelationValue: "subject-1", ExpectedSchemaRef: "event/v1", AcceptedSources: []string{"source"},
			Ordering: stepSignal.OrderingNone, CreatedAt: signalAt})
	})
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.Receive(context.Background(), tx, signalRequest(tenant, "same-key", []byte(`{"first":true}`)), acceptingVerifier{})
		return err
	})
	var got signals.Receipt
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		got, err = store.Receive(context.Background(), tx, signalRequest(tenant, "same-key", []byte(`{"second":true}`)), acceptingVerifier{})
		return err
	})
	if len(got.Dispositions) != 1 || got.Dispositions[0].Status != stepSignal.StatusRefusedDuplicateDifferentBytes {
		t.Fatalf("mutation receive = %+v, want REFUSED_DUPLICATE_DIFFERENT_BYTES", got)
	}
}

func TestTodo_WF_RUN_005_Security(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "wf-run-005-security")
	other := newTenant(t, db, "wf-run-005-security-other")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		return store.Subscribe(context.Background(), tx, signals.Subscription{TenantID: tenant, SubscriptionID: uuid.New(),
			InstanceID: instance, NodeID: "await", NodeAttempt: 1, EventType: "event.secure", CorrelationKey: "subject",
			CorrelationValue: "subject-1", ExpectedSchemaRef: "event/v1", AcceptedSources: []string{"source"},
			Ordering: stepSignal.OrderingNone, CreatedAt: signalAt})
	})
	otherInstance := newInstance(t, db, other)
	_ = otherInstance
	var got signals.Receipt
	transaction(t, conn, other, func(tx dbport.Tx) error {
		var err error
		got, err = store.Receive(context.Background(), tx, signalRequest(other, "cross-tenant", []byte(`{"ok":true}`)), acceptingVerifier{})
		return err
	})
	if len(got.Dispositions) != 1 || got.Dispositions[0].Status != stepSignal.StatusRefusedUnmatched {
		t.Fatalf("cross-tenant receive = %+v, want REFUSED_UNMATCHED with no tenant leak", got)
	}
}

func signalRequest(tenant uuid.UUID, key string, payload []byte) signals.ReceiveRequest {
	eventType := "event.ready"
	schemaRef := "event/v1"
	source := "source"
	correlationKey, correlationValue := "subject", "subject-1"
	switch key {
	case "attempt-primary":
		eventType, schemaRef = "approval.completed", "approval/v1"
		correlationKey, correlationValue = "proposal", "proposal-1"
		source = "approvals"
	case "fault":
		eventType = "event.fault"
	case "same-key":
		eventType = "event.mutation"
	case "cross-tenant":
		eventType = "event.secure"
	}
	return signals.ReceiveRequest{AttemptID: uuid.New(), SignalID: uuid.New(), ReceivedAt: signalAt,
		Signal: stepSignal.Signal{Tenant: values.TenantId(tenant.String()), Source: source, EventType: eventType,
			SchemaRef: schemaRef, CorrelationKey: correlationKey, CorrelationValue: correlationValue, IdempotencyKey: key,
			Payload: payload, ReceivedAt: values.NewInstant(signalAt)}}
}

func newTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, id, key, "tenant "+key)
	return id
}

func newInstance(t *testing.T, db *pgtest.DB, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO workflow_instance
		(tenant_id, instance_id, cell_id, workflow_id, workflow_version, compiled_plan_hash,
		 business_subject_refs, execution_mode, runtime_status, completion_dimensions, input_ref,
		 variable_revision_head, current_node_ids, correlation_id, created_at)
		VALUES ($1, $2, 'cell-local', 'promotion', 1, repeat('a', 64), '{}', 'EXECUTE', 'WAITING', '{}',
		        'input:one', 0, '{await}', 'correlation-1', $3)`, tenant, id, signalAt)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	return conn
}

func transaction(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	if err := transactionErr(conn, tenant, fn); err != nil {
		t.Fatal(err)
	}
}

func transactionErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func assertCounts(t *testing.T, conn *pgxadapter.Conn, tenant, instance, subscription uuid.UUID, dispositions, continuations, ready int, message string) {
	t.Helper()
	var gotDisposition, gotContinuation, gotReady int
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM workflow_signal_disposition`).Scan(&gotDisposition); err != nil {
			return err
		}
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM workflow_continuation WHERE instance_id = $1`, instance).Scan(&gotContinuation); err != nil {
			return err
		}
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM workflow_ready_work WHERE instance_id = $1`, instance).Scan(&gotReady)
	})
	if gotDisposition != dispositions || gotContinuation != continuations || gotReady != ready {
		t.Fatalf("%s: disposition=%d continuation=%d ready=%d", message, gotDisposition, gotContinuation, gotReady)
	}
	_ = subscription
}
