package attestationstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/attestation"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the transaction capability the adapter needs. It intentionally keeps
// the PostgreSQL driver behind internal/data/dbport.
type DB interface{ dbport.Beginner }

// Store implements attestation.Store over PostgreSQL.
type Store struct{ db DB }

var _ attestation.Store = (*Store)(nil)

// New returns a PostgreSQL-backed attestation store.
func New(db DB) *Store { return &Store{db: db} }

func (s *Store) withTenant(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx) error) error {
	tenantID, err := uuid.Parse(tenant.String())
	if err != nil || tenantID == uuid.Nil {
		return invalid("tenant", tenant.String(), "tenant must be a non-nil UUID")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return failure(CodeDatabase, "transaction", "begin", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return failure(CodeDatabase, "tenant", tenant.String(), err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return failure(CodeDatabase, "transaction", "commit", err)
	}
	return nil
}

// PutStatement appends the next immutable statement revision. An optional
// expected version is a CAS fence; when omitted the current version is used.
func (s *Store) PutStatement(ctx context.Context, tenant values.TenantId, stmt attestation.AttestationStatement, expectedVersion ...uint64) error {
	if err := stmt.Validate(); err != nil {
		return failure(CodeInvalid, "attestation_statement", stmt.ID, err)
	}
	if len(expectedVersion) > 1 {
		return invalid("attestation_statement", stmt.ID, "at most one expected version is allowed")
	}
	expected := uint64(0)
	if len(expectedVersion) == 1 {
		expected = expectedVersion[0]
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		latest, err := latestVersion(ctx, tx, tenantID(tenant), "attestation_statement", "statement_id", "version", stmt.ID)
		if err != nil {
			return err
		}
		if stmt.Version <= latest {
			return failure(CodeDuplicateRevision, "attestation_statement", stmt.ID,
				attestation.ErrStatementDuplicate)
		}
		if expected != latest || stmt.Version != latest+1 {
			return failure(CodeVersionConflict, "attestation_statement", stmt.ID, attestation.ErrVersionConflict)
		}
		subject, err := uuid.Parse(stmt.SubjectRef.Id)
		if err != nil {
			return invalid("attestation_statement", stmt.ID, "subject_ref must be a UUID")
		}
		jurisdiction, err := uuid.Parse(stmt.JurisdictionRef.Id)
		if err != nil {
			return invalid("attestation_statement", stmt.ID, "jurisdiction_ref must be a UUID")
		}
		attester, err := json.Marshal(statementAttesterPayload{
			AttesterPrincipal:  stmt.Attester,
			SubjectTenant:      string(stmt.SubjectRef.Tenant),
			SubjectKind:        string(stmt.SubjectRef.Kind),
			JurisdictionTenant: string(stmt.JurisdictionRef.Tenant),
			JurisdictionKind:   string(stmt.JurisdictionRef.Kind),
		})
		if err != nil {
			return failure(CodeInvalid, "attestation_statement", stmt.ID, err)
		}
		evidence, err := json.Marshal(stmt.EvidenceRefs)
		if err != nil {
			return failure(CodeInvalid, "attestation_statement", stmt.ID, err)
		}
		var revocation any
		if stmt.RevocationLink != nil {
			revocation, err = json.Marshal(stmt.RevocationLink)
			if err != nil {
				return failure(CodeInvalid, "attestation_statement", stmt.ID, err)
			}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO attestation_statement (
				tenant_id, row_id, statement_id, version, kind, subject_ref,
				attester, text_digest, evidence_refs, validity_from, validity_to,
				jurisdiction_ref, revocation_link, digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			tenantID(tenant), uuid.New(), stmt.ID, int64(stmt.Version), string(stmt.Kind), subject,
			attester, storageDigest(stmt.TextDigest), evidence, stmt.ValidityWindow.StartsAt.Time(), stmt.ValidityWindow.ExpiresAt.Time(),
			jurisdiction, revocation, storageDigest(stmt.Digest()))
		if err != nil {
			return mapWriteError("attestation_statement", stmt.ID, err, attestation.ErrStatementDuplicate, CodeDuplicateRevision)
		}
		return nil
	})
}

// SaveStatement is a descriptive alias for PutStatement.
func (s *Store) SaveStatement(ctx context.Context, tenant values.TenantId, stmt attestation.AttestationStatement, expectedVersion ...uint64) error {
	return s.PutStatement(ctx, tenant, stmt, expectedVersion...)
}

// GetStatement loads one statement revision.
func (s *Store) GetStatement(ctx context.Context, tenant values.TenantId, id string, version uint64) (attestation.AttestationStatement, error) {
	var out attestation.AttestationStatement
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		var (
			subject, jurisdiction          uuid.UUID
			attester, evidence, revocation []byte
			kind, textDigest, digest       string
			storedVersion                  int64
			validFrom, validTo             *time.Time
		)
		err := tx.QueryRow(ctx, `
			SELECT statement_id, version, kind, subject_ref, attester, text_digest,
				evidence_refs, validity_from, validity_to, jurisdiction_ref,
				revocation_link, digest
			FROM attestation_statement
			WHERE tenant_id=$1 AND statement_id=$2 AND version=$3`, tenantID(tenant), id, int64(version)).Scan(
			&out.ID, &storedVersion, &kind, &subject, &attester, &textDigest, &evidence,
			&validFrom, &validTo, &jurisdiction, &revocation, &digest)
		if errors.Is(err, dbport.ErrNoRows) {
			return failure(CodeNotFound, "attestation_statement", id, attestation.ErrStatementNotFound)
		}
		if err != nil {
			return failure(CodeDatabase, "attestation_statement", id, err)
		}
		var payload statementAttesterPayload
		if err := json.Unmarshal(attester, &payload); err != nil {
			return failure(CodeDatabase, "attestation_statement", id, err)
		}
		if err := json.Unmarshal(evidence, &out.EvidenceRefs); err != nil {
			return failure(CodeDatabase, "attestation_statement", id, err)
		}
		out.Version = uint64(storedVersion)
		out.Kind = attestation.StatementKind(kind)
		out.SubjectRef = values.EntityRef{Tenant: values.TenantId(payload.SubjectTenant), Kind: values.Kind(payload.SubjectKind), Id: subject.String()}
		out.Attester = payload.AttesterPrincipal
		out.TextDigest = domainDigest(textDigest)
		if validFrom != nil && validTo != nil {
			out.ValidityWindow = attestation.ValidityWindow{StartsAt: values.NewInstant(*validFrom), ExpiresAt: values.NewInstant(*validTo)}
		}
		out.JurisdictionRef = values.EntityRef{Tenant: values.TenantId(payload.JurisdictionTenant), Kind: values.Kind(payload.JurisdictionKind), Id: jurisdiction.String()}
		if len(revocation) > 0 && string(revocation) != "null" {
			var link attestation.RevocationLink
			if err := json.Unmarshal(revocation, &link); err != nil {
				return failure(CodeDatabase, "attestation_statement", id, err)
			}
			out.RevocationLink = &link
		}
		if storageDigest(out.Digest()) != digest {
			return failure(CodeDatabase, "attestation_statement", id, fmt.Errorf("stored digest does not match rehydrated statement"))
		}
		return nil
	})
	return out, err
}

