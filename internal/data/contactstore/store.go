package contactstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/contact"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DB is the driver-free transaction capability required by Store.
type DB interface{ dbport.Beginner }

// Store implements contact.Store over migration 00080.
type Store struct{ db DB }

var _ contact.Store = (*Store)(nil)

// New returns a PostgreSQL contact store over db.
func New(db DB) *Store { return &Store{db: db} }

// Code and Error are aliases to the domain port's stable typed failure
// vocabulary, making the adapter convenient to inspect without duplicating it.
type Code = contact.StoreErrorCode
type Error = contact.StoreError

const (
	CodeInvalid        = contact.StoreInvalidCode
	CodeNotFound       = contact.StoreNotFoundCode
	CodeDuplicate      = contact.StoreDuplicateCode
	CodeDuplicateEvent = contact.StoreDuplicateEventCode
	CodeStaleCAS       = contact.StoreStaleCASCode
	CodeDatabase       = contact.StoreDatabaseCode
)

// CodeOf returns the stable code carried by a contact persistence error.
func CodeOf(err error) Code { return contact.CodeOf(err) }

func (s *Store) withTenant(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx, uuid.UUID) error) error {
	if s == nil || s.db == nil {
		return failure(contact.StoreInvalidCode, "database is nil", contact.ErrStoreInvalid)
	}
	if err := tenant.Validate(); err != nil {
		return failure(contact.StoreInvalidCode, "tenant is invalid", err)
	}
	tenantID, err := uuid.Parse(tenant.String())
	if err != nil || tenantID == uuid.Nil || tenantID.String() != tenant.String() {
		return failure(contact.StoreInvalidCode, "tenant must be a canonical UUID", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return failure(contact.StoreDatabaseCode, "begin transaction", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return failure(contact.StoreDatabaseCode, "set tenant", err)
	}
	if err := fn(tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return failure(contact.StoreDatabaseCode, "commit transaction", err)
	}
	return nil
}

// PutEndpointRevision appends exactly the next immutable endpoint revision.
// An omitted expected revision is valid only for the first revision; later
// writes must name the current revision as a compare-and-set value.
func (s *Store) PutEndpointRevision(ctx context.Context, tenant values.TenantId, revision contact.ContactEndpointRevision, expected ...uint64) error {
	if err := revision.Validate(); err != nil {
		return failure(contact.StoreInvalidCode, "endpoint revision: "+err.Error(), err)
	}
	if revision.Subject.Tenant != tenant {
		return failure(contact.StoreInvalidCode, "endpoint subject tenant differs from store tenant", contact.ErrStoreInvalid)
	}
	if len(expected) > 1 {
		return failure(contact.StoreInvalidCode, "at most one expected revision is allowed", contact.ErrStoreInvalid)
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		endpointID, err := parseUUID(revision.EndpointID, "endpoint id")
		if err != nil {
			return err
		}
		subjectID, err := parseUUID(revision.Subject.Id, "subject id")
		if err != nil {
			return err
		}
		latest, err := latestEndpointRevision(ctx, tx, tenantID, endpointID)
		if err != nil {
			return err
		}
		if revision.Revision <= latest {
			return duplicate("endpoint revision already exists")
		}
		if len(expected) == 0 {
			if latest != 0 {
				return stale("", latest)
			}
		} else if expected[0] != latest || revision.Revision != latest+1 || revision.SupersedesRevision != latest {
			return stale(fmt.Sprintf("%d", expected[0]), latest)
		}
		if latest == 0 && (revision.Revision != 1 || revision.SupersedesRevision != 0) {
			return failure(contact.StoreInvalidCode, "initial endpoint revision must start at one", contact.ErrStoreInvalid)
		}
		verification, err := json.Marshal(map[string]string{
			"state":        string(revision.Verification),
			"subject_kind": revision.Subject.Kind.String(),
		})
		if err != nil {
			return failure(contact.StoreInvalidCode, "verification metadata", err)
		}
		var supersedes any
		if revision.SupersedesRevision != 0 {
			supersedes = int64(revision.SupersedesRevision)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO contact_endpoint_revision (
				row_id, tenant_id, subject_ref, endpoint_id, revision,
				supersedes_revision, kind, purpose, priority, source,
				normalized_value_digest, display_hint, verification, canonical_digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			uuid.New(), tenantID, subjectID, endpointID, int64(revision.Revision), supersedes,
			string(revision.Kind), revision.Purpose, revision.Priority, revision.Source,
			storageDigest(revision.NormalizedValueDigest), revision.DisplayHint, verification,
			storageDigest(revision.CanonicalDigest))
		if err != nil {
			return mapWriteError(contact.StoreDuplicateCode, "endpoint revision", err)
		}
		return nil
	})
}

func latestEndpointRevision(ctx context.Context, q dbport.Querier, tenantID, endpointID uuid.UUID) (uint64, error) {
	var latest int64
	err := q.QueryRow(ctx, `SELECT COALESCE(MAX(revision),0) FROM contact_endpoint_revision WHERE tenant_id=$1 AND endpoint_id=$2`, tenantID, endpointID).Scan(&latest)
	if err != nil {
		return 0, failure(contact.StoreDatabaseCode, "load endpoint revision", err)
	}
	return uint64(latest), nil
}

// GetEndpointRevision loads one immutable endpoint revision.
func (s *Store) GetEndpointRevision(ctx context.Context, tenant values.TenantId, id string, revision uint64) (contact.ContactEndpointRevision, error) {
	var out contact.ContactEndpointRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		endpointID, err := parseUUID(id, "endpoint id")
		if err != nil {
			return err
		}
		var (
			rowID, subjectID                     uuid.UUID
			storedRevision                       int64
			supersedes                           *int64
			kind, purpose, normalized, canonical string
			source, display                      *string
			priority                             *int
			verification                         []byte
			subjectKind                          string
		)
		err = tx.QueryRow(ctx, `
			SELECT row_id, subject_ref, revision, supersedes_revision, kind, purpose,
				priority, source, normalized_value_digest, display_hint, verification,
				canonical_digest, COALESCE(verification->>'subject_kind','worker')
			FROM contact_endpoint_revision
			WHERE tenant_id=$1 AND endpoint_id=$2 AND revision=$3`,
			tenantID, endpointID, int64(revision)).Scan(
			&rowID, &subjectID, &storedRevision, &supersedes, &kind, &purpose,
			&priority, &source, &normalized, &display, &verification, &canonical, &subjectKind)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("endpoint revision")
		}
		if err != nil {
			return failure(contact.StoreDatabaseCode, "load endpoint revision", err)
		}
		state, metadataErr := verificationState(verification)
		if metadataErr != nil {
			return failure(contact.StoreDatabaseCode, "decode verification metadata", metadataErr)
		}
		if priority == nil {
			zero := 0
			priority = &zero
		}
		out = contact.ContactEndpointRevision{
			Subject:    values.EntityRef{Tenant: tenant, Kind: values.Kind(subjectKind), Id: subjectID.String()},
			EndpointID: id, Revision: uint64(storedRevision), Kind: contact.EndpointType(kind),
			Purpose: purpose, Priority: *priority, Source: deref(source),
			NormalizedValueDigest: domainDigest(normalized), DisplayHint: deref(display),
			Verification: state, CanonicalDigest: domainDigest(canonical),
		}
		if supersedes != nil {
			out.SupersedesRevision = uint64(*supersedes)
		}
		if err := out.Validate(); err != nil {
			return failure(contact.StoreDatabaseCode, "rehydrated endpoint revision", err)
		}
		_ = rowID
		return nil
	})
	return out, err
}

