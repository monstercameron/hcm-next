// DOC-MAL-001: the PostgreSQL-backed implementation of
// internal/domains/asset/quarantine.Store, persisting into the two tables
// migrations/00030_artifact_quarantine.sql adds to this package's own
// companion artifact-store schema (see that migration's header comment for
// why quarantine intake lives beside, but separate from, this file's own
// [Put]-owned `artifact` table).
//
// # Two tables, one append-only discipline
//
// `artifact_quarantine` is quarantine's own byte storage: one immutable row
// per (tenant, content id), written once by [RecordQuarantined] and never
// touched again -- the same forbid_mutation discipline this package's own
// `artifact` table uses. `artifact_quarantine_state` is the append-only
// verdict log: [RecordQuarantined] appends the initial QUARANTINED fact in
// the same call that writes the bytes, and [RecordVerdict] appends exactly
// one more row, ADMITTED or REJECTED, once quarantine.Upload reaches a
// decision. [CurrentQuarantineState] always reads the most recently
// appended row for a content id, never a mutable status column.
package artifacts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset/quarantine"
)

// QuarantineStore adapts one transaction and companion schema to
// [quarantine.Store], the persistence port internal/domains/asset/quarantine
// declares and speaks exclusively in its own types. It carries no tenant of
// its own: every [quarantine.Store] method receives its tenant as a string,
// which this adapter parses as a UUID before delegating to this file's own
// functions (the same explicit-schema, explicit-tenant style [Put],
// [AddReference] and [Retrieve] already use).
type QuarantineStore struct {
	Tx     dbport.Tx
	Schema string
}

var _ quarantine.Store = QuarantineStore{}

func (s QuarantineStore) RecordQuarantined(ctx context.Context, tenant string, rec quarantine.QuarantinedRecord) error {
	tid, err := parseQuarantineTenant(tenant)
	if err != nil {
		return err
	}
	return RecordQuarantined(ctx, s.Tx, s.Schema, tid, rec)
}

func (s QuarantineStore) RecordVerdict(ctx context.Context, tenant string, rec quarantine.VerdictRecord) error {
	tid, err := parseQuarantineTenant(tenant)
	if err != nil {
		return err
	}
	return RecordVerdict(ctx, s.Tx, s.Schema, tid, rec)
}

func (s QuarantineStore) CurrentState(ctx context.Context, tenant, contentID string) (quarantine.StateRecord, error) {
	tid, err := parseQuarantineTenant(tenant)
	if err != nil {
		return quarantine.StateRecord{}, err
	}
	return CurrentQuarantineState(ctx, s.Tx, s.Schema, tid, contentID)
}

func parseQuarantineTenant(tenant string) (uuid.UUID, error) {
	tid, err := uuid.Parse(tenant)
	if err != nil {
		return uuid.Nil, fmt.Errorf("artifacts: quarantine tenant %q is not a uuid: %w", tenant, err)
	}
	if tid == uuid.Nil {
		return uuid.Nil, ErrRequestInvalid{Field: "Tenant", Reason: "is required"}
	}
	return tid, nil
}

// quarantineRow is `artifact_quarantine`'s identity metadata, read back to
// decide whether a [RecordQuarantined] replay is an idempotent no-op or an
// identity conflict.
type quarantineRow struct {
	ByteSize            int64
	DeclaredContentType string
	SniffedContentType  string
	CreatorPrincipalRef string
}

