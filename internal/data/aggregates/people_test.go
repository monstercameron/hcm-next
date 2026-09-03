package aggregates_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/monstercameron/hcm-next/internal/data/aggregates"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
)

// TestTodo_DB_008 proves DB-008's Person/IdentityClaim/Worker/Employment/
// Assignment adapter: Put appends and correctly supersedes, a KnownAsOf read
// pinned to a past instant ignores a later supersession (the bitemporal RED
// case this todo's Refs call for), and the append-only trigger refuses a
// direct in-place update of a business column (the "supersede, never update
// in place" RED case).
func TestTodo_DB_008(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	store := aggregates.PeopleStore{}
	personID := uuid.New()

	t.Run("append and read current", func(t *testing.T) {
		p, err := aggregates.NewPerson(tenant, personID, date(t, "2024-01-01"), nil,
			instant(t, "2024-01-02T09:00:00Z"), "ACTIVE", "Jane Doe", "Jane")
		if err != nil {
			t.Fatalf("NewPerson: %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := store.PutPerson(ctx, tx, p)
			return err
		})

		got, err := store.CurrentPerson(ctx, db.Conn, tenant, personID, date(t, "2024-06-01"))
		if err != nil {
			t.Fatalf("CurrentPerson: %v", err)
		}
		if got.LegalName != "Jane Doe" || got.LifecycleState != "ACTIVE" {
			t.Fatalf("CurrentPerson = %+v, want legal_name Jane Doe / ACTIVE", got)
		}
		if got.Digest == "" || got.DigestAlgorithm != aggregates.DigestAlgorithm {
			t.Fatalf("CurrentPerson digest not populated: %+v", got.Envelope)
		}
	})

	t.Run("supersede on a name change, and a past KnownAsOf ignores it", func(t *testing.T) {
		correctionRecordedAt := instant(t, "2025-01-10T12:00:00Z")
		corrected, err := aggregates.NewPerson(tenant, personID, date(t, "2025-01-01"), nil,
			correctionRecordedAt, "ACTIVE", "Jane Smith", "Jane")
		if err != nil {
			t.Fatalf("NewPerson (corrected): %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := store.PutPerson(ctx, tx, corrected)
			return err
		})

		// Business time after the correction's effective date, but a known-at
		// instant before the correction was ever recorded: the original name
		// must still be what a caller could have known then.
		before, err := store.KnownAsOfPerson(ctx, db.Conn, tenant, personID,
			date(t, "2025-06-01"), instant(t, "2025-01-05T00:00:00Z"))
		if err != nil {
			t.Fatalf("KnownAsOfPerson (before correction recorded): %v", err)
		}
		if before.LegalName != "Jane Doe" {
			t.Fatalf("KnownAsOfPerson before the correction was recorded = %q, want the original Jane Doe", before.LegalName)
		}

		// The same business time, known as of right after the correction:
		// the corrected name.
		after, err := store.KnownAsOfPerson(ctx, db.Conn, tenant, personID,
			date(t, "2025-06-01"), correctionRecordedAt)
		if err != nil {
			t.Fatalf("KnownAsOfPerson (after correction recorded): %v", err)
		}
		if after.LegalName != "Jane Smith" {
			t.Fatalf("KnownAsOfPerson after the correction was recorded = %q, want Jane Smith", after.LegalName)
		}

		current, err := store.CurrentPerson(ctx, db.Conn, tenant, personID, date(t, "2025-06-01"))
		if err != nil {
			t.Fatalf("CurrentPerson after correction: %v", err)
		}
		if current.LegalName != "Jane Smith" {
			t.Fatalf("CurrentPerson after correction = %q, want Jane Smith", current.LegalName)
		}
	})

	t.Run("identity claim append and read", func(t *testing.T) {
		claimID := uuid.New()
		claim, err := aggregates.NewIdentityClaim(tenant, claimID, &personID, date(t, "2024-01-01"), nil,
			instant(t, "2024-01-01T00:00:00Z"), "EMAIL", "hcmnext.people", "sha256:deadbeef", "hcmnext.people", "VERIFIED")
		if err != nil {
			t.Fatalf("NewIdentityClaim: %v", err)
		}
		inTx(t, db, func(tx pgx.Tx) error {
			_, err := store.PutIdentityClaim(ctx, tx, claim)
			return err
		})

		got, err := store.CurrentIdentityClaim(ctx, db.Conn, tenant, claimID, date(t, "2024-06-01"))
		if err != nil {
			t.Fatalf("CurrentIdentityClaim: %v", err)
		}
		if got.PersonRef == nil || *got.PersonRef != personID {
			t.Fatalf("CurrentIdentityClaim.PersonRef = %v, want %s", got.PersonRef, personID)
		}
		if got.Assurance != "VERIFIED" {
			t.Fatalf("CurrentIdentityClaim.Assurance = %q, want VERIFIED", got.Assurance)
		}
	})

	t.Run("RED: a direct in-place update of a business column is refused", func(t *testing.T) {
		err := db.ExecErr(`UPDATE person SET legal_name = 'Forged Name' WHERE entity_id = $1 AND superseded_at IS NULL`, personID)
		if err == nil {
			t.Fatal("a direct UPDATE of person.legal_name was accepted; the append-only trigger should have refused it")
		}
	})

	t.Run("RED: re-superseding an already-superseded row is refused", func(t *testing.T) {
		var oldRowID uuid.UUID
		row := db.QueryRow(ctx, `SELECT row_id FROM person WHERE entity_id = $1 AND superseded_at IS NOT NULL LIMIT 1`, personID)
		if err := row.Scan(&oldRowID); err != nil {
			t.Fatalf("find an already-superseded row: %v", err)
		}
		err := db.ExecErr(`UPDATE person SET superseded_at = now() WHERE row_id = $1`, oldRowID)
		if err == nil {
			t.Fatal("superseding an already-superseded row a second time was accepted")
		}
	})
}

