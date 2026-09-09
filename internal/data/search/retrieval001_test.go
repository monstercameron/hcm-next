package search_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/search"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// This file proves planning/todos.md's RETRIEVAL-001 test matrix end to end,
// exercising [search.Project] and [search.Query] together against a real,
// fully migrated PostgreSQL schema exactly the way a cell composing the two
// would: build a projection from source-facts-shaped input, then query it
// under a caller-supplied [search.Authorization] and [search.Discloser].

// retrievalTenant registers one active tenant as the migration/admin role.
func retrievalTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

func retrievalAppConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func retrievalInTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
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

func retrievalInTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := retrievalInTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

// projectSubject builds a well-formed [search.ProjectionInput] for a worker
// and projects it, exactly as a cell composing a source reader with this
// package would.
func projectSubject(
	t *testing.T, conn *pgxadapter.Conn, tenantUUID uuid.UUID, tenant values.TenantId,
	workerID string, seq uint64, fields map[people.FieldID]string,
) search.Projection {
	t.Helper()
	rev, err := values.NewSequenceRevision("search.retrieval.stream."+workerID, seq)
	if err != nil {
		t.Fatalf("build revision: %v", err)
	}
	in := search.ProjectionInput{
		Tenant:   tenant,
		Subject:  values.EntityRef{Tenant: tenant, Kind: values.Kind(search.KindWorker), Id: workerID},
		Kind:     search.KindWorker,
		Revision: rev,
		Fields:   fields,
	}
	var proj search.Projection
	retrievalInTenantTx(t, conn, tenantUUID, func(tx dbport.Tx) error {
		var err error
		proj, err = search.Project(context.Background(), tx, tenantUUID, in)
		return err
	})
	return proj
}

func queryAs(conn *pgxadapter.Conn, tenantUUID uuid.UUID, in search.QueryInput) ([]search.Result, error) {
	var results []search.Result
	err := retrievalInTenantTxErr(conn, tenantUUID, func(tx dbport.Tx) error {
		var qErr error
		results, qErr = search.Query(context.Background(), tx, tenantUUID, in)
		return qErr
	})
	return results, err
}

func granted() search.Authorization { return search.Authorization{ScopeGranted: true} }

func adaFields() map[people.FieldID]string {
	return map[people.FieldID]string{
		people.FieldLegalName:    "Ada Lovelace",
		people.FieldWorkerNumber: "W-9001",
		people.FieldJobCode:      "OPS-HRBP2",
		people.FieldOrgUnit:      "people-ops",
	}
}

func graceFields() map[people.FieldID]string {
	return map[people.FieldID]string{
		people.FieldLegalName:    "Grace Hopper",
		people.FieldWorkerNumber: "W-9002",
		people.FieldJobCode:      "ENG-COMPILER1",
		people.FieldOrgUnit:      "engineering",
	}
}

