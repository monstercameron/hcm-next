package provenance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/gen/wire"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/outbox"
)

// recordNamespace derives deterministic record IDs, the same pattern
// internal/intent/app/pgstore.tenantNamespace uses to derive tenant IDs from
// slugs: the same (tenant, source kind, source ref) triple always names the
// same record ID, in this process and after a restart, with no registry to
// keep in sync.
var recordNamespace = uuid.MustParse("2f6a6f0e-6a8b-4b7b-9a2e-9a6a3a2d5b41")

func recordID(tenant uuid.UUID, kind SourceKind, sourceRef string) uuid.UUID {
	return uuid.NewSHA1(recordNamespace, []byte(tenant.String()+"|"+string(kind)+"|"+sourceRef))
}

// OutboxSchemaRef is the payload_schema this package's outbox message
// registers under. A caller bootstrapping a tenant for this package must
// register it (message_full_name 'google.protobuf.Struct', wire_format
// 'PROTOBUF', canonicalization_profile 'EVIDENCE_MANIFEST') the same way it
// registers every other schema_ref an internal/data/outbox row cites
// (migrations/00006, outbox_schema foreign key) - see fixtures_test.go for
// the exact statement.
const OutboxSchemaRef = "hcmnext.provenance.v1.record@1"

// EffectIdentityPrefix names the outbox effect-identity family Publish
// enqueues under: "provenance.published:<record id>".
const EffectIdentityPrefix = "provenance.published:"

// Publish validates req, records one immutable provenance_record row and
// enqueues one "provenance.published" internal/data/outbox message, inside
// the caller's transaction. It never appends to the ledger itself -
// req describes provenance for an event or observation the caller already
// persisted - so it takes no internal/data/ledger.Appender; a caller
// publishing provenance for a fresh append composes Publish with its own
// ledger.Append or internal/data/outbox.Commit call in the same
// transaction. See the package doc for the full DATA-014 contract.
func Publish(ctx context.Context, tx dbport.Tx, req PublishRequest) (Record, error) {
	if req.Tenant == uuid.Nil {
		return Record{}, ErrRequestInvalid{Field: "Tenant", Reason: "is required"}
	}
	if !req.SourceKind.Valid() {
		return Record{}, ErrInvalidSourceKind{SourceKind: req.SourceKind}
	}
	if req.SourceRef == "" {
		return Record{}, ErrRequestInvalid{Field: "SourceRef", Reason: "is required"}
	}
	if req.IntentRef == "" {
		return Record{}, ErrRequestInvalid{Field: "IntentRef", Reason: "is required"}
	}
	if req.SourceAuthority == "" {
		return Record{}, ErrRequestInvalid{Field: "SourceAuthority", Reason: "is required"}
	}
	if req.PrincipalRef == "" {
		return Record{}, ErrRequestInvalid{Field: "PrincipalRef", Reason: "is required"}
	}
	if len(req.EvidenceIDs) == 0 {
		return Record{}, ErrMissingEvidence{SourceKind: req.SourceKind, SourceRef: req.SourceRef}
	}
	for _, id := range req.EvidenceIDs {
		if id == "" {
			return Record{}, ErrMissingEvidence{SourceKind: req.SourceKind, SourceRef: req.SourceRef}
		}
	}
	if len(req.Digests) == 0 {
		return Record{}, ErrMissingDigest{SourceKind: req.SourceKind, SourceRef: req.SourceRef}
	}
	for _, d := range req.Digests {
		if d.Kind == "" || d.Algorithm == "" || d.Digest == "" {
			return Record{}, ErrMissingDigest{SourceKind: req.SourceKind, SourceRef: req.SourceRef}
		}
	}

	publishedAt := req.PublishedAt
	if publishedAt.IsZero() {
		publishedAt = time.Now().UTC()
	}

	id := recordID(req.Tenant, req.SourceKind, req.SourceRef)
	digestsJSON, err := json.Marshal(req.Digests)
	if err != nil {
		return Record{}, fmt.Errorf("provenance: marshal digests: %w", err)
	}

	var (
		streamKey      *string
		sequence       *int64
		eventID        *uuid.UUID
		observationRef *string
		connectorRef   *string
	)
	if req.StreamKey != "" {
		streamKey = &req.StreamKey
	}
	if req.Sequence != 0 {
		sequence = &req.Sequence
	}
	if req.EventID != uuid.Nil {
		eventID = &req.EventID
	}
	if req.ObservationRef != "" {
		observationRef = &req.ObservationRef
	}
	if req.ConnectorRef != "" {
		connectorRef = &req.ConnectorRef
	}

	affected, err := tx.Exec(ctx, `
		INSERT INTO provenance_record (
			tenant_id, record_id, source_kind, source_ref, intent_ref,
			stream_key, sequence, event_id, observation_ref, connector_ref,
			source_authority, principal_ref, evidence_ids, digests, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (tenant_id, record_id) DO NOTHING`,
		req.Tenant, id, string(req.SourceKind), req.SourceRef, req.IntentRef,
		streamKey, sequence, eventID, observationRef, connectorRef,
		req.SourceAuthority, req.PrincipalRef, req.EvidenceIDs, digestsJSON, publishedAt)
	if err != nil {
		return Record{}, fmt.Errorf("provenance: publish %s %s: %w", req.SourceKind, req.SourceRef, err)
	}

	var record Record
	if affected == 1 {
		record = Record{
			Tenant: req.Tenant, RecordID: id, IntentRef: req.IntentRef,
			SourceKind: req.SourceKind, SourceRef: req.SourceRef,
			StreamKey: req.StreamKey, Sequence: req.Sequence, EventID: req.EventID,
			ObservationRef: req.ObservationRef, ConnectorRef: req.ConnectorRef,
			SourceAuthority: req.SourceAuthority, PrincipalRef: req.PrincipalRef,
			EvidenceIDs: req.EvidenceIDs, Digests: req.Digests,
			PublishedAt: publishedAt, Published: true,
		}
	} else {
		existing, err := readRecord(ctx, tx, req.Tenant, id)
		if err != nil {
			return Record{}, err
		}
		existing.Published = false
		record = existing
	}

	payload, err := marshalOutboxPayload(record)
	if err != nil {
		return Record{}, err
	}
	if _, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
		Tenant:         req.Tenant,
		OutboxID:       id,
		EffectIdentity: EffectIdentityPrefix + id.String(),
		OrderingKey:    req.IntentRef,
		SchemaRef:      OutboxSchemaRef,
		Payload:        payload,
	}); err != nil {
		return Record{}, fmt.Errorf("provenance: enqueue provenance.published for %s: %w", id, err)
	}

	return record, nil
}

