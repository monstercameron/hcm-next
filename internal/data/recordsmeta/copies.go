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
	// ErrCopyHeld is returned when disposition is attempted against a copy
	// whose hold_state is HELD. It blocks destruction of a held copy the
	// same way ErrHoldBlocksDisposition blocks a declaration-level
	// retention_disposition, only at the granularity of one material copy
	// of any copy_type record_copy_link tracks (RECORDS-HOLD-001 RED: "a
	// held canonical fact must not survive while a related export/
	// provider/backup copy is destroyed").
	ErrCopyHeld = errors.New("recordsmeta: copy is held and cannot be disposed")
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

// matchedReason explains, deterministically, why one copy is gripped by one
// hold: a pure function of the copy's own identity and the hold's scope
// digest, never of row order or which copy was scanned first
// (RECORDS-HOLD-001 REFACTOR: hold matching is deterministic and
// explainable). Two copies with the same copy_type and store_ref matched
// against the same scope digest -- whether under the same declaration or
// two different ones -- always produce byte-identical reasons.
func matchedReason(copyType, storeRef, scopeDigest string) string {
	return fmt.Sprintf("copy_type=%s store_ref=%s matches hold scope digest %s", copyType, storeRef, scopeDigest)
}

// PropagateHold freezes every tracked copy, records one hold_intersection
// per copy naming the exact record_copy_link row it grips, and enqueues one
// stable notification. The caller supplies a transaction that also inserts
// the legal_hold row, so the hold and its propagation are one unit.
//
// It is idempotent: calling it again for the same (tenant, declaration,
// hold) -- a retry, or a genuine concurrent duplicate -- neither creates a
// second intersection per copy (migration 00284's hold_intersection_unique_link
// partial index refuses the duplicate insert, which this function treats as
// "already gripped" rather than an error) nor a second outbox row (outbox's
// own effect-identity dedup).
func PropagateHold(ctx context.Context, tx dbport.Tx, tenantID, declarationID, holdID uuid.UUID, at time.Time) (HoldPropagation, error) {
	if tenantID == uuid.Nil || declarationID == uuid.Nil || holdID == uuid.Nil {
		return HoldPropagation{}, ErrCopyHoldRequired
	}
	if at.IsZero() {
		return HoldPropagation{}, fmt.Errorf("%w: placement time is required", ErrCopyInvalid)
	}
	hold, err := LoadLegalHold(ctx, tx, tenantID, holdID)
	if err != nil {
		return HoldPropagation{}, fmt.Errorf("recordsmeta: load hold for propagation: %w", err)
	}
	// A stable scan order (rather than whatever physical order Postgres
	// happens to return) is what keeps concurrent placements of the same
	// hold from deadlocking against each other or against ReleaseHold: both
	// always acquire their record_copy_link row locks link_id-ascending.
	rows, err := tx.Query(ctx, `SELECT link_id, copy_type, store_ref, artifact_ref, ledger_stream, ledger_sequence, outbox_id, hold_state, disposition_state, exception_reason FROM record_copy_link WHERE tenant_id=$1 AND declaration_id=$2 ORDER BY link_id FOR UPDATE`, tenantID, declarationID)
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
		linkID := link.LinkID
		reason := matchedReason(link.CopyType, link.StoreRef, hold.ScopeDigest)
		var intersectionID uuid.UUID
		newErr := tx.QueryRow(ctx, `
			INSERT INTO hold_intersection (tenant_id, intersection_id, hold_id, declaration_id, link_id, matched_reason, matched_at, state)
			VALUES ($1,$2,$3,$4,$5,$6,$7,'ACTIVE')
			ON CONFLICT (tenant_id, hold_id, declaration_id, link_id) WHERE link_id IS NOT NULL DO NOTHING
			RETURNING intersection_id`,
			tenantID, uuid.New(), holdID, declarationID, linkID, reason, at.UTC()).Scan(&intersectionID)
		grippedNow := true
		if newErr != nil {
			if !errors.Is(newErr, dbport.ErrNoRows) {
				return HoldPropagation{}, fmt.Errorf("recordsmeta: grip copy %s: %w", linkID, newErr)
			}
			// ON CONFLICT ... DO NOTHING: this copy is already gripped by
			// this hold from an earlier or concurrent placement.
			grippedNow = false
		}
		if _, err := tx.Exec(ctx, `UPDATE record_copy_link SET hold_state='HELD', disposition_state='HELD', updated_at=$4 WHERE tenant_id=$1 AND declaration_id=$2 AND link_id=$3`, tenantID, declarationID, link.LinkID, at.UTC()); err != nil {
			return HoldPropagation{}, fmt.Errorf("recordsmeta: hold copy %s: %w", link.LinkID, err)
		}
		if grippedNow {
			if _, err := tx.Exec(ctx, `INSERT INTO record_copy_event (tenant_id, event_id, link_id, declaration_id, event_type, hold_id, detail, recorded_at) VALUES ($1,$2,$3,$4,'HOLD_PLACED',$5,$6,$7)`, tenantID, uuid.New(), link.LinkID, declarationID, holdID, json.RawMessage(`{"hold":"HELD"}`), at.UTC()); err != nil {
				return HoldPropagation{}, fmt.Errorf("recordsmeta: record hold copy %s: %w", link.LinkID, err)
			}
		}
		link.HoldState, link.DispositionState = "HELD", "HELD"
		result.Copies = append(result.Copies, link)
	}
	// The effect identity is scoped by declaration as well as hold: a hold
	// whose scope spans several declarations (RECORDS-HOLD-001's partial
	// release needs exactly this) calls PropagateHold once per declaration,
	// and each one is its own idempotent effect. OutboxID is left for
	// Enqueue to generate -- only EffectIdentity is the dedup key here, and
	// pinning OutboxID to holdID would collide across declarations sharing
	// one hold.
	record, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
		Tenant: tenantID, EffectIdentity: fmt.Sprintf("records.hold.propagated:%s:%s", holdID, declarationID),
		OrderingKey: declarationID.String(), SchemaRef: RecordsCopySchemaRef,
		Payload: []byte(fmt.Sprintf(`{"hold_id":%q,"declaration_id":%q,"copies":%d}`, holdID, declarationID, len(result.Copies))),
	})
	if err != nil {
		return HoldPropagation{}, fmt.Errorf("recordsmeta: enqueue hold propagation: %w", err)
	}
	result.Outbox = record
	return result, nil
}