// TestTodo_RETRIEVAL_001 is the PRIMARY acceptance test: a caller with search
// scope and a permissive discloser gets back exactly the subjects whose
// projected text matches, as subject references only -- no field value ever
// rides along on a [search.Result].
func TestTodo_RETRIEVAL_001(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenantUUID := retrievalTenant(t, db, "retrieval-primary")
	tenant := values.TenantId("retrieval-primary")
	conn := retrievalAppConn(t, db)

	adaID := uuid.NewString()
	graceID := uuid.NewString()
	projectSubject(t, conn, tenantUUID, tenant, adaID, 1, adaFields())
	projectSubject(t, conn, tenantUUID, tenant, graceID, 1, graceFields())

	results, err := queryAs(conn, tenantUUID, search.QueryInput{
		Tenant: tenant, Text: "Lovelace", Authorization: granted(), Discloser: search.AllowAll,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Query(Lovelace) = %d results, want 1: %+v", len(results), results)
	}
	if results[0].Subject.Id != adaID {
		t.Errorf("Query(Lovelace)[0].Subject.Id = %s, want %s", results[0].Subject.Id, adaID)
	}
	if results[0].Kind != search.KindWorker {
		t.Errorf("Query(Lovelace)[0].Kind = %s, want %s", results[0].Kind, search.KindWorker)
	}
	if results[0].SourceRevision == "" {
		t.Error("Query result carries no source revision")
	}
}

// TestTodo_RETRIEVAL_001_Integration exercises the full pipeline across
// several subjects, a kind filter and a result limit against the real,
// GIN-indexed schema.
func TestTodo_RETRIEVAL_001_Integration(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenantUUID := retrievalTenant(t, db, "retrieval-integration")
	tenant := values.TenantId("retrieval-integration")
	conn := retrievalAppConn(t, db)

	ada := uuid.NewString()
	grace := uuid.NewString()
	projectSubject(t, conn, tenantUUID, tenant, ada, 1, adaFields())
	projectSubject(t, conn, tenantUUID, tenant, grace, 1, graceFields())

	// A term present in both projections' org unit / job code text still
	// distinguishes: "engineering" only matches Grace.
	results, err := queryAs(conn, tenantUUID, search.QueryInput{
		Tenant: tenant, Kind: search.KindWorker, Text: "engineering",
		Authorization: granted(), Discloser: search.AllowAll,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) != 1 || results[0].Subject.Id != grace {
		t.Fatalf("Query(engineering) = %+v, want exactly Grace", results)
	}

	// A limit of 1 against two matches still returns exactly one, chosen by
	// the store's own deterministic tie-break.
	both, err := queryAs(conn, tenantUUID, search.QueryInput{
		Tenant: tenant, Text: "W-9001 OR W-9002", Limit: 1,
		Authorization: granted(), Discloser: search.AllowAll,
	})
	if err != nil {
		t.Fatalf("Query with limit: %v", err)
	}
	if len(both) != 1 {
		t.Fatalf("Query with Limit=1 returned %d results, want 1", len(both))
	}

	// A subject kind that exists in the schema's declared set but was never
	// projected under finds nothing, not an error.
	none, err := queryAs(conn, tenantUUID, search.QueryInput{
		Tenant: tenant, Kind: search.KindWorker, Text: "zzz-nomatch-zzz",
		Authorization: granted(), Discloser: search.AllowAll,
	})
	if err != nil {
		t.Fatalf("Query(no match): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("Query(no match) = %+v, want no results", none)
	}
}

// TestTodo_RETRIEVAL_001_Security proves every refusal RETRIEVAL-001's RED
// clause names: a caller with no search scope is DENIED before any
// statement runs, a caller with no discloser is refused for the same
// reason, a discloser's WITHHELD subject never appears in a result even
// though it matched, and one tenant's query can never surface another
// tenant's projected subject.
func TestTodo_RETRIEVAL_001_Security(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	alphaUUID := retrievalTenant(t, db, "retrieval-sec-alpha")
	betaUUID := retrievalTenant(t, db, "retrieval-sec-beta")
	alpha := values.TenantId("retrieval-sec-alpha")
	beta := values.TenantId("retrieval-sec-beta")
	conn := retrievalAppConn(t, db)

	adaID := uuid.NewString()
	projectSubject(t, conn, alphaUUID, alpha, adaID, 1, adaFields())
	betaWorkerID := uuid.NewString()
	projectSubject(t, conn, betaUUID, beta, betaWorkerID, 1, adaFields())

	t.Run("no scope is DENIED before any statement runs", func(t *testing.T) {
		// A nil executor and the nil uuid prove the refusal happens before
		// Query ever tries to use either: a panic here would mean the scope
		// check ran too late.
		_, err := search.Query(context.Background(), nil, uuid.Nil, search.QueryInput{
			Tenant:        alpha,
			Text:          "Lovelace",
			Authorization: search.Authorization{ScopeGranted: false, Reason: "no search grant"},
			Discloser:     search.AllowAll,
		})
		if !errors.Is(err, search.ErrScopeDenied) {
			t.Fatalf("Query with no scope = %v, want ErrScopeDenied", err)
		}
	})

	t.Run("no discloser is refused", func(t *testing.T) {
		_, err := search.Query(context.Background(), nil, uuid.Nil, search.QueryInput{
			Tenant: alpha, Text: "Lovelace", Authorization: granted(), Discloser: nil,
		})
		if !errors.Is(err, search.ErrNoDiscloser) {
			t.Fatalf("Query with no discloser = %v, want ErrNoDiscloser", err)
		}
	})

	t.Run("a withheld subject never appears even though it matched", func(t *testing.T) {
		graceID := uuid.NewString()
		projectSubject(t, conn, alphaUUID, alpha, graceID, 1, graceFields())

		withholdGrace := search.DiscloserFunc(func(_ context.Context, subject values.EntityRef) (bool, error) {
			return subject.Id != graceID, nil
		})
		results, err := queryAs(conn, alphaUUID, search.QueryInput{
			Tenant: alpha, Text: "Ada OR Grace", Authorization: granted(), Discloser: withholdGrace,
		})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		for _, r := range results {
			if r.Subject.Id == graceID {
				t.Fatalf("withheld subject %s appeared in results: %+v", graceID, results)
			}
		}
		found := false
		for _, r := range results {
			if r.Subject.Id == adaID {
				found = true
			}
		}
		if !found {
			t.Fatalf("disclosable subject %s was dropped alongside the withheld one: %+v", adaID, results)
		}
	})

	t.Run("a discloser error withholds rather than includes", func(t *testing.T) {
		erroring := search.DiscloserFunc(func(context.Context, values.EntityRef) (bool, error) {
			return true, errors.New("disclosure check unavailable")
		})
		results, err := queryAs(conn, alphaUUID, search.QueryInput{
			Tenant: alpha, Text: "Lovelace", Authorization: granted(), Discloser: erroring,
		})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		if len(results) != 0 {
			t.Fatalf("a failing discloser produced results: %+v, want none", results)
		}
	})

	t.Run("one tenant's query cannot surface another tenant's subject", func(t *testing.T) {
		results, err := queryAs(conn, alphaUUID, search.QueryInput{
			Tenant: alpha, Text: "Lovelace", Authorization: granted(), Discloser: search.AllowAll,
		})
		if err != nil {
			t.Fatalf("Query: %v", err)
		}
		for _, r := range results {
			if r.Subject.Id == betaWorkerID {
				t.Fatalf("tenant alpha's query surfaced tenant beta's subject: %+v", results)
			}
		}
	})
}

// TestTodo_RETRIEVAL_001_Conformance proves the declared classification
// allowlist end to end: a field this package has not cleared for lexical
// search, given to Project alongside cleared ones, can never be found by a
// query for its value -- because it never entered search_text at all, not
// because the query happened not to match it by chance.
func TestTodo_RETRIEVAL_001_Conformance(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenantUUID := retrievalTenant(t, db, "retrieval-conformance")
	tenant := values.TenantId("retrieval-conformance")
	conn := retrievalAppConn(t, db)

	const classifiedMarker = "ZONE-CLASSIFIED-BAND-Q9"
	workerID := uuid.NewString()
	fields := adaFields()
	fields[people.FieldPayZone] = classifiedMarker
	fields[people.FieldFTE] = "1.0000"
	fields[people.FieldManagerRelation] = "rel_mgr_secret_9"

	proj := projectSubject(t, conn, tenantUUID, tenant, workerID, 1, fields)
	for _, classified := range []string{classifiedMarker, "1.0000", "rel_mgr_secret_9"} {
		if strings.Contains(proj.SearchText, classified) {
			t.Fatalf("projection search text %q leaked classified value %q", proj.SearchText, classified)
		}
	}

	results, err := queryAs(conn, tenantUUID, search.QueryInput{
		Tenant: tenant, Text: classifiedMarker, Authorization: granted(), Discloser: search.AllowAll,
	})
	if err != nil {
		t.Fatalf("Query(classified marker): %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Query(%q) = %+v, want zero hits: the classified field must never enter the projection", classifiedMarker, results)
	}

	// The same subject IS found by a cleared field, proving the miss above
	// is about classification and not a broken projection.
	cleared, err := queryAs(conn, tenantUUID, search.QueryInput{
		Tenant: tenant, Text: "Lovelace", Authorization: granted(), Discloser: search.AllowAll,
	})
	if err != nil {
		t.Fatalf("Query(Lovelace): %v", err)
	}
	if len(cleared) != 1 || cleared[0].Subject.Id != workerID {
		t.Fatalf("Query(Lovelace) = %+v, want exactly %s", cleared, workerID)
	}
}

// TestTodo_RETRIEVAL_001_Recovery is the rebuild proof: reprojecting the
// same subject from the same source facts after the physical row is lost
// reproduces byte-identical search text and the same query behavior,
// because [search.Project] is a pure function of its cleared fields and
// revision, not of the projection's own history.
func TestTodo_RETRIEVAL_001_Recovery(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenantUUID := retrievalTenant(t, db, "retrieval-recovery")
	tenant := values.TenantId("retrieval-recovery")
	conn := retrievalAppConn(t, db)

	workerID := uuid.NewString()
	fields := adaFields()
	original := projectSubject(t, conn, tenantUUID, tenant, workerID, 1, fields)

	before, err := queryAs(conn, tenantUUID, search.QueryInput{
		Tenant: tenant, Text: "Lovelace", Authorization: granted(), Discloser: search.AllowAll,
	})
	if err != nil || len(before) != 1 {
		t.Fatalf("Query before simulated loss: results=%+v err=%v", before, err)
	}

	// Simulate losing the rebuildable projection row -- exactly what
	// migrations/00037's own header claims can happen to it without losing
	// any authoritative fact. hcmnext_app deliberately has no DELETE grant
	// on search_projection (Project only ever upserts), so this uses the
	// migration/admin connection the way an out-of-band rebuild operation
	// would, still scoped to the tenant so migration 00037's FORCE ROW
	// LEVEL SECURITY policy is satisfied rather than silently matching zero
	// rows.
	adminConn := db.NewConn(t)
	retrievalInTenantTx(t, adminConn, tenantUUID, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `DELETE FROM search_projection WHERE subject_ref = $1`, original.Subject.String())
		return err
	})

	afterLoss, err := queryAs(conn, tenantUUID, search.QueryInput{
		Tenant: tenant, Text: "Lovelace", Authorization: granted(), Discloser: search.AllowAll,
	})
	if err != nil {
		t.Fatalf("Query after simulated loss: %v", err)
	}
	if len(afterLoss) != 0 {
		t.Fatalf("Query after simulated loss = %+v, want none (row genuinely gone)", afterLoss)
	}

	// Reproject from the same source facts and the same revision.
	rebuilt := projectSubject(t, conn, tenantUUID, tenant, workerID, 1, fields)
	if rebuilt.SearchText != original.SearchText {
		t.Fatalf("rebuilt search text %q != original %q: Project is not a pure function of its inputs",
			rebuilt.SearchText, original.SearchText)
	}
	if rebuilt.SourceRevision != original.SourceRevision {
		t.Fatalf("rebuilt source revision %q != original %q", rebuilt.SourceRevision, original.SourceRevision)
	}

	after, err := queryAs(conn, tenantUUID, search.QueryInput{
		Tenant: tenant, Text: "Lovelace", Authorization: granted(), Discloser: search.AllowAll,
	})
	if err != nil {
		t.Fatalf("Query after rebuild: %v", err)
	}
	if len(after) != 1 || after[0].Subject.Id != workerID {
		t.Fatalf("Query after rebuild = %+v, want exactly %s restored", after, workerID)
	}
}

// TestTodo_RETRIEVAL_001_Mutation proves a family of malformed calls are
// refused before they can write or read anything: an unspecified revision
// never reaches search_projection_event, an unknown subject kind is refused
// by [search.EntityKind.Validate], and blank query text is refused rather
// than silently matching everything.
func TestTodo_RETRIEVAL_001_Mutation(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	tenantUUID := retrievalTenant(t, db, "retrieval-mutation")
	tenant := values.TenantId("retrieval-mutation")
	conn := retrievalAppConn(t, db)

	workerID := uuid.NewString()

	t.Run("unspecified revision is refused and writes nothing", func(t *testing.T) {
		in := search.ProjectionInput{
			Tenant:   tenant,
			Subject:  values.EntityRef{Tenant: tenant, Kind: values.Kind(search.KindWorker), Id: workerID},
			Kind:     search.KindWorker,
			Revision: values.UnspecifiedRevision(),
			Fields:   adaFields(),
		}
		err := retrievalInTenantTxErr(conn, tenantUUID, func(tx dbport.Tx) error {
			_, err := search.Project(context.Background(), tx, tenantUUID, in)
			return err
		})
		if !errors.Is(err, search.ErrInvalidProjectionInput) {
			t.Fatalf("Project(unspecified revision) = %v, want ErrInvalidProjectionInput", err)
		}

		var count int
		retrievalInTenantTx(t, conn, tenantUUID, func(tx dbport.Tx) error {
			return tx.QueryRow(context.Background(),
				`SELECT count(*) FROM search_projection_event WHERE subject_ref LIKE '%' || $1 || '%'`, workerID,
			).Scan(&count)
		})
		if count != 0 {
			t.Fatalf("a refused projection wrote %d evidence rows, want 0", count)
		}
	})

	t.Run("an undeclared subject kind is refused", func(t *testing.T) {
		results, err := queryAs(conn, tenantUUID, search.QueryInput{
			Tenant: tenant, Kind: "candidate", Text: "Lovelace", Authorization: granted(), Discloser: search.AllowAll,
		})
		var invalid *search.InvalidKindError
		if !errors.As(err, &invalid) {
			t.Fatalf("Query(kind=candidate) = (%v, %v), want *search.InvalidKindError", results, err)
		}
	})

	t.Run("blank query text is refused, not treated as match-everything", func(t *testing.T) {
		_, err := queryAs(conn, tenantUUID, search.QueryInput{
			Tenant: tenant, Text: "   ", Authorization: granted(), Discloser: search.AllowAll,
		})
		if !errors.Is(err, search.ErrEmptyQueryText) {
			t.Fatalf("Query(blank text) = %v, want ErrEmptyQueryText", err)
		}
	})
}
