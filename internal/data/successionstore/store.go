// Package successionstore persists the immutable succession revision families
// declared by migrations/00120_succession.sql.
package successionstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/succession"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DB is the transaction capability required by Store.
type DB interface{ dbport.Beginner }

// Store implements [succession.Store]. Every public operation owns its
// transaction so the RLS scope and the immutable write or read are atomic.
type Store struct{ db DB }

var _ succession.Store = (*Store)(nil)

// New returns a PostgreSQL succession store over db. The supplied connection
// or pool must be able to assume the hcmnext_app role for RLS-enforced use.
func New(db DB) *Store { return &Store{db: db} }

func invalid(detail string) error {
	return &succession.StoreError{Code: succession.StoreInvalidCode, Detail: detail}
}

func notFound(detail string) error {
	return &succession.StoreError{Code: succession.StoreNotFoundCode, Detail: detail}
}

func duplicate(detail string) error {
	return &succession.StoreError{Code: succession.StoreDuplicateCode, Detail: detail}
}

func stale(expected, actual uint64, detail string) error {
	return &succession.StoreError{Code: succession.StoreStaleCASCode, Expected: expected, Actual: actual, Detail: detail}
}

func (s *Store) withTenant(ctx context.Context, tenant succession.TenantID, fn func(dbport.Tx, uuid.UUID) error) error {
	if s == nil || s.db == nil {
		return invalid("database capability is required")
	}
	if ctx == nil {
		return invalid("context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if tenant == nil || strings.TrimSpace(tenant.String()) == "" {
		return invalid("tenant id is required")
	}
	tenantID, err := uuid.Parse(tenant.String())
	if err != nil || tenantID == uuid.Nil {
		return invalid("tenant id must be a non-nil UUID")
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("successionstore: begin transaction: %w", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return fmt.Errorf("successionstore: commit transaction: %w", err)
	}
	return nil
}

// SaveCriticalRole appends one critical-role revision.
func (s *Store) SaveCriticalRole(ctx context.Context, tenant succession.TenantID, in succession.CriticalRole) error {
	if in.CanonicalDigest == "" {
		var err error
		in, err = succession.NewCriticalRole(in)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := in.Validate(); err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		return saveCriticalRoleTx(ctx, tx, tenantID, in)
	})
}

func saveCriticalRoleTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, in succession.CriticalRole) error {
	if err := lockRevision(ctx, tx, tenantID, "critical-role", in.RoleID); err != nil {
		return err
	}
	head, exists, err := currentRevision(ctx, tx, "succession_critical_role", "role_id", tenantID, in.RoleID)
	if err != nil {
		return err
	}
	if present, err := revisionExists(ctx, tx, "succession_critical_role", "role_id", tenantID, in.RoleID, in.Revision); err != nil {
		return err
	} else if present {
		return duplicate(fmt.Sprintf("critical role %s revision %d", in.RoleID, in.Revision))
	}
	if err := checkAppend(head, exists, in.Revision, in.ParentRevision, in.ParentDigest, func() (string, error) {
		return currentDigest(ctx, tx, "succession_critical_role", "role_id", tenantID, in.RoleID, head)
	}, "critical role"); err != nil {
		return err
	}
	evidence, err := json.Marshal(in.EvidenceRefs)
	if err != nil {
		return fmt.Errorf("successionstore: encode critical role evidence: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO succession_critical_role (
			tenant_id, row_id, role_id, revision, parent_revision, parent_digest,
			position_ref, job_revision_ref, owner_ref, authority_ref,
			effective_at, known_at, evidence_refs, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb,$14)`,
		tenantID, uuid.New(), in.RoleID, int64(in.Revision), nullableRevision(in.ParentRevision), nullableDigest(in.ParentDigest),
		in.PositionRef, in.JobRevisionRef, in.OwnerRef, in.AuthorityRef, in.EffectiveAt.Time(), in.KnownAt.Time(), string(evidence), storageDigest(in.CanonicalDigest))
	if err != nil {
		return mapWriteError("critical role", in.RoleID, err)
	}
	return nil
}

// LoadCriticalRole loads one critical-role revision.
func (s *Store) LoadCriticalRole(ctx context.Context, tenant succession.TenantID, id string, revision uint64) (succession.CriticalRole, error) {
	var out succession.CriticalRole
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var err error
		out, err = loadCriticalRoleTx(ctx, tx, tenantID, id, revision)
		return err
	})
	return out, err
}

func loadCriticalRoleTx(ctx context.Context, tx dbport.Querier, tenantID uuid.UUID, id string, revision uint64) (succession.CriticalRole, error) {
	var (
		rowID                                    uuid.UUID
		roleID                                   string
		storedRevision                           int64
		parentRevision                           *int64
		parentDigest, position, job, owner, auth *string
		effective, known                         *time.Time
		evidenceJSON                             []byte
		digest                                   string
	)
	err := tx.QueryRow(ctx, `
		SELECT row_id, role_id, revision, parent_revision, parent_digest,
			position_ref, job_revision_ref, owner_ref, authority_ref,
			effective_at, known_at, evidence_refs::text, canonical_digest
		FROM succession_critical_role
		WHERE tenant_id=$1 AND role_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(
		&rowID, &roleID, &storedRevision, &parentRevision, &parentDigest,
		&position, &job, &owner, &auth, &effective, &known, &evidenceJSON, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return succession.CriticalRole{}, notFound(fmt.Sprintf("critical role %s revision %d", id, revision))
		}
		return succession.CriticalRole{}, fmt.Errorf("successionstore: load critical role %s/%d: %w", id, revision, err)
	}
	if effective == nil || known == nil || evidenceJSON == nil {
		return succession.CriticalRole{}, fmt.Errorf("successionstore: stored critical role %s/%d is incomplete", id, revision)
	}
	var evidence []string
	if err := json.Unmarshal(evidenceJSON, &evidence); err != nil {
		return succession.CriticalRole{}, fmt.Errorf("successionstore: decode critical role evidence: %w", err)
	}
	in := succession.CriticalRole{
		RoleID: roleID, Revision: uint64(storedRevision), ParentRevision: uint64Value(parentRevision), ParentDigest: domainDigest(stringValue(parentDigest)),
		PositionRef: stringValue(position), JobRevisionRef: stringValue(job), OwnerRef: stringValue(owner), AuthorityRef: stringValue(auth),
		EffectiveAt: valuesInstant(*effective), KnownAt: valuesInstant(*known), EvidenceRefs: evidence, CanonicalDigest: domainDigest(digest),
	}
	if in.ParentRevision == 0 {
		in.ParentDigest = ""
	}
	stored, err := succession.NewCriticalRole(in)
	if err != nil || stored.CanonicalDigest != domainDigest(digest) {
		return succession.CriticalRole{}, fmt.Errorf("successionstore: stored critical role %s/%d failed integrity validation", id, revision)
	}
	return stored, nil
}

