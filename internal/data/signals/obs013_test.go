package signals_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/runtimestate"
	"github.com/monstercameron/hcm-next/internal/data/signals"
	stepSignal "github.com/monstercameron/hcm-next/internal/workflow/steps/signal"
)

func TestTodo_OBS_013_SignalCausalMetadataRoundTripReplayAndTenantIsolation(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "obs-013-signal")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	causal := &runtimestate.CausalMetadata{CorrelationID: "corr", CausationID: "cause", LogicalOperationID: "logical", AttemptID: "attempt", TraceLink: &runtimestate.TraceLinkMetadata{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", TraceFlags: 1, ExpiresAt: signalAt.Add(time.Hour)}}
	store := signals.Store{}
	subID := uuid.New()
	request := signalRequest(tenant, "obs-013-signal", []byte(`{"ok":true}`))
	request.Causal = causal
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		return store.Subscribe(context.Background(), tx, signals.Subscription{TenantID: tenant, SubscriptionID: subID, InstanceID: instance, NodeID: "await", NodeAttempt: 1, EventType: "event.ready", CorrelationKey: "subject", CorrelationValue: "subject-1", ExpectedSchemaRef: "event/v1", AcceptedSources: []string{"source"}, Ordering: stepSignal.OrderingNone, CreatedAt: signalAt, Causal: causal})
	})
	var firstAttempt uuid.UUID
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		got, err := store.Receive(context.Background(), tx, request, acceptingVerifier{})
		if err != nil {
			return err
		}
		if got.Causal == nil || got.Causal.LogicalOperationID != "logical" || got.Causal.TraceLink == nil {
			t.Fatalf("receipt causal = %#v", got.Causal)
		}
		firstAttempt = got.AttemptID
		return nil
	})
	request.AttemptID = uuid.Nil
	request.Causal = nil
	var replayAttempt uuid.UUID
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		got, err := store.Receive(context.Background(), tx, request, acceptingVerifier{})
		if err != nil {
			return err
		}
		if len(got.Dispositions) != 1 || got.Dispositions[0].Status != stepSignal.StatusDuplicateSameBytes {
			t.Fatalf("replay = %+v", got)
		}
		if got.AttemptID == firstAttempt || got.Causal == nil || got.Causal.LogicalOperationID != "logical" || got.Causal.AttemptID != got.AttemptID.String() {
			t.Fatalf("replay identities = %+v, first attempt %s", got, firstAttempt)
		}
		replayAttempt = got.AttemptID
		return nil
	})
	transaction(t, conn, tenant, func(tx dbport.Tx) error {
		var signalLogical, dispositionLogical, dispositionAttempt, continuationLogical, readyLogical string
		if err := tx.QueryRow(context.Background(), `SELECT logical_operation_id FROM workflow_signal WHERE signal_id = $1`, request.SignalID).Scan(&signalLogical); err != nil {
			return err
		}
		if err := tx.QueryRow(context.Background(), `SELECT logical_operation_id, causal_attempt_id FROM workflow_signal_disposition WHERE attempt_id = $1`, replayAttempt).Scan(&dispositionLogical, &dispositionAttempt); err != nil {
			return err
		}
		if err := tx.QueryRow(context.Background(), `SELECT logical_operation_id FROM workflow_continuation WHERE instance_id = $1`, instance).Scan(&continuationLogical); err != nil {
			return err
		}
		if err := tx.QueryRow(context.Background(), `SELECT logical_operation_id FROM workflow_ready_work WHERE instance_id = $1`, instance).Scan(&readyLogical); err != nil {
			return err
		}
		if signalLogical != "logical" || dispositionLogical != "logical" || dispositionAttempt == "attempt" || continuationLogical != "logical" || readyLogical != "logical" {
			t.Fatalf("causal persistence signal=%q disposition=%q/%q continuation=%q ready=%q", signalLogical, dispositionLogical, dispositionAttempt, continuationLogical, readyLogical)
		}
		return nil
	})
	other := uuid.New()
	if _, err := store.Receive(context.Background(), conn, signalRequest(other, "obs-013-cross", []byte(`{"ok":true}`)), acceptingVerifier{}); err == nil {
		t.Fatal("cross-tenant receive unexpectedly succeeded")
	}
}

func TestTodo_OBS_013_InvalidOrExpiredTraceIsOperationallyEquivalentToMissingTrace(t *testing.T) {
	db := pgtest.New(t)
	tenant := newTenant(t, db, "obs-013-optional-trace")
	instance := newInstance(t, db, tenant)
	conn := appConn(t, db)
	store := signals.Store{}
	for i, link := range []*runtimestate.TraceLinkMetadata{
		nil,
		{TraceID: "not-a-trace", SpanID: "00f067aa0ba902b7", TraceFlags: 1},
		{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7", TraceFlags: 1, ExpiresAt: signalAt.Add(-time.Second)},
	} {
		event := "event.optional." + string(rune('a'+i))
		correlation := "subject-" + string(rune('a'+i))
		subID := uuid.New()
		transaction(t, conn, tenant, func(tx dbport.Tx) error {
			return store.Subscribe(context.Background(), tx, signals.Subscription{TenantID: tenant, SubscriptionID: subID, InstanceID: instance, NodeID: "await-" + string(rune('a'+i)), NodeAttempt: 1, EventType: event, CorrelationKey: "subject", CorrelationValue: correlation, ExpectedSchemaRef: "event/v1", AcceptedSources: []string{"source"}, Ordering: stepSignal.OrderingNone, CreatedAt: signalAt})
		})
		req := signalRequest(tenant, "optional-"+string(rune('a'+i)), []byte(`{"ok":true}`))
		req.Signal.EventType, req.Signal.CorrelationValue = event, correlation
		req.Causal = &runtimestate.CausalMetadata{CorrelationID: "corr", CausationID: "cause", LogicalOperationID: "logical-" + string(rune('a'+i)), AttemptID: "producer", TraceLink: link}
		transaction(t, conn, tenant, func(tx dbport.Tx) error {
			got, err := store.Receive(context.Background(), tx, req, acceptingVerifier{})
			if err != nil {
				return err
			}
			if len(got.Dispositions) != 1 || got.Dispositions[0].Status != stepSignal.StatusAccepted || got.Causal == nil || got.Causal.TraceLink != nil {
				t.Fatalf("optional trace case %d changed business result: %+v", i, got)
			}
			return nil
		})
	}
}
