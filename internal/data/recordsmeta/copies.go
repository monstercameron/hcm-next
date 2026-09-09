package recordsmeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
)

// CopyLink is the inventory row tying a material copy to its record
// declaration. Artifact and outbox references are retained as metadata; the
// canonical record remains the declaration and the ledger remains chronology.
type CopyLink struct {
	TenantID         uuid.UUID
	LinkID           uuid.UUID
	DeclarationID    uuid.UUID
	CopyType         string
	StoreRef         string
	ArtifactRef      string
	LedgerStream     string
	LedgerSequence   int64
	OutboxID         uuid.UUID
	HoldState        string
	DispositionState string
	ExceptionReason  string
}

// HoldPropagation reports the durable fan-out of one hold to every known
// copy, including the stable outbox effect identity.
type HoldPropagation struct {
	HoldID        uuid.UUID
	DeclarationID uuid.UUID
	Copies        []CopyLink
	Outbox        outbox.Record
}

const RecordsCopySchemaRef = "hcmnext.records.copy.v1"

var (
	ErrCopyInvalid      = errors.New("recordsmeta: invalid copy link")
	ErrCopyNotFound     = errors.New("recordsmeta: copy link not found")
	ErrCopyHoldRequired = errors.New("recordsmeta: hold propagation requires a hold")
)

// RegisterCopy records one copy and a durable REGISTERED event. It is
// idempotent for the declaration/copy-type/store identity.
func RegisterCopy(ctx context.Context, tx dbport.Tx, link CopyLink) (CopyLink, error) {
	if link.TenantID == uuid.Nil || link.DeclarationID == uuid.Nil || link.CopyType == "" || link.StoreRef == "" {
		return CopyLink{}, ErrCopyInvalid
	}
	if link.LinkID == uuid.Nil {
		link.LinkID = uuid.New()
	}
	if link.HoldState == "" {
		link.HoldState = "NONE"
	}
	if link.DispositionState == "" {
		link.DispositionState = "PENDING"
	}
	result, err := tx.Exec(ctx, `
		INSERT INTO record_copy_link (tenant_id, link_id, declaration_id, copy_type, store_ref, artifact_ref, ledger_stream, ledger_sequence, outbox_id, hold_state, disposition_state)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (tenant_id, declaration_id, copy_type, store_ref) DO NOTHING`,
		link.TenantID, link.LinkID, link.DeclarationID, link.CopyType, link.StoreRef, link.ArtifactRef, link.LedgerStream, link.LedgerSequence, nullableUUID(link.OutboxID), link.HoldState, link.DispositionState)
	if err != nil {
		return CopyLink{}, fmt.Errorf("recordsmeta: register copy: %w", err)
	}
	var storedOutboxID *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT link_id, artifact_ref, ledger_stream, ledger_sequence, outbox_id, hold_state, disposition_state, exception_reason FROM record_copy_link WHERE tenant_id=$1 AND declaration_id=$2 AND copy_type=$3 AND store_ref=$4`, link.TenantID, link.DeclarationID, link.CopyType, link.StoreRef).Scan(&link.LinkID, &link.ArtifactRef, &link.LedgerStream, &link.LedgerSequence, &storedOutboxID, &link.HoldState, &link.DispositionState, &link.ExceptionReason); err != nil {
		return CopyLink{}, fmt.Errorf("recordsmeta: read registered copy: %w", err)
	}
	if storedOutboxID != nil {
		link.OutboxID = *storedOutboxID
	}
	if result == 1 {
		if _, err := tx.Exec(ctx, `INSERT INTO record_copy_event (tenant_id, event_id, link_id, declaration_id, event_type, detail) VALUES ($1,$2,$3,$4,'REGISTERED',$5)`, link.TenantID, uuid.New(), link.LinkID, link.DeclarationID, json.RawMessage(`{}`)); err != nil {
			return CopyLink{}, fmt.Errorf("recordsmeta: record copy registration: %w", err)
		}
	}
	return link, nil
}

// PropagateHold freezes every tracked copy before enqueueing one stable
// notification. The caller supplies a transaction that also inserts the
// legal_hold row, so the hold and its propagation are one unit.
func PropagateHold(ctx context.Context, tx dbport.Tx, tenantID, declarationID, holdID uuid.UUID, at time.Time) (HoldPropagation, error) {
	if tenantID == uuid.Nil || declarationID == uuid.Nil || holdID == uuid.Nil {
		return HoldPropagation{}, ErrCopyHoldRequired
	}
	if at.IsZero() {
		return HoldPropagation{}, fmt.Errorf("%w: placement time is required", ErrCopyInvalid)
	}
	rows, err := tx.Query(ctx, `SELECT link_id, copy_type, store_ref, artifact_ref, ledger_stream, ledger_sequence, outbox_id, hold_state, disposition_state, exception_reason FROM record_copy_link WHERE tenant_id=$1 AND declaration_id=$2 FOR UPDATE`, tenantID, declarationID)
	if err != nil {
		return HoldPropagation{}, fmt.Errorf("recordsmeta: list copies for hold: %w", err)
	}
	result := HoldPropagation{HoldID: holdID, DeclarationID: declarationID}
	var links []CopyLink
	for rows.Next() {
		var link CopyLink
		var storedOutboxID *uuid.UUID
		if err := rows.Scan(&link.LinkID, &link.CopyType, &link.StoreRef, &link.ArtifactRef, &link.LedgerStream, &link.LedgerSequence, &storedOutboxID, &link.HoldState, &link.DispositionState, &link.ExceptionReason); err != nil {
			return HoldPropagation{}, fmt.Errorf("recordsmeta: scan copy for hold: %w", err)
		}
		if storedOutboxID != nil {
			link.OutboxID = *storedOutboxID
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return HoldPropagation{}, fmt.Errorf("recordsmeta: list copies for hold: %w", err)
	}
	rows.Close()
	for _, link := range links {
		if _, err := tx.Exec(ctx, `UPDATE record_copy_link SET hold_state='HELD', disposition_state='HELD', updated_at=$4 WHERE tenant_id=$1 AND declaration_id=$2 AND link_id=$3`, tenantID, declarationID, link.LinkID, at.UTC()); err != nil {
			return HoldPropagation{}, fmt.Errorf("recordsmeta: hold copy %s: %w", link.LinkID, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO record_copy_event (tenant_id, event_id, link_id, declaration_id, event_type, hold_id, detail, recorded_at) VALUES ($1,$2,$3,$4,'HOLD_PLACED',$5,$6,$7)`, tenantID, uuid.New(), link.LinkID, declarationID, holdID, json.RawMessage(`{"hold":"HELD"}`), at.UTC()); err != nil {
			return HoldPropagation{}, fmt.Errorf("recordsmeta: record hold copy %s: %w", link.LinkID, err)
		}
		link.HoldState, link.DispositionState = "HELD", "HELD"
		result.Copies = append(result.Copies, link)
	}
	record, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
		Tenant: tenantID, OutboxID: holdID, EffectIdentity: "records.hold.propagated:" + holdID.String(),
		OrderingKey: declarationID.String(), SchemaRef: RecordsCopySchemaRef,
		Payload: []byte(fmt.Sprintf(`{"hold_id":%q,"declaration_id":%q,"copies":%d}`, holdID, declarationID, len(result.Copies))),
	})
	if err != nil {
		return HoldPropagation{}, fmt.Errorf("recordsmeta: enqueue hold propagation: %w", err)
	}
	result.Outbox = record
	return result, nil
}