// LoadStatement is a descriptive alias for GetStatement.
func (s *Store) LoadStatement(ctx context.Context, tenant values.TenantId, id string, version uint64) (attestation.AttestationStatement, error) {
	return s.GetStatement(ctx, tenant, id, version)
}

// ListStatementVersions returns statement revisions in ascending order.
func (s *Store) ListStatementVersions(ctx context.Context, tenant values.TenantId, id string) ([]attestation.AttestationStatement, error) {
	var versions []uint64
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT version FROM attestation_statement WHERE tenant_id=$1 AND statement_id=$2 ORDER BY version`, tenantID(tenant), id)
		if err != nil {
			return failure(CodeDatabase, "attestation_statement", id, err)
		}
		defer rows.Close()
		for rows.Next() {
			var version int64
			if err := rows.Scan(&version); err != nil {
				return failure(CodeDatabase, "attestation_statement", id, err)
			}
			versions = append(versions, uint64(version))
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, failure(CodeNotFound, "attestation_statement", id, attestation.ErrStatementNotFound)
	}
	out := make([]attestation.AttestationStatement, 0, len(versions))
	for _, version := range versions {
		stmt, err := s.GetStatement(ctx, tenant, id, version)
		if err != nil {
			return nil, err
		}
		out = append(out, stmt)
	}
	return out, nil
}

// AppendBinding appends exactly the next binding version.
func (s *Store) AppendBinding(ctx context.Context, tenant values.TenantId, binding attestation.Binding) error {
	if err := binding.Validate(); err != nil {
		return failure(CodeInvalid, "attestation_binding", binding.StatementID, err)
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		latest, err := latestVersion(ctx, tx, tenantID(tenant), "attestation_binding", "statement_id", "binding_version", binding.StatementID)
		if err != nil {
			return err
		}
		if binding.BindingVersion <= latest {
			return failure(CodeDuplicateEvent, "attestation_binding", binding.StatementID, attestation.ErrBindingDuplicate)
		}
		if binding.BindingVersion != latest+1 {
			return failure(CodeVersionConflict, "attestation_binding", binding.StatementID, attestation.ErrVersionConflict)
		}
		signer, err := uuid.Parse(binding.Signer.Id)
		if err != nil {
			return invalid("attestation_binding", binding.StatementID, "signer must be a UUID")
		}
		evidence, err := json.Marshal(bindingPayload{Context: binding.ContextDigest, Evidence: binding.EvidenceBindings})
		if err != nil {
			return failure(CodeInvalid, "attestation_binding", binding.StatementID, err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO attestation_binding (
				tenant_id, row_id, statement_id, statement_version, context_digest,
				evidence_bindings, binding_version, bound_at, signer, digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			tenantID(tenant), uuid.New(), binding.StatementID, int64(binding.StatementVersion), storageDigest(contextDigest(binding.ContextDigest)),
			evidence, int64(binding.BindingVersion), binding.BoundAt.Time(), signer, storageDigest(binding.Digest()))
		if err != nil {
			return mapWriteError("attestation_binding", binding.StatementID, err, attestation.ErrBindingDuplicate, CodeDuplicateEvent)
		}
		return nil
	})
}

// SaveBinding is a descriptive alias for AppendBinding.
func (s *Store) SaveBinding(ctx context.Context, tenant values.TenantId, binding attestation.Binding) error {
	return s.AppendBinding(ctx, tenant, binding)
}

// ListBindings returns binding history in ascending version order.
func (s *Store) ListBindings(ctx context.Context, tenant values.TenantId, id string) ([]attestation.Binding, error) {
	var out []attestation.Binding
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT statement_id, statement_version, context_digest, evidence_bindings,
				binding_version, bound_at, signer
			FROM attestation_binding
			WHERE tenant_id=$1 AND statement_id=$2 ORDER BY binding_version`, tenantID(tenant), id)
		if err != nil {
			return failure(CodeDatabase, "attestation_binding", id, err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				b                                attestation.Binding
				contextHash                      string
				evidence                         []byte
				statementVersion, bindingVersion int64
				boundAt                          time.Time
				signer                           uuid.UUID
			)
			if err := rows.Scan(&b.StatementID, &statementVersion, &contextHash, &evidence, &bindingVersion, &boundAt, &signer); err != nil {
				return failure(CodeDatabase, "attestation_binding", id, err)
			}
			var payload bindingPayload
			if err := json.Unmarshal(evidence, &payload); err != nil {
				return failure(CodeDatabase, "attestation_binding", id, err)
			}
			b.StatementVersion = uint64(statementVersion)
			b.ContextDigest = payload.Context
			b.EvidenceBindings = payload.Evidence
			b.BindingVersion = uint64(bindingVersion)
			b.BoundAt = values.NewInstant(boundAt)
			b.Signer = values.EntityRef{Tenant: tenant, Kind: "person", Id: signer.String()}
			if storageDigest(contextDigest(b.ContextDigest)) != contextHash {
				return failure(CodeDatabase, "attestation_binding", id, fmt.Errorf("stored context digest does not match"))
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, failure(CodeNotFound, "attestation_binding", id, attestation.ErrStatementNotFound)
	}
	return out, nil
}

