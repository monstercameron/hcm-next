package equitystore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/equitystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/equity"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func testDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	value, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func testDate(t *testing.T, text string) values.LocalDate {
	t.Helper()
	date, err := values.ParseLocalDate(text)
	if err != nil {
		t.Fatal(err)
	}
	return date
}

func testPlan(t *testing.T) equity.EquityPlanRevision {
	t.Helper()
	plan, err := equity.NewEquityPlanRevision(equity.EquityPlanRevision{
		PlanID: "plan-2026", Revision: 1, Name: "Long-term Incentive Plan", PoolRef: "pool-2026",
		AuthorizedQuantity: testDecimal(t, "100000.00"), Currency: "USD",
		InstrumentKinds: []equity.InstrumentKind{equity.InstrumentOption, equity.InstrumentRSU}, ApprovalRef: "board-approval",
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func testGrant(t *testing.T, plan equity.EquityPlanRevision) equity.EquityGrant {
	t.Helper()
	worker := "00000000-0000-0000-0000-000000000001"
	grant, err := equity.NewEquityGrant(equity.EquityGrant{
		GrantID: "grant-1", Revision: 1, PlanDigest: plan.CanonicalDigest, PlanRevision: plan.Revision,
		PoolRef: plan.PoolRef, WorkerRef: worker, Instrument: equity.InstrumentOption,
		Quantity: testDecimal(t, "1200.00"), GrantDate: testDate(t, "2026-01-31"), StrikePrice: testDecimal(t, "12.50"), Currency: "USD",
		Vesting: equity.VestingSchedule{CalendarRule: equity.CalendarGregorian, CliffMonths: 12, PeriodicMonths: 3, TrancheCount: 4}, State: equity.GrantProposed,
	})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func testAcceptance(t *testing.T, grant equity.EquityGrant) equity.AcceptanceEvent {
	t.Helper()
	event, err := equity.NewAcceptanceEvent(equity.AcceptanceEvent{
		EventID: "acceptance-1", GrantDigest: grant.CanonicalDigest, GrantRevision: grant.Revision,
		AcceptedBy: "00000000-0000-0000-0000-000000000001", AcceptedAt: values.NewInstant(time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)), EvidenceRef: "signed-document-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
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

func tenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancySet(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func tenancySet(ctx context.Context, tx dbport.Tx, tenant uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT set_config($1,$2,true)`, "app.tenant_id", tenant.String())
	return err
}

func expectMutationRefusal(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, statement string) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancySet(ctx, tx, tenant); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, statement, tenant); err == nil {
		_ = tx.Rollback(ctx)
		t.Fatal("mutation was accepted")
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	// Apply the predecessor tree, then this lane's migration directly. A later
	// concurrent lane currently has an invalid 00082 dollar-quoted statement;
	// stopping at 00070 keeps this package's evidence scoped to its own schema.
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 70); err != nil {
		t.Fatalf("apply predecessor migrations: %v", err)
	}
	body, err := migrations.FS.ReadFile("00090_equity.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, _, ok := strings.Cut(string(body), "-- +goose Down")
	if !ok {
		t.Fatal("equity migration has no down marker")
	}
	if _, err := db.SQL.ExecContext(context.Background(), up); err != nil {
		t.Fatalf("apply equity migration: %v", err)
	}
	return db
}

func TestTodo_PERSIST_EQUITY_001(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	store := equitystore.New(conn)
	tenant := insertTenant(t, db, "equity-primary")
	plan := testPlan(t)
	grant := testGrant(t, plan)
	event := testAcceptance(t, grant)

	if err := store.SavePlan(context.Background(), tenant.String(), plan); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGrant(context.Background(), tenant.String(), grant); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAcceptance(context.Background(), tenant.String(), event); err != nil {
		t.Fatal(err)
	}
	gotPlan, err := store.LoadPlan(context.Background(), tenant.String(), plan.PlanID, plan.Revision)
	if err != nil || gotPlan.CanonicalDigest != plan.CanonicalDigest {
		t.Fatalf("plan reload = %s, %v", gotPlan.CanonicalDigest, err)
	}
	gotGrant, err := store.LoadGrant(context.Background(), tenant.String(), grant.GrantID, grant.Revision)
	if err != nil || gotGrant.CanonicalDigest != grant.CanonicalDigest || gotGrant.Quantity.String() != grant.Quantity.String() {
		t.Fatalf("grant reload = %s quantity=%s, %v", gotGrant.CanonicalDigest, gotGrant.Quantity.String(), err)
	}
	events, err := store.ListAcceptanceEvents(context.Background(), tenant.String(), grant.CanonicalDigest)
	if err != nil || len(events) != 1 || events[0].Digest != event.Digest {
		t.Fatalf("acceptance events = %#v, %v", events, err)
	}
}

func TestTodo_PERSIST_EQUITY_001_Integration(t *testing.T) {
	db := newDB(t)
	writer, reader := appConn(t, db), appConn(t, db)
	store := equitystore.New(writer)
	readStore := equitystore.New(reader)
	tenant := insertTenant(t, db, "equity-integration")
	plan := testPlan(t)
	if err := store.SavePlan(context.Background(), tenant.String(), plan); err != nil {
		t.Fatal(err)
	}
	grant := testGrant(t, plan)
	if err := store.SaveGrant(context.Background(), tenant.String(), grant); err != nil {
		t.Fatal(err)
	}
	got, err := readStore.LoadGrant(context.Background(), tenant.String(), grant.GrantID, grant.Revision)
	if err != nil || got.Digest != grant.Digest {
		t.Fatalf("fresh connection grant = %s, %v", got.Digest, err)
	}
}

func TestTodo_PERSIST_EQUITY_001_Security(t *testing.T) {
	db := newDB(t)
	alpha, beta := insertTenant(t, db, "equity-alpha"), insertTenant(t, db, "equity-beta")
	conn := appConn(t, db)
	store := equitystore.New(conn)
	plan := testPlan(t)
	if err := store.SavePlan(context.Background(), alpha.String(), plan); err != nil {
		t.Fatal(err)
	}
	tenantTx(t, conn, beta, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM equity_plan_revision`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("beta saw %d alpha plan rows", count)
		}
		return nil
	})
	if _, err := store.LoadPlan(context.Background(), beta.String(), plan.PlanID, plan.Revision); !errors.Is(err, equity.ErrStoreNotFound) {
		t.Fatalf("cross-tenant load = %v", err)
	}
}

func TestTodo_PERSIST_EQUITY_001_Recovery(t *testing.T) {
	db := newDB(t)
	writer, reader := appConn(t, db), appConn(t, db)
	store := equitystore.New(writer)
	readStore := equitystore.New(reader)
	tenant := insertTenant(t, db, "equity-recovery")
	plan := testPlan(t)
	grant := testGrant(t, plan)
	if err := store.SavePlan(context.Background(), tenant.String(), plan); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGrant(context.Background(), tenant.String(), grant); err != nil {
		t.Fatal(err)
	}
	fresh := appConn(t, db)
	got, err := equitystore.New(fresh).LoadGrant(context.Background(), tenant.String(), grant.GrantID, grant.Revision)
	if err != nil || got.CanonicalDigest != grant.CanonicalDigest {
		t.Fatalf("fresh connection digest = %s, want %s (%v)", got.CanonicalDigest, grant.CanonicalDigest, err)
	}
	_ = readStore
}

func TestTodo_PERSIST_EQUITY_001_Fault(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	store := equitystore.New(conn)
	tenant := insertTenant(t, db, "equity-fault")
	plan := testPlan(t)
	if err := store.SavePlan(context.Background(), tenant.String(), plan); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(context.Background(), tenant.String(), plan); !errors.Is(err, equity.ErrStoreDuplicate) {
		t.Fatalf("duplicate plan = %v", err)
	}
	stale := plan
	stale.Revision = 2
	stale.ParentDigest = "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"
	stale.SupersedesRevision = 1
	stale.CanonicalDigest, stale.Digest = "", ""
	stale, err := equity.NewEquityPlanRevision(stale)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(context.Background(), tenant.String(), stale); !errors.Is(err, equity.ErrStoreStaleCAS) {
		t.Fatalf("stale plan = %v", err)
	}
	grant := testGrant(t, plan)
	grant.PlanDigest = "sha256:" + "1111111111111111111111111111111111111111111111111111111111111111"
	grant.CanonicalDigest, grant.Digest = "", ""
	grant, err = equity.NewEquityGrant(grant)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGrant(context.Background(), tenant.String(), grant); !errors.Is(err, equity.ErrStorePlanMismatch) {
		t.Fatalf("plan mismatch = %v", err)
	}
}

func TestTodo_PERSIST_EQUITY_001_Mutation(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	store := equitystore.New(conn)
	tenant := insertTenant(t, db, "equity-mutation")
	plan := testPlan(t)
	grant := testGrant(t, plan)
	event := testAcceptance(t, grant)
	if err := store.SavePlan(context.Background(), tenant.String(), plan); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGrant(context.Background(), tenant.String(), grant); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordAcceptance(context.Background(), tenant.String(), event); err != nil {
		t.Fatal(err)
	}
	expectMutationRefusal(t, conn, tenant, `UPDATE equity_plan_revision SET name='changed' WHERE tenant_id=$1`)
	expectMutationRefusal(t, conn, tenant, `DELETE FROM equity_acceptance_event WHERE tenant_id=$1`)
}