// TestTodo_DB_008_Security proves migrations/00011's row level security: a
// connection scoped to hcmnext_app under one tenant sees only that tenant's
// person rows, even though both tenants' rows live in the same table.
func TestTodo_DB_008_Security(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenantA := insertTenant(t, db)
	tenantB := insertTenant(t, db)
	store := aggregates.PeopleStore{}

	personA := uuid.New()
	pA, err := aggregates.NewPerson(tenantA, personA, date(t, "2024-01-01"), nil,
		instant(t, "2024-01-01T00:00:00Z"), "ACTIVE", "Tenant A Person", "")
	if err != nil {
		t.Fatalf("NewPerson A: %v", err)
	}
	inTx(t, db, func(tx pgx.Tx) error {
		_, err := store.PutPerson(ctx, tx, pA)
		return err
	})

	personB := uuid.New()
	pB, err := aggregates.NewPerson(tenantB, personB, date(t, "2024-01-01"), nil,
		instant(t, "2024-01-01T00:00:00Z"), "ACTIVE", "Tenant B Person", "")
	if err != nil {
		t.Fatalf("NewPerson B: %v", err)
	}
	inTx(t, db, func(tx pgx.Tx) error {
		_, err := store.PutPerson(ctx, tx, pB)
		return err
	})

	connA := asAppRole(t, db, tenantA)
	got, err := store.CurrentPerson(ctx, connA, tenantA, personA, date(t, "2024-06-01"))
	if err != nil {
		t.Fatalf("tenant A reading its own person: %v", err)
	}
	if got.LegalName != "Tenant A Person" {
		t.Fatalf("tenant A read %q, want its own row", got.LegalName)
	}

	if _, err := store.CurrentPerson(ctx, connA, tenantB, personB, date(t, "2024-06-01")); !errors.Is(err, aggregates.ErrNotFound) {
		t.Fatalf("tenant A's connection read tenant B's person (err=%v); row level security should hide it", err)
	}

	// A raw query naming no tenant_id predicate at all still must not surface
	// tenant B's row on tenant A's session: row level security is enforced
	// by migrations/00008's FORCE ROW LEVEL SECURITY policy on the app.tenant_id
	// session setting, not by this package's own WHERE clause -- a repository
	// bug that forgot to filter on tenant would still be caught here.
	var count int
	if err := connA.QueryRow(ctx, `SELECT count(*) FROM person WHERE entity_id = $1`, personB).Scan(&count); err != nil {
		t.Fatalf("raw count query under tenant A's session: %v", err)
	}
	if count != 0 {
		t.Fatalf("tenant A's session saw %d row(s) for tenant B's person via a query with no tenant_id predicate", count)
	}
}

// TestTodo_DB_008_Mutation exercises the fixture loader end to end and
// proves the loaded rows are consistent with each other across the whole
// DB-008 family (worker -> employment -> assignment chain resolves).
func TestTodo_DB_008_Mutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db)

	var loaded *aggregates.LoadedFixtures
	inTx(t, db, func(tx pgx.Tx) error {
		var err error
		loaded, err = aggregates.LoadFixtures(ctx, tx, tenant)
		return err
	})
	if len(loaded.WorkerID) == 0 {
		t.Fatal("LoadFixtures loaded no workers")
	}

	people := aggregates.PeopleStore{}
	for key, workerID := range loaded.WorkerID {
		worker, err := people.CurrentWorker(ctx, db.Conn, tenant, workerID, farFuture(t))
		if err != nil {
			t.Fatalf("CurrentWorker %s: %v", key, err)
		}
		if worker.PersonRef != loaded.PersonID[key] {
			t.Fatalf("worker %s person_ref = %s, want %s", key, worker.PersonRef, loaded.PersonID[key])
		}

		employment, err := people.CurrentEmployment(ctx, db.Conn, tenant, loaded.EmploymentID[key], farFuture(t))
		if err != nil {
			t.Fatalf("CurrentEmployment %s: %v", key, err)
		}
		if employment.WorkerRef != workerID {
			t.Fatalf("employment %s worker_ref = %s, want %s", key, employment.WorkerRef, workerID)
		}

		assignment, err := people.CurrentAssignment(ctx, db.Conn, tenant, loaded.AssignmentID[key], farFuture(t))
		if err != nil {
			t.Fatalf("CurrentAssignment %s: %v", key, err)
		}
		if assignment.EmploymentRef != loaded.EmploymentID[key] {
			t.Fatalf("assignment %s employment_ref = %s, want %s", key, assignment.EmploymentRef, loaded.EmploymentID[key])
		}
	}
}

// farFuture is a business instant safely after every fixture record's
// effective_from, so a "current as of" read resolves the loaded row
// regardless of which fixture worker it is looking at.
func farFuture(t *testing.T) time.Time {
	t.Helper()
	return date(t, "2030-01-01")
}
