package search

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Executor is the minimal database capability this package needs. A
// [dbport.Tx] and a [dbport.Conn] both satisfy it. Every method takes it
// explicitly rather than holding a handle, the same way
// internal/data/workforce's own store does: both search_projection and
// search_projection_event are row-level-security protected, so the caller
// has to have scoped its transaction to a tenant
// (internal/data/tenancy.WithTenant) before any statement here runs.
type Executor interface {
	dbport.Execer
	dbport.Querier
}

// scanner is the one shape a single-row read and a cursor row share.
type scanner interface {
	Scan(dest ...any) error
}

// row is one physical search_projection row, scoped by the physical tenant
// uuid rather than by [values.TenantId]: the mapping between the two is the
// composition layer's job, the same split internal/data/workforce.Facts
// draws between its TenantUUID function and people.FactQuery.Tenant.
type row struct {
	TenantID       uuid.UUID
	SubjectKind    string
	SubjectRef     string
	SearchText     string
	SourceRevision string
	ProjectedAt    time.Time
}

// Store implements the durable search projection over
// migrations/00037_lexical_search.sql. It holds no state; every method takes
// its [Executor] explicitly.
type Store struct{}

// upsert writes one search_projection row -- an idempotent projection
// rebuild, `ON CONFLICT (tenant_id, subject_kind, subject_ref) DO UPDATE` --
// and appends one search_projection_event evidence row for it, both inside
// the caller's own transaction. Re-running upsert with the same
// (SearchText, SourceRevision) is exactly what a rebuild replaying the same
// source revision does: the stored row ends up byte-identical, and the event
// log gains one more entry proving the replay happened.
func (s Store) upsert(ctx context.Context, ex Executor, r row) (row, error) {
	if ex == nil {
		return row{}, fmt.Errorf("search: no executor")
	}
	stored, err := scanRow(ex.QueryRow(ctx, `
		INSERT INTO search_projection (
			tenant_id, subject_kind, subject_ref, search_text, source_revision, projected_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (tenant_id, subject_kind, subject_ref) DO UPDATE SET
			search_text     = EXCLUDED.search_text,
			source_revision = EXCLUDED.source_revision,
			projected_at    = now()
		RETURNING tenant_id, subject_kind, subject_ref, search_text, source_revision, projected_at`,
		r.TenantID, r.SubjectKind, r.SubjectRef, r.SearchText, r.SourceRevision))
	if err != nil {
		return row{}, fmt.Errorf("search: upsert projection for %s/%s: %w", r.SubjectKind, r.SubjectRef, err)
	}

	if _, err := ex.Exec(ctx, `
		INSERT INTO search_projection_event (
			event_id, tenant_id, subject_kind, subject_ref, source_revision, event_type, recorded_at)
		VALUES ($1, $2, $3, $4, $5, 'PROJECTED', now())`,
		uuid.New(), r.TenantID, r.SubjectKind, r.SubjectRef, r.SourceRevision); err != nil {
		return row{}, fmt.Errorf("search: record projection event for %s/%s: %w", r.SubjectKind, r.SubjectRef, err)
	}
	return stored, nil
}

func scanRow(src scanner) (row, error) {
	var r row
	if err := src.Scan(
		&r.TenantID, &r.SubjectKind, &r.SubjectRef, &r.SearchText, &r.SourceRevision, &r.ProjectedAt,
	); err != nil {
		return row{}, err
	}
	r.ProjectedAt = r.ProjectedAt.UTC()
	return r, nil
}

// hit is one raw full-text match, before any disclosure filtering.
type hit struct {
	SubjectKind    string
	SubjectRef     string
	SourceRevision string
	ProjectedAt    time.Time
	Rank           float64
}

// search runs the tenant-scoped full-text match. kind is "" for every
// declared subject kind. Ranking ties break on subject_ref ascending, so two
// otherwise-tied hits always come back in the same order.
func (s Store) search(ctx context.Context, ex Executor, tenantID uuid.UUID, kind, tsQuery string, limit int) ([]hit, error) {
	if ex == nil {
		return nil, fmt.Errorf("search: no executor")
	}

	var (
		rows dbport.Rows
		err  error
	)
	if kind == "" {
		rows, err = ex.Query(ctx, `
			SELECT subject_kind, subject_ref, source_revision, projected_at,
			       ts_rank(search_vector, websearch_to_tsquery('simple', $2)) AS rank
			FROM search_projection
			WHERE tenant_id = $1
			  AND search_vector @@ websearch_to_tsquery('simple', $2)
			ORDER BY rank DESC, subject_ref ASC
			LIMIT $3`, tenantID, tsQuery, limit)
	} else {
		rows, err = ex.Query(ctx, `
			SELECT subject_kind, subject_ref, source_revision, projected_at,
			       ts_rank(search_vector, websearch_to_tsquery('simple', $2)) AS rank
			FROM search_projection
			WHERE tenant_id = $1 AND subject_kind = $3
			  AND search_vector @@ websearch_to_tsquery('simple', $2)
			ORDER BY rank DESC, subject_ref ASC
			LIMIT $4`, tenantID, tsQuery, kind, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	defer rows.Close()

	var out []hit
	for rows.Next() {
		var h hit
		if err := rows.Scan(&h.SubjectKind, &h.SubjectRef, &h.SourceRevision, &h.ProjectedAt, &h.Rank); err != nil {
			return nil, fmt.Errorf("search: scan hit: %w", err)
		}
		h.ProjectedAt = h.ProjectedAt.UTC()
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	return out, nil
}