// CurrentCriticalRole loads the highest stored role revision.
func (s *Store) CurrentCriticalRole(ctx context.Context, tenant succession.TenantID, id string) (succession.CriticalRole, error) {
	var out succession.CriticalRole
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		revision, exists, err := currentRevision(ctx, tx, "succession_critical_role", "role_id", tenantID, id)
		if err != nil {
			return err
		}
		if !exists {
			return notFound(fmt.Sprintf("critical role %s", id))
		}
		out, err = loadCriticalRoleTx(ctx, tx, tenantID, id, revision)
		return err
	})
	return out, err
}

// SaveReadiness appends one successor-readiness revision.
func (s *Store) SaveReadiness(ctx context.Context, tenant succession.TenantID, in succession.SuccessorReadinessRevision) error {
	if in.CanonicalDigest == "" {
		var err error
		in, err = succession.NewSuccessorReadinessRevision(in)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := in.Validate(); err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		return saveReadinessTx(ctx, tx, tenantID, in, false)
	})
}

func saveReadinessTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, in succession.SuccessorReadinessRevision, idempotent bool) error {
	if err := lockRevision(ctx, tx, tenantID, "readiness", in.SuccessorID); err != nil {
		return err
	}
	head, exists, err := currentRevision(ctx, tx, "succession_readiness_revision", "successor_id", tenantID, in.SuccessorID)
	if err != nil {
		return err
	}
	if present, err := revisionExists(ctx, tx, "succession_readiness_revision", "successor_id", tenantID, in.SuccessorID, in.Revision); err != nil {
		return err
	} else if present {
		if idempotent {
			var digest string
			if err := tx.QueryRow(ctx, `SELECT canonical_digest FROM succession_readiness_revision WHERE tenant_id=$1 AND successor_id=$2 AND revision=$3`, tenantID, in.SuccessorID, int64(in.Revision)).Scan(&digest); err != nil {
				return err
			}
			if domainDigest(digest) == in.CanonicalDigest {
				return nil
			}
		}
		return duplicate(fmt.Sprintf("readiness %s revision %d", in.SuccessorID, in.Revision))
	}
	if err := checkAppend(head, exists, in.Revision, in.ParentRevision, in.ParentDigest, func() (string, error) {
		return currentDigest(ctx, tx, "succession_readiness_revision", "successor_id", tenantID, in.SuccessorID, head)
	}, "readiness"); err != nil {
		return err
	}
	evidence, err := json.Marshal(in.EvidenceRefs)
	if err != nil {
		return fmt.Errorf("successionstore: encode readiness evidence: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO succession_readiness_revision (
			tenant_id, row_id, successor_id, revision, parent_revision,
			readiness, vacancy_risk, assessed_by, assessment_source,
			evidence_refs, effective_at, known_at, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13)`,
		tenantID, uuid.New(), in.SuccessorID, int64(in.Revision), nullableRevision(in.ParentRevision), string(in.Readiness), string(in.VacancyRisk), in.AssessedBy, in.AssessmentSource,
		string(evidence), in.EffectiveAt.Time(), in.KnownAt.Time(), storageDigest(in.CanonicalDigest))
	if err != nil {
		return mapWriteError("readiness", in.SuccessorID, err)
	}
	return nil
}

// LoadReadiness loads one successor-readiness revision.
func (s *Store) LoadReadiness(ctx context.Context, tenant succession.TenantID, id string, revision uint64) (succession.SuccessorReadinessRevision, error) {
	var out succession.SuccessorReadinessRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var err error
		out, err = loadReadinessTx(ctx, tx, tenantID, id, revision)
		return err
	})
	return out, err
}

func loadReadinessTx(ctx context.Context, tx dbport.Querier, tenantID uuid.UUID, id string, revision uint64) (succession.SuccessorReadinessRevision, error) {
	var (
		rowID                                       uuid.UUID
		successorID                                 string
		storedRevision                              int64
		parentRevision                              *int64
		readiness, risk, assessedBy, source, digest string
		evidenceJSON                                []byte
		effective, known                            *time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT row_id, successor_id, revision, parent_revision, readiness,
			vacancy_risk, assessed_by, assessment_source, evidence_refs::text,
			effective_at, known_at, canonical_digest
		FROM succession_readiness_revision
		WHERE tenant_id=$1 AND successor_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(
		&rowID, &successorID, &storedRevision, &parentRevision, &readiness, &risk, &assessedBy, &source, &evidenceJSON, &effective, &known, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return succession.SuccessorReadinessRevision{}, notFound(fmt.Sprintf("readiness %s revision %d", id, revision))
		}
		return succession.SuccessorReadinessRevision{}, fmt.Errorf("successionstore: load readiness %s/%d: %w", id, revision, err)
	}
	if effective == nil || known == nil || evidenceJSON == nil {
		return succession.SuccessorReadinessRevision{}, fmt.Errorf("successionstore: stored readiness %s/%d is incomplete", id, revision)
	}
	var evidence []string
	if err := json.Unmarshal(evidenceJSON, &evidence); err != nil {
		return succession.SuccessorReadinessRevision{}, fmt.Errorf("successionstore: decode readiness evidence: %w", err)
	}
	parentDigest := ""
	if parentRevision != nil {
		if err := tx.QueryRow(ctx, `SELECT canonical_digest FROM succession_readiness_revision WHERE tenant_id=$1 AND successor_id=$2 AND revision=$3`, tenantID, id, *parentRevision).Scan(&parentDigest); err != nil {
			return succession.SuccessorReadinessRevision{}, fmt.Errorf("successionstore: load readiness parent %s/%d: %w", id, *parentRevision, err)
		}
		parentDigest = domainDigest(parentDigest)
	}
	in := succession.SuccessorReadinessRevision{
		SuccessorID: successorID, Revision: uint64(storedRevision), ParentRevision: uint64Value(parentRevision), ParentDigest: parentDigest,
		Readiness: succession.ReadinessBand(readiness), VacancyRisk: succession.VacancyRisk(risk), AssessedBy: assessedBy, AssessmentSource: source,
		EvidenceRefs: evidence, EffectiveAt: valuesInstant(*effective), KnownAt: valuesInstant(*known), CanonicalDigest: domainDigest(digest),
	}
	stored, err := succession.NewSuccessorReadinessRevision(in)
	if err != nil || stored.CanonicalDigest != domainDigest(digest) {
		return succession.SuccessorReadinessRevision{}, fmt.Errorf("successionstore: stored readiness %s/%d failed integrity validation", id, revision)
	}
	return stored, nil
}

// CurrentReadiness loads the highest stored readiness revision.
func (s *Store) CurrentReadiness(ctx context.Context, tenant succession.TenantID, id string) (succession.SuccessorReadinessRevision, error) {
	var out succession.SuccessorReadinessRevision
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		revision, exists, err := currentRevision(ctx, tx, "succession_readiness_revision", "successor_id", tenantID, id)
		if err != nil {
			return err
		}
		if !exists {
			return notFound(fmt.Sprintf("readiness %s", id))
		}
		out, err = loadReadinessTx(ctx, tx, tenantID, id, revision)
		return err
	})
	return out, err
}

type slatePayload struct {
	ParentDigest              string                                  `json:"parent_digest"`
	CriticalRoleDigest        string                                  `json:"critical_role_digest"`
	NominatorRole             string                                  `json:"nominator_role"`
	NominatorAuthorizationRef string                                  `json:"nominator_authorization_ref"`
	NominatorAuthorized       bool                                    `json:"nominator_authorized"`
	DeclaredScopes            []string                                `json:"declared_scopes"`
	Candidates                []succession.SuccessorReadinessRevision `json:"candidates"`
}

// SaveSlate appends one slate revision and makes its candidate readiness
// revisions durable in the same transaction.
func (s *Store) SaveSlate(ctx context.Context, tenant succession.TenantID, in succession.SuccessionSlate) error {
	if in.CanonicalDigest == "" {
		var err error
		in, err = succession.NewSuccessionSlate(in)
		if err != nil {
			return invalid(err.Error())
		}
	} else if err := in.Validate(); err != nil {
		return invalid(err.Error())
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		return saveSlateTx(ctx, tx, tenantID, in)
	})
}

func saveSlateTx(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, in succession.SuccessionSlate) error {
	if err := lockRevision(ctx, tx, tenantID, "slate", in.SlateID); err != nil {
		return err
	}
	var roleDigest string
	if err := tx.QueryRow(ctx, `SELECT canonical_digest FROM succession_critical_role WHERE tenant_id=$1 AND role_id=$2 AND revision=$3`, tenantID, in.CriticalRoleID, int64(in.CriticalRoleRevision)).Scan(&roleDigest); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return notFound(fmt.Sprintf("critical role %s revision %d", in.CriticalRoleID, in.CriticalRoleRevision))
		}
		return fmt.Errorf("successionstore: check slate critical role: %w", err)
	}
	if domainDigest(roleDigest) != in.CriticalRoleDigest {
		return invalid("slate critical role digest does not match stored role revision")
	}
	for _, candidate := range in.Candidates {
		if err := saveReadinessTx(ctx, tx, tenantID, candidate, true); err != nil {
			return err
		}
	}
	head, exists, err := currentRevision(ctx, tx, "succession_slate", "slate_id", tenantID, in.SlateID)
	if err != nil {
		return err
	}
	if present, err := revisionExists(ctx, tx, "succession_slate", "slate_id", tenantID, in.SlateID, in.Revision); err != nil {
		return err
	} else if present {
		return duplicate(fmt.Sprintf("slate %s revision %d", in.SlateID, in.Revision))
	}
	if err := checkAppend(head, exists, in.Revision, in.ParentRevision, in.ParentDigest, func() (string, error) {
		return currentDigest(ctx, tx, "succession_slate", "slate_id", tenantID, in.SlateID, head)
	}, "slate"); err != nil {
		return err
	}
	payload, err := json.Marshal(slatePayload{
		ParentDigest: in.ParentDigest, CriticalRoleDigest: in.CriticalRoleDigest,
		NominatorRole: in.NominatorRole, NominatorAuthorizationRef: in.NominatorAuthorizationRef,
		NominatorAuthorized: in.NominatorAuthorized, DeclaredScopes: append([]string(nil), in.DeclaredScopes...),
		Candidates: append([]succession.SuccessorReadinessRevision(nil), in.Candidates...),
	})
	if err != nil {
		return fmt.Errorf("successionstore: encode slate candidates: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO succession_slate (
			tenant_id, row_id, slate_id, revision, parent_revision,
			critical_role_id, critical_role_revision, position_ref, job_revision_ref,
			nominator_id, visibility, candidates, effective_at, known_at, canonical_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,$13,$14,$15)`,
		tenantID, uuid.New(), in.SlateID, int64(in.Revision), nullableRevision(in.ParentRevision), in.CriticalRoleID, int64(in.CriticalRoleRevision),
		in.PositionRef, in.JobRevisionRef, in.NominatorID, string(in.Visibility), string(payload), in.EffectiveAt.Time(), in.KnownAt.Time(), storageDigest(in.CanonicalDigest))
	if err != nil {
		return mapWriteError("slate", in.SlateID, err)
	}
	return nil
}