// ListEndpointRevisions loads the complete immutable endpoint history in
// ascending revision order.
func (s *Store) ListEndpointRevisions(ctx context.Context, tenant values.TenantId, id string) ([]contact.ContactEndpointRevision, error) {
	var revisions []uint64
	if err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		endpointID, err := parseUUID(id, "endpoint id")
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT revision FROM contact_endpoint_revision WHERE tenant_id=$1 AND endpoint_id=$2 ORDER BY revision`, tenantID, endpointID)
		if err != nil {
			return failure(contact.StoreDatabaseCode, "list endpoint revisions", err)
		}
		defer rows.Close()
		for rows.Next() {
			var revision int64
			if err := rows.Scan(&revision); err != nil {
				return failure(contact.StoreDatabaseCode, "scan endpoint revision", err)
			}
			revisions = append(revisions, uint64(revision))
		}
		if err := rows.Err(); err != nil {
			return failure(contact.StoreDatabaseCode, "list endpoint revisions", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, notFound("endpoint revision")
	}
	out := make([]contact.ContactEndpointRevision, 0, len(revisions))
	for _, revision := range revisions {
		item, err := s.GetEndpointRevision(ctx, tenant, id, revision)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

// PutChallenge creates or CAS-updates current challenge state and appends any
// new events in the same transaction. Existing event rows are never rewritten.
func (s *Store) PutChallenge(ctx context.Context, tenant values.TenantId, challenge contact.ContactVerificationChallenge, expected ...string) error {
	if err := challenge.Validate(); err != nil {
		return failure(contact.StoreInvalidCode, "challenge: "+err.Error(), err)
	}
	if challenge.Subject.Tenant != tenant {
		return failure(contact.StoreInvalidCode, "challenge subject tenant differs from store tenant", contact.ErrStoreInvalid)
	}
	if len(expected) > 1 {
		return failure(contact.StoreInvalidCode, "at most one expected digest is allowed", contact.ErrStoreInvalid)
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		return s.putChallengeTx(ctx, tx, tenantID, tenant, challenge, expected...)
	})
}

func (s *Store) putChallengeTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, tenant values.TenantId, challenge contact.ContactVerificationChallenge, expected ...string) error {
	challengeID, err := parseUUID(challenge.ChallengeID, "challenge id")
	if err != nil {
		return err
	}
	subjectID, err := parseUUID(challenge.Subject.Id, "subject id")
	if err != nil {
		return err
	}
	endpointID, err := parseUUID(challenge.EndpointID, "endpoint id")
	if err != nil {
		return err
	}
	var rowID uuid.UUID
	var currentDigest string
	err = tx.QueryRow(ctx, `SELECT row_id, canonical_digest FROM contact_verification_challenge WHERE tenant_id=$1 AND challenge_id=$2`, tenantID, challengeID).Scan(&rowID, &currentDigest)
	if errors.Is(err, dbport.ErrNoRows) {
		if len(expected) == 1 && expected[0] != "" {
			return stale(expected[0], 0)
		}
		rowID = uuid.New()
		if err := insertChallenge(ctx, tx, tenantID, rowID, challengeID, subjectID, endpointID, challenge); err != nil {
			return err
		}
		return insertEvents(ctx, tx, tenantID, rowID, challenge.Events, 1)
	}
	if err != nil {
		return failure(contact.StoreDatabaseCode, "load challenge", err)
	}
	if len(expected) == 0 || storageDigest(expected[0]) != currentDigest {
		return &contact.StoreError{Code: contact.StoreStaleCASCode, Detail: "challenge canonical digest changed", Expected: firstExpected(expected), Actual: domainDigest(currentDigest), Cause: contact.ErrStoreStaleCAS}
	}
	existing, err := loadEvents(ctx, tx, tenantID, rowID)
	if err != nil {
		return err
	}
	if len(challenge.Events) < len(existing) {
		return failure(contact.StoreStaleCASCode, "challenge event history regressed", contact.ErrStoreStaleCAS)
	}
	for i := range existing {
		if challenge.Events[i].Digest != existing[i].Digest {
			return failure(contact.StoreStaleCASCode, "challenge event history was rewritten", contact.ErrStoreStaleCAS)
		}
	}
	if err := updateChallenge(ctx, tx, tenantID, rowID, challenge, currentDigest); err != nil {
		return err
	}
	return insertEvents(ctx, tx, tenantID, rowID, challenge.Events[len(existing):], len(existing)+1)
}

func insertChallenge(ctx context.Context, tx dbport.Tx, tenantID, rowID, challengeID, subjectID, endpointID uuid.UUID, challenge contact.ContactVerificationChallenge) error {
	var endpointDigest any
	if challenge.EndpointRevisionDigest != "" {
		endpointDigest = storageDigest(challenge.EndpointRevisionDigest)
	}
	var tokenDigest any
	if challenge.TokenDigest != "" {
		tokenDigest = storageDigest(challenge.TokenDigest)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO contact_verification_challenge (
			row_id, tenant_id, challenge_id, subject_ref, endpoint_id,
			endpoint_revision_digest, normalized_value_digest, purpose,
			issued_at, expires_at, attempt_budget, attempts, token_digest,
			status, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		rowID, tenantID, challengeID, subjectID, endpointID, endpointDigest,
		storageDigest(challenge.NormalizedValueDigest), challenge.Purpose,
		challenge.IssuedAt.UTC(), challenge.ExpiresAt.UTC(), challenge.AttemptBudget,
		challenge.Attempts, tokenDigest, string(challenge.Status), storageDigest(challenge.CanonicalDigest))
	if err != nil {
		return mapWriteError(contact.StoreDuplicateCode, "challenge", err)
	}
	return nil
}

func updateChallenge(ctx context.Context, tx dbport.Tx, tenantID, rowID uuid.UUID, challenge contact.ContactVerificationChallenge, currentDigest string) error {
	_, err := tx.Exec(ctx, `
		UPDATE contact_verification_challenge
		SET attempts=$1, status=$2, canonical_digest=$3
		WHERE tenant_id=$4 AND row_id=$5 AND canonical_digest=$6`,
		challenge.Attempts, string(challenge.Status), storageDigest(challenge.CanonicalDigest), tenantID, rowID, currentDigest)
	if err != nil {
		return mapWriteError(contact.StoreStaleCASCode, "challenge", err)
	}
	return nil
}

func insertEvents(ctx context.Context, tx dbport.Tx, tenantID, rowID uuid.UUID, events []contact.ContactChallengeEvent, firstSequence int) error {
	for index, event := range events {
		if err := event.Validate(); err != nil {
			return failure(contact.StoreInvalidCode, "challenge event", err)
		}
		var answer any
		if event.AnswerDigest != "" {
			answer = storageDigest(event.AnswerDigest)
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO contact_challenge_event (
				row_id, tenant_id, challenge_ref, kind, at, attempt,
				answer_digest, event_sequence, digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
			uuid.New(), tenantID, rowID, string(event.Kind), event.At.UTC(), event.Attempt,
			answer, firstSequence+index, storageDigest(event.Digest))
		if err != nil {
			return mapWriteError(contact.StoreDuplicateEventCode, "challenge event", err)
		}
	}
	return nil
}

// GetChallenge loads current challenge state and its complete immutable event
// history, then validates the reconstructed domain object and stored digest.
func (s *Store) GetChallenge(ctx context.Context, tenant values.TenantId, id string) (contact.ContactVerificationChallenge, error) {
	var out contact.ContactVerificationChallenge
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		challengeID, err := parseUUID(id, "challenge id")
		if err != nil {
			return err
		}
		var (
			rowID, subjectID, endpointID uuid.UUID
			endpointDigest, tokenDigest  *string
			normalizedDigest, canonical  string
			purpose, status, subjectKind string
			issuedAt, expiresAt          time.Time
			budget, attempts             int
		)
		err = tx.QueryRow(ctx, `
			SELECT c.row_id, c.subject_ref, c.endpoint_id, c.endpoint_revision_digest,
				c.normalized_value_digest, c.purpose, c.issued_at, c.expires_at,
				c.attempt_budget, c.attempts, c.token_digest, c.status, c.canonical_digest,
				COALESCE((SELECT e.verification->>'subject_kind'
				          FROM contact_endpoint_revision e
				          WHERE e.tenant_id=c.tenant_id AND e.endpoint_id=c.endpoint_id
				            AND e.canonical_digest=c.endpoint_revision_digest
				          LIMIT 1), 'worker')
			FROM contact_verification_challenge c
			WHERE c.tenant_id=$1 AND c.challenge_id=$2`, tenantID, challengeID).Scan(
			&rowID, &subjectID, &endpointID, &endpointDigest, &normalizedDigest, &purpose,
			&issuedAt, &expiresAt, &budget, &attempts, &tokenDigest, &status, &canonical, &subjectKind)
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound("challenge")
		}
		if err != nil {
			return failure(contact.StoreDatabaseCode, "load challenge", err)
		}
		events, err := loadEvents(ctx, tx, tenantID, rowID)
		if err != nil {
			return err
		}
		out = contact.ContactVerificationChallenge{
			ChallengeID: id, Subject: values.EntityRef{Tenant: tenant, Kind: values.Kind(subjectKind), Id: subjectID.String()},
			EndpointID: endpointID.String(), EndpointRevisionDigest: domainDigest(deref(endpointDigest)),
			NormalizedValueDigest: domainDigest(normalizedDigest), Purpose: purpose,
			IssuedAt: issuedAt.UTC(), ExpiresAt: expiresAt.UTC(), AttemptBudget: budget,
			Attempts: attempts, TokenDigest: domainDigest(deref(tokenDigest)), Status: contact.ContactChallengeStatus(status),
			Events: events, CanonicalDigest: domainDigest(canonical),
		}
		if err := out.Validate(); err != nil {
			return failure(contact.StoreDatabaseCode, "rehydrated challenge", err)
		}
		return nil
	})
	return out, err
}

func loadEvents(ctx context.Context, q dbport.Querier, tenantID, rowID uuid.UUID) ([]contact.ContactChallengeEvent, error) {
	rows, err := q.Query(ctx, `
		SELECT kind, at, attempt, answer_digest, digest
		FROM contact_challenge_event
		WHERE tenant_id=$1 AND challenge_ref=$2 ORDER BY event_sequence`, tenantID, rowID)
	if err != nil {
		return nil, failure(contact.StoreDatabaseCode, "load challenge events", err)
	}
	defer rows.Close()
	var out []contact.ContactChallengeEvent
	for rows.Next() {
		var event contact.ContactChallengeEvent
		var answer *string
		var digest string
		if err := rows.Scan(&event.Kind, &event.At, &event.Attempt, &answer, &digest); err != nil {
			return nil, failure(contact.StoreDatabaseCode, "scan challenge event", err)
		}
		event.AnswerDigest = domainDigest(deref(answer))
		event.Digest = domainDigest(digest)
		event.At = event.At.UTC()
		out = append(out, event)
	}
	if err := rows.Err(); err != nil {
		return nil, failure(contact.StoreDatabaseCode, "load challenge events", err)
	}
	return out, nil
}

// ListChallengeEvents returns the permanent event history in sequence order.
func (s *Store) ListChallengeEvents(ctx context.Context, tenant values.TenantId, id string) ([]contact.ContactChallengeEvent, error) {
	challenge, err := s.GetChallenge(ctx, tenant, id)
	if err != nil {
		return nil, err
	}
	return append([]contact.ContactChallengeEvent(nil), challenge.Events...), nil
}

func parseUUID(value, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return uuid.Nil, failure(contact.StoreInvalidCode, field+" must be a canonical UUID", err)
	}
	return id, nil
}

func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" {
		return ""
	}
	return "sha256:" + strings.TrimPrefix(value, "sha256:")
}

func verificationState(raw []byte) (contact.VerificationState, error) {
	var metadata struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return "", err
	}
	state := contact.VerificationState(metadata.State)
	if state != contact.Unverified && state != contact.Verified {
		return "", fmt.Errorf("unknown verification state %q", metadata.State)
	}
	return state, nil
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func failure(code contact.StoreErrorCode, detail string, cause error) error {
	return &contact.StoreError{Code: code, Detail: detail, Cause: cause}
}

func duplicate(detail string) error {
	return failure(contact.StoreDuplicateCode, detail, contact.ErrStoreDuplicate)
}

func notFound(detail string) error {
	return failure(contact.StoreNotFoundCode, detail, contact.ErrStoreNotFound)
}

func stale(expected string, actual uint64) error {
	return &contact.StoreError{Code: contact.StoreStaleCASCode, Detail: fmt.Sprintf("expected %q, actual %d", expected, actual), Expected: expected, Actual: fmt.Sprintf("%d", actual), Cause: contact.ErrStoreStaleCAS}
}

func firstExpected(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func mapWriteError(code contact.StoreErrorCode, detail string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return failure(code, detail, contact.ErrStoreDuplicate)
	}
	return failure(contact.StoreDatabaseCode, detail, err)
}