// marshalOutboxPayload projects record through gen/wire's temporary Struct
// adapter. The data package owns the record facts; the generated-wire tree
// owns protobuf construction and serialization.
func marshalOutboxPayload(r Record) ([]byte, error) {
	digests := make([]any, len(r.Digests))
	for i, d := range r.Digests {
		digests[i] = map[string]any{"kind": d.Kind, "algorithm": d.Algorithm, "digest": d.Digest}
	}
	evidence := make([]any, len(r.EvidenceIDs))
	for i, id := range r.EvidenceIDs {
		evidence[i] = id
	}
	fields := map[string]any{
		"record_id":        r.RecordID.String(),
		"intent_ref":       r.IntentRef,
		"source_kind":      string(r.SourceKind),
		"source_ref":       r.SourceRef,
		"source_authority": r.SourceAuthority,
		"principal_ref":    r.PrincipalRef,
		"evidence_ids":     evidence,
		"digests":          digests,
		"published_at":     r.PublishedAt.UTC().Format(time.RFC3339Nano),
	}
	if r.StreamKey != "" {
		fields["stream_key"] = r.StreamKey
		fields["sequence"] = float64(r.Sequence)
	}
	if r.EventID != uuid.Nil {
		fields["event_id"] = r.EventID.String()
	}
	if r.ObservationRef != "" {
		fields["observation_ref"] = r.ObservationRef
	}
	if r.ConnectorRef != "" {
		fields["connector_ref"] = r.ConnectorRef
	}

	b, err := wire.MarshalStruct(fields)
	if err != nil {
		return nil, fmt.Errorf("provenance: marshal outbox payload: %w", err)
	}
	return b, nil
}

func readRecord(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) dbport.Row
}, tenant uuid.UUID, id uuid.UUID) (Record, error) {
	return scanRecord(q.QueryRow(ctx, selectRecordSQL+" WHERE tenant_id = $1 AND record_id = $2", tenant, id))
}

const selectRecordSQL = `
	SELECT tenant_id, record_id, source_kind, source_ref, intent_ref,
		stream_key, sequence, event_id, observation_ref, connector_ref,
		source_authority, principal_ref, evidence_ids, digests, published_at, recorded_at
	FROM provenance_record`

func scanRecord(row dbport.Row) (Record, error) {
	var (
		r              Record
		sourceKind     string
		streamKey      *string
		sequence       *int64
		eventID        *uuid.UUID
		observationRef *string
		connectorRef   *string
		digestsJSON    []byte
	)
	if err := row.Scan(
		&r.Tenant, &r.RecordID, &sourceKind, &r.SourceRef, &r.IntentRef,
		&streamKey, &sequence, &eventID, &observationRef, &connectorRef,
		&r.SourceAuthority, &r.PrincipalRef, &r.EvidenceIDs, &digestsJSON, &r.PublishedAt, &r.RecordedAt,
	); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return Record{}, fmt.Errorf("provenance: record not found")
		}
		return Record{}, fmt.Errorf("provenance: read record: %w", err)
	}
	r.SourceKind = SourceKind(sourceKind)
	if streamKey != nil {
		r.StreamKey = *streamKey
	}
	if sequence != nil {
		r.Sequence = *sequence
	}
	if eventID != nil {
		r.EventID = *eventID
	}
	if observationRef != nil {
		r.ObservationRef = *observationRef
	}
	if connectorRef != nil {
		r.ConnectorRef = *connectorRef
	}
	if len(digestsJSON) > 0 {
		if err := json.Unmarshal(digestsJSON, &r.Digests); err != nil {
			return Record{}, fmt.Errorf("provenance: decode digests: %w", err)
		}
	}
	return r, nil
}
