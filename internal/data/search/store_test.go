package search

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// insertTenant registers one active tenant as the migration/admin role, the
// same helper internal/data/workforce's own store_test.go uses.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// appConn opens a fresh connection on db's schema and assumes the
// least-privilege hcmnext_app role, the only way a test observes migration
// 00037's row level security policies.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func testRow(tenant uuid.UUID, subjectRef string) row {
	return row{
		TenantID:       tenant,
		SubjectKind:    "worker",
		SubjectRef:     subjectRef,
		SearchText:     "Ada Lovelace people-ops",
		SourceRevision: "rev:v1:search.test.stream:s1",
	}
}

// TestStoreUpsertRoundTripsAndIsIdempotent proves the ON CONFLICT upsert: a
// second upsert of the same subject with the same content replaces the row
// in place rather than colliding, and returns the same stored content back
// -- the rebuild-proof property at the raw storage layer.
func TestStoreUpsertRoundTripsAndIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "search-upsert")
	conn := appConn(t, db)
	store := Store{}
	in := testRow(tenant, "eref:v1:search-upsert:worker:018f5a2e-6b3a-7c3a-8b7a-1a2b3c4d5e6f")

	var first row
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		first, err = store.upsert(ctx, tx, in)
		return err
	})
	if first.SearchText != in.SearchText || first.SourceRevision != in.SourceRevision {
		t.Fatalf("upsert returned %+v, want content matching %+v", first, in)
	}

	var second row
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		second, err = store.upsert(ctx, tx, in)
		return err
	})
	if second.SearchText != first.SearchText || second.SourceRevision != first.SourceRevision {
		t.Fatalf("second upsert = %+v, want identical content %+v", second, first)
	}
	if second.ProjectedAt.Before(first.ProjectedAt) {
		t.Errorf("second upsert's ProjectedAt %s went backwards from %s", second.ProjectedAt, first.ProjectedAt)
	}

	var eventCount int
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM search_projection_event WHERE subject_ref = $1`, in.SubjectRef).Scan(&eventCount)
	})
	if eventCount != 2 {
		t.Fatalf("search_projection_event has %d rows for %s, want 2 (one per upsert)", eventCount, in.SubjectRef)
	}

	var projectionCount int
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM search_projection WHERE subject_ref = $1`, in.SubjectRef).Scan(&projectionCount)
	})
	if projectionCount != 1 {
		t.Fatalf("search_projection has %d rows for %s, want 1 (upsert, not append)", projectionCount, in.SubjectRef)
	}
}

// TestStoreSearchMatchesAndRanksByTsRank proves the GIN-indexed tsvector
// match: a query for a term in one subject's search_text finds it, a term
// in nobody's search_text finds nothing, and the more relevant of two
// matches is returned first.
func TestStoreSearchMatchesAndRanksByTsRank(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "search-rank")
	conn := appConn(t, db)
	store := Store{}

	rows := []row{
		{TenantID: tenant, SubjectKind: "worker", SubjectRef: "ref-1", SearchText: "Ada Lovelace people-ops", SourceRevision: "rev:v1:s:s1"},
		{TenantID: tenant, SubjectKind: "worker", SubjectRef: "ref-2", SearchText: "Ada Ada Ada engineering", SourceRevision: "rev:v1:s:s1"},
	}
	for _, r := range rows {
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			_, err := store.upsert(ctx, tx, r)
			return err
		})
	}

	var hits []hit
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		hits, err = store.search(ctx, tx, tenant, "", "Ada", 10)
		return err
	})
	if len(hits) != 2 {
		t.Fatalf("search(Ada) returned %d hits, want 2: %+v", len(hits), hits)
	}
	if hits[0].SubjectRef != "ref-2" {
		t.Errorf("search(Ada)[0] = %s, want ref-2 (three occurrences of Ada ranks higher than one)", hits[0].SubjectRef)
	}

	var none []hit
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		none, err = store.search(ctx, tx, tenant, "", "nonexistentterm", 10)
		return err
	})
	if len(none) != 0 {
		t.Fatalf("search(nonexistentterm) = %+v, want no hits", none)
	}
}

// TestStoreSearchIsTenantIsolated proves migration 00037's row level
// security: one tenant's connection cannot see another tenant's projected
// subjects even when the query text matches.
func TestStoreSearchIsTenantIsolated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	alpha := insertTenant(t, db, "search-alpha")
	beta := insertTenant(t, db, "search-beta")
	conn := appConn(t, db)
	store := Store{}

	inTenantTx(t, conn, alpha, func(tx dbport.Tx) error {
		_, err := store.upsert(ctx, tx, row{TenantID: alpha, SubjectKind: "worker", SubjectRef: "alpha-1", SearchText: "Ada Lovelace", SourceRevision: "rev:v1:s:s1"})
		return err
	})
	inTenantTx(t, conn, beta, func(tx dbport.Tx) error {
		_, err := store.upsert(ctx, tx, row{TenantID: beta, SubjectKind: "worker", SubjectRef: "beta-1", SearchText: "Ada Lovelace", SourceRevision: "rev:v1:s:s1"})
		return err
	})

	var alphaHits []hit
	inTenantTx(t, conn, alpha, func(tx dbport.Tx) error {
		var err error
		alphaHits, err = store.search(ctx, tx, alpha, "", "Ada", 10)
		return err
	})
	if len(alphaHits) != 1 || alphaHits[0].SubjectRef != "alpha-1" {
		t.Fatalf("tenant alpha sees %+v, want only alpha-1", alphaHits)
	}

	// Even naming the other tenant's physical id explicitly discloses
	// nothing: the policy predicate is the session setting, not the WHERE
	// clause's tenant_id argument.
	var crossTenant []hit
	inTenantTx(t, conn, alpha, func(tx dbport.Tx) error {
		var err error
		crossTenant, err = store.search(ctx, tx, beta, "", "Ada", 10)
		return err
	})
	if len(crossTenant) != 0 {
		t.Fatalf("tenant alpha's session, queried with tenant beta's id, saw %+v", crossTenant)
	}
}

// TestSearchProjectionEventIsAppendOnly proves the forbid_mutation trigger
// and the SELECT/INSERT-only grant on search_projection_event: a projection
// event is evidence, not an editable row.
func TestSearchProjectionEventIsAppendOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "search-append-only")
	conn := appConn(t, db)
	store := Store{}
	in := testRow(tenant, "immutable-ref-1")
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.upsert(ctx, tx, in)
		return err
	})

	var eventID uuid.UUID
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT event_id FROM search_projection_event WHERE subject_ref = $1`, in.SubjectRef).Scan(&eventID)
	})

	for name, sql := range map[string]string{
		"update": `UPDATE search_projection_event SET event_type = 'PROJECTED' WHERE event_id = $1`,
		"delete": `DELETE FROM search_projection_event WHERE event_id = $1`,
	} {
		err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, execErr := tx.Exec(ctx, sql, eventID)
			return execErr
		})
		if err == nil {
			t.Fatalf("%s of a projection event was accepted", name)
		}
	}
}
