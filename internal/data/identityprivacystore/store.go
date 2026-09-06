// Package identityprivacystore persists proofing revisions from migration
// 00096. Pseudonym escrow ciphertext remains behind the custody-service role;
// this adapter only implements the proofing domain repository port.
package identityprivacystore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/proofing"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// DB is the transaction capability required by Store.
type DB interface{ dbport.Beginner }

// Store implements proofing.Repository over PostgreSQL.
type Store struct{ db DB }

var _ proofing.Repository = (*Store)(nil)

// New returns a PostgreSQL identity/privacy store over db.
func New(db DB) *Store { return &Store{db: db} }

type sessionEnvelope struct {
	SubjectTenant string                  `json:"subject_tenant"`
	SubjectKind   string                  `json:"subject_kind"`
	Evidence      []proofing.EvidenceItem `json:"evidence"`
}

type sourceEnvelope struct {
	SubjectTenant string `json:"subject_tenant"`
	SubjectKind   string `json:"subject_kind"`
	SourceRef     string `json:"source_ref"`
}

func invalid(detail string) error {
	return &proofing.StoreError{Code: proofing.StoreInvalidCode, Detail: detail}
}

func notFound(detail string) error {
	return &proofing.StoreError{Code: proofing.StoreNotFoundCode, Detail: detail}
}

func duplicate(detail string) error {
	return &proofing.StoreError{Code: proofing.StoreDuplicateCode, Detail: detail}
}

