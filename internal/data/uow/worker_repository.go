package uow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/aggregates"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
)

// PostgresWorkerRepository is DB-018's reference [AggregateRepository]
// implementation, over the worker table migrations/00011_people_aggregates.sql
// (DB-008) already declares. It is read-only against every column that
// table's own trigger already governs (row_id, canonical_id, the bitemporal
// envelope, digest) and writes only through the same "supersede the live row,
// then insert" shape internal/data/aggregates' Put helper uses -- this
// package adds no new table and no new migration, only the version check
// that helper does not perform.
//
// PostgresWorkerRepository holds no state; every method takes its
// [dbport.Conn] explicitly, the same shape internal/data/aggregates.PeopleStore
// uses.
type PostgresWorkerRepository struct{}

var _ AggregateRepository[aggregates.Worker] = PostgresWorkerRepository{}

// workerColumns lists every column of the worker table in scan order. It is
// this file's own copy of the same list internal/data/aggregates keeps
// unexported for its own PutWorker/CurrentWorker/KnownAsOfWorker -- both
// packages read the same migration-owned table, and neither exports the
// column list for the other to share, so each names it once for itself.
const workerColumns = "row_id, tenant_id, entity_id, canonical_id, person_ref, worker_number, worker_type, " +
	"lifecycle_status, effective_from, effective_to, recorded_at, superseded_at, digest_algorithm, digest"

var (
	workerCurrentSQL = fmt.Sprintf(`
		SELECT %s FROM worker
		WHERE tenant_id = $1 AND entity_id = $2 AND superseded_at IS NULL
		  AND effective_from <= $3 AND (effective_to IS NULL OR effective_to > $3)`, workerColumns)

	workerListSQL = fmt.Sprintf(`
		SELECT %s FROM worker
		WHERE tenant_id = $1 AND entity_id = $2
		ORDER BY recorded_at ASC, row_id ASC`, workerColumns)
)

// scanner is satisfied by both a [dbport.Row] (QueryRow) and a [dbport.Rows]
// (Query, per row), so one scan function serves Load and ListRevisions alike.
type scanner interface {
	Scan(dest ...any) error
}

func scanWorkerRow(s scanner) (aggregates.Worker, error) {
	var w aggregates.Worker
	var workerNumber *string
	err := s.Scan(&w.RowID, &w.Tenant, &w.EntityID, &w.CanonicalID, &w.PersonRef, &workerNumber, &w.WorkerType,
		&w.LifecycleStatus, &w.EffectiveFrom, &w.EffectiveTo, &w.RecordedAt, &w.SupersededAt,
		&w.DigestAlgorithm, &w.Digest)
	if workerNumber != nil {
		w.WorkerNumber = *workerNumber
	}
	w.Kind = aggregates.KindWorker
	return w, err
}

// countWorkerRevisions returns the number of worker rows ever recorded for
// (tenant, entityID) -- this repository's [Version]. It is itself a plain
// read with no locking of its own; the atomicity a compare-and-swap needs
// comes from [PostgresWorkerRepository.Save]'s own statements re-checking an
// equivalent count against the freshest committed data, never from this
// helper being called twice with anything guaranteed to agree.
func countWorkerRevisions(ctx context.Context, ex dbport.Conn, tenant, entityID uuid.UUID) (Version, error) {
	var n int64
	if err := ex.QueryRow(ctx,
		`SELECT count(*) FROM worker WHERE tenant_id = $1 AND entity_id = $2`,
		tenant, entityID).Scan(&n); err != nil {
		return 0, fmt.Errorf("uow: count worker revisions for %s: %w", entityID, err)
	}
	return Version(n), nil
}

// Load returns the Worker revision live at businessAt, together with the
// total count of revisions ever recorded for entityID (entityID's current
// [Version], regardless of whether the one businessAt matched is the most
// recent). A caller intending to [Stage] a Save against what Load returned
// passes that Version back as expectedVersion.
func (PostgresWorkerRepository) Load(ctx context.Context, ex dbport.Conn, tenant, entityID uuid.UUID, businessAt time.Time) (aggregates.Worker, Version, error) {
	version, err := countWorkerRevisions(ctx, ex, tenant, entityID)
	if err != nil {
		return aggregates.Worker{}, 0, err
	}
	row := ex.QueryRow(ctx, workerCurrentSQL, tenant, entityID, businessAt)
	w, err := scanWorkerRow(row)
	if err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return aggregates.Worker{}, version, fmt.Errorf("%w: worker %s", aggregates.ErrNotFound, entityID)
		}
		return aggregates.Worker{}, version, fmt.Errorf("uow: load worker %s: %w", entityID, err)
	}
	return w, version, nil
}