// LoadSlate loads one slate revision, including its protected candidate
// payload. Candidate access remains governed by the domain's CandidatesFor.
func (s *Store) LoadSlate(ctx context.Context, tenant succession.TenantID, id string, revision uint64) (succession.SuccessionSlate, error) {
	var out succession.SuccessionSlate
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var err error
		out, err = loadSlateTx(ctx, tx, tenantID, id, revision)
		return err
	})
	return out, err
}

func loadSlateTx(ctx context.Context, tx dbport.Querier, tenantID uuid.UUID, id string, revision uint64) (succession.SuccessionSlate, error) {
	var (
		rowID                                        uuid.UUID
		storedID, roleID                             string
		storedRevision, roleRevision                 int64
		parentRevision                               *int64
		position, job, nominator, visibility, digest string
		candidatesJSON                               []byte
		effective, known                             *time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT row_id, slate_id, revision, parent_revision,
			critical_role_id, critical_role_revision, position_ref, job_revision_ref,
			nominator_id, visibility, candidates::text, effective_at, known_at,
			canonical_digest
		FROM succession_slate
		WHERE tenant_id=$1 AND slate_id=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(
		&rowID, &storedID, &storedRevision, &parentRevision, &roleID, &roleRevision,
		&position, &job, &nominator, &visibility, &candidatesJSON, &effective, &known, &digest)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return succession.SuccessionSlate{}, notFound(fmt.Sprintf("slate %s revision %d", id, revision))
		}
		return succession.SuccessionSlate{}, fmt.Errorf("successionstore: load slate %s/%d: %w", id, revision, err)
	}
	if effective == nil || known == nil || candidatesJSON == nil {
		return succession.SuccessionSlate{}, fmt.Errorf("successionstore: stored slate %s/%d is incomplete", id, revision)
	}
	var payload slatePayload
	if err := json.Unmarshal(candidatesJSON, &payload); err != nil {
		return succession.SuccessionSlate{}, fmt.Errorf("successionstore: decode slate candidates: %w", err)
	}
	criticalDigest, err := roleDigestAt(ctx, tx, tenantID, roleID, uint64(roleRevision))
	if err != nil {
		return succession.SuccessionSlate{}, err
	}
	if payload.CriticalRoleDigest == "" {
		payload.CriticalRoleDigest = criticalDigest
	}
	if payload.ParentDigest == "" && parentRevision != nil {
		var parent string
		if err := tx.QueryRow(ctx, `SELECT canonical_digest FROM succession_slate WHERE tenant_id=$1 AND slate_id=$2 AND revision=$3`, tenantID, id, *parentRevision).Scan(&parent); err != nil {
			return succession.SuccessionSlate{}, fmt.Errorf("successionstore: load slate parent %s/%d: %w", id, *parentRevision, err)
		}
		payload.ParentDigest = domainDigest(parent)
	}
	in := succession.SuccessionSlate{
		SlateID: storedID, Revision: uint64(storedRevision), ParentRevision: uint64Value(parentRevision), ParentDigest: payload.ParentDigest,
		CriticalRoleID: roleID, CriticalRoleRevision: uint64(roleRevision), CriticalRoleDigest: payload.CriticalRoleDigest,
		PositionRef: position, JobRevisionRef: job, NominatorID: nominator, NominatorRole: payload.NominatorRole,
		NominatorAuthorizationRef: payload.NominatorAuthorizationRef, NominatorAuthorized: payload.NominatorAuthorized,
		Visibility: succession.DisclosureScope(visibility), DeclaredScopes: payload.DeclaredScopes, Candidates: payload.Candidates,
		EffectiveAt: valuesInstant(*effective), KnownAt: valuesInstant(*known), CanonicalDigest: domainDigest(digest),
	}
	stored, err := succession.NewSuccessionSlate(in)
	if err != nil || stored.CanonicalDigest != domainDigest(digest) {
		return succession.SuccessionSlate{}, fmt.Errorf("successionstore: stored slate %s/%d failed integrity validation", id, revision)
	}
	return stored, nil
}