func stale(expected, actual uint64, detail string) error {
	return &proofing.StoreError{Code: proofing.StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

func parseTenant(tenantID string) (uuid.UUID, error) {
	tid, err := uuid.Parse(tenantID)
	if err != nil || tid == uuid.Nil {
		return uuid.Nil, invalid(fmt.Sprintf("tenant id %q is not a valid uuid", tenantID))
	}
	return tid, nil
}

func subjectUUID(subject values.EntityRef) (uuid.UUID, error) {
	id, err := uuid.Parse(subject.Id)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, invalid(fmt.Sprintf("subject id %q is not a uuid", subject.Id))
	}
	return id, nil
}

func (s *Store) withTenant(ctx context.Context, tenantID uuid.UUID, fn func(dbport.Tx) error) error {
	if s == nil || s.db == nil {
		return invalid("database capability is required")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("identityprivacystore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("identityprivacystore: commit transaction: %w", err)
	}
	return nil
}

// SaveSession appends one immutable proofing-session revision. expectedRevision
// is zero for the first revision and otherwise must name the current tip.
func (s *Store) SaveSession(ctx context.Context, tenantID string, session proofing.ProofingSession, expectedRevision uint64) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if err := session.Validate(); err != nil {
		return invalid(err.Error())
	}
	subjectID, err := subjectUUID(session.Subject)
	if err != nil {
		return err
	}
	envelope, err := json.Marshal(sessionEnvelope{SubjectTenant: session.Subject.Tenant.String(), SubjectKind: session.Subject.Kind.String(), Evidence: session.Evidence})
	if err != nil {
		return invalid(fmt.Sprintf("encode session evidence: %v", err))
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		if err := lock(ctx, tx, tid, "session:"+session.SessionID); err != nil {
			return err
		}
		exists, err := revisionExists(ctx, tx, `SELECT EXISTS (SELECT 1 FROM proofing_session WHERE tenant_id=$1 AND session_id=$2 AND revision=$3)`, tid, session.SessionID, session.Revision)
		if err != nil {
			return err
		}
		if exists {
			return duplicate(fmt.Sprintf("session %s revision %d", session.SessionID, session.Revision))
		}
		actual, found, err := currentSessionRevision(ctx, tx, tid, session.SessionID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO proofing_session (
				row_id, tenant_id, session_id, subject_ref, purpose, target_assurance,
				evidence, verifier_principal, outcome, expires_at, revision,
				supersedes_revision, canonical_digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (tenant_id, session_id, revision) DO NOTHING`,
			uuid.New(), tid, session.SessionID, subjectID, nullableString(session.Purpose), session.Target,
			envelope, nullableString(session.VerifierPrincipal), nullableOutcome(session.Outcome), session.ExpiresAt.Time(), session.Revision,
			nullableRevision(session.SupersedesRevision), storageDigest(session.CanonicalDigest)); err != nil {
			return fmt.Errorf("identityprivacystore: insert session %s/%d: %w", session.SessionID, session.Revision, err)
		}
		if err := checkCAS(expectedRevision, actual, found, session.SupersedesRevision, "session"); err != nil {
			return err
		}
		return nil
	})
}

// LoadSession reads one persisted proofing revision.
func (s *Store) LoadSession(ctx context.Context, tenantID, sessionID string, revision uint64) (proofing.ProofingSession, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return proofing.ProofingSession{}, err
	}
	var out proofing.ProofingSession
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var err error
		out, err = loadSession(ctx, tx, tid, sessionID, revision)
		return err
	})
	return out, err
}

// CurrentSession returns the tip of a session's immutable revision chain.
func (s *Store) CurrentSession(ctx context.Context, tenantID, sessionID string) (proofing.ProofingSession, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return proofing.ProofingSession{}, err
	}
	var out proofing.ProofingSession
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		revision, found, err := currentSessionRevision(ctx, tx, tid, sessionID)
		if err != nil {
			return err
		}
		if !found {
			return notFound(fmt.Sprintf("session %s", sessionID))
		}
		out, err = loadSession(ctx, tx, tid, sessionID, revision)
		return err
	})
	return out, err
}

// ListSessions returns all revisions in ascending revision order.
func (s *Store) ListSessions(ctx context.Context, tenantID, sessionID string) ([]proofing.ProofingSession, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	var out []proofing.ProofingSession
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM proofing_session WHERE tenant_id=$1 AND session_id=$2 ORDER BY revision`, tid, sessionID)
		if err != nil {
			return fmt.Errorf("identityprivacystore: list sessions: %w", err)
		}
		var revisions []uint64
		for rows.Next() {
			var revision uint64
			if err := rows.Scan(&revision); err != nil {
				rows.Close()
				return err
			}
			revisions = append(revisions, revision)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(revisions) == 0 {
			return notFound(fmt.Sprintf("session %s", sessionID))
		}
		for _, revision := range revisions {
			value, err := loadSession(ctx, tx, tid, sessionID, revision)
			if err != nil {
				return err
			}
			out = append(out, value)
		}
		return nil
	})
	return out, err
}

// SaveAuthorization appends one immutable work-authorization evidence revision.
func (s *Store) SaveAuthorization(ctx context.Context, tenantID string, value proofing.WorkAuthorizationEvidence, expectedRevision uint64) error {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return invalid(err.Error())
	}
	subjectID, err := subjectUUID(value.Subject)
	if err != nil {
		return err
	}
	encodedSource, err := encodeSource(value.Subject, value.SourceRef)
	if err != nil {
		return invalid(fmt.Sprintf("encode authorization source: %v", err))
	}
	return s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		if err := lock(ctx, tx, tid, "authorization:"+value.EvidenceID); err != nil {
			return err
		}
		exists, err := revisionExists(ctx, tx, `SELECT EXISTS (SELECT 1 FROM work_authorization_evidence WHERE tenant_id=$1 AND evidence_id=$2 AND revision=$3)`, tid, value.EvidenceID, value.Revision)
		if err != nil {
			return err
		}
		if exists {
			return duplicate(fmt.Sprintf("authorization %s revision %d", value.EvidenceID, value.Revision))
		}
		actual, found, err := currentAuthorizationRevision(ctx, tx, tid, value.EvidenceID)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO work_authorization_evidence (
				row_id, tenant_id, evidence_id, subject_ref, revision,
				supersedes_revision, document_class, verification_method,
				jurisdiction, category, valid_from, valid_until, reverification_due,
				evidence_digest, source_ref, canonical_digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
			ON CONFLICT (tenant_id, evidence_id, revision) DO NOTHING`,
			uuid.New(), tid, value.EvidenceID, subjectID, value.Revision,
			nullableRevision(value.SupersedesRevision), nullableString(string(value.DocumentClass)), nullableString(string(value.VerificationMethod)),
			nullableString(value.Jurisdiction), nullableString(value.Category), dateValue(value.ValidFrom), dateValue(value.ValidUntil), dateValue(value.ReverificationDue),
			storageDigest(value.EvidenceDigest), encodedSource, storageDigest(value.CanonicalDigest)); err != nil {
			return fmt.Errorf("identityprivacystore: insert authorization %s/%d: %w", value.EvidenceID, value.Revision, err)
		}
		return checkCAS(expectedRevision, actual, found, value.SupersedesRevision, "authorization")
	})
}

func (s *Store) LoadAuthorization(ctx context.Context, tenantID, evidenceID string, revision uint64) (proofing.WorkAuthorizationEvidence, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return proofing.WorkAuthorizationEvidence{}, err
	}
	var out proofing.WorkAuthorizationEvidence
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		var err error
		out, err = loadAuthorization(ctx, tx, tid, evidenceID, revision)
		return err
	})
	return out, err
}

func (s *Store) CurrentAuthorization(ctx context.Context, tenantID, evidenceID string) (proofing.WorkAuthorizationEvidence, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return proofing.WorkAuthorizationEvidence{}, err
	}
	var out proofing.WorkAuthorizationEvidence
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		revision, found, err := currentAuthorizationRevision(ctx, tx, tid, evidenceID)
		if err != nil {
			return err
		}
		if !found {
			return notFound(fmt.Sprintf("authorization %s", evidenceID))
		}
		out, err = loadAuthorization(ctx, tx, tid, evidenceID, revision)
		return err
	})
	return out, err
}

func (s *Store) ListAuthorizations(ctx context.Context, tenantID, evidenceID string) ([]proofing.WorkAuthorizationEvidence, error) {
	tid, err := parseTenant(tenantID)
	if err != nil {
		return nil, err
	}
	var out []proofing.WorkAuthorizationEvidence
	err = s.withTenant(ctx, tid, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT revision FROM work_authorization_evidence WHERE tenant_id=$1 AND evidence_id=$2 ORDER BY revision`, tid, evidenceID)
		if err != nil {
			return err
		}
		var revisions []uint64
		for rows.Next() {
			var revision uint64
			if err := rows.Scan(&revision); err != nil {
				rows.Close()
				return err
			}
			revisions = append(revisions, revision)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(revisions) == 0 {
			return notFound(fmt.Sprintf("authorization %s", evidenceID))
		}
		for _, revision := range revisions {
			value, err := loadAuthorization(ctx, tx, tid, evidenceID, revision)
			if err != nil {
				return err
			}
			out = append(out, value)
		}
		return nil
	})
	return out, err
}

