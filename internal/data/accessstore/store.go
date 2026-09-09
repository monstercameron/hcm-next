// Package accessstore is the PostgreSQL adapter for internal/domains/access.
// It appends validated revisions and provider evidence to migration 00052's
// tenant-isolated tables; it does not make access decisions.
package accessstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/access"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type DB interface{ dbport.Beginner }
type Executor interface {
	dbport.Execer
	dbport.Querier
}

var (
	ErrInvalidRow      = errors.New("accessstore: invalid row")
	ErrDuplicate       = errors.New("accessstore: duplicate revision")
	ErrVersionConflict = errors.New("accessstore: stale revision")
	ErrNotFound        = errors.New("accessstore: row not found")
)

// Store implements access.Repository over PostgreSQL.
type Store struct{ db DB }

var _ access.Repository = (*Store)(nil)

func New(db DB) *Store { return &Store{db: db} }

// Add appends one authoritative revision. Observation records must use Observe.
func (s *Store) Add(ctx context.Context, record access.Record) error {
	tenant, err := recordTenant(record)
	if err != nil {
		return err
	}
	return s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		_, err := s.appendRecord(ctx, tx, tenantID, record)
		return err
	})
}

// Observe appends provider evidence separately from the authoritative graph.
func (s *Store) Observe(ctx context.Context, observation access.ExternalAccessObservation) error {
	return s.withTenant(ctx, observation.Tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		_, err := s.AppendExternalAccessObservation(ctx, tx, tenantID, observation)
		return err
	})
}

// Snapshot loads the latest revision for each natural graph key.
func (s *Store) Snapshot(ctx context.Context, tenant values.TenantId) (access.Graph, error) {
	var out access.Graph
	err := s.withTenant(ctx, tenant, func(tx dbport.Tx, tenantID uuid.UUID) error {
		var err error
		out, err = loadSnapshot(ctx, tx, tenantID, tenant)
		return err
	})
	if err != nil {
		return access.Graph{}, err
	}
	return out, nil
}

// Load is an alias for Snapshot.
func (s *Store) Load(ctx context.Context, tenant values.TenantId) (access.Graph, error) {
	return s.Snapshot(ctx, tenant)
}

