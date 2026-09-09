package incentivestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/incentive"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func TestTodo_PERSIST_INCENTIVE_001(t *testing.T) {
	db, tenantID, store := fixture(t)
	plan := testPlan(t, 1, "")
	if err := store.SavePlan(context.Background(), tenantID.String(), plan); err != nil {
		t.Fatal(err)
	}
	observation := testObservation(t, tenantID, plan)
	if err := store.AppendObservation(context.Background(), tenantID.String(), observation, 1); err != nil {
		t.Fatal(err)
	}
	award := testAward(t, tenantID, plan)
	if err := store.SaveAward(context.Background(), tenantID.String(), award); err != nil {
		t.Fatal(err)
	}
	loadedPlan, err := store.LoadPlan(context.Background(), tenantID.String(), plan.PlanID, plan.Revision)
	if err != nil || loadedPlan.Digest != plan.Digest {
		t.Fatalf("loaded plan = %#v, err = %v", loadedPlan, err)
	}
	loadedObservations, err := store.ListObservations(context.Background(), tenantID.String(), observation.WorkerRef, plan.PlanID)
	if err != nil || len(loadedObservations) != 1 {
		t.Fatalf("loaded observations = %#v, err = %v", loadedObservations, err)
	}
	loadedAward, err := store.LoadAward(context.Background(), tenantID.String(), award.CalculationID, award.Revision)
	if err != nil || loadedAward.PlanDigest != award.PlanDigest {
		t.Fatalf("loaded award = %#v, err = %v", loadedAward, err)
	}
	_ = db
}

func TestTodo_PERSIST_INCENTIVE_001_Fault(t *testing.T) {
	_, tenantID, store := fixture(t)
	plan := testPlan(t, 1, "")
	if err := store.SavePlan(context.Background(), tenantID.String(), plan); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePlan(context.Background(), tenantID.String(), plan); !errors.Is(err, incentive.ErrStoreDuplicate) || CodeOf(err) != incentive.StoreDuplicateCode {
		t.Fatalf("duplicate plan = %v, code %s", err, CodeOf(err))
	}
	stalePlan := testPlan(t, 3, plan.Digest)
	if err := store.SavePlan(context.Background(), tenantID.String(), stalePlan); !errors.Is(err, incentive.ErrStoreStaleCAS) || CodeOf(err) != incentive.StoreStaleCASCode {
		t.Fatalf("stale plan = %v, code %s", err, CodeOf(err))
	}
	badAward := testAward(t, tenantID, plan)
	badAward.PlanDigest = "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"
	badAward.CanonicalDigest = ""
	badAward.Digest = ""
	badAward, err := incentive.NewAwardCalculation(badAward)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAward(context.Background(), tenantID.String(), badAward); !errors.Is(err, incentive.ErrStorePlanMismatch) || CodeOf(err) != incentive.StorePlanMismatchCode {
		t.Fatalf("mismatched award = %v, code %s", err, CodeOf(err))
	}
}

func TestTodo_PERSIST_INCENTIVE_001_Integration(t *testing.T) {
	db, tenantID, store := fixture(t)
	plan := testPlan(t, 1, "")
	if err := store.SavePlan(context.Background(), tenantID.String(), plan); err != nil {
		t.Fatal(err)
	}
	var tableCount int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('incentive_plan_revision','attainment_observation','award_calculation')`).Scan(&tableCount); err != nil {
		t.Fatal(err)
	}
	if tableCount != 3 {
		t.Fatalf("incentive table count = %d, want 3", tableCount)
	}
}

func TestTodo_PERSIST_INCENTIVE_001_Security(t *testing.T) {
	db, tenantA, store := fixture(t)
	plan := testPlan(t, 1, "")
	if err := store.SavePlan(context.Background(), tenantA.String(), plan); err != nil {
		t.Fatal(err)
	}
	tenantB := uuid.New()
	insertTenant(t, db, tenantB)
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenantB); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM incentive_plan_revision`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("tenant B saw %d tenant A plan rows", count)
	}
}

