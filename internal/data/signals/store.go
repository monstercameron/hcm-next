// Package signals owns the durable signal receive path.  It is deliberately
// caller-transactional: the caller scopes the transaction to a tenant and
// commits the receipt, disposition, continuation and ready-work enqueue as
// one unit.
package signals

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/runtimestate"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
	stepSignal "github.com/monstercameron/hcm-next/internal/workflow/steps/signal"
)

var signalNamespace = uuid.MustParse("a1b2c3d4-e5f6-4789-8012-3456789abcde")

type Subscription struct {
	TenantID          uuid.UUID
	SubscriptionID    uuid.UUID
	InstanceID        uuid.UUID
	NodeID            string
	NodeAttempt       int
	EventType         string
	CorrelationKey    string
	CorrelationValue  string
	ExpectedSchemaRef string
	AcceptedSources   []string
	Ordering          stepSignal.OrderingExpectation
	ClosesAt          time.Time
	CreatedAt         time.Time
}

type ReceiveRequest struct {
	AttemptID  uuid.UUID
	SignalID   uuid.UUID
	Signal     stepSignal.Signal
	ReceivedAt time.Time
}

type Disposition struct {
	DispositionID   uuid.UUID
	AttemptID       uuid.UUID
	SignalID        uuid.UUID
	SubscriptionID  uuid.UUID
	Status          stepSignal.Status
	Reason          string
	ContinuationRef string
	RecordedAt      time.Time
}

type Receipt struct {
	SignalID     uuid.UUID
	AttemptID    uuid.UUID
	Dispositions []Disposition
}

type Store struct{}

type Executor interface {
	dbport.Execer
	dbport.Querier
}