func loadSession(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, id string, revision uint64) (proofing.ProofingSession, error) {
	var (
		rowID, storedTenant, subjectID uuid.UUID
		storedID, target, canonical    string
		purpose, verifier, outcome     *string
		expires                        *time.Time
		supersedes                     *uint64
		evidence                       []byte
	)
	err := tx.QueryRow(ctx, `SELECT row_id, tenant_id, session_id, subject_ref, purpose, target_assurance, evidence, verifier_principal, outcome, expires_at, revision, supersedes_revision, canonical_digest FROM proofing_session WHERE tenant_id=$1 AND session_id=$2 AND revision=$3`, tenantID, id, revision).
		Scan(&rowID, &storedTenant, &storedID, &subjectID, &purpose, &target, &evidence, &verifier, &outcome, &expires, &revision, &supersedes, &canonical)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return proofing.ProofingSession{}, notFound(fmt.Sprintf("session %s/%d", id, revision))
		}
		return proofing.ProofingSession{}, fmt.Errorf("identityprivacystore: load session: %w", err)
	}
	var envelope sessionEnvelope
	if len(evidence) > 0 {
		if err := json.Unmarshal(evidence, &envelope); err != nil {
			return proofing.ProofingSession{}, invalid(fmt.Sprintf("stored session evidence: %v", err))
		}
	}
	if envelope.SubjectTenant == "" {
		envelope.SubjectTenant = storedTenant.String()
	}
	if envelope.SubjectKind == "" {
		envelope.SubjectKind = "worker"
	}
	value := proofing.ProofingSession{
		SessionID: storedID, Subject: values.EntityRef{Tenant: values.TenantId(envelope.SubjectTenant), Kind: values.Kind(envelope.SubjectKind), Id: subjectID.String()},
		Evidence: envelope.Evidence, Revision: revision, CanonicalDigest: domainDigest(canonical),
	}
	if purpose != nil {
		value.Purpose = *purpose
	}
	value.Target = proofing.AssuranceLevel(target)
	if verifier != nil {
		value.VerifierPrincipal = *verifier
	}
	if outcome != nil {
		value.Outcome = proofing.ProofingOutcome(*outcome)
	}
	if expires != nil {
		value.ExpiresAt = values.NewInstant(expires.UTC())
	}
	if supersedes != nil {
		value.SupersedesRevision = *supersedes
	}
	if err := value.Validate(); err != nil {
		return proofing.ProofingSession{}, invalid(fmt.Sprintf("stored session failed validation: %v", err))
	}
	return value, nil
}

