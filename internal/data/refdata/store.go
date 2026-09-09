// Package refdata persists immutable reference releases and tenant adoption
// events. Release rows are global, while adoption rows are protected by the
// tenant session boundary established by [tenancy.WithTenant].
package refdata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	dataref "github.com/monstercameron/human-capital-management-suite/internal/domains/refdata"
)

// DB is the driver-free transaction capability required by Store.
type DB interface{ dbport.Beginner }

// Store implements migrations 00130 and 00131. It holds no process-local
// state, so a restart cannot lose a release or split a tenant's adoption
// history from its source digest.
type Store struct{ db DB }

// NewStore constructs a durable reference-data store over db.
func NewStore(db DB) *Store { return &Store{db: db} }

// NewPGStore is a descriptive compatibility alias for NewStore.
func NewPGStore(db DB) *Store { return NewStore(db) }

var (
	ErrDuplicate = errors.New("refdata store: duplicate immutable row")
	ErrNotFound  = errors.New("refdata store: row not found")
	ErrInvalid   = errors.New("refdata store: invalid input")
)

// PutRelease stores one published release. The insert is append-only and
// idempotency is intentionally not inferred from a mutable name: the
// dataset/version primary key and content digest must agree exactly.
func (s *Store) PutRelease(ctx context.Context, release dataref.Release) error {
	if s == nil || s.db == nil || release.State != dataref.StatePublished {
		return ErrInvalid
	}
	if _, err := dataref.ValidateRelease(release, release.Source.RetrievedAt); err != nil {
		return fmt.Errorf("refdata store: validate release: %w", err)
	}
	source, err := json.Marshal(release.Source)
	if err != nil {
		return fmt.Errorf("refdata store: encode source: %w", err)
	}
	applicability, err := json.Marshal(release.Applicability)
	if err != nil {
		return fmt.Errorf("refdata store: encode applicability: %w", err)
	}
	members, err := json.Marshal(release.Members)
	if err != nil {
		return fmt.Errorf("refdata store: encode members: %w", err)
	}
	consumers, err := json.Marshal(release.ConsumerRefs)
	if err != nil {
		return fmt.Errorf("refdata store: encode consumers: %w", err)
	}
	impacts, err := json.Marshal(release.AffectedIntents)
	if err != nil {
		return fmt.Errorf("refdata store: encode impacts: %w", err)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("refdata store: begin release: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := tx.Exec(ctx, `
		INSERT INTO reference_dataset_release (
			dataset_id, version, release_digest, state, source, schema_digest,
			applicability, members, consumer_refs, affected_intents,
			effective_from, effective_to, known_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT (dataset_id, version) DO NOTHING`,
		release.DatasetID, release.Version, release.Digest, string(release.State), source, release.SchemaDigest,
		applicability, members, consumers, impacts, release.EffectiveFrom.UTC(), nullableTime(release.EffectiveTo), release.KnownAt.UTC())
	if err != nil {
		return fmt.Errorf("refdata store: insert release: %w", err)
	}
	if result != 1 {
		return ErrDuplicate
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("refdata store: commit release: %w", err)
	}
	return nil
}

// SaveRelease is the persistence-oriented alias for PutRelease.
func (s *Store) SaveRelease(ctx context.Context, release dataref.Release) error {
	return s.PutRelease(ctx, release)
}

// GetRelease loads the exact immutable release identified by dataset and
// version. It does not resolve a latest version implicitly.
func (s *Store) GetRelease(ctx context.Context, datasetID, version string) (dataref.Release, bool, error) {
	if s == nil || s.db == nil || datasetID == "" || version == "" {
		return dataref.Release{}, false, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return dataref.Release{}, false, fmt.Errorf("refdata store: begin release read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	release, err := scanRelease(tx.QueryRow(ctx, `
		SELECT dataset_id, version, state, source, schema_digest,
		       applicability, members, consumer_refs, affected_intents,
		       effective_from, effective_to, known_at, release_digest
		FROM reference_dataset_release
		WHERE dataset_id = $1 AND version = $2`, datasetID, version))
	if errors.Is(err, dbport.ErrNoRows) {
		return dataref.Release{}, false, nil
	}
	if err != nil {
		return dataref.Release{}, false, fmt.Errorf("refdata store: read release: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return dataref.Release{}, false, fmt.Errorf("refdata store: commit release read: %w", err)
	}
	return release, true, nil
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

func scanRelease(row dbport.Row) (dataref.Release, error) {
	var r dataref.Release
	var source, applicability, members, consumers, impacts []byte
	var effectiveTo *time.Time
	if err := row.Scan(&r.DatasetID, &r.Version, &r.State, &source, &r.SchemaDigest, &applicability, &members, &consumers, &impacts, &r.EffectiveFrom, &effectiveTo, &r.KnownAt, &r.Digest); err != nil {
		return dataref.Release{}, err
	}
	if effectiveTo != nil {
		r.EffectiveTo = effectiveTo.UTC()
	}
	if err := json.Unmarshal(source, &r.Source); err != nil {
		return dataref.Release{}, fmt.Errorf("refdata store: decode source: %w", err)
	}
	for _, item := range []struct {
		payload []byte
		target  any
	}{{applicability, &r.Applicability}, {members, &r.Members}, {consumers, &r.ConsumerRefs}, {impacts, &r.AffectedIntents}} {
		if err := json.Unmarshal(item.payload, item.target); err != nil {
			return dataref.Release{}, fmt.Errorf("refdata store: decode release payload: %w", err)
		}
	}
	return r, nil
}

// Adopt validates against the current tenant pin and records one append-only
// event. The read and insert share a tenant-scoped transaction.
func (s *Store) Adopt(ctx context.Context, tenantID uuid.UUID, release dataref.Release, req dataref.AdoptionRequest, adoptedAt time.Time) (dataref.Adoption, error) {
	if s == nil || s.db == nil || tenantID == uuid.Nil || req.TenantID != tenantID.String() {
		return dataref.Adoption{}, ErrInvalid
	}
	return s.adopt(ctx, tenantID, release, req, adoptedAt)
}

func (s *Store) adopt(ctx context.Context, tenantID uuid.UUID, release dataref.Release, req dataref.AdoptionRequest, adoptedAt time.Time) (dataref.Adoption, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return dataref.Adoption{}, fmt.Errorf("refdata store: begin adoption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return dataref.Adoption{}, err
	}
	stored, err := loadReleaseTx(ctx, tx, release.DatasetID, release.Version)
	if err != nil {
		return dataref.Adoption{}, err
	}
	if stored.Digest != release.Digest {
		return dataref.Adoption{}, fmt.Errorf("%w: release digest differs from durable source row", ErrInvalid)
	}
	release = stored
	current, err := latestTx(ctx, tx, tenantID, release.DatasetID)
	if err != nil {
		return dataref.Adoption{}, err
	}
	adoption, err := dataref.Adopt(current, release, req, adoptedAt)
	if err != nil {
		return dataref.Adoption{}, err
	}
	sequence, err := nextSequence(ctx, tx, tenantID, release.DatasetID)
	if err != nil {
		return dataref.Adoption{}, err
	}
	if err := insertAdoption(ctx, tx, tenantID, sequence, adoption); err != nil {
		return dataref.Adoption{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return dataref.Adoption{}, fmt.Errorf("refdata store: commit adoption: %w", err)
	}
	return adoption, nil
}

// Rollback appends a new event targeting target, retaining current history.
func (s *Store) Rollback(ctx context.Context, tenantID uuid.UUID, target dataref.Release, actor string, effectiveAt, adoptedAt time.Time) (dataref.Adoption, error) {
	if s == nil || s.db == nil || tenantID == uuid.Nil {
		return dataref.Adoption{}, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return dataref.Adoption{}, fmt.Errorf("refdata store: begin rollback: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return dataref.Adoption{}, err
	}
	stored, err := loadReleaseTx(ctx, tx, target.DatasetID, target.Version)
	if err != nil {
		return dataref.Adoption{}, err
	}
	if stored.Digest != target.Digest {
		return dataref.Adoption{}, fmt.Errorf("%w: rollback target differs from durable source row", ErrInvalid)
	}
	target = stored
	current, err := latestTx(ctx, tx, tenantID, target.DatasetID)
	if err != nil {
		return dataref.Adoption{}, err
	}
	if current == nil {
		return dataref.Adoption{}, dataref.ErrRollbackUnavailable
	}
	adoption, err := dataref.Rollback(*current, target, actor, effectiveAt, adoptedAt)
	if err != nil {
		return dataref.Adoption{}, err
	}
	sequence, err := nextSequence(ctx, tx, tenantID, target.DatasetID)
	if err != nil {
		return dataref.Adoption{}, err
	}
	if err := insertAdoption(ctx, tx, tenantID, sequence, adoption); err != nil {
		return dataref.Adoption{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return dataref.Adoption{}, fmt.Errorf("refdata store: commit rollback: %w", err)
	}
	return adoption, nil
}

func loadReleaseTx(ctx context.Context, q dbport.Querier, datasetID, version string) (dataref.Release, error) {
	release, err := scanRelease(q.QueryRow(ctx, `
		SELECT dataset_id, version, state, source, schema_digest,
		       applicability, members, consumer_refs, affected_intents,
		       effective_from, effective_to, known_at, release_digest
		FROM reference_dataset_release
		WHERE dataset_id = $1 AND version = $2`, datasetID, version))
	if errors.Is(err, dbport.ErrNoRows) {
		return dataref.Release{}, ErrNotFound
	}
	if err != nil {
		return dataref.Release{}, fmt.Errorf("refdata store: load release: %w", err)
	}
	return release, nil
}

// LatestAdoption returns the current tenant pin without exposing a mutable
// pointer or resolving a release by name alone.
func (s *Store) LatestAdoption(ctx context.Context, tenantID uuid.UUID, datasetID string) (dataref.Adoption, bool, error) {
	if s == nil || s.db == nil || tenantID == uuid.Nil || datasetID == "" {
		return dataref.Adoption{}, false, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return dataref.Adoption{}, false, fmt.Errorf("refdata store: begin latest adoption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return dataref.Adoption{}, false, err
	}
	a, err := latestTx(ctx, tx, tenantID, datasetID)
	if err != nil {
		return dataref.Adoption{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return dataref.Adoption{}, false, fmt.Errorf("refdata store: commit latest adoption: %w", err)
	}
	if a == nil {
		return dataref.Adoption{}, false, nil
	}
	return *a, true, nil
}

func latestTx(ctx context.Context, q dbport.Querier, tenantID uuid.UUID, datasetID string) (*dataref.Adoption, error) {
	a, err := scanAdoption(q.QueryRow(ctx, `
		SELECT tenant_id, dataset_id, version, release_digest, event, previous_version,
		       effective_at, adopted_at, actor, rollout_digest, overrides,
		       consumer_refs, impact_refs, record_digest
		FROM reference_dataset_adoption
		WHERE tenant_id = $1 AND dataset_id = $2
		ORDER BY event_sequence DESC LIMIT 1`, tenantID, datasetID))
	if errors.Is(err, dbport.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("refdata store: read latest adoption: %w", err)
	}
	return &a, nil
}

func nextSequence(ctx context.Context, q dbport.Querier, tenantID uuid.UUID, datasetID string) (int64, error) {
	var sequence int64
	err := q.QueryRow(ctx, `SELECT COALESCE(MAX(event_sequence), 0) FROM reference_dataset_adoption WHERE tenant_id = $1 AND dataset_id = $2`, tenantID, datasetID).Scan(&sequence)
	if err != nil {
		return 0, fmt.Errorf("refdata store: next adoption sequence: %w", err)
	}
	return sequence + 1, nil
}

func insertAdoption(ctx context.Context, ex dbport.Execer, tenantID uuid.UUID, sequence int64, a dataref.Adoption) error {
	// The adoption columns are constrained to JSON arrays; a nil slice would
	// marshal to JSON null and be refused by the table's own CHECK.
	if a.Overrides == nil {
		a.Overrides = []dataref.Override{}
	}
	if a.ConsumerRefs == nil {
		a.ConsumerRefs = []string{}
	}
	if a.ImpactRefs == nil {
		a.ImpactRefs = []string{}
	}
	overrides, err := json.Marshal(a.Overrides)
	if err != nil {
		return err
	}
	consumers, err := json.Marshal(a.ConsumerRefs)
	if err != nil {
		return err
	}
	impacts, err := json.Marshal(a.ImpactRefs)
	if err != nil {
		return err
	}
	_, err = ex.Exec(ctx, `
		INSERT INTO reference_dataset_adoption (
			tenant_id, adoption_id, dataset_id, version, release_digest, event,
			previous_version, effective_at, adopted_at, actor, rollout_digest,
			overrides, consumer_refs, impact_refs, event_sequence, record_digest)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`,
		tenantID, uuid.New(), a.DatasetID, a.Version, a.ReleaseDigest, string(a.Event), a.PreviousVersion,
		a.EffectiveAt.UTC(), a.AdoptedAt.UTC(), a.Actor, a.RolloutDigest, overrides, consumers, impacts, sequence, a.Digest)
	if err != nil {
		return fmt.Errorf("refdata store: insert adoption: %w", err)
	}
	return nil
}

func scanAdoption(row dbport.Row) (dataref.Adoption, error) {
	var a dataref.Adoption
	var tenantID uuid.UUID
	var event string
	var overrides, consumers, impacts []byte
	var previous string
	if err := row.Scan(&tenantID, &a.DatasetID, &a.Version, &a.ReleaseDigest, &event, &previous, &a.EffectiveAt, &a.AdoptedAt, &a.Actor, &a.RolloutDigest, &overrides, &consumers, &impacts, &a.Digest); err != nil {
		return dataref.Adoption{}, err
	}
	a.TenantID, a.Event, a.PreviousVersion = tenantID.String(), dataref.AdoptionEventKind(event), previous
	for _, item := range []struct {
		payload []byte
		target  any
	}{{overrides, &a.Overrides}, {consumers, &a.ConsumerRefs}, {impacts, &a.ImpactRefs}} {
		if err := json.Unmarshal(item.payload, item.target); err != nil {
			return dataref.Adoption{}, fmt.Errorf("refdata store: decode adoption payload: %w", err)
		}
	}
	a.EffectiveAt, a.AdoptedAt = a.EffectiveAt.UTC(), a.AdoptedAt.UTC()
	return a, nil
}

// ListAdoptions returns the complete tenant history, oldest first. It is the
// evidence source for reconstructing historical execution pins.
func (s *Store) ListAdoptions(ctx context.Context, tenantID uuid.UUID, datasetID string) ([]dataref.Adoption, error) {
	if s == nil || s.db == nil || tenantID == uuid.Nil || datasetID == "" {
		return nil, ErrInvalid
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("refdata store: begin adoption list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT tenant_id, dataset_id, version, release_digest, event, previous_version,
		       effective_at, adopted_at, actor, rollout_digest, overrides,
		       consumer_refs, impact_refs, record_digest
		FROM reference_dataset_adoption
		WHERE tenant_id = $1 AND dataset_id = $2
		ORDER BY event_sequence ASC`, tenantID, datasetID)
	if err != nil {
		return nil, fmt.Errorf("refdata store: list adoptions: %w", err)
	}
	defer rows.Close()
	var out []dataref.Adoption
	for rows.Next() {
		a, scanErr := scanAdoption(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("refdata store: list adoption rows: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("refdata store: commit adoption list: %w", err)
	}
	return out, nil
}
