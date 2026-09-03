// Package artifacts owns the immutable, content-addressed artifact store and
// its retrieval authorization and reference accounting (owner: data plane;
// phase: P1A; MODEL-029, DATA-016).
//
// # Content addressing
//
// An artifact's identity is the sha256 digest of its own bytes ([Put] and
// [PutStream] compute it; a caller may claim one up front, but a mismatch
// against the actual bytes is rejected before anything is written). Put is
// idempotent by that content id: writing the same bytes twice returns the
// row already on file rather than a second one, and disagreement between the
// two calls' identity metadata (media type, size, classification, retention
// class, creator principal) is rejected as [ErrImmutableConflict] rather than
// silently accepted from whichever call happened to run first. Bytes and
// identity metadata never mutate afterward: there is no update path at all,
// only PostgreSQL's forbid_mutation trigger refusing UPDATE and DELETE
// outright (migrations/00010_artifacts.sql). A correction is not an edit; it
// is a new Put under the new content its different bytes hash to.
//
// # Where the bytes live
//
// The artifact table lives in a companion schema next to whatever schema the
// rest of the migration tree was applied into (see that migration's header
// comment for why). Every function here takes that companion schema name
// explicitly as a schema parameter -- computed once via [Schema] from the
// core schema -- rather than assuming it from the caller's connection
// search_path.
//
// # Retrieval and reference accounting
//
// [Retrieve] is the only way to read bytes back out, and it is a policy
// enforcement point, not a policy engine: it takes a [RetrievalAuthorization]
// that some other component (a purpose declaration, a classification
// allow-list and a subject scope resolved the way internal/trust/authz
// resolves a [Decision]) already computed, and enforces it against the
// artifact's actual classification and current references. A denial returns
// no bytes and records durable refusal evidence (DATA-016) whether or not the
// requested content id even exists, so a guessed digest is refused exactly
// like an out-of-scope one.
//
// [AddReference] and [RemoveReference] record which owner records (proposal
// revisions, observations, receipts) currently hold a reference to a content
// id; [ReferenceCount] and [SweepCandidates] are built on that same append-
// only log, never on a mutable counter column. No deletion is implemented
// anywhere in this package: [SweepCandidates] only lists artifacts that are
// both unreferenced and past their retention window, for a later capability
// to act on.
package artifacts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/intent/model"
)

// Algorithm is the only digest algorithm this store computes or accepts, the
// same identifier internal/data/ledger and migrations/00002's digest_algorithm
// domain use.
const Algorithm = "sha256"

// DefaultMaxContentBytes bounds [Put]. It matches the artifact table's own
// artifact_byte_size_bounded CHECK constraint (migrations/00010_artifacts.sql):
// a caller wanting a smaller cap passes it to [PutStream] directly, but no
// cap larger than the table's own backstop can ever succeed.
const DefaultMaxContentBytes int64 = 64 << 20 // 64 MiB

var contentIDPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidContentID reports whether id is a syntactically well-formed sha256
// content id: exactly 64 lowercase hex characters. It says nothing about
// whether an artifact with that id has ever been written.
func ValidContentID(id string) bool { return contentIDPattern.MatchString(id) }

// Querier is the minimal database capability a read-only call needs: a
// single-row lookup or a multi-row query, over the caller's own transaction or
// connection. A [dbport.Tx] and a [dbport.Conn] both satisfy it.
type Querier = dbport.Querier

// Record is one artifact row: everything about a content id except its bytes.
type Record struct {
	Tenant              uuid.UUID
	ContentID           string
	DigestAlgorithm     string
	MediaType           string
	ByteSize            int64
	Classification      model.ClassificationLabel
	RetentionClass      string
	CreatorPrincipalRef string
	EvidenceID          string
	CreatedAt           time.Time
	RecordedAt          time.Time
}

// PutRequest is one small (already in memory) artifact write.
type PutRequest struct {
	Tenant uuid.UUID
	// Content is the artifact's bytes. Its sha256 digest becomes ContentID.
	Content []byte
	// ContentID, if set, is a caller-claimed content id. It is verified
	// against the computed digest and rejected on mismatch; it is never
	// trusted in place of computing the digest.
	ContentID           string
	MediaType           string
	Classification      model.ClassificationLabel
	RetentionClass      string
	CreatorPrincipalRef string
	EvidenceID          string
}