func loadAuthorization(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, id string, revision uint64) (proofing.WorkAuthorizationEvidence, error) {
	var (
		rowID, storedTenant, subjectID              uuid.UUID
		storedID, evidenceDigest, source, canonical string
		document, method, jurisdiction, category    *string
		validFrom, validUntil, due                  *time.Time
		supersedes                                  *uint64
	)
	err := tx.QueryRow(ctx, `SELECT row_id, tenant_id, evidence_id, subject_ref, revision, supersedes_revision, document_class, verification_method, jurisdiction, category, valid_from, valid_until, reverification_due, evidence_digest, source_ref, canonical_digest FROM work_authorization_evidence WHERE tenant_id=$1 AND evidence_id=$2 AND revision=$3`, tenantID, id, revision).
		Scan(&rowID, &storedTenant, &storedID, &subjectID, &revision, &supersedes, &document, &method, &jurisdiction, &category, &validFrom, &validUntil, &due, &evidenceDigest, &source, &canonical)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return proofing.WorkAuthorizationEvidence{}, notFound(fmt.Sprintf("authorization %s/%d", id, revision))
		}
		return proofing.WorkAuthorizationEvidence{}, fmt.Errorf("identityprivacystore: load authorization: %w", err)
	}
	subjectTenant, subjectKind, sourceRef := decodeSource(source, storedTenant.String())
	value := proofing.WorkAuthorizationEvidence{EvidenceID: storedID, Subject: values.EntityRef{Tenant: values.TenantId(subjectTenant), Kind: values.Kind(subjectKind), Id: subjectID.String()}, Revision: revision, EvidenceDigest: domainDigest(evidenceDigest), CanonicalDigest: domainDigest(canonical), SourceRef: sourceRef}
	if supersedes != nil {
		value.SupersedesRevision = *supersedes
	}
	if document != nil {
		value.DocumentClass = proofing.DocumentClass(*document)
	}
	if method != nil {
		value.VerificationMethod = proofing.VerificationMethod(*method)
	}
	if jurisdiction != nil {
		value.Jurisdiction = *jurisdiction
	}
	if category != nil {
		value.Category = *category
	}
	var dateErr error
	if value.ValidFrom, dateErr = localDate(validFrom); dateErr != nil {
		return proofing.WorkAuthorizationEvidence{}, invalid(dateErr.Error())
	}
	if value.ValidUntil, dateErr = localDate(validUntil); dateErr != nil {
		return proofing.WorkAuthorizationEvidence{}, invalid(dateErr.Error())
	}
	if value.ReverificationDue, dateErr = localDate(due); dateErr != nil {
		return proofing.WorkAuthorizationEvidence{}, invalid(dateErr.Error())
	}
	if err := value.Validate(); err != nil {
		return proofing.WorkAuthorizationEvidence{}, invalid(fmt.Sprintf("stored authorization failed validation: %v", err))
	}
	return value, nil
}