// DisposeCopy records a disposition action against one tracked copy. It
// refuses to dispose a copy whose hold_state is HELD, whatever its
// copy_type: a hold blocks destruction of a material copy the same way it
// blocks a declaration-level retention_disposition, and
// record_copy_link_hold_blocks_disposal (migration 00284) refuses the same
// row at the schema level if a raw SQL path ever tried it.
func DisposeCopy(ctx context.Context, tx dbport.Tx, tenantID, declarationID, linkID uuid.UUID, at time.Time) (CopyLink, error) {
	if tenantID == uuid.Nil || declarationID == uuid.Nil || linkID == uuid.Nil {
		return CopyLink{}, ErrCopyInvalid
	}
	if err := ensureTenant(ctx, tx, tenantID); err != nil {
		return CopyLink{}, err
	}
	var link CopyLink
	var storedOutboxID *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT link_id, copy_type, store_ref, artifact_ref, ledger_stream, ledger_sequence, outbox_id, hold_state, disposition_state, exception_reason FROM record_copy_link WHERE tenant_id=$1 AND declaration_id=$2 AND link_id=$3 FOR UPDATE`, tenantID, declarationID, linkID).
		Scan(&link.LinkID, &link.CopyType, &link.StoreRef, &link.ArtifactRef, &link.LedgerStream, &link.LedgerSequence, &storedOutboxID, &link.HoldState, &link.DispositionState, &link.ExceptionReason)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return CopyLink{}, ErrCopyNotFound
		}
		return CopyLink{}, fmt.Errorf("recordsmeta: load copy %s for disposition: %w", linkID, err)
	}
	if storedOutboxID != nil {
		link.OutboxID = *storedOutboxID
	}
	link.TenantID, link.DeclarationID = tenantID, declarationID
	if link.HoldState == "HELD" {
		return CopyLink{}, fmt.Errorf("%w: copy %s (%s) is held", ErrCopyHeld, linkID, link.CopyType)
	}
	if _, err := tx.Exec(ctx, `UPDATE record_copy_link SET disposition_state='DISPOSED', updated_at=$4 WHERE tenant_id=$1 AND declaration_id=$2 AND link_id=$3`, tenantID, declarationID, linkID, at.UTC()); err != nil {
		return CopyLink{}, fmt.Errorf("recordsmeta: dispose copy %s: %w", linkID, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO record_copy_event (tenant_id, event_id, link_id, declaration_id, event_type, detail, recorded_at) VALUES ($1,$2,$3,$4,'DISPOSITIONED',$5,$6)`, tenantID, uuid.New(), linkID, declarationID, json.RawMessage(`{"disposition":"DISPOSED"}`), at.UTC()); err != nil {
		return CopyLink{}, fmt.Errorf("recordsmeta: record disposition of copy %s: %w", linkID, err)
	}
	link.DispositionState = "DISPOSED"
	return link, nil
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