// RequirementRecord is the lossless persistence projection of an approval
// requirement. Structured columns remain JSON so the kernel owns their schema.
type RequirementRecord struct {
	TenantID         uuid.UUID
	RowID            uuid.UUID
	RequirementID    string
	Revision         uint64
	Stage            uint32
	Candidates       json.RawMessage
	AuthorityFloor   json.RawMessage
	Quorum           json.RawMessage
	Deadline         time.Duration
	Escalation       json.RawMessage
	Separation       json.RawMessage
	Invalidators     json.RawMessage
	Source           string
	ExpressionDigest string
}

// PutApprovalRequirement appends the next immutable requirement revision.
func (s *Store) PutApprovalRequirement(ctx context.Context, tenant values.TenantId, req humanwork.ApprovalRequirement, expectedRevision ...uint64) error {
	if req.RequirementID == "" || req.Revision == 0 || req.Stage == 0 || req.ExpressionDigest == "" {
		return invalid("approval_requirement", req.RequirementID, "requirement id, revision, stage and expression digest are required")
	}
	if len(expectedRevision) > 1 {
		return invalid("approval_requirement", req.RequirementID, "at most one expected revision is allowed")
	}
	expected := uint64(0)
	if len(expectedRevision) == 1 {
		expected = expectedRevision[0]
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		latest, err := latestVersion(ctx, tx, tenantID(tenant), "approval_requirement", "requirement_id", "revision", req.RequirementID)
		if err != nil {
			return err
		}
		if req.Revision <= latest {
			return failure(CodeDuplicateRevision, "approval_requirement", req.RequirementID, attestation.ErrStatementDuplicate)
		}
		if expected != latest || req.Revision != latest+1 {
			return failure(CodeVersionConflict, "approval_requirement", req.RequirementID, attestation.ErrVersionConflict)
		}
		candidates, _ := json.Marshal(req.Candidates)
		floor, _ := json.Marshal(req.AuthorityFloor)
		quorum, _ := json.Marshal(req.Quorum)
		escalation, _ := json.Marshal(req.Escalation)
		separation, _ := json.Marshal(req.Separation)
		invalidators, _ := json.Marshal(req.Invalidators)
		source, err := json.Marshal(req.Source)
		if err != nil {
			return failure(CodeInvalid, "approval_requirement", req.RequirementID, err)
		}
		deadline := req.Deadline.Expiry.Time().Sub(req.Deadline.DecideBy.Time())
		_, err = tx.Exec(ctx, `
			INSERT INTO approval_requirement (
				tenant_id, row_id, requirement_id, revision, stage, candidates,
				authority_floor, quorum, deadline, escalation, separation,
				invalidators, source, expression_digest)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			tenantID(tenant), uuid.New(), req.RequirementID, int64(req.Revision), int64(req.Stage), candidates,
			floor, quorum, deadline, escalation, separation, invalidators, string(source), storageDigest(req.ExpressionDigest))
		if err != nil {
			return mapWriteError("approval_requirement", req.RequirementID, err, attestation.ErrStatementDuplicate, CodeDuplicateRevision)
		}
		return nil
	})
}

// LoadApprovalRequirement returns the stored column projection.
func (s *Store) LoadApprovalRequirement(ctx context.Context, tenant values.TenantId, id string, revision uint64) (RequirementRecord, error) {
	var out RequirementRecord
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		var stage, rev int64
		var source string
		var deadline *time.Duration
		err := tx.QueryRow(ctx, `SELECT tenant_id,row_id,requirement_id,revision,stage,candidates,authority_floor,quorum,deadline,escalation,separation,invalidators,source,expression_digest FROM approval_requirement WHERE tenant_id=$1 AND requirement_id=$2 AND revision=$3`, tenantID(tenant), id, int64(revision)).Scan(
			&out.TenantID, &out.RowID, &out.RequirementID, &rev, &stage, &out.Candidates, &out.AuthorityFloor, &out.Quorum, &deadline, &out.Escalation, &out.Separation, &out.Invalidators, &source, &out.ExpressionDigest)
		if errors.Is(err, dbport.ErrNoRows) {
			return failure(CodeNotFound, "approval_requirement", id, attestation.ErrStatementNotFound)
		}
		if err != nil {
			return failure(CodeDatabase, "approval_requirement", id, err)
		}
		out.Revision, out.Stage, out.Source = uint64(rev), uint32(stage), source
		out.ExpressionDigest = domainDigest(out.ExpressionDigest)
		if deadline != nil {
			out.Deadline = *deadline
		}
		return nil
	})
	return out, err
}

// AppendResolution persists one immutable resolution event.
func (s *Store) AppendResolution(ctx context.Context, tenant values.TenantId, resolution humanwork.Resolution, eventSequence uint64) error {
	if resolution.RequirementID == "" || resolution.RequirementRevision == 0 || resolution.Outcome == "" || eventSequence == 0 {
		return invalid("approval_resolution", resolution.RequirementID, "requirement, revision, outcome and event sequence are required")
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx) error {
		latest, err := latestVersion(ctx, tx, tenantID(tenant), "approval_resolution", "requirement_id", "event_sequence", resolution.RequirementID)
		if err != nil {
			return err
		}
		if eventSequence <= latest {
			return failure(CodeDuplicateEvent, "approval_resolution", resolution.RequirementID, attestation.ErrBindingDuplicate)
		}
		if eventSequence != latest+1 {
			return failure(CodeVersionConflict, "approval_resolution", resolution.RequirementID, attestation.ErrVersionConflict)
		}
		candidates, _ := json.Marshal(resolution.Candidates)
		excluded, _ := json.Marshal(resolution.Excluded)
		_, err = tx.Exec(ctx, `INSERT INTO approval_resolution (tenant_id,row_id,requirement_id,requirement_revision,outcome,candidates,excluded,fallback_used,resolved_at,effective_at,directory_version,expression_digest,requirement_digest,quorum_required,event_sequence) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
			tenantID(tenant), uuid.New(), resolution.RequirementID, int64(resolution.RequirementRevision), string(resolution.Outcome), candidates, excluded, resolution.FallbackUsed,
			resolution.ResolvedAt.Time(), resolution.EffectiveAt.Time(), resolution.DirectoryVersion, storageDigest(resolution.ExpressionDigest), storageDigest(resolution.RequirementDigest), int64(resolution.QuorumRequired), int64(eventSequence))
		if err != nil {
			return mapWriteError("approval_resolution", resolution.RequirementID, err, attestation.ErrBindingDuplicate, CodeDuplicateEvent)
		}
		return nil
	})
}