// CurrentSlate loads the highest stored slate revision.
func (s *Store) CurrentSlate(ctx context.Context, tenant succession.TenantID, id string) (succession.SuccessionSlate, error) {
	var out succession.SuccessionSlate
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		revision, exists, err := currentRevision(ctx, tx, "succession_slate", "slate_id", tenantID, id)
		if err != nil {
			return err
		}
		if !exists {
			return notFound(fmt.Sprintf("slate %s", id))
		}
		out, err = loadSlateTx(ctx, tx, tenantID, id, revision)
		return err
	})
	return out, err
}

func lockRevision(ctx context.Context, tx dbport.Execer, tenantID uuid.UUID, kind, id string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, tenantID.String()+":"+kind+":"+id)
	if err != nil {
		return fmt.Errorf("successionstore: lock %s %s: %w", kind, id, err)
	}
	return nil
}

func currentRevision(ctx context.Context, tx dbport.Querier, table, idColumn string, tenantID uuid.UUID, id string) (uint64, bool, error) {
	var revision int64
	err := tx.QueryRow(ctx, `SELECT revision FROM `+table+` WHERE tenant_id=$1 AND `+idColumn+`=$2 ORDER BY revision DESC LIMIT 1`, tenantID, id).Scan(&revision)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("successionstore: find current %s %s: %w", table, id, err)
	}
	return uint64(revision), true, nil
}

