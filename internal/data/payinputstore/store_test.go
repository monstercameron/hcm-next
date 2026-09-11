package payinputstore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/payinputstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/payinput"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 8); err != nil {
		t.Fatalf("apply prerequisite migrations through 00008: %v", err)
	}
	migration, err := migrations.FS.ReadFile("00104_payinput.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
	db.Exec(t, up)
	return db
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
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
func testInterval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start, err := values.NewLocalDate(2026, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2027, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	interval, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "payroll", Version: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	return interval
}

func testDefinition(t *testing.T, id string, revision uint64, predecessor payinput.Definition) payinput.Definition {
	t.Helper()
	definition := payinput.Definition{
		DefinitionID: id, Code: "BASE", Kind: payinput.KindEarning, Currency: "USD",
		Taxability:       map[payinput.JurisdictionClass]bool{payinput.JurisdictionFederal: true},
		CalculationBasis: payinput.BasisHourly, AccountingCode: "BASE", OwnerRef: "payinput",
		Effective: testInterval(t), Version: "v" + string(rune('0'+revision)), Revision: revision,
		State: payinput.StatePublished,
	}
	if revision > 1 {
		definition.SupersedesRevision = predecessor.Revision
		definition.SupersedesDigest = predecessor.CanonicalDigest
	}
	definition, err := payinput.NewDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func testAssignment(t *testing.T, definition payinput.Definition, id string) payinput.WorkerAssignment {
	t.Helper()
	assignment, err := payinput.NewWorkerAssignment(definition, payinput.WorkerAssignment{
		AssignmentID: id, WorkerRef: uuid.NewString(), Effective: testInterval(t),
		Amount: mustDecimal(t, "25.00"), Recurrence: payinput.RecurrencePerPayroll, RecurrenceRule: "PAY_PERIOD",
	})
	if err != nil {
		t.Fatal(err)
	}
	return assignment
}

func mustDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestTodo_PERSIST_PAYINPUT_001(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "payinput-primary")
	store := payinputstore.New(appConn(t, db))
	definition := testDefinition(t, "earning-base", 1, payinput.Definition{})
	assignment := testAssignment(t, definition, "assignment-primary")
	ctx := context.Background()
	if err := store.SaveDefinition(ctx, tenant.String(), definition, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAssignment(ctx, tenant.String(), assignment); err != nil {
		t.Fatal(err)
	}
	gotDefinition, err := store.LoadDefinition(ctx, tenant.String(), definition.DefinitionID, definition.Revision)
	if err != nil || gotDefinition.CanonicalDigest != definition.CanonicalDigest {
		t.Fatalf("definition = %+v, err=%v", gotDefinition, err)
	}
	gotAssignment, err := store.LoadAssignment(ctx, tenant.String(), assignment.AssignmentID)
	if err != nil || gotAssignment.CanonicalDigest != assignment.CanonicalDigest || gotAssignment.WorkerRef != assignment.WorkerRef {
		t.Fatalf("assignment = %+v, err=%v", gotAssignment, err)
	}
	for _, table := range []string{"payinput_definition", "payinput_worker_assignment"} {
		var count int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", tenant).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("%s count=%d, want 1", table, count)
		}
	}
}

func TestTodo_PERSIST_PAYINPUT_001_Integration(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "payinput-integration")
	definition := testDefinition(t, "earning-integration", 1, payinput.Definition{})
	store := payinputstore.New(appConn(t, db))
	if err := store.SaveDefinition(context.Background(), tenant.String(), definition, 0); err != nil {
		t.Fatal(err)
	}
	fresh := payinputstore.New(appConn(t, db))
	got, err := fresh.CurrentDefinition(context.Background(), tenant.String(), definition.DefinitionID)
	if err != nil || got.CanonicalDigest != definition.CanonicalDigest {
		t.Fatalf("fresh connection definition = %+v, err=%v", got, err)
	}
}

func TestTodo_PERSIST_PAYINPUT_001_Security(t *testing.T) {
	db := newDB(t)
	alpha, beta := insertTenant(t, db, "payinput-alpha"), insertTenant(t, db, "payinput-beta")
	store := payinputstore.New(appConn(t, db))
	definition := testDefinition(t, "earning-isolated", 1, payinput.Definition{})
	if err := store.SaveDefinition(context.Background(), alpha.String(), definition, 0); err != nil {
		t.Fatal(err)
	}
	conn := appConn(t, db)
	var count int
	err := tenantTxErr(conn, beta, func(tx dbport.Tx) error {
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM payinput_definition WHERE tenant_id=$1`, alpha).Scan(&count); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("beta saw %d alpha definitions", count)
	}
	if _, err := store.LoadDefinition(context.Background(), beta.String(), definition.DefinitionID, definition.Revision); !errors.Is(err, payinput.ErrStoreNotFound) {
		t.Fatalf("cross-tenant load = %v", err)
	}
}

func TestTodo_PERSIST_PAYINPUT_001_Recovery(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "payinput-recovery")
	definition := testDefinition(t, "earning-recovery", 1, payinput.Definition{})
	if err := payinputstore.New(appConn(t, db)).SaveDefinition(context.Background(), tenant.String(), definition, 0); err != nil {
		t.Fatal(err)
	}
	got, err := payinputstore.New(appConn(t, db)).LoadDefinition(context.Background(), tenant.String(), definition.DefinitionID, 1)
	if err != nil || got.CanonicalDigest != definition.CanonicalDigest {
		t.Fatalf("reloaded definition = %+v, err=%v", got, err)
	}
}

func TestTodo_PERSIST_PAYINPUT_001_Fault(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "payinput-fault")
	store := payinputstore.New(appConn(t, db))
	first := testDefinition(t, "earning-fault", 1, payinput.Definition{})
	if err := store.SaveDefinition(context.Background(), tenant.String(), first, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDefinition(context.Background(), tenant.String(), first, 0); !errors.Is(err, payinput.ErrStoreDuplicate) {
		t.Fatalf("duplicate = %v", err)
	}
	next := testDefinition(t, first.DefinitionID, 2, first)
	if err := store.SaveDefinition(context.Background(), tenant.String(), next, 99); !errors.Is(err, payinput.ErrStoreStaleCAS) {
		t.Fatalf("stale CAS = %v", err)
	}
	bad := testAssignment(t, first, "assignment-bad-ref")
	bad.DefinitionRef.Digest = "sha256:" + "0" + first.CanonicalDigest[len("sha256:")+1:]
	if err := store.SaveAssignment(context.Background(), tenant.String(), bad); !errors.Is(err, payinput.ErrStoreReference) {
		t.Fatalf("bad definition reference = %v", err)
	}
}

func TestTodo_PERSIST_PAYINPUT_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "payinput-mutation")
	definition := testDefinition(t, "earning-mutation", 1, payinput.Definition{})
	assignment := testAssignment(t, definition, "assignment-mutation")
	store := payinputstore.New(appConn(t, db))
	if err := store.SaveDefinition(context.Background(), tenant.String(), definition, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAssignment(context.Background(), tenant.String(), assignment); err != nil {
		t.Fatal(err)
	}
	conn := appConn(t, db)
	for _, table := range []string{"payinput_definition", "payinput_worker_assignment"} {
		if err := tenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(context.Background(), "UPDATE "+table+" SET canonical_digest=canonical_digest WHERE tenant_id=$1", tenant)
			return err
		}); err == nil {
			t.Fatalf("UPDATE %s succeeded", table)
		}
		if err := tenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(context.Background(), "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
			return err
		}); err == nil {
			t.Fatalf("DELETE %s succeeded", table)
		}
	}
}