// Save appends w as entityID's next revision. expectedVersion is checked
// atomically against entityID's current version:
//
//   - expectedVersion == 0 means "no revision exists yet". The check is the
//     INSERT ... ON CONFLICT DO NOTHING that registers entityID in
//     aggregate_entity (migrations/00011): this repository's very first
//     write for an entity is also the write that wins the race to create it,
//     so a second concurrent first-write finds the row already there and
//     affects zero.
//   - expectedVersion > 0 means "revision expectedVersion is still the live
//     one". The check is the UPDATE that supersedes the live row, gated on
//     both superseded_at IS NULL and a recount of every revision for the
//     entity matching expectedVersion exactly; either condition already
//     having moved on makes the UPDATE affect zero rows.
//
// Either branch's zero-rows-affected outcome is reported as
// [*ErrStaleVersion], carrying the version this call re-derives by counting
// again -- not stale in the sense of "wrong forever", just "as of the moment
// the conflict was detected", which is all optimistic concurrency ever
// promises.
func (PostgresWorkerRepository) Save(ctx context.Context, ex dbport.Conn, tenant, entityID uuid.UUID, w aggregates.Worker, expectedVersion Version) error {
	if w.Tenant != tenant {
		return fmt.Errorf("uow: worker %s: aggregate tenant %s does not match the unit of work's tenant %s", entityID, w.Tenant, tenant)
	}
	if w.EntityID != entityID {
		return fmt.Errorf("uow: worker %s: aggregate carries a different entity id %s", entityID, w.EntityID)
	}

	var (
		affected int64
		err      error
	)
	if expectedVersion == 0 {
		affected, err = ex.Exec(ctx, `
			INSERT INTO aggregate_entity (tenant_id, entity_id, kind, canonical_id)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (tenant_id, entity_id) DO NOTHING`,
			tenant, entityID, string(aggregates.KindWorker), w.CanonicalID)
		if err != nil {
			return fmt.Errorf("uow: register worker entity %s: %w", entityID, err)
		}
	} else {
		affected, err = ex.Exec(ctx, `
			UPDATE worker
			SET superseded_at = $1
			WHERE tenant_id = $2 AND entity_id = $3 AND superseded_at IS NULL
			  AND (SELECT count(*) FROM worker w2 WHERE w2.tenant_id = $2 AND w2.entity_id = $3) = $4`,
			w.RecordedAt, tenant, entityID, int64(expectedVersion))
		if err != nil {
			return fmt.Errorf("uow: supersede worker %s: %w", entityID, err)
		}
	}
	if affected == 0 {
		actual, cerr := countWorkerRevisions(ctx, ex, tenant, entityID)
		if cerr != nil {
			return cerr
		}
		return &ErrStaleVersion{Actual: actual}
	}

	if _, err := ex.Exec(ctx, `
		INSERT INTO worker (
			row_id, tenant_id, entity_id, canonical_id, person_ref, worker_number, worker_type, lifecycle_status,
			effective_from, effective_to, recorded_at, digest_algorithm, digest)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		w.RowID, w.Tenant, w.EntityID, w.CanonicalID, w.PersonRef, nullableWorkerText(w.WorkerNumber), w.WorkerType, w.LifecycleStatus,
		w.EffectiveFrom, w.EffectiveTo, w.RecordedAt, w.DigestAlgorithm, w.Digest); err != nil {
		return fmt.Errorf("uow: insert worker %s revision: %w", entityID, err)
	}
	return nil
}

// ListRevisions returns every Worker revision ever recorded for entityID,
// oldest first: the Nth element (0-based) is the aggregate as it stood at
// Version(N+1).
func (PostgresWorkerRepository) ListRevisions(ctx context.Context, ex dbport.Conn, tenant, entityID uuid.UUID) ([]aggregates.Worker, error) {
	rows, err := ex.Query(ctx, workerListSQL, tenant, entityID)
	if err != nil {
		return nil, fmt.Errorf("uow: list worker revisions for %s: %w", entityID, err)
	}
	defer rows.Close()
	var out []aggregates.Worker
	for rows.Next() {
		w, err := scanWorkerRow(rows)
		if err != nil {
			return nil, fmt.Errorf("uow: scan worker revision for %s: %w", entityID, err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("uow: read worker revisions for %s: %w", entityID, err)
	}
	return out, nil
}

func nullableWorkerText(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