func (s *Store) PutWorkforceIdentity(ctx context.Context, ex Executor, tenantID uuid.UUID, in access.WorkforceIdentity) (uuid.UUID, error) {
	if err := in.Validate(); err != nil {
		return uuid.Nil, invalid("workforce_identity", err)
	}
	worker, err := uuid.Parse(in.WorkerRef.Id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: worker_ref must be a uuid: %v", ErrInvalidRow, err)
	}
	revision, err := sequence(in.Revision)
	if err != nil {
		return uuid.Nil, err
	}
	from, to, known, _, err := coordinates(in.Effective, in.KnownAt, in.Provenance)
	if err != nil {
		return uuid.Nil, err
	}
	if err := checkRevision(ctx, ex, "workforce_identity", "identity_id", in.ID, revision, tenantID); err != nil {
		return uuid.Nil, err
	}
	rowID := uuid.New()
	affected, err := ex.Exec(ctx, `INSERT INTO workforce_identity (row_id,tenant_id,identity_id,subject,system,worker_ref,revision,authority_class,effective_from,effective_to,known_at,lifecycle,digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		rowID, tenantID, in.ID, in.Subject, in.System, worker, revision, in.Authority.String(), from, to, known, in.Lifecycle.String(), digest(in.Canonical()))
	if err != nil {
		return uuid.Nil, fmt.Errorf("accessstore: insert workforce_identity: %w", err)
	}
	if affected != 1 {
		return uuid.Nil, fmt.Errorf("%w: workforce_identity %s revision %d", ErrDuplicate, in.ID, revision)
	}
	return rowID, nil
}

func (s *Store) PutAccountLink(ctx context.Context, ex Executor, tenantID uuid.UUID, in access.AccountLink) (uuid.UUID, error) {
	if err := in.Validate(); err != nil {
		return uuid.Nil, invalid("account_link", err)
	}
	identity, err := latestRef(ctx, ex, "workforce_identity", "identity_id", in.WorkforceIdentityID, tenantID)
	if err != nil {
		return uuid.Nil, err
	}
	revision, err := sequence(in.Revision)
	if err != nil {
		return uuid.Nil, err
	}
	from, to, _, _, err := coordinates(in.Effective, in.KnownAt, in.Provenance)
	if err != nil {
		return uuid.Nil, err
	}
	if err := checkRevision(ctx, ex, "account_link", "link_id", in.ID, revision, tenantID); err != nil {
		return uuid.Nil, err
	}
	rowID := uuid.New()
	affected, err := ex.Exec(ctx, `INSERT INTO account_link (row_id,tenant_id,link_id,workforce_identity_ref,source,application,account_id,revision,effective_from,effective_to,digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		rowID, tenantID, in.ID, identity, in.Source, in.Application, in.AccountID, revision, from, to, digest(in.Canonical()))
	if err != nil {
		return uuid.Nil, fmt.Errorf("accessstore: insert account_link: %w", err)
	}
	if affected != 1 {
		return uuid.Nil, fmt.Errorf("%w: account_link %s revision %d", ErrDuplicate, in.ID, revision)
	}
	return rowID, nil
}

func (s *Store) PutEntitlementDefinition(ctx context.Context, ex Executor, tenantID uuid.UUID, in access.EntitlementDefinition) (uuid.UUID, error) {
	if err := in.Validate(); err != nil {
		return uuid.Nil, invalid("entitlement_definition", err)
	}
	revision, err := sequence(in.Revision)
	if err != nil {
		return uuid.Nil, err
	}
	from, to, _, _, err := coordinates(in.Effective, in.KnownAt, in.Provenance)
	if err != nil {
		return uuid.Nil, err
	}
	if err := checkRevision(ctx, ex, "entitlement_definition", "definition_id", in.ID, revision, tenantID); err != nil {
		return uuid.Nil, err
	}
	rowID := uuid.New()
	affected, err := ex.Exec(ctx, `INSERT INTO entitlement_definition (row_id,tenant_id,definition_id,application,code,version,risk_class,owner,revision,effective_from,effective_to,digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		rowID, tenantID, in.ID, in.Application, in.Code, in.Version, in.RiskClass.String(), in.Owner, revision, from, to, digest(in.Canonical()))
	if err != nil {
		return uuid.Nil, fmt.Errorf("accessstore: insert entitlement_definition: %w", err)
	}
	if affected != 1 {
		return uuid.Nil, fmt.Errorf("%w: entitlement_definition %s revision %d", ErrDuplicate, in.ID, revision)
	}
	return rowID, nil
}

func (s *Store) PutExpectedEntitlement(ctx context.Context, ex Executor, tenantID uuid.UUID, in access.ExpectedEntitlement) (uuid.UUID, error) {
	if err := in.Validate(); err != nil {
		return uuid.Nil, invalid("expected_entitlement", err)
	}
	identity, err := latestRef(ctx, ex, "workforce_identity", "identity_id", in.WorkforceIdentityID, tenantID)
	if err != nil {
		return uuid.Nil, err
	}
	entitlement, err := latestRef(ctx, ex, "entitlement_definition", "definition_id", in.EntitlementID, tenantID)
	if err != nil {
		return uuid.Nil, err
	}
	var account any
	if in.AccountLinkID != "" {
		account, err = latestRef(ctx, ex, "account_link", "link_id", in.AccountLinkID, tenantID)
		if err != nil {
			return uuid.Nil, err
		}
	}
	employment, err := optionalUUID(in.EmploymentRef, "employment_ref")
	if err != nil {
		return uuid.Nil, err
	}
	position, err := optionalUUID(in.PositionRef, "position_ref")
	if err != nil {
		return uuid.Nil, err
	}
	policy, err := optionalUUID(in.PolicyRef, "policy_ref")
	if err != nil {
		return uuid.Nil, err
	}
	revision, err := sequence(in.Revision)
	if err != nil {
		return uuid.Nil, err
	}
	from, to, _, _, err := coordinates(in.Effective, in.KnownAt, in.Provenance)
	if err != nil {
		return uuid.Nil, err
	}
	if err := checkRevision(ctx, ex, "expected_entitlement", "expected_id", in.ID, revision, tenantID); err != nil {
		return uuid.Nil, err
	}
	rowID := uuid.New()
	affected, err := ex.Exec(ctx, `INSERT INTO expected_entitlement (row_id,tenant_id,expected_id,workforce_identity_ref,account_link_ref,entitlement_ref,employment_ref,position_ref,policy_ref,revision,effective_from,effective_to,digest) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		rowID, tenantID, in.ID, identity, account, entitlement, employment, position, policy, revision, from, to, digest(in.Canonical()))
	if err != nil {
		return uuid.Nil, fmt.Errorf("accessstore: insert expected_entitlement: %w", err)
	}
	if affected != 1 {
		return uuid.Nil, fmt.Errorf("%w: expected_entitlement %s revision %d", ErrDuplicate, in.ID, revision)
	}
	return rowID, nil
}

// AppendExternalAccessObservation records provider evidence. The domain's
// observation has no identity-id field; its account_id resolves the linked
// workforce identity, which is also how provider observations enter the graph.
func (s *Store) AppendExternalAccessObservation(ctx context.Context, ex Executor, tenantID uuid.UUID, in access.ExternalAccessObservation) (uuid.UUID, error) {
	if err := in.Validate(); err != nil {
		return uuid.Nil, invalid("external_access_observation", err)
	}
	identity, err := observationIdentity(ctx, ex, tenantID, in)
	if err != nil {
		return uuid.Nil, err
	}
	seq, err := sequence(in.Revision)
	if err != nil {
		return uuid.Nil, err
	}
	observedAt, ok := in.Effective.StartInstant()
	if !ok {
		return uuid.Nil, fmt.Errorf("%w: observed interval must use instants", ErrInvalidRow)
	}
	if err := checkObservationSequence(ctx, ex, tenantID, identity, seq); err != nil {
		return uuid.Nil, err
	}
	rowID := uuid.New()
	affected, err := ex.Exec(ctx, `INSERT INTO external_access_observation (row_id,tenant_id,observed_workforce_identity_ref,provider_version,observed_state,event_sequence,observed_at,recorded_at) VALUES ($1,$2,$3,$4,to_jsonb($5::text),$6,$7,$8)`,
		rowID, tenantID, identity, in.ProviderVersion, in.ObservedState, seq, observedAt.Time(), in.Provenance.RecordedAt.Instant().Time())
	if err != nil {
		return uuid.Nil, fmt.Errorf("accessstore: insert external_access_observation: %w", err)
	}
	if affected != 1 {
		return uuid.Nil, fmt.Errorf("%w: external observation sequence %d", ErrDuplicate, seq)
	}
	return rowID, nil
}

func (s *Store) appendRecord(ctx context.Context, tx dbport.Tx, tenantID uuid.UUID, record access.Record) (uuid.UUID, error) {
	switch r := record.(type) {
	case access.WorkforceIdentity:
		return s.PutWorkforceIdentity(ctx, tx, tenantID, r)
	case *access.WorkforceIdentity:
		if r != nil {
			return s.PutWorkforceIdentity(ctx, tx, tenantID, *r)
		}
	case access.AccountLink:
		return s.PutAccountLink(ctx, tx, tenantID, r)
	case *access.AccountLink:
		if r != nil {
			return s.PutAccountLink(ctx, tx, tenantID, *r)
		}
	case access.EntitlementDefinition:
		return s.PutEntitlementDefinition(ctx, tx, tenantID, r)
	case *access.EntitlementDefinition:
		if r != nil {
			return s.PutEntitlementDefinition(ctx, tx, tenantID, *r)
		}
	case access.ExpectedEntitlement:
		return s.PutExpectedEntitlement(ctx, tx, tenantID, r)
	case *access.ExpectedEntitlement:
		if r != nil {
			return s.PutExpectedEntitlement(ctx, tx, tenantID, *r)
		}
	}
	return uuid.Nil, fmt.Errorf("%w: observations use Observe", ErrInvalidRow)
}

func (s *Store) withTenant(ctx context.Context, tenant values.TenantId, fn func(dbport.Tx, uuid.UUID) error) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: database is nil", ErrInvalidRow)
	}
	if err := tenant.Validate(); err != nil {
		return fmt.Errorf("%w: tenant: %v", ErrInvalidRow, err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("accessstore: begin: %w", err)
	}
	tenantID, err := resolveTenant(ctx, tx, tenant)
	if err == nil {
		err = tenancy.WithTenant(ctx, tx, tenantID)
	}
	if err == nil {
		err = fn(tx, tenantID)
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("accessstore: commit: %w", err)
	}
	return nil
}