func TestTodo_PERSIST_INCENTIVE_001_Recovery(t *testing.T) {
	db, tenantID, store := fixture(t)
	plan := testPlan(t, 1, "")
	if err := store.SavePlan(context.Background(), tenantID.String(), plan); err != nil {
		t.Fatal(err)
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	fresh := New(conn)
	loaded, err := fresh.LoadPlan(context.Background(), tenantID.String(), plan.PlanID, plan.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PlanID != plan.PlanID || loaded.Digest != plan.Digest {
		t.Fatalf("fresh connection loaded %#v", loaded)
	}
}

func TestTodo_PERSIST_INCENTIVE_001_Mutation(t *testing.T) {
	db, tenantID, store := fixture(t)
	plan := testPlan(t, 1, "")
	if err := store.SavePlan(context.Background(), tenantID.String(), plan); err != nil {
		t.Fatal(err)
	}
	observation := testObservation(t, tenantID, plan)
	if err := store.AppendObservation(context.Background(), tenantID.String(), observation, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE attainment_observation SET source_ref='rewritten'`); err == nil {
		t.Fatal("attainment observation update unexpectedly succeeded")
	}
	if err := db.ExecErr(`DELETE FROM attainment_observation`); err == nil {
		t.Fatal("attainment observation delete unexpectedly succeeded")
	}
}

func fixture(t *testing.T) (*pgtest.DB, uuid.UUID, *Store) {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 98); err != nil {
		t.Fatalf("apply migrations through incentive migration: %v", err)
	}
	tenantID := uuid.New()
	insertTenant(t, db, tenantID)
	if _, err := db.Conn.Exec(context.Background(), `SET ROLE hcmnext_app`); err != nil {
		t.Fatal(err)
	}
	return db, tenantID, New(db.Conn)
}

func insertTenant(t *testing.T, db *pgtest.DB, tenantID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,$3,$4,'ACTIVE',$5)`, tenantID, "tenant-"+tenantID.String(), "cell-test", "Test tenant", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func testPlan(t *testing.T, revision uint64, parentDigest string) incentive.IncentivePlanRevision {
	t.Helper()
	plan, err := incentive.NewIncentivePlanRevision(incentive.IncentivePlanRevision{
		PlanID: "plan-2026", Revision: revision, Name: "FY26 plan", Currency: "USD", PeriodRef: "FY26",
		EligibilityRef: "eligible", FormulaRef: "formula", ParentDigest: parentDigest, SupersedesRevision: func() uint64 {
			if revision > 1 {
				return revision - 1
			}
			return 0
		}(), Measures: []incentive.PlanMeasure{{ID: "bookings", Name: "Bookings", Kind: incentive.MeasureBookings,
			Target: decimal(t, "100.00"), Weight: decimal(t, "100.00"), SourceRef: "crm"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func testObservation(t *testing.T, tenantID uuid.UUID, plan incentive.IncentivePlanRevision) incentive.AttainmentObservation {
	t.Helper()
	known, err := values.NewKnownAt(values.NewInstant(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	date, err := values.NewLocalDate(2026, time.January, 31)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := incentive.NewAttainmentObservation(incentive.AttainmentObservation{
		ObservationID: "observation-1", PlanID: plan.PlanID, PlanRevision: plan.Revision, MeasureID: "bookings",
		WorkerRef: uuid.New().String(), Value: decimal(t, "85.25"), AsOfEffective: date, AsKnownAt: known,
		SourceRef: "crm", Watermark: "crm-2026-01-31",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = tenantID
	return observation
}

func testAward(t *testing.T, tenantID uuid.UUID, plan incentive.IncentivePlanRevision) incentive.AwardCalculation {
	t.Helper()
	award, err := incentive.NewAwardCalculation(incentive.AwardCalculation{
		CalculationID: "award-1", WorkerRef: uuid.New().String(), PlanDigest: plan.Digest, PlanRevision: plan.Revision,
		PeriodRef: "FY26", EligibilityRef: "eligible", FormulaRef: "formula", Currency: "USD", State: incentive.AwardCalculated,
		Revision: 1, Amount: decimal(t, "10.00"), Inputs: []incentive.AwardInput{{Name: "bookings", MeasureID: "bookings", ObservationDigest: plan.Digest, Watermark: "crm-2026-01-31", Attainment: decimal(t, "85.25"), Target: decimal(t, "100.00"), Weight: decimal(t, "100.00")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = tenantID
	return award
}

func decimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