// RecordQuarantined writes rec as the initial QUARANTINED fact for its
// content id: one row in `artifact_quarantine` holding the bytes, and one
// row in `artifact_quarantine_state` recording that the content was
// quarantined, both in the same transaction. Replaying an identical
// (tenant, content id, byte size, declared type, sniffed type, creator) is a
// no-op -- neither table gains a second row -- exactly like [Put]'s own
// idempotent-by-content-id behavior; replaying the same content id with any
// of those fields changed is refused as [ErrImmutableConflict].
func RecordQuarantined(ctx context.Context, tx dbport.Tx, schema string, tenant uuid.UUID, rec quarantine.QuarantinedRecord) error {
	if err := validateQuarantinedRecord(rec); err != nil {
		return err
	}

	table := pgx.Identifier{schema, "artifact_quarantine"}.Sanitize()
	affected, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			tenant_id, content_id, digest_algorithm, byte_size,
			declared_content_type, sniffed_content_type, creator_principal_ref,
			evidence_id, content
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (tenant_id, content_id) DO NOTHING`, table),
		tenant, rec.ContentID, rec.DigestAlgorithm, rec.ByteSize,
		string(rec.DeclaredContentType), string(rec.SniffedCategory), rec.CreatorPrincipalRef,
		rec.EvidenceID, rec.Content)
	if err != nil {
		return fmt.Errorf("artifacts: record quarantined %s: %w", rec.ContentID, err)
	}

	if affected == 1 {
		return insertQuarantineState(ctx, tx, schema, tenant, rec.ContentID, quarantine.Quarantined, "", "", "", rec.EvidenceID)
	}

	existing, err := readQuarantineRow(ctx, tx, schema, tenant, rec.ContentID)
	if err != nil {
		return err
	}
	if field := quarantineIdentityMismatch(existing, rec); field != "" {
		return ErrImmutableConflict{ContentID: rec.ContentID, Field: field}
	}
	return nil
}

// RecordVerdict appends rec -- always ADMITTED or REJECTED -- to the verdict
// log for a content id [RecordQuarantined] already recorded. It reports
// [ErrNotFound] when no such content id has ever been quarantined for this
// tenant; migrations/00030_artifact_quarantine.sql's own foreign key would
// refuse the insert anyway, but this check reports the same
// [quarantine.ErrNotFound] shape [quarantine.Store]'s contract documents,
// rather than a raw constraint-violation error.
func RecordVerdict(ctx context.Context, tx dbport.Tx, schema string, tenant uuid.UUID, rec quarantine.VerdictRecord) error {
	if rec.State != quarantine.Admitted && rec.State != quarantine.Rejected {
		return fmt.Errorf("artifacts: record quarantine verdict: state must be ADMITTED or REJECTED, got %q", rec.State)
	}
	if rec.ScannerID == "" || rec.ScannerVersion == "" {
		return ErrRequestInvalid{Field: "ScannerID/ScannerVersion", Reason: "a verdict must name the scanner that produced it"}
	}
	if rec.State == quarantine.Rejected && rec.Reason == "" {
		return ErrRequestInvalid{Field: "Reason", Reason: "a REJECTED verdict must carry a non-empty reason"}
	}
	if rec.EvidenceID == "" {
		return ErrRequestInvalid{Field: "EvidenceID", Reason: "is required"}
	}
	if _, err := readQuarantineRow(ctx, tx, schema, tenant, rec.ContentID); err != nil {
		return err
	}
	return insertQuarantineState(ctx, tx, schema, tenant, rec.ContentID, rec.State, rec.ScannerID, rec.ScannerVersion, rec.Reason, rec.EvidenceID)
}

// CurrentQuarantineState returns the most recently recorded
// `artifact_quarantine_state` row for contentID, or [quarantine.ErrNotFound]
// when [RecordQuarantined] has never been called for it. "Most recently
// recorded" is decided by `seq`, an identity column, not by `recorded_at`:
// RecordQuarantined's initial QUARANTINED row and the verdict row that
// follows it are usually written in the same transaction and therefore
// share one `now()`-derived `recorded_at`, so only the strictly increasing
// `seq` reflects the true append order (migrations/00030_artifact_quarantine.sql's
// own comment on the table explains this in full).
func CurrentQuarantineState(ctx context.Context, q Querier, schema string, tenant uuid.UUID, contentID string) (quarantine.StateRecord, error) {
	table := pgx.Identifier{schema, "artifact_quarantine_state"}.Sanitize()
	row := q.QueryRow(ctx, fmt.Sprintf(`
		SELECT state, scanner_id, scanner_version, reason, evidence_id, recorded_at
		FROM %s
		WHERE tenant_id = $1 AND content_id = $2
		ORDER BY seq DESC
		LIMIT 1`, table),
		tenant, contentID)

	var state, scannerID, scannerVersion, reason, evidenceID string
	var recordedAt time.Time
	err := row.Scan(&state, &scannerID, &scannerVersion, &reason, &evidenceID, &recordedAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return quarantine.StateRecord{}, quarantine.ErrNotFound{ContentID: contentID}
	}
	if err != nil {
		return quarantine.StateRecord{}, fmt.Errorf("artifacts: read quarantine state of %s: %w", contentID, err)
	}
	return quarantine.StateRecord{
		ContentID:      contentID,
		State:          quarantine.State(state),
		ScannerID:      scannerID,
		ScannerVersion: scannerVersion,
		Reason:         reason,
		EvidenceID:     evidenceID,
		RecordedAt:     recordedAt,
	}, nil
}

func insertQuarantineState(ctx context.Context, tx dbport.Tx, schema string, tenant uuid.UUID, contentID string, state quarantine.State, scannerID, scannerVersion, reason, evidenceID string) error {
	table := pgx.Identifier{schema, "artifact_quarantine_state"}.Sanitize()
	_, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (tenant_id, content_id, state_id, state, scanner_id, scanner_version, reason, evidence_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, table),
		tenant, contentID, uuid.New(), string(state), scannerID, scannerVersion, reason, evidenceID)
	if err != nil {
		return fmt.Errorf("artifacts: record quarantine %s state %s: %w", contentID, state, err)
	}
	return nil
}

func readQuarantineRow(ctx context.Context, q Querier, schema string, tenant uuid.UUID, contentID string) (quarantineRow, error) {
	table := pgx.Identifier{schema, "artifact_quarantine"}.Sanitize()
	row := q.QueryRow(ctx, fmt.Sprintf(`
		SELECT byte_size, declared_content_type, sniffed_content_type, creator_principal_ref
		FROM %s WHERE tenant_id = $1 AND content_id = $2`, table),
		tenant, contentID)

	var r quarantineRow
	err := row.Scan(&r.ByteSize, &r.DeclaredContentType, &r.SniffedContentType, &r.CreatorPrincipalRef)
	if errors.Is(err, dbport.ErrNoRows) {
		return quarantineRow{}, quarantine.ErrNotFound{ContentID: contentID}
	}
	if err != nil {
		return quarantineRow{}, fmt.Errorf("artifacts: read quarantine %s: %w", contentID, err)
	}
	return r, nil
}

// quarantineIdentityMismatch returns the name of the first identity field on
// which have (the row already on file) disagrees with want (a replayed
// [RecordQuarantined] call), or "" when they agree on every field this store
// treats as immutable identity.
func quarantineIdentityMismatch(have quarantineRow, want quarantine.QuarantinedRecord) string {
	switch {
	case have.ByteSize != want.ByteSize:
		return "byte_size"
	case have.DeclaredContentType != string(want.DeclaredContentType):
		return "declared_content_type"
	case have.SniffedContentType != string(want.SniffedCategory):
		return "sniffed_content_type"
	case have.CreatorPrincipalRef != want.CreatorPrincipalRef:
		return "creator_principal_ref"
	default:
		return ""
	}
}

func validateQuarantinedRecord(rec quarantine.QuarantinedRecord) error {
	if !ValidContentID(rec.ContentID) {
		return ErrRequestInvalid{Field: "ContentID", Reason: "must be a 64 character lowercase hex sha256 digest"}
	}
	if rec.ByteSize != int64(len(rec.Content)) {
		return ErrRequestInvalid{Field: "ByteSize", Reason: "must match len(Content)"}
	}
	if rec.DeclaredContentType == "" {
		return ErrRequestInvalid{Field: "DeclaredContentType", Reason: "is required"}
	}
	if rec.SniffedCategory == "" {
		return ErrRequestInvalid{Field: "SniffedCategory", Reason: "is required"}
	}
	if rec.CreatorPrincipalRef == "" {
		return ErrRequestInvalid{Field: "CreatorPrincipalRef", Reason: "is required"}
	}
	if rec.EvidenceID == "" {
		return ErrRequestInvalid{Field: "EvidenceID", Reason: "is required"}
	}
	return nil
}