func revisionExists(ctx context.Context, tx dbport.Querier, table, idColumn string, tenantID uuid.UUID, id string, revision uint64) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM `+table+` WHERE tenant_id=$1 AND `+idColumn+`=$2 AND revision=$3)`, tenantID, id, int64(revision)).Scan(&exists); err != nil {
		return false, fmt.Errorf("successionstore: check existing %s %s/%d: %w", table, id, revision, err)
	}
	return exists, nil
}

func currentDigest(ctx context.Context, tx dbport.Querier, table, idColumn string, tenantID uuid.UUID, id string, revision uint64) (string, error) {
	var digest string
	if err := tx.QueryRow(ctx, `SELECT canonical_digest FROM `+table+` WHERE tenant_id=$1 AND `+idColumn+`=$2 AND revision=$3`, tenantID, id, int64(revision)).Scan(&digest); err != nil {
		return "", fmt.Errorf("successionstore: load current digest %s %s/%d: %w", table, id, revision, err)
	}
	return domainDigest(digest), nil
}

func checkAppend(head uint64, exists bool, revision, parentRevision uint64, parentDigest string, digest func() (string, error), kind string) error {
	if !exists {
		if revision != 1 {
			return stale(0, 0, fmt.Sprintf("first %s revision must be 1", kind))
		}
		if parentRevision != 0 || parentDigest != "" {
			return invalid(fmt.Sprintf("first %s revision cannot have a parent", kind))
		}
		return nil
	}
	if revision != head+1 || parentRevision != head {
		return stale(head, head, fmt.Sprintf("%s revision does not extend current head", kind))
	}
	actual, err := digest()
	if err != nil {
		return err
	}
	if parentDigest != actual {
		return stale(head, head, fmt.Sprintf("%s parent digest is stale", kind))
	}
	return nil
}

func roleDigestAt(ctx context.Context, tx dbport.Querier, tenantID uuid.UUID, roleID string, revision uint64) (string, error) {
	var digest string
	if err := tx.QueryRow(ctx, `SELECT canonical_digest FROM succession_critical_role WHERE tenant_id=$1 AND role_id=$2 AND revision=$3`, tenantID, roleID, int64(revision)).Scan(&digest); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return "", notFound(fmt.Sprintf("critical role %s revision %d", roleID, revision))
		}
		return "", fmt.Errorf("successionstore: load critical role digest: %w", err)
	}
	return domainDigest(digest), nil
}

func mapWriteError(kind, id string, err error) error {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "duplicate key") || strings.Contains(message, "unique constraint") {
		return duplicate(fmt.Sprintf("%s %s revision already exists", kind, id))
	}
	if strings.Contains(message, "foreign key") {
		return notFound(fmt.Sprintf("referenced row for %s %s is absent", kind, id))
	}
	return fmt.Errorf("successionstore: insert %s %s: %w", kind, id, err)
}

func nullableRevision(value uint64) any {
	if value == 0 {
		return nil
	}
	return int64(value)
}

func nullableDigest(value string) any {
	if value == "" {
		return nil
	}
	return storageDigest(value)
}

func storageDigest(value string) string { return strings.TrimPrefix(value, "sha256:") }

func domainDigest(value string) string {
	if strings.HasPrefix(value, "sha256:") {
		return value
	}
	return "sha256:" + value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func uint64Value(value *int64) uint64 {
	if value == nil {
		return 0
	}
	return uint64(*value)
}

func valuesInstant(value time.Time) values.Instant { return values.NewInstant(value.UTC()) }
