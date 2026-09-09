package locationstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/locationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/location"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var fixedInstant = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_app"); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func tenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, "app.tenant_id", tenant.String()); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func tenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	if err := tenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func testAddress(t *testing.T) location.Address {
	t.Helper()
	a, err := location.NewAddress(location.Address{
		Lines: []string{"1 Main Street", "Suite 4"}, Locality: "New York",
		AdministrativeArea: "New York", PostalCode: "10001", CountryCode: "US", SubdivisionCode: "US-NY",
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func testInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start, _ := values.NewLocalDate(2026, time.January, 1)
	end, _ := values.NewLocalDate(2027, time.January, 1)
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func testKnownAt(t *testing.T) values.KnownAt {
	t.Helper()
	known, err := values.NewKnownAt(values.NewInstant(fixedInstant))
	if err != nil {
		t.Fatal(err)
	}
	return known
}

func testWorkLocation(t *testing.T, id string) location.WorkLocationRevision {
	t.Helper()
	item, err := location.NewWorkLocationRevision(location.WorkLocationRevision{
		LocationID: id, Revision: 1, Address: testAddress(t),
		LocalityCandidates: []string{"New York"}, TimezoneCandidates: []string{"America/New_York"},
		SourceAuthority: "location.registry/v1", Confidence: location.ConfidenceHigh,
		Effective: testInterval(t), KnownAt: testKnownAt(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func testWorksite(t *testing.T, id, locationRef string) location.WorksiteRevision {
	t.Helper()
	item, err := location.NewWorksiteRevision(location.WorksiteRevision{
		WorksiteID: id, Revision: 1, WorkLocationRef: locationRef, Name: "New York Office",
		Address: testAddress(t), LocalityCandidates: []string{"New York"}, TimezoneCandidates: []string{"America/New_York"},
		SourceAuthority: "worksite.registry/v1", Confidence: location.ConfidenceHigh,
		Effective: testInterval(t), KnownAt: testKnownAt(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func testTable(t *testing.T) location.JurisdictionTable {
	t.Helper()
	table, err := location.NewJurisdictionTable(location.JurisdictionTable{
		TableID: "tax-labor-table", Version: "2026.1",
		Rules: []location.JurisdictionRule{
			{CountryCode: "US", SubdivisionCode: "US-NY", Locality: "New York", FederalTax: "US-FED-TAX", StateTax: "US-NY-TAX", LocalTax: "US-NYC-TAX", FederalLabor: "US-FED-LABOR", StateLabor: "US-NY-LABOR", LocalLabor: "US-NYC-LABOR"},
			{CountryCode: "US", FederalTax: "US-FED-TAX", StateTax: "US-DEFAULT-TAX", LocalTax: "US-DEFAULT-LOCAL-TAX", FederalLabor: "US-FED-LABOR", StateLabor: "US-DEFAULT-LABOR", LocalLabor: "US-DEFAULT-LOCAL-LABOR"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func TestTodo_PERSIST_LOCATION_001(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "location-primary")
	store := locationstore.Store{}
	ctx := context.Background()
	workLocation := testWorkLocation(t, "location:001")
	worksite := testWorksite(t, "worksite:001", "location:001")
	table := testTable(t)

	tenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		stored, err := store.PutWorkLocation(ctx, tx, tenant, workLocation)
		if err != nil {
			return err
		}
		if stored.CanonicalDigest != workLocation.CanonicalDigest {
			t.Fatalf("stored location digest = %s, want %s", stored.CanonicalDigest, workLocation.CanonicalDigest)
		}
		storedSite, err := store.PutWorksite(ctx, tx, tenant, worksite)
		if err != nil {
			return err
		}
		if storedSite.CanonicalDigest != worksite.CanonicalDigest {
			t.Fatalf("stored worksite digest = %s, want %s", storedSite.CanonicalDigest, worksite.CanonicalDigest)
		}
		return nil
	})

	// Platform reference data has no tenant column and is inserted by the
	// migration role; the application role can only read it.
	if err := tenantTxErr(db.Conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutJurisdictionTable(ctx, tx, table)
		return err
	}); err != nil {
		t.Fatalf("insert jurisdiction table: %v", err)
	}

	tenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		got, err := store.LoadWorkLocation(ctx, tx, tenant, "location:001", 1)
		if err != nil {
			return err
		}
		if got.CanonicalDigest != workLocation.CanonicalDigest || got.Address.CanonicalDigest != workLocation.Address.CanonicalDigest {
			t.Fatalf("reloaded work location = %+v, want digest %s", got, workLocation.CanonicalDigest)
		}
		gotSite, err := store.LoadWorksite(ctx, tx, tenant, "worksite:001", 1)
		if err != nil {
			return err
		}
		if gotSite.WorkLocationRef != worksite.WorkLocationRef || gotSite.CanonicalDigest != worksite.CanonicalDigest {
			t.Fatalf("reloaded worksite = %+v", gotSite)
		}
		return nil
	})

	gotTable := location.JurisdictionTable{}
	if err := tenantTxErr(db.Conn, tenant, func(tx dbport.Tx) error {
		var err error
		gotTable, err = store.LoadJurisdictionTable(ctx, tx, table.TableID, table.Version)
		return err
	}); err != nil {
		t.Fatalf("load jurisdiction table: %v", err)
	}
	if gotTable.CanonicalDigest != table.CanonicalDigest || len(gotTable.Rules) != len(table.Rules) {
		t.Fatalf("reloaded jurisdiction table = %+v", gotTable)
	}
}

func TestTodo_PERSIST_LOCATION_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	writer, reader := appConn(t, db), appConn(t, db)
	tenant := insertTenant(t, db, "location-recovery")
	store := locationstore.Store{}
	in := testWorkLocation(t, "location:recovery")
	tenantTx(t, writer, tenant, func(tx dbport.Tx) error {
		_, err := store.PutWorkLocation(context.Background(), tx, tenant, in)
		return err
	})
	var got location.WorkLocationRevision
	tenantTx(t, reader, tenant, func(tx dbport.Tx) error {
		var err error
		got, err = store.LoadWorkLocation(context.Background(), tx, tenant, in.LocationID, in.Revision)
		return err
	})
	if got.CanonicalDigest != in.CanonicalDigest || got.Effective.String() != in.Effective.String() {
		t.Fatalf("fresh connection lost durable location: got digest=%s interval=%s", got.CanonicalDigest, got.Effective.String())
	}
}

func TestTodo_PERSIST_LOCATION_001_Security(t *testing.T) {
	db := pgtest.New(t)
	alpha, beta := insertTenant(t, db, "location-alpha"), insertTenant(t, db, "location-beta")
	conn := appConn(t, db)
	store := locationstore.Store{}
	in := testWorkLocation(t, "location:isolated")
	tenantTx(t, conn, alpha, func(tx dbport.Tx) error {
		_, err := store.PutWorkLocation(context.Background(), tx, alpha, in)
		return err
	})
	var count int
	tenantTx(t, conn, beta, func(tx dbport.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM work_location_revision`).Scan(&count); err != nil {
			return err
		}
		_, err := store.LoadWorkLocation(context.Background(), tx, beta, in.LocationID, in.Revision)
		if !errors.Is(err, locationstore.ErrNotFound) {
			t.Fatalf("cross-tenant load = %v, want ErrNotFound", err)
		}
		return nil
	})
	if count != 0 {
		t.Fatalf("tenant beta saw %d alpha rows", count)
	}
}

func TestTodo_PERSIST_LOCATION_001_Recovery(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "location-recovery-matrix")
	store := locationstore.Store{}
	in := testWorkLocation(t, "location:matrix")
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutWorkLocation(context.Background(), tx, tenant, in)
		return err
	})
	fresh := appConn(t, db)
	tenantTx(t, fresh, tenant, func(tx dbport.Tx) error {
		got, err := store.GetWorkLocation(context.Background(), tx, tenant, "location:matrix", 1)
		if err != nil {
			return err
		}
		if got.CanonicalDigest != in.CanonicalDigest {
			t.Fatalf("fresh connection digest = %s, want %s", got.CanonicalDigest, in.CanonicalDigest)
		}
		return nil
	})
}

func TestTodo_PERSIST_LOCATION_001_Fault(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "location-fault")
	store := locationstore.Store{}
	first := testWorkLocation(t, "location:fault")
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutWorkLocation(context.Background(), tx, tenant, first)
		return err
	})
	err := tenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutWorkLocation(context.Background(), tx, tenant, first)
		return err
	})
	if !errors.Is(err, locationstore.ErrDuplicate) {
		t.Fatalf("duplicate revision = %v, want ErrDuplicate", err)
	}
	var duplicate *locationstore.Error
	if !errors.As(err, &duplicate) || duplicate.Code != locationstore.CodeDuplicateRevision {
		t.Fatalf("duplicate revision error = %T/%v, want typed duplicate code", err, err)
	}
	stale, err := location.NewWorkLocationRevision(location.WorkLocationRevision{
		LocationID: first.LocationID, Revision: 3, ParentRevision: 1, ParentDigest: first.CanonicalDigest,
		Address: testAddress(t), SourceAuthority: "stale", Confidence: location.ConfidenceHigh,
		Effective: testInterval(t), KnownAt: testKnownAt(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	next, err := first.Successor(location.WorkLocationRevision{Address: testAddress(t), SourceAuthority: "next", Confidence: location.ConfidenceHigh, Effective: testInterval(t), KnownAt: testKnownAt(t)})
	if err != nil {
		t.Fatal(err)
	}
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutWorkLocation(context.Background(), tx, tenant, next)
		return err
	})
	err = tenantTxErr(conn, tenant, func(tx dbport.Tx) error {
		_, err := store.PutWorkLocation(context.Background(), tx, tenant, stale)
		return err
	})
	if !errors.Is(err, locationstore.ErrVersionConflict) {
		t.Fatalf("stale parent = %v, want ErrVersionConflict", err)
	}
	var staleError *locationstore.Error
	if !errors.As(err, &staleError) || staleError.Code != locationstore.CodeVersionConflict {
		t.Fatalf("stale parent error = %T/%v, want typed conflict code", err, err)
	}
}

func TestTodo_PERSIST_LOCATION_001_Mutation(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "location-mutation")
	store := locationstore.Store{}
	in := testWorkLocation(t, "location:immutable")
	site := testWorksite(t, "worksite:immutable", in.LocationID)
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if _, err := store.PutWorkLocation(context.Background(), tx, tenant, in); err != nil {
			return err
		}
		_, err := store.PutWorksite(context.Background(), tx, tenant, site)
		return err
	})
	for _, statement := range []string{
		`UPDATE work_location_revision SET source_authority='rewritten' WHERE tenant_id=$1`,
		`DELETE FROM work_location_revision WHERE tenant_id=$1`,
		`UPDATE worksite_revision SET name='rewritten' WHERE tenant_id=$1`,
		`DELETE FROM worksite_revision WHERE tenant_id=$1`,
	} {
		err := tenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(context.Background(), statement, tenant)
			return err
		})
		if err == nil {
			t.Fatalf("immutable table accepted %s", statement)
		}
	}
}
