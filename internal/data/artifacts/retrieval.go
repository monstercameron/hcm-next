package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
)

// SubjectScope names which owner records a retrieval is allowed to see an
// artifact through. It is the "subject scope" half of DATA-016's input
// decision; the "purpose" and "classification allow-list" halves live on
// [RetrievalAuthorization] itself.
type SubjectScope struct {
	// AllSubjects grants visibility across every current reference in the
	// tenant, skipping the reference check entirely. A caller sets it only
	// when it already holds tenant-wide authority (an administrator, a
	// repair job) from some other, already-evaluated decision; this package
	// never decides that on its own.
	AllSubjects bool
	// AllowedOwnerRefs is the set of owner ids (see [OwnerRef].ID) the
	// requester's own scope covers. A retrieval is authorized by subject
	// scope when the artifact carries at least one current reference whose
	// owner id appears here.
	AllowedOwnerRefs []string
}

func (s SubjectScope) allows(ownerID string) bool {
	if s.AllSubjects {
		return true
	}
	for _, id := range s.AllowedOwnerRefs {
		if id == ownerID {
			return true
		}
	}
	return false
}

// RetrievalAuthorization is DATA-016's input decision: purpose, a
// classification allow-list and a subject scope, resolved by some other
// component (mirroring the shape of internal/trust/authz.Decision, without
// importing that frozen package) and handed to [Retrieve] as data. [Retrieve]
// enforces it; it never computes who the principal is or what they are
// generally allowed to do.
type RetrievalAuthorization struct {
	// Purpose is the declared purpose of use. Empty is never valid: a
	// retrieval with no stated purpose is refused exactly like one with a
	// purpose the artifact's classification does not allow.
	Purpose string
	// AllowedClassifications is the classification allow-list: the
	// retrieval may see an artifact only when its classification appears
	// here.
	AllowedClassifications []model.ClassificationLabel
	// Scope is the subject scope: see [SubjectScope].
	Scope SubjectScope
	// RequestedBy is the requesting principal's reference, recorded on a
	// refusal so the evidence names who was refused.
	RequestedBy string
	// ExpiresAt, when non-zero, is when this authorization itself expires.
	// A [Retrieve] evaluated at or after ExpiresAt is a stale grant and is
	// refused (DATA-016 RED "stale grant ... is returned incorrectly"),
	// even if every other check would have passed.
	ExpiresAt time.Time
}

func (a RetrievalAuthorization) allowsClassification(c model.ClassificationLabel) bool {
	for _, allowed := range a.AllowedClassifications {
		if allowed == c {
			return true
		}
	}
	return false
}