func (s Store) Subscribe(ctx context.Context, ex Executor, in Subscription) error {
	if err := validateSubscription(in); err != nil {
		return err
	}
	sources, err := json.Marshal(in.AcceptedSources)
	if err != nil {
		return fmt.Errorf("signals: encode accepted sources: %w", err)
	}
	var closes any
	if !in.ClosesAt.IsZero() {
		closes = in.ClosesAt.UTC()
	}
	created := in.CreatedAt.UTC()
	if created.IsZero() {
		created = time.Now().UTC()
	}
	rows, err := ex.Exec(ctx, `
		INSERT INTO workflow_signal_subscription (
			tenant_id, subscription_id, instance_id, node_id, signal_name,
			correlation_key, subscription_state, expires_at, created_at,
			event_type, correlation_value, expected_schema_ref, accepted_sources,
			ordering_expectation, node_attempt)
		VALUES ($1, $2, $3, $4, $5, $6, 'OPEN', $7, $8, $5, $9, $10, $11, $12, $13)
		ON CONFLICT DO NOTHING`,
		in.TenantID, in.SubscriptionID, in.InstanceID, in.NodeID, in.EventType,
		in.CorrelationKey, closes, created, in.CorrelationValue,
		in.ExpectedSchemaRef, sources, string(in.Ordering), in.NodeAttempt)
	if err != nil {
		return fmt.Errorf("signals: insert subscription: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("%w: signal subscription %s", runtimestate.ErrDuplicate, in.SubscriptionID)
	}
	return nil
}

func (s Store) Receive(ctx context.Context, ex Executor, req ReceiveRequest, verify stepSignal.Verifier) (Receipt, error) {
	if err := validateReceive(req); err != nil {
		return Receipt{}, err
	}
	if verify == nil {
		return Receipt{}, errors.New("signals: verifier is required")
	}
	sig := req.Signal
	if req.SignalID == uuid.Nil {
		req.SignalID = uuid.NewSHA1(signalNamespace, []byte(sig.Tenant.String()+"\x00"+sig.EventType+"\x00"+sig.CorrelationKey+"\x00"+sig.IdempotencyKey))
	}
	if req.AttemptID == uuid.Nil {
		req.AttemptID = uuid.NewSHA1(signalNamespace, []byte("attempt\x00"+req.SignalID.String()))
	}
	received := req.ReceivedAt.UTC()
	if received.IsZero() {
		received = sig.ReceivedAt.Time()
	}
	if received.IsZero() {
		return Receipt{}, errors.New("signals: received_at is required")
	}
	digest := payloadDigest(sig.Payload)
	var canonical canonicalSignal
	duplicateDifferentBytes := false
	rows, err := ex.Exec(ctx, `
		INSERT INTO workflow_signal (
			tenant_id, signal_id, signal_name, correlation_key, dedupe_token,
			schema_ref, payload, payload_digest, delivered_at,
			event_type, source, correlation_value, sequence_number, received_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, $3, $10, $11, $12, $9)
		ON CONFLICT DO NOTHING`,
		uuid.MustParse(sig.Tenant.String()), req.SignalID, sig.EventType, sig.CorrelationKey,
		sig.IdempotencyKey, sig.SchemaRef, string(sig.Payload), digest, received,
		sig.Source, sig.CorrelationValue, sig.SequenceNumber)
	if err != nil {
		return Receipt{}, fmt.Errorf("signals: record signal: %w", err)
	}
	if rows == 0 {
		row := ex.QueryRow(ctx, `
			SELECT signal_id, event_type, source, correlation_key, correlation_value,
			       schema_ref, dedupe_token, sequence_number, payload, payload_digest, received_at
			FROM workflow_signal
			WHERE tenant_id = $1 AND signal_name = $2 AND correlation_key = $3 AND dedupe_token = $4`,
			uuid.MustParse(sig.Tenant.String()), sig.EventType, sig.CorrelationKey, sig.IdempotencyKey)
		if err := row.Scan(&canonical.ID, &canonical.EventType, &canonical.Source,
			&canonical.CorrelationKey, &canonical.CorrelationValue, &canonical.SchemaRef,
			&canonical.DedupToken, &canonical.SequenceNumber, &canonical.Payload, &canonical.Digest, &canonical.ReceivedAt); err != nil {
			return Receipt{}, fmt.Errorf("signals: load canonical signal: %w", err)
		}
		duplicateDifferentBytes = canonical.Digest != digest
	} else {
		canonical = canonicalSignal{ID: req.SignalID, EventType: sig.EventType, Source: sig.Source,
			CorrelationKey: sig.CorrelationKey, CorrelationValue: sig.CorrelationValue,
			SchemaRef: sig.SchemaRef, DedupToken: sig.IdempotencyKey, SequenceNumber: sig.SequenceNumber, Payload: sig.Payload,
			Digest: digest, ReceivedAt: received}
	}
	canonicalSig, err := canonical.step(sig.Tenant)
	if err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{SignalID: canonical.ID, AttemptID: req.AttemptID}
	rowsSub, err := ex.Query(ctx, `
		SELECT subscription_id, instance_id, node_id, node_attempt, event_type,
		       correlation_key, correlation_value, expected_schema_ref,
		       accepted_sources, ordering_expectation, expires_at, subscription_version
		FROM workflow_signal_subscription
		WHERE tenant_id = $1 AND subscription_state <> 'CANCELLED'
		  AND event_type = $2 AND correlation_key = $3 AND correlation_value = $4
		ORDER BY created_at, subscription_id
		FOR UPDATE`, uuid.MustParse(sig.Tenant.String()), canonical.EventType,
		canonical.CorrelationKey, canonical.CorrelationValue)
	if err != nil {
		return Receipt{}, fmt.Errorf("signals: find subscriptions: %w", err)
	}
	var subscriptions []durableSubscription
	for rowsSub.Next() {
		var sub durableSubscription
		if err := rowsSub.Scan(&sub.ID, &sub.InstanceID, &sub.NodeID, &sub.NodeAttempt,
			&sub.EventType, &sub.CorrelationKey, &sub.CorrelationValue, &sub.ExpectedSchemaRef,
			&sub.AcceptedSources, &sub.Ordering, &sub.ClosesAt, &sub.Version); err != nil {
			return Receipt{}, fmt.Errorf("signals: scan subscription: %w", err)
		}
		subscriptions = append(subscriptions, sub)
	}
	rowsSub.Close()
	if err := rowsSub.Err(); err != nil {
		return Receipt{}, fmt.Errorf("signals: iterate subscriptions: %w", err)
	}
	found := len(subscriptions) > 0
	for _, sub := range subscriptions {
		prior, err := priorAccepted(ctx, ex, uuid.MustParse(sig.Tenant.String()), sub.ID, sig.Tenant, sub.step(sig.Tenant).Digest())
		if err != nil {
			return Receipt{}, err
		}
		var decision stepSignal.Result
		if duplicateDifferentBytes {
			decision = stepSignal.Result{Status: stepSignal.StatusRefusedDuplicateDifferentBytes,
				Reason: "idempotency key was reused with different payload bytes"}
		} else {
			decision, err = stepSignal.Accept(sub.step(sig.Tenant), canonicalSig, prior, verify, values.NewInstant(received))
			if err != nil {
				return Receipt{}, fmt.Errorf("signals: evaluate subscription %s: %w", sub.ID, err)
			}
		}
		disposition := Disposition{DispositionID: uuid.New(), AttemptID: req.AttemptID, SignalID: canonical.ID,
			SubscriptionID: sub.ID, Status: decision.Status, Reason: decision.Reason, RecordedAt: received}
		if decision.Continuation {
			disposition.ContinuationRef = "signal:" + canonical.ID.String()
			if err := applyAccepted(ctx, ex, sig.Tenant, sub, canonical.ID, received); err != nil {
				return Receipt{}, err
			}
		}
		if err := recordDisposition(ctx, ex, sig.Tenant, disposition); err != nil {
			return Receipt{}, err
		}
		receipt.Dispositions = append(receipt.Dispositions, disposition)
	}
	if !found {
		disposition := Disposition{DispositionID: uuid.New(), AttemptID: req.AttemptID, SignalID: canonical.ID,
			Status: stepSignal.StatusRefusedUnmatched, Reason: "no open subscription matched event and correlation", RecordedAt: received}
		if err := recordDisposition(ctx, ex, sig.Tenant, disposition); err != nil {
			return Receipt{}, err
		}
		receipt.Dispositions = append(receipt.Dispositions, disposition)
	}
	return receipt, nil
}

type canonicalSignal struct {
	ID                                                             uuid.UUID
	EventType, Source, CorrelationKey, CorrelationValue, SchemaRef string
	DedupToken                                                     string
	SequenceNumber                                                 uint64
	Payload                                                        []byte
	Digest                                                         string
	ReceivedAt                                                     time.Time
}

func (s canonicalSignal) step(tenant values.TenantId) (stepSignal.Signal, error) {
	at := values.NewInstant(s.ReceivedAt)
	return stepSignal.Signal{Tenant: tenant, Source: s.Source, EventType: s.EventType,
		SchemaRef: s.SchemaRef, CorrelationKey: s.CorrelationKey, CorrelationValue: s.CorrelationValue,
		SequenceNumber: s.SequenceNumber, Payload: append([]byte(nil), s.Payload...),
		ReceivedAt: at, IdempotencyKey: s.DedupToken}, nil
}

type durableSubscription struct {
	ID, InstanceID                                                 uuid.UUID
	NodeID                                                         string
	NodeAttempt                                                    int
	EventType, CorrelationKey, CorrelationValue, ExpectedSchemaRef string
	AcceptedSources                                                []byte
	Ordering                                                       string
	ClosesAt                                                       *time.Time
	Version                                                        uint64
}

func (s durableSubscription) step(tenant values.TenantId) stepSignal.SignalSubscription {
	var sources []string
	_ = json.Unmarshal(s.AcceptedSources, &sources)
	return stepSignal.SignalSubscription{Tenant: tenant, WorkflowInstanceID: s.InstanceID.String(), NodeID: s.NodeID,
		EventType: s.EventType, CorrelationKey: s.CorrelationKey, CorrelationValue: s.CorrelationValue,
		ExpectedSchemaRef: s.ExpectedSchemaRef, AcceptedSources: sources,
		Ordering: stepSignal.OrderingExpectation(s.Ordering), ClosesAt: optionalInstant(s.ClosesAt)}
}

func optionalInstant(at *time.Time) values.Instant {
	if at == nil {
		return values.Instant{}
	}
	return values.NewInstant(at.UTC())
}

func priorAccepted(ctx context.Context, ex Executor, tenant, subscription uuid.UUID, tenantValue values.TenantId, subscriptionDigest string) ([]stepSignal.LogEntry, error) {
	rows, err := ex.Query(ctx, `
		SELECT s.source, s.event_type, s.schema_ref, s.correlation_key,
		       s.correlation_value, s.sequence_number, s.dedupe_token,
		       s.payload, s.received_at
		FROM workflow_signal_receipt r
		JOIN workflow_signal s ON s.tenant_id = r.tenant_id AND s.signal_id = r.signal_id
		WHERE r.tenant_id = $1 AND r.subscription_id = $2
		ORDER BY s.received_at, s.signal_id`, tenant, subscription)
	if err != nil {
		return nil, fmt.Errorf("signals: load prior receipts: %w", err)
	}
	defer rows.Close()
	var out []stepSignal.LogEntry
	for rows.Next() {
		var source, eventType, schema, key, value, token string
		var seq uint64
		var payload []byte
		var at time.Time
		if err := rows.Scan(&source, &eventType, &schema, &key, &value, &seq, &token, &payload, &at); err != nil {
			return nil, fmt.Errorf("signals: scan prior receipt: %w", err)
		}
		out = append(out, stepSignal.LogEntry{SubscriptionDigest: subscriptionDigest, Status: stepSignal.StatusAccepted, Continuation: true,
			Signal: stepSignal.Signal{Tenant: tenantValue, Source: source, EventType: eventType, SchemaRef: schema,
				CorrelationKey: key, CorrelationValue: value, SequenceNumber: seq, IdempotencyKey: token,
				Payload: append([]byte(nil), payload...), ReceivedAt: values.NewInstant(at)}})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("signals: iterate prior receipts: %w", err)
	}
	return out, nil
}

func applyAccepted(ctx context.Context, ex Executor, tenant values.TenantId, sub durableSubscription, signalID uuid.UUID, at time.Time) error {
	tenantID := uuid.MustParse(tenant.String())
	_, err := ex.Exec(ctx, `
		INSERT INTO workflow_signal_receipt
			(tenant_id, signal_id, subscription_id, instance_id, applied_at, applied_at_version)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT DO NOTHING`, tenantID, signalID, sub.ID, sub.InstanceID, at.UTC(), sub.Version)
	if err != nil {
		return fmt.Errorf("signals: record receipt: %w", err)
	}
	_, err = ex.Exec(ctx, `
		UPDATE workflow_signal_subscription
		SET subscription_state = 'SATISFIED', closed_at = $3, subscription_version = subscription_version + 1
		WHERE tenant_id = $1 AND subscription_id = $2 AND subscription_state = 'OPEN'`, tenantID, sub.ID, at.UTC())
	if err != nil {
		return fmt.Errorf("signals: close subscription: %w", err)
	}
	cont := runtime.ContinuationRecord{TenantID: tenantID, InstanceID: sub.InstanceID,
		SourceNodeID: sub.NodeID, SourceAttempt: sub.NodeAttempt, TargetNodeID: sub.NodeID,
		Kind: frontier.IntentReady, RouteKey: "SIGNAL", Ref: "signal:" + signalID.String(), RecordedAt: at.UTC()}
	if err := (runtime.ContinuationStore{}).MarkReady(ctx, ex, cont); err != nil {
		return fmt.Errorf("signals: record continuation: %w", err)
	}
	readyID := uuid.NewSHA1(signalNamespace, []byte("ready\x00"+tenant.String()+"\x00"+sub.InstanceID.String()+"\x00"+sub.NodeID+"\x00"+fmt.Sprint(sub.NodeAttempt)))
	err = (runtimestate.ReadyWorkStore{}).Enqueue(ctx, ex, runtimestate.ReadyWork{TenantID: tenantID, ReadyWorkID: readyID,
		InstanceID: sub.InstanceID, NodeID: sub.NodeID, Attempt: sub.NodeAttempt, State: runtimestate.ReadyReady,
		EligibleAt: at.UTC(), EnqueuedAt: at.UTC()})
	if errors.Is(err, runtimestate.ErrDuplicate) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("signals: enqueue ready work: %w", err)
	}
	return nil
}