func latestVersion(ctx context.Context, q dbport.Querier, tenant uuid.UUID, table, keyColumn, versionColumn, key string) (uint64, error) {
	var latest int64
	if err := q.QueryRow(ctx, fmt.Sprintf("SELECT COALESCE(MAX(%s),0) FROM %s WHERE tenant_id=$1 AND %s=$2", versionColumn, table, keyColumn), tenant, key).Scan(&latest); err != nil {
		return 0, failure(CodeDatabase, table, key, err)
	}
	return uint64(latest), nil
}

func tenantID(tenant values.TenantId) uuid.UUID {
	id, _ := uuid.Parse(tenant.String())
	return id
}

type statementAttesterPayload struct {
	attestation.AttesterPrincipal
	SubjectTenant      string `json:"_subject_tenant"`
	SubjectKind        string `json:"_subject_kind"`
	JurisdictionTenant string `json:"_jurisdiction_tenant"`
	JurisdictionKind   string `json:"_jurisdiction_kind"`
}

type bindingPayload struct {
	Context  attestation.ContextDigest     `json:"context"`
	Evidence []attestation.EvidenceBinding `json:"evidence"`
}

func contextDigest(contextDigest attestation.ContextDigest) string {
	raw := []byte(contextDigest.SubjectAsOf + "\x00" + contextDigest.Jurisdiction + "\x00" + contextDigest.Locale + "\x00" + contextDigest.RenderedTextHash)
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum[:])
}

// content_digest is the database's bare lowercase SHA-256 hex domain, while
// the kernel canonicalbytes engine carries the same digest with "sha256:".
func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if value == "" || strings.Contains(value, ":") || len(value) != 64 {
		return value
	}
	return "sha256:" + value
}

func mapWriteError(table, key string, err error, cause error, code Code) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return failure(code, table, key, cause)
	}
	return failure(CodeDatabase, table, key, err)
}
