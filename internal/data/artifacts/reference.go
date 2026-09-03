package artifacts

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// OwnerKind is the class of record that may hold a reference to an artifact
// (DATA-016): a proposal revision, an observation, or a receipt. These are
// exactly the kinds migrations/00010_artifacts.sql's
// artifact_reference_event_owner_kind_allowed CHECK permits.
type OwnerKind string

// The three declared owner kinds. There is no fourth.
const (
	OwnerProposalRevision OwnerKind = "PROPOSAL_REVISION"
	OwnerObservation      OwnerKind = "OBSERVATION"
	OwnerReceipt          OwnerKind = "RECEIPT"
)

// Valid reports whether k is one of the three declared owner kinds.
func (k OwnerKind) Valid() bool {
	switch k {
	case OwnerProposalRevision, OwnerObservation, OwnerReceipt:
		return true
	default:
		return false
	}
}

// referenceAction is the two-valued action an [OwnerRef] performs against an
// artifact's reference log. It is a private type: callers state ADD or
// REMOVE by calling [AddReference] or [RemoveReference], never by
// constructing an event directly.
type referenceAction string

const (
	referenceAdd    referenceAction = "ADD"
	referenceRemove referenceAction = "REMOVE"
)

// OwnerRef identifies the one record that holds (or held) a reference to an
// artifact: proposal_revision.artifact_ref and ledger_event.artifact_ref
// (migrations/00004, 00005) already name a content id from the other side,
// and OwnerRef is the pointer back from the artifact to whichever one of
// those -- or a future observation or receipt record -- did the naming.
// OwnerID is whatever string uniquely identifies that record within its own
// kind (e.g. "<intent_id>:<revision>" for a proposal revision); this package
// does not interpret it.
type OwnerRef struct {
	Kind OwnerKind
	ID   string
}

// Validate rejects an owner reference with an unrecognized kind or no id.
func (o OwnerRef) Validate() error {
	if !o.Kind.Valid() {
		return ErrRequestInvalid{Field: "OwnerKind", Reason: "must be PROPOSAL_REVISION, OBSERVATION or RECEIPT"}
	}
	if o.ID == "" {
		return ErrRequestInvalid{Field: "OwnerID", Reason: "is required"}
	}
	return nil
}

// AddReference records that owner now holds a reference to the artifact
// identified by contentID. It is idempotent: calling it twice for the same
// owner leaves that owner's reference state unchanged (still exactly one
// active reference), because [ReferenceCount] and [activeOwners] both read
// only the most recent event per owner, never a running tally of every event
// ever appended.
//
// AddReference rejects a content id with no artifact on file
// ([ErrNotFound]): [Put] never allows an artifact to be written without its
// retention class, creator principal and evidence populated, so there is no
// way to reach a reference on an artifact that lacks them (MODEL-029 RED
// "reference without retention/authority metadata").
func AddReference(ctx context.Context, tx dbport.Tx, schema string, tenant uuid.UUID, contentID string, owner OwnerRef) error {
	return appendReferenceEvent(ctx, tx, schema, tenant, contentID, owner, referenceAdd)
}

// RemoveReference records that owner no longer holds a reference to the
// artifact identified by contentID. It rejects removing a reference the
// owner does not currently hold ([ErrReferenceNotFound]): REMOVE is not a
// free-standing fact, it only makes sense relative to a prior ADD.
func RemoveReference(ctx context.Context, tx dbport.Tx, schema string, tenant uuid.UUID, contentID string, owner OwnerRef) error {
	active, err := ownerHoldsReference(ctx, tx, schema, tenant, contentID, owner)
	if err != nil {
		return err
	}
	if !active {
		return ErrReferenceNotFound{ContentID: contentID, OwnerKind: owner.Kind, OwnerID: owner.ID}
	}
	return appendReferenceEvent(ctx, tx, schema, tenant, contentID, owner, referenceRemove)
}

func appendReferenceEvent(ctx context.Context, tx dbport.Tx, schema string, tenant uuid.UUID, contentID string, owner OwnerRef, action referenceAction) error {
	if tenant == uuid.Nil {
		return ErrRequestInvalid{Field: "Tenant", Reason: "is required"}
	}
	if !ValidContentID(contentID) {
		return ErrNotFound{ContentID: contentID}
	}
	if err := owner.Validate(); err != nil {
		return err
	}
	// Confirms the artifact exists (and therefore already carries complete
	// identity metadata) before any reference event is recorded against it.
	if _, err := readRecord(ctx, tx, schema, tenant, contentID); err != nil {
		return err
	}

	table := pgx.Identifier{schema, "artifact_reference_event"}.Sanitize()
	_, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (tenant_id, content_id, event_id, owner_kind, owner_id, action)
		VALUES ($1, $2, $3, $4, $5, $6)`, table),
		tenant, contentID, uuid.New(), string(owner.Kind), owner.ID, string(action))
	if err != nil {
		return fmt.Errorf("artifacts: record %s reference event for %s: %w", action, contentID, err)
	}
	return nil
}

func ownerHoldsReference(ctx context.Context, q Querier, schema string, tenant uuid.UUID, contentID string, owner OwnerRef) (bool, error) {
	table := pgx.Identifier{schema, "artifact_reference_event"}.Sanitize()
	var action string
	err := q.QueryRow(ctx, fmt.Sprintf(`
		SELECT action FROM %s
		WHERE tenant_id = $1 AND content_id = $2 AND owner_kind = $3 AND owner_id = $4
		ORDER BY recorded_at DESC, event_id DESC
		LIMIT 1`, table),
		tenant, contentID, string(owner.Kind), owner.ID).Scan(&action)
	if errors.Is(err, dbport.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("artifacts: read reference state of %s for %s %s: %w", contentID, owner.Kind, owner.ID, err)
	}
	return action == string(referenceAdd), nil
}

// activeOwners returns every owner whose most recent reference event against
// contentID is ADD -- the artifact's current reference holders.
func activeOwners(ctx context.Context, q Querier, schema string, tenant uuid.UUID, contentID string) ([]OwnerRef, error) {
	table := pgx.Identifier{schema, "artifact_reference_event"}.Sanitize()
	rows, err := q.Query(ctx, fmt.Sprintf(`
		SELECT DISTINCT ON (owner_kind, owner_id) owner_kind, owner_id, action
		FROM %s
		WHERE tenant_id = $1 AND content_id = $2
		ORDER BY owner_kind, owner_id, recorded_at DESC, event_id DESC`, table),
		tenant, contentID)
	if err != nil {
		return nil, fmt.Errorf("artifacts: read reference state of %s: %w", contentID, err)
	}
	defer rows.Close()

	var owners []OwnerRef
	for rows.Next() {
		var kind, id, action string
		if err := rows.Scan(&kind, &id, &action); err != nil {
			return nil, fmt.Errorf("artifacts: scan reference state of %s: %w", contentID, err)
		}
		if action == string(referenceAdd) {
			owners = append(owners, OwnerRef{Kind: OwnerKind(kind), ID: id})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("artifacts: read reference state of %s: %w", contentID, err)
	}
	return owners, nil
}

// ReferenceCount returns the number of owners currently holding a reference
// to contentID: the count of distinct owners whose most recent event is ADD,
// never a running sum of every ADD and REMOVE ever appended.
func ReferenceCount(ctx context.Context, q Querier, schema string, tenant uuid.UUID, contentID string) (int64, error) {
	owners, err := activeOwners(ctx, q, schema, tenant, contentID)
	if err != nil {
		return 0, err
	}
	return int64(len(owners)), nil
}