func recordDisposition(ctx context.Context, ex Executor, tenant values.TenantId, d Disposition) error {
	_, err := ex.Exec(ctx, `
		INSERT INTO workflow_signal_disposition
			(tenant_id, disposition_id, attempt_id, signal_id, subscription_id, status, reason, continuation_ref, recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9)
		ON CONFLICT DO NOTHING`, uuid.MustParse(tenant.String()), d.DispositionID, d.AttemptID, d.SignalID,
		nullableUUID(d.SubscriptionID), string(d.Status), d.Reason, d.ContinuationRef, d.RecordedAt.UTC())
	if err != nil {
		return fmt.Errorf("signals: record disposition: %w", err)
	}
	return nil
}

func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func validateSubscription(in Subscription) error {
	if in.TenantID == uuid.Nil || in.SubscriptionID == uuid.Nil || in.InstanceID == uuid.Nil {
		return errors.New("signals: subscription identity is required")
	}
	if in.NodeID == "" || in.NodeAttempt < 1 || in.EventType == "" || in.CorrelationKey == "" || in.CorrelationValue == "" || in.ExpectedSchemaRef == "" || len(in.AcceptedSources) == 0 {
		return errors.New("signals: subscription fields are incomplete")
	}
	if !in.Ordering.Valid() {
		return errors.New("signals: invalid ordering expectation")
	}
	return nil
}

func validateReceive(req ReceiveRequest) error {
	if req.Signal.Tenant == "" || req.Signal.EventType == "" || req.Signal.CorrelationKey == "" || req.Signal.CorrelationValue == "" || req.Signal.SchemaRef == "" || req.Signal.Source == "" || req.Signal.IdempotencyKey == "" {
		return errors.New("signals: signal fields are incomplete")
	}
	if _, err := uuid.Parse(req.Signal.Tenant.String()); err != nil {
		return fmt.Errorf("signals: tenant is not a UUID: %w", err)
	}
	if len(req.Signal.Payload) == 0 || !json.Valid(req.Signal.Payload) || !strings.HasPrefix(strings.TrimSpace(string(req.Signal.Payload)), "{") {
		return errors.New("signals: payload must be a JSON object")
	}
	return nil
}

func payloadDigest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
