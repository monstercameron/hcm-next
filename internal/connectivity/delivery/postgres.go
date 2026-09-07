package delivery

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
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
)

// PostgresAttemptStore is the durable AttemptStore used by the worker-hosted
// delivery role. It stores only semantic references and digests; provider
// addresses, rendered bodies and credentials stay behind Transport.
//
// A delivery_attempt is append-only in the DB-014 schema. Consequently a
// replay of an idempotency key returns the original attempt and never invokes
// Transport again. Provider retry policy belongs to the transport adapter;
// this store is the durable fence against a second logical effect.
type PostgresAttemptStore struct {
	DB       dbport.Beginner
	Provider string
	Clock    func() time.Time
}

var (
	ErrInvalidStoreEnvelope  = errors.New("delivery: envelope cannot be mapped to messaging metadata")
	ErrDeliveryTargetMissing = errors.New("delivery: recipient delivery target was not found")
)

func (s PostgresAttemptStore) now() time.Time {
	if s.Clock != nil {
		return s.Clock().UTC()
	}
	return time.Now().UTC()
}

func (s PostgresAttemptStore) Claim(ctx context.Context, envelope Envelope, attempt int) (Claim, error) {
	if attempt < 1 {
		return Claim{}, fmt.Errorf("%w: attempt must be positive", ErrInvalidEnvelope)
	}
	tenantID, intentID, endpointID, err := envelopeIDs(envelope)
	if err != nil {
		return Claim{}, err
	}
	if s.DB == nil {
		return Claim{}, ErrNoStore
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Claim{}, fmt.Errorf("delivery: begin attempt claim: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return Claim{}, err
	}
	messageID, err := recipientMessageID(ctx, tx, tenantID, intentID, endpointID, envelope.RecipientRef)
	if err != nil {
		return Claim{}, err
	}
	var existingID uuid.UUID
	var lastEvent string
	var lastAttempt int64
	err = tx.QueryRow(ctx, `
		SELECT a.attempt_id,
			COALESCE((SELECT r.event_type FROM delivery_receipt r
				WHERE r.tenant_id=a.tenant_id AND r.attempt_id=a.attempt_id
				ORDER BY r.received_at DESC LIMIT 1), ''),
			COALESCE((SELECT max(r.sequence_no) FROM delivery_receipt r
				WHERE r.tenant_id=a.tenant_id AND r.attempt_id=a.attempt_id), 0)
		FROM delivery_attempt a
		WHERE a.tenant_id=$1 AND a.recipient_message_id=$2 AND a.endpoint_id=$3 AND a.idempotency_key=$4`,
		tenantID, messageID, endpointID, envelope.IdempotencyKey).Scan(&existingID, &lastEvent, &lastAttempt)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return Claim{}, fmt.Errorf("delivery: commit existing attempt: %w", err)
		}
		committed = true
		// A failed provider observation is retryable up to Runner's bound. A
		// submitted or ambiguous observation is a durable stop: retrying it
		// could create a second external effect whose outcome is unknown.
		nextAttempt := int(lastAttempt) + 1
		if nextAttempt < attempt {
			nextAttempt = attempt
		}
		alreadyObserved := lastEvent != "" && lastEvent != string(StateFailed)
		if alreadyObserved {
			nextAttempt = int(maxInt64(lastAttempt, 1))
		}
		return Claim{AttemptID: existingID.String(), Attempt: nextAttempt, AlreadyObserved: alreadyObserved}, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return Claim{}, fmt.Errorf("delivery: find attempt: %w", err)
	}

	now := s.now()
	attemptID := uuid.New()
	provider := strings.TrimSpace(s.Provider)
	if provider == "" {
		provider = "hcmnext.messaging"
	}
	affected, err := tx.Exec(ctx, `
		INSERT INTO delivery_attempt (
			tenant_id, attempt_id, recipient_message_id, endpoint_id, provider,
			idempotency_key, payload_digest, state, submitted_at, deadline_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'QUEUED',$8,$9)
		ON CONFLICT (tenant_id, recipient_message_id, idempotency_key) DO NOTHING`,
		tenantID, attemptID, messageID, endpointID, provider, envelope.IdempotencyKey,
		envelope.ContentDigest, now, envelope.ExpiresAt)
	if err != nil {
		return Claim{}, fmt.Errorf("delivery: insert attempt: %w", err)
	}
	if affected == 0 {
		// Another worker won the unique idempotency race. Re-read its
		// durable attempt rather than returning the locally generated UUID.
		if err := tx.QueryRow(ctx, `
			SELECT attempt_id
			FROM delivery_attempt
			WHERE tenant_id=$1 AND recipient_message_id=$2 AND endpoint_id=$3 AND idempotency_key=$4`,
			tenantID, messageID, endpointID, envelope.IdempotencyKey).Scan(&attemptID); err != nil {
			return Claim{}, fmt.Errorf("delivery: read concurrent attempt: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Claim{}, fmt.Errorf("delivery: commit attempt: %w", err)
	}
	committed = true
	return Claim{AttemptID: attemptID.String(), Attempt: attempt, AlreadyObserved: false}, nil
}

func recipientMessageID(ctx context.Context, q dbport.Querier, tenantID, intentID, endpointID uuid.UUID, recipientRef string) (uuid.UUID, error) {
	var messageID uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT recipient_message_id
		FROM recipient_message
		WHERE tenant_id=$1 AND message_intent_id=$2 AND endpoint_id=$3 AND recipient_ref=$4`,
		tenantID, intentID, endpointID, recipientRef).Scan(&messageID)
	if errors.Is(err, dbport.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("%w: intent=%s recipient=%s endpoint=%s", ErrDeliveryTargetMissing, intentID, recipientRef, endpointID)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("delivery: find recipient message: %w", err)
	}
	return messageID, nil
}

func envelopeIDs(envelope Envelope) (uuid.UUID, uuid.UUID, uuid.UUID, error) {
	tenantID, err := uuid.Parse(envelope.TenantID)
	if err != nil || tenantID == uuid.Nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("%w: tenant_id must be a non-nil UUID", ErrInvalidStoreEnvelope)
	}
	intentID, err := uuid.Parse(envelope.IntentID)
	if err != nil || intentID == uuid.Nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("%w: intent_id must be a non-nil UUID", ErrInvalidStoreEnvelope)
	}
	endpointID, err := uuid.Parse(envelope.EndpointRef)
	if err != nil || endpointID == uuid.Nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, fmt.Errorf("%w: endpoint_ref must be a non-nil UUID", ErrInvalidStoreEnvelope)
	}
	return tenantID, intentID, endpointID, nil
}

func (s PostgresAttemptStore) Observe(ctx context.Context, envelope Envelope, claim Claim, observation Observation) error {
	if s.DB == nil {
		return ErrNoStore
	}
	tenantID, _, _, err := envelopeIDs(envelope)
	if err != nil {
		return err
	}
	attemptID, err := uuid.Parse(claim.AttemptID)
	if err != nil || attemptID == uuid.Nil {
		return fmt.Errorf("%w: attempt_id must be a non-nil UUID", ErrInvalidStoreEnvelope)
	}
	now := s.now()
	providerEventID := observation.ProviderRef
	if providerEventID == "" {
		providerEventID = claim.AttemptID + ":" + fmt.Sprint(claim.Attempt)
	}
	payload := map[string]any{
		"attempt_id": claim.AttemptID,
		"intent_id":  envelope.IntentID,
		"state":      string(observation.State),
	}
	if observation.ProviderRef != "" {
		payload["provider_ref"] = observation.ProviderRef
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("delivery: marshal observation signal: %w", err)
	}
	signalDigest := sha256.Sum256(payloadBytes)
	signalName := "communications.delivery.observed"
	if observation.State == StateFailed {
		signalName = "communications.delivery.failed"
	} else if observation.State == StateReviewRequired {
		signalName = "communications.delivery.review_required"
	}

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return fmt.Errorf("delivery: begin observation: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	receiptID := uuid.New()
	normalized := "UNKNOWN"
	if observation.State == StateFailed {
		normalized = "REJECTED"
	}
	if err := insertReceipt(ctx, tx, tenantID, receiptID, attemptID, providerEventID, observation, normalized, now); err != nil {
		return err
	}
	signal := runtimestate.Signal{
		TenantID: tenantID, SignalID: uuid.New(), SignalName: signalName,
		CorrelationKey: envelope.CorrelationID, DedupeToken: envelope.IdempotencyKey + ":" + string(observation.State),
		SchemaRef: "hcmnext.communications.delivery.observation@1", Payload: payloadBytes,
		PayloadDigest: hex.EncodeToString(signalDigest[:]), DeliveredAt: now,
	}
	// Migration 00156 adds non-blank event/source/correlation fields to the
	// durable signal row. Write the complete current shape here so delivery's
	// receipt and signal remain one transaction rather than relying on the old
	// SignalStore insert's now-invalid empty correlation_value default.
	if affected, err := tx.Exec(ctx, `
		INSERT INTO workflow_signal (
			tenant_id, signal_id, signal_name, event_type, source,
			correlation_key, correlation_value, dedupe_token, schema_ref,
			payload, payload_digest, delivered_at, received_at, sequence_number)
		VALUES ($1, $2, $3, $3, $4, $5, $5, $6, $7, $8::jsonb, $9, $10, $10, 1)
		ON CONFLICT DO NOTHING`,
		signal.TenantID, signal.SignalID, signal.SignalName, "hcmnext.messaging.delivery",
		signal.CorrelationKey, signal.DedupeToken, signal.SchemaRef, string(signal.Payload),
		signal.PayloadDigest, signal.DeliveredAt.UTC()); err != nil {
		return fmt.Errorf("delivery: insert workflow signal: %w", err)
	} else if affected == 0 {
		// A duplicate signal is expected on a replay. The receipt remains the
		// durable observation for this attempt and is itself append-only.
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("delivery: commit observation: %w", err)
	}
	committed = true
	return nil
}

func insertReceipt(ctx context.Context, tx dbport.Tx, tenantID, receiptID, attemptID uuid.UUID, providerEventID string, observation Observation, normalized string, now time.Time) error {
	eventAt := observation.RecordedAt.UTC()
	if eventAt.IsZero() {
		eventAt = now
	}
	if eventAt.After(now) {
		eventAt = now
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO delivery_receipt (
			tenant_id, receipt_id, attempt_id, provider_event_id, event_type,
			event_time, received_at, signature_result, sequence_no, dedupe_state,
			normalized_result, raw_artifact_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'ABSENT',$8,'FIRST',$9,$10)`,
		tenantID, receiptID, attemptID, providerEventID, string(observation.State),
		eventAt, now, maxInt64(int64(observation.Attempt), 1), normalized,
		digestObservation(observation))
	if err != nil {
		return fmt.Errorf("delivery: insert receipt: %w", err)
	}
	return nil
}

func maxInt64(value, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}

func digestObservation(observation Observation) string {
	b, _ := json.Marshal(observation)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