// Retrieve is the only way this package returns artifact bytes. It enforces
// auth against the artifact identified by contentID and, only when every
// check passes, returns its bytes alongside the artifact's metadata. A denial
// returns no bytes, a typed [ErrRetrievalDenied], and a durable refusal
// record -- whether or not contentID names an artifact that actually exists,
// so a guessed digest is refused and evidenced exactly like a real one that
// simply fails purpose, classification or scope (DATA-016).
//
// Retrieve runs inside the caller's transaction tx, and a successful read
// observes the same snapshot the authorization checks did.
//
// # A denial is not a transaction failure
//
// When Retrieve returns [ErrRetrievalDenied] it has already written the
// refusal row inside tx. That row is durable only if the caller still
// commits tx -- exactly as if a business write had succeeded and the caller
// reflexively rolled it back anyway because some unrelated later step
// returned an error. A caller must not treat ErrRetrievalDenied as a signal
// to roll back; it is a normal, successful outcome of calling Retrieve, and
// rolling back on it silently destroys the DATA-016 evidence this call was
// asked to produce. Roll back only on a different error -- one that reached
// Retrieve before any refusal was recorded, or a genuine storage failure.
func Retrieve(ctx context.Context, tx dbport.Tx, schema string, tenant uuid.UUID, contentID string, auth RetrievalAuthorization, now time.Time) ([]byte, Record, error) {
	deny := func(reason string) ([]byte, Record, error) {
		if refErr := recordRefusal(ctx, tx, schema, tenant, contentID, auth, reason, now); refErr != nil {
			return nil, Record{}, refErr
		}
		return nil, Record{}, ErrRetrievalDenied{ContentID: contentID, Reason: reason}
	}

	if tenant == uuid.Nil {
		return nil, Record{}, ErrRequestInvalid{Field: "Tenant", Reason: "is required"}
	}
	if auth.Purpose == "" {
		return deny("no purpose declared")
	}
	if !auth.ExpiresAt.IsZero() && !now.Before(auth.ExpiresAt) {
		return deny("authorization grant has expired")
	}
	if !ValidContentID(contentID) {
		return deny("no such artifact")
	}

	rec, err := readRecord(ctx, tx, schema, tenant, contentID)
	if err != nil {
		var notFound ErrNotFound
		if errors.As(err, &notFound) {
			return deny("no such artifact")
		}
		return nil, Record{}, err
	}

	if !auth.allowsClassification(rec.Classification) {
		return deny(fmt.Sprintf("classification %s is outside the allowed set", rec.Classification))
	}

	if !auth.Scope.AllSubjects {
		owners, err := activeOwners(ctx, tx, schema, tenant, contentID)
		if err != nil {
			return nil, Record{}, err
		}
		inScope := false
		for _, owner := range owners {
			if auth.Scope.allows(owner.ID) {
				inScope = true
				break
			}
		}
		if !inScope {
			return deny("subject scope does not cover any current reference to this artifact")
		}
	}

	table := pgx.Identifier{schema, "artifact"}.Sanitize()
	var content []byte
	if err := tx.QueryRow(ctx, fmt.Sprintf(`SELECT content FROM %s WHERE tenant_id = $1 AND content_id = $2`, table),
		tenant, contentID).Scan(&content); err != nil {
		return nil, Record{}, fmt.Errorf("artifacts: read bytes of %s: %w", contentID, err)
	}
	sum := sha256.Sum256(content)
	if actual := hex.EncodeToString(sum[:]); actual != rec.ContentID {
		return nil, Record{}, ErrDigestMismatch{Claimed: rec.ContentID, Actual: actual}
	}
	return content, rec, nil
}

func recordRefusal(ctx context.Context, tx dbport.Tx, schema string, tenant uuid.UUID, contentID string, auth RetrievalAuthorization, reason string, at time.Time) error {
	table := pgx.Identifier{schema, "artifact_retrieval_refusal"}.Sanitize()
	_, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (tenant_id, refusal_id, content_id, purpose, requested_by, reason, evidence_id, refused_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, table),
		tenant, uuid.New(), contentID, auth.Purpose, auth.RequestedBy, reason,
		refusalEvidenceID(tenant, contentID, auth.Purpose, reason, auth.RequestedBy, at), at)
	if err != nil {
		return fmt.Errorf("artifacts: record retrieval refusal for %s: %w", contentID, err)
	}
	return nil
}

// refusalEvidenceID computes a deterministic evidence identifier the same way
// internal/trust/authz.Decision.EvidenceID does: a stable prefix naming what
// produced it, plus a digest over exactly the inputs the refusal decision was
// made from, so replaying the identical refusal always yields the identical
// evidence id.
func refusalEvidenceID(tenant uuid.UUID, contentID, purpose, reason, requestedBy string, at time.Time) string {
	h := sha256.New()
	writeField := func(s string) {
		fmt.Fprintf(h, "%d:%s\x00", len(s), s)
	}
	writeField(tenant.String())
	writeField(contentID)
	writeField(purpose)
	writeField(reason)
	writeField(requestedBy)
	writeField(at.UTC().Format(time.RFC3339Nano))
	return "ev:artifact:refusal:" + hex.EncodeToString(h.Sum(nil))[:32]
}