func resolveTenant(ctx context.Context, ex Executor, tenant values.TenantId) (uuid.UUID, error) {
	if id, err := uuid.Parse(tenant.String()); err == nil {
		return id, nil
	}
	var id uuid.UUID
	if err := ex.QueryRow(ctx, `SELECT tenant_id FROM tenant WHERE tenant_key = $1`, tenant.String()).Scan(&id); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("%w: tenant %q", ErrNotFound, tenant)
		}
		return uuid.Nil, fmt.Errorf("accessstore: resolve tenant: %w", err)
	}
	return id, nil
}

func recordTenant(record access.Record) (values.TenantId, error) {
	switch r := record.(type) {
	case access.WorkforceIdentity:
		return r.Tenant, nil
	case *access.WorkforceIdentity:
		if r != nil {
			return r.Tenant, nil
		}
	case access.AccountLink:
		return r.Tenant, nil
	case *access.AccountLink:
		if r != nil {
			return r.Tenant, nil
		}
	case access.EntitlementDefinition:
		return r.Tenant, nil
	case *access.EntitlementDefinition:
		if r != nil {
			return r.Tenant, nil
		}
	case access.ExpectedEntitlement:
		return r.Tenant, nil
	case *access.ExpectedEntitlement:
		if r != nil {
			return r.Tenant, nil
		}
	}
	return "", fmt.Errorf("%w: unsupported or nil record", ErrInvalidRow)
}