// ListCopies returns the complete tracked copy inventory for a declaration in
// deterministic store order.
func ListCopies(ctx context.Context, q dbport.Querier, tenantID, declarationID uuid.UUID) ([]CopyLink, error) {
	rows, err := q.Query(ctx, `SELECT link_id, copy_type, store_ref, artifact_ref, ledger_stream, ledger_sequence, outbox_id, hold_state, disposition_state, exception_reason FROM record_copy_link WHERE tenant_id=$1 AND declaration_id=$2 ORDER BY copy_type, store_ref`, tenantID, declarationID)
	if err != nil {
		return nil, fmt.Errorf("recordsmeta: list copies: %w", err)
	}
	defer rows.Close()
	var result []CopyLink
	for rows.Next() {
		var link CopyLink
		var storedOutboxID *uuid.UUID
		if err := rows.Scan(&link.LinkID, &link.CopyType, &link.StoreRef, &link.ArtifactRef, &link.LedgerStream, &link.LedgerSequence, &storedOutboxID, &link.HoldState, &link.DispositionState, &link.ExceptionReason); err != nil {
			return nil, fmt.Errorf("recordsmeta: scan copy: %w", err)
		}
		link.TenantID, link.DeclarationID = tenantID, declarationID
		if storedOutboxID != nil {
			link.OutboxID = *storedOutboxID
		}
		result = append(result, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recordsmeta: list copies: %w", err)
	}
	return result, nil
}

func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