func currentSessionRevision(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, id string) (uint64, bool, error) {
	return currentRevision(ctx, tx, `proofing_session`, `session_id`, tenantID, id)
}

func currentAuthorizationRevision(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, id string) (uint64, bool, error) {
	return currentRevision(ctx, tx, `work_authorization_evidence`, `evidence_id`, tenantID, id)
}

func currentRevision(ctx context.Context, tx dbport.Tx, table, idColumn string, tenantID uuid.UUID, id string) (uint64, bool, error) {
	query := fmt.Sprintf(`SELECT current_row.revision FROM %s AS current_row WHERE current_row.tenant_id=$1 AND current_row.%s=$2 AND NOT EXISTS (SELECT 1 FROM %s AS successor WHERE successor.tenant_id=current_row.tenant_id AND successor.%s=current_row.%s AND successor.supersedes_revision=current_row.revision) ORDER BY current_row.row_id DESC LIMIT 1`, table, idColumn, table, idColumn, idColumn)
	var revision uint64
	if err := tx.QueryRow(ctx, query, tenantID, id).Scan(&revision); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("identityprivacystore: current %s: %w", table, err)
	}
	return revision, true, nil
}

func revisionExists(ctx context.Context, tx dbport.Tx, query string, args ...any) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, query, args...).Scan(&exists); err != nil {
		return false, fmt.Errorf("identityprivacystore: check revision: %w", err)
	}
	return exists, nil
}

func checkCAS(expected, actual uint64, found bool, supersedes uint64, kind string) error {
	if expected == 0 {
		if found {
			return stale(expected, actual, fmt.Sprintf("%s already has current revision %d", kind, actual))
		}
		if supersedes != 0 {
			return invalid(fmt.Sprintf("initial %s cannot supersede a revision", kind))
		}
		return nil
	}
	if !found || actual != expected || supersedes != expected {
		if !found {
			actual = 0
		}
		return stale(expected, actual, fmt.Sprintf("%s current revision is %d", kind, actual))
	}
	return nil
}

func lock(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, key string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, tenantID.String()+":"+key); err != nil {
		return fmt.Errorf("identityprivacystore: lock %s: %w", key, err)
	}
	return nil
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
func nullableOutcome(value proofing.ProofingOutcome) any {
	if value == "" {
		return nil
	}
	return string(value)
}
func nullableRevision(value uint64) any {
	if value == 0 {
		return nil
	}
	return value
}
func dateValue(value values.LocalDate) any { return value.String() }

func encodeSource(subject values.EntityRef, source string) (string, error) {
	payload, err := json.Marshal(sourceEnvelope{SubjectTenant: subject.Tenant.String(), SubjectKind: subject.Kind.String(), SourceRef: source})
	if err != nil {
		return "", err
	}
	return "hcmnext.subject.v1:" + base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeSource(value, fallbackTenant string) (string, string, string) {
	const prefix = "hcmnext.subject.v1:"
	if strings.HasPrefix(value, prefix) {
		payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
		if err == nil {
			var envelope sourceEnvelope
			if json.Unmarshal(payload, &envelope) == nil && envelope.SourceRef != "" {
				return envelope.SubjectTenant, envelope.SubjectKind, envelope.SourceRef
			}
		}
	}
	return fallbackTenant, "worker", value
}

func localDate(value *time.Time) (values.LocalDate, error) {
	if value == nil {
		return values.LocalDate{}, fmt.Errorf("stored date is null")
	}
	return values.NewLocalDate(value.Year(), value.Month(), value.Day())
}

func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}