func sequence(revision values.RevisionToken) (int64, error) {
	n, ok := revision.Sequence()
	if !ok || n == 0 || n > uint64(^uint64(0)>>1) {
		return 0, fmt.Errorf("%w: revision must be a positive sequence token", ErrInvalidRow)
	}
	return int64(n), nil
}

func coordinates(interval values.EffectiveInterval, known values.KnownAt, provenance evidence.Provenance) (time.Time, *time.Time, time.Time, time.Time, error) {
	if err := interval.Validate(); err != nil {
		return time.Time{}, nil, time.Time{}, time.Time{}, fmt.Errorf("%w: effective interval: %v", ErrInvalidRow, err)
	}
	from, ok := interval.StartInstant()
	if !ok {
		return time.Time{}, nil, time.Time{}, time.Time{}, fmt.Errorf("%w: effective interval must use instants", ErrInvalidRow)
	}
	var to *time.Time
	if end, exists := interval.EndInstant(); exists {
		value := end.Time()
		to = &value
	}
	if known.Canonical() == nil {
		return time.Time{}, nil, time.Time{}, time.Time{}, fmt.Errorf("%w: known_at is required", ErrInvalidRow)
	}
	if err := provenance.Validate(); err != nil {
		return time.Time{}, nil, time.Time{}, time.Time{}, fmt.Errorf("%w: provenance: %v", ErrInvalidRow, err)
	}
	return from.Time(), to, known.Instant().Time(), provenance.RecordedAt.Instant().Time(), nil
}