// PutStreamRequest is one artifact write read incrementally from Reader,
// which is exactly [PutRequest] with Content replaced by a reader: see
// [PutStream] for why a caller with a large or not-yet-fully-buffered source
// wants this instead of [Put].
type PutStreamRequest struct {
	Tenant              uuid.UUID
	Reader              io.Reader
	ContentID           string
	MediaType           string
	Classification      model.ClassificationLabel
	RetentionClass      string
	CreatorPrincipalRef string
	EvidenceID          string
}

// Put writes req.Content as a new artifact, or returns the row already on
// file for the same content id. It is [PutStream] with the content already in
// memory and [DefaultMaxContentBytes] as the cap.
func Put(ctx context.Context, tx dbport.Tx, schema string, req PutRequest) (Record, bool, error) {
	return PutStream(ctx, tx, schema, PutStreamRequest{
		Tenant:              req.Tenant,
		Reader:              bytes.NewReader(req.Content),
		ContentID:           req.ContentID,
		MediaType:           req.MediaType,
		Classification:      req.Classification,
		RetentionClass:      req.RetentionClass,
		CreatorPrincipalRef: req.CreatorPrincipalRef,
		EvidenceID:          req.EvidenceID,
	}, DefaultMaxContentBytes)
}

// PutStream reads req.Reader incrementally, computing its sha256 digest while
// it copies -- never buffering more than maxBytes+1 bytes before recognizing
// an oversize source and refusing it, so an attacker-controlled or merely
// mis-sized reader cannot force this call to buffer an unbounded amount of
// memory before the size cap takes effect. It writes the result as a new
// artifact and reports true, or finds the identical content id already on
// file, verifies the two calls agree on identity metadata, and reports false
// with the original row.
//
// PutStream runs inside the caller's transaction tx and performs no external
// call of any kind.
func PutStream(ctx context.Context, tx dbport.Tx, schema string, req PutStreamRequest, maxBytes int64) (Record, bool, error) {
	if err := validatePutStreamRequest(req, maxBytes); err != nil {
		return Record{}, false, err
	}

	hasher := sha256.New()
	limited := io.LimitReader(req.Reader, maxBytes+1)
	content, err := io.ReadAll(io.TeeReader(limited, hasher))
	if err != nil {
		return Record{}, false, fmt.Errorf("artifacts: read content: %w", err)
	}
	if int64(len(content)) > maxBytes {
		return Record{}, false, ErrContentTooLarge{Limit: maxBytes}
	}
	computed := hex.EncodeToString(hasher.Sum(nil))
	if req.ContentID != "" && req.ContentID != computed {
		return Record{}, false, ErrDigestMismatch{Claimed: req.ContentID, Actual: computed}
	}

	rec := Record{
		Tenant:              req.Tenant,
		ContentID:           computed,
		DigestAlgorithm:     Algorithm,
		MediaType:           req.MediaType,
		ByteSize:            int64(len(content)),
		Classification:      req.Classification,
		RetentionClass:      req.RetentionClass,
		CreatorPrincipalRef: req.CreatorPrincipalRef,
		EvidenceID:          req.EvidenceID,
	}

	table := pgx.Identifier{schema, "artifact"}.Sanitize()
	affected, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			tenant_id, content_id, digest_algorithm, media_type, byte_size,
			classification, retention_class, creator_principal_ref, evidence_id, content
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (tenant_id, content_id) DO NOTHING`, table),
		rec.Tenant, rec.ContentID, rec.DigestAlgorithm, rec.MediaType, rec.ByteSize,
		string(rec.Classification), rec.RetentionClass, rec.CreatorPrincipalRef, rec.EvidenceID, content)
	if err != nil {
		return Record{}, false, fmt.Errorf("artifacts: put %s: %w", rec.ContentID, err)
	}

	existing, err := readRecord(ctx, tx, schema, rec.Tenant, rec.ContentID)
	if err != nil {
		return Record{}, false, err
	}
	if affected == 1 {
		return existing, true, nil
	}
	if field := identityMismatch(existing, rec); field != "" {
		return Record{}, false, ErrImmutableConflict{ContentID: rec.ContentID, Field: field}
	}
	return existing, false, nil
}

// identityMismatch returns the name of the first identity field on which want
// disagrees with have, or "" when every field this store treats as immutable
// identity agrees. EvidenceID is deliberately excluded: it names the evidence
// for *this* write attempt, and an idempotent replay is expected to carry its
// own evidence trail even when it changes nothing about the artifact itself.
func identityMismatch(have, want Record) string {
	switch {
	case have.MediaType != want.MediaType:
		return "media_type"
	case have.ByteSize != want.ByteSize:
		return "byte_size"
	case have.Classification != want.Classification:
		return "classification"
	case have.RetentionClass != want.RetentionClass:
		return "retention_class"
	case have.CreatorPrincipalRef != want.CreatorPrincipalRef:
		return "creator_principal_ref"
	default:
		return ""
	}
}

func validatePutStreamRequest(req PutStreamRequest, maxBytes int64) error {
	if req.Tenant == uuid.Nil {
		return ErrRequestInvalid{Field: "Tenant", Reason: "is required"}
	}
	if req.Reader == nil {
		return ErrRequestInvalid{Field: "Reader", Reason: "is required"}
	}
	if maxBytes <= 0 {
		return ErrRequestInvalid{Field: "maxBytes", Reason: "must be positive"}
	}
	if req.ContentID != "" && !ValidContentID(req.ContentID) {
		return ErrRequestInvalid{Field: "ContentID", Reason: "must be a 64 character lowercase hex sha256 digest"}
	}
	if req.MediaType == "" {
		return ErrRequestInvalid{Field: "MediaType", Reason: "is required"}
	}
	if !req.Classification.Valid() {
		return ErrRequestInvalid{Field: "Classification", Reason: "must be one of model.ClassificationLabel's declared labels"}
	}
	if req.RetentionClass == "" {
		return ErrRequestInvalid{Field: "RetentionClass", Reason: "is required; an artifact with no retention class can never be referenced"}
	}
	if req.CreatorPrincipalRef == "" {
		return ErrRequestInvalid{Field: "CreatorPrincipalRef", Reason: "is required; an artifact with no creator authority can never be referenced"}
	}
	if req.EvidenceID == "" {
		return ErrRequestInvalid{Field: "EvidenceID", Reason: "is required"}
	}
	return nil
}

const recordColumns = `tenant_id, content_id, digest_algorithm, media_type, byte_size,
	classification, retention_class, creator_principal_ref, evidence_id, created_at, recorded_at`

// scanner is the minimal capability both [dbport.Row] (QueryRow) and
// [dbport.Rows] (Query, one row at a time via Next) share, the same technique
// internal/data/outbox.scanRecord uses.
type scanner interface {
	Scan(dest ...any) error
}

func scanRecord(row scanner) (Record, error) {
	var rec Record
	var classification string
	if err := row.Scan(
		&rec.Tenant, &rec.ContentID, &rec.DigestAlgorithm, &rec.MediaType, &rec.ByteSize,
		&classification, &rec.RetentionClass, &rec.CreatorPrincipalRef, &rec.EvidenceID,
		&rec.CreatedAt, &rec.RecordedAt,
	); err != nil {
		return Record{}, err
	}
	rec.Classification = model.ClassificationLabel(classification)
	return rec, nil
}

// readRecord returns one artifact's metadata by tenant and content id.
func readRecord(ctx context.Context, q Querier, schema string, tenant uuid.UUID, contentID string) (Record, error) {
	table := pgx.Identifier{schema, "artifact"}.Sanitize()
	row := q.QueryRow(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE tenant_id = $1 AND content_id = $2`, recordColumns, table),
		tenant, contentID)
	rec, err := scanRecord(row)
	if errors.Is(err, dbport.ErrNoRows) {
		return Record{}, ErrNotFound{ContentID: contentID}
	}
	if err != nil {
		return Record{}, fmt.Errorf("artifacts: read %s: %w", contentID, err)
	}
	return rec, nil
}

// Read returns one artifact's metadata without its bytes and without any
// authorization check. It exists for callers that already hold their own
// authority over the whole tenant (a migration, a repair job, an admin
// report) and explicitly do not want [Retrieve]'s DATA-016 enforcement or its
// refusal evidence; ordinary product code retrieving bytes for a purpose
// always goes through [Retrieve] instead, which calls this internally after
// its own checks pass.
func Read(ctx context.Context, q Querier, schema string, tenant uuid.UUID, contentID string) (Record, error) {
	if !ValidContentID(contentID) {
		return Record{}, ErrNotFound{ContentID: contentID}
	}
	return readRecord(ctx, q, schema, tenant, contentID)
}