func digest(raw []byte) string {
	return strings.TrimPrefix(canonicalbytes.Digest(raw), canonicalbytes.DigestAlgorithm+":")
}
func invalid(kind string, err error) error { return fmt.Errorf("%w: %s: %v", ErrInvalidRow, kind, err) }

func latestRef(ctx context.Context, ex Executor, table, keyColumn, key string, tenant uuid.UUID) (uuid.UUID, error) {
	if strings.TrimSpace(key) == "" {
		return uuid.Nil, fmt.Errorf("%w: empty %s", ErrInvalidRow, keyColumn)
	}
	var rowID uuid.UUID
	query := fmt.Sprintf(`SELECT row_id FROM %s WHERE tenant_id=$1 AND %s=$2 ORDER BY revision DESC LIMIT 1`, table, keyColumn)
	if err := ex.QueryRow(ctx, query, tenant, key).Scan(&rowID); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("%w: %s %q", access.ErrUnownedReference, keyColumn, key)
		}
		return uuid.Nil, fmt.Errorf("accessstore: resolve %s: %w", table, err)
	}
	return rowID, nil
}

func observationIdentity(ctx context.Context, ex Executor, tenant uuid.UUID, in access.ExternalAccessObservation) (uuid.UUID, error) {
	var rowID uuid.UUID
	err := ex.QueryRow(ctx, `SELECT workforce_identity_ref FROM account_link WHERE tenant_id=$1 AND account_id=$2 ORDER BY revision DESC LIMIT 1`, tenant, in.AccountID).Scan(&rowID)
	if err == nil {
		return rowID, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return uuid.Nil, err
	}
	err = ex.QueryRow(ctx, `SELECT row_id FROM workforce_identity WHERE tenant_id=$1 AND subject=$2 ORDER BY revision DESC LIMIT 1`, tenant, in.Subject).Scan(&rowID)
	if err == nil {
		return rowID, nil
	}
	if !errors.Is(err, dbport.ErrNoRows) {
		return uuid.Nil, err
	}
	return latestRef(ctx, ex, "workforce_identity", "identity_id", in.ID, tenant)
}

func optionalUUID(text, field string) (any, error) {
	if text == "" {
		return nil, nil
	}
	id, err := uuid.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be a uuid: %v", ErrInvalidRow, field, err)
	}
	return id, nil
}

func checkRevision(ctx context.Context, ex Executor, table, keyColumn, key string, revision int64, tenant uuid.UUID) error {
	var current *int64
	query := fmt.Sprintf(`SELECT max(revision) FROM %s WHERE tenant_id=$1 AND %s=$2`, table, keyColumn)
	if err := ex.QueryRow(ctx, query, tenant, key).Scan(&current); err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	if revision == *current {
		return fmt.Errorf("%w: %s %q revision %d", ErrDuplicate, table, key, revision)
	}
	if revision < *current {
		return fmt.Errorf("%w: %s %q revision %d after %d", ErrVersionConflict, table, key, revision, *current)
	}
	return nil
}

func checkObservationSequence(ctx context.Context, ex Executor, tenant, identity uuid.UUID, seq int64) error {
	var current *int64
	if err := ex.QueryRow(ctx, `SELECT max(event_sequence) FROM external_access_observation WHERE tenant_id=$1 AND observed_workforce_identity_ref=$2`, tenant, identity).Scan(&current); err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	if seq == *current {
		return fmt.Errorf("%w: observation sequence %d", ErrDuplicate, seq)
	}
	if seq < *current {
		return fmt.Errorf("%w: observation sequence %d after %d", ErrVersionConflict, seq, *current)
	}
	return nil
}
