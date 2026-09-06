package meritstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/meritstore"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/merit"
	"github.com/monstercameron/hcm-next/internal/domains/performance"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const meritDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func insertTenant(t *testing.T, db *pgtest.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, "merit-"+id.String(), "merit-"+id.String())
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("SET ROLE: %v", err)
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

func testInstant() values.Instant {
	return values.NewInstant(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
}

func testDecimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	return values.MustDecimal(text, 2, values.RoundingHalfEven)
}

func testCycle(t *testing.T, id string) merit.MeritCycle {
	t.Helper()
	money, err := values.NewMoney("100.00", "USD", 2, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	watermark, err := values.NewSequenceRevision("merit-population", 1)
	if err != nil {
		t.Fatal(err)
	}
	population, err := merit.NewPopulationSnapshot(merit.PopulationSnapshot{
		SnapshotID: "population-" + id, Revision: 1, Frozen: true, FrozenAt: testInstant(), Watermark: watermark,
		Members: []merit.PopulationMember{{ParticipantID: "worker-1", ManagerID: "manager-1", BasePay: money,
			PerformanceRating: testDecimal(t, "4.00"), BandPosition: testDecimal(t, "0.50"), SalaryRevisionRef: "salary-1",
			PerformanceRef: "performance-1", EffectiveAt: testInstant(), KnownAt: testInstant()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	guidelines, err := merit.NewGuidelineMatrix(merit.GuidelineMatrix{
		MatrixID: "matrix-1", Version: "1", Rules: []merit.GuidelineRule{{
			RatingMin: testDecimal(t, "4.00"), RatingMax: testDecimal(t, "4.00"), BandPositionMin: testDecimal(t, "0.00"), BandPositionMax: testDecimal(t, "1.00"), MinimumRate: testDecimal(t, "0.05"), MaximumRate: testDecimal(t, "0.10"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cycle, err := merit.NewMeritCycle(merit.MeritCycle{
		CycleID: id, Revision: 1, Population: population, Guidelines: guidelines, Budget: testDecimal(t, "20.00"),
		Currency: "USD", State: merit.CycleDraft, EffectiveAt: testInstant(), KnownAt: testInstant(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return cycle
}

func TestTodo_PERSIST_MERIT_001(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	store := meritstore.New(appConn(t, db))
	base := testCycle(t, "cycle-primary")
	if err := store.Save(context.Background(), tenant.String(), base); err != nil {
		t.Fatal(err)
	}
	cycle, recommendation, err := base.Propose("worker-1", testDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), tenant.String(), cycle); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background(), tenant.String(), cycle.CycleID, cycle.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != cycle.CanonicalDigest || got.Population.CanonicalDigest != base.Population.CanonicalDigest || len(got.Recommendations) != 1 || got.Recommendations[0].CanonicalDigest != recommendation.CanonicalDigest {
		t.Fatalf("round trip = %+v, want cycle digest %s and recommendation %s", got, cycle.CanonicalDigest, recommendation.CanonicalDigest)
	}
}

func TestTodo_PERSIST_MERIT_001_Fault(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	store := meritstore.New(appConn(t, db))
	base := testCycle(t, "cycle-fault")
	if err := store.Save(context.Background(), tenant.String(), base); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), tenant.String(), base); !errors.Is(err, merit.ErrStoreDuplicate) {
		t.Fatalf("duplicate = %v, want ErrStoreDuplicate", err)
	}
	next, _, err := base.Propose("worker-1", testDecimal(t, "0.05"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), tenant.String(), next); err != nil {
		t.Fatal(err)
	}
	stale := next
	stale.Revision = 3
	stale.ParentRevision = 1
	stale.ParentDigest = base.CanonicalDigest
	stale.CanonicalDigest = ""
	err = store.Save(context.Background(), tenant.String(), stale)
	if !errors.Is(err, merit.ErrStoreStaleCAS) {
		t.Fatalf("stale = %v, want ErrStoreStaleCAS", err)
	}
	var typed *merit.StoreError
	if !errors.As(err, &typed) || typed.Code != merit.StoreStaleCASCode {
		t.Fatalf("stale error = %T/%v, want typed stale-CAS code", err, err)
	}
}

func TestTodo_PERSIST_MERIT_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	base := testCycle(t, "cycle-integration")
	writer := meritstore.New(appConn(t, db))
	if err := writer.Save(context.Background(), tenant.String(), base); err != nil {
		t.Fatal(err)
	}
	cycle, _, err := base.Propose("worker-1", testDecimal(t, "0.10"), "manager-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Save(context.Background(), tenant.String(), cycle); err != nil {
		t.Fatal(err)
	}
	reader := meritstore.New(appConn(t, db))
	got, err := reader.Current(context.Background(), tenant.String(), base.CycleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 2 || got.Recommendations[0].Amount.String() != "10.00" {
		t.Fatalf("fresh connection current = %+v, want revision 2 and amount 10.00", got)
	}
	calibrated, err := cycle.Calibrate("worker-1", merit.CalibrationAdjustment{
		ParticipantID: "worker-1", From: testDecimal(t, "10.00"), To: testDecimal(t, "11.00"),
		Reason: performance.CalibrationReasonEvidence, AdjusterID: "peer-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Save(context.Background(), tenant.String(), calibrated); err != nil {
		t.Fatal(err)
	}
	got, err = meritstore.New(appConn(t, db)).Current(context.Background(), tenant.String(), base.CycleID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != calibrated.Revision || got.Recommendations[0].Amount.String() != "11.00" || len(got.Recommendations[0].Adjustments) != 1 {
		t.Fatalf("calibrated current = %+v, want revision %d and one 11.00 adjustment", got, calibrated.Revision)
	}
}

func TestTodo_PERSIST_MERIT_001_Security(t *testing.T) {
	db := pgtest.New(t)
	alpha, beta := insertTenant(t, db), insertTenant(t, db)
	alphaStore := meritstore.New(appConn(t, db))
	cycle := testCycle(t, "cycle-security")
	if err := alphaStore.Save(context.Background(), alpha.String(), cycle); err != nil {
		t.Fatal(err)
	}
	betaConn := appConn(t, db)
	betaStore := meritstore.New(betaConn)
	if _, err := betaStore.Current(context.Background(), beta.String(), cycle.CycleID); !errors.Is(err, merit.ErrStoreNotFound) {
		t.Fatalf("cross-tenant current = %v, want ErrStoreNotFound", err)
	}
	var count int
	if err := tenantTxErr(betaConn, beta, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM merit_cycle_revision`).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("beta saw %d alpha cycle rows", count)
	}
}

func TestTodo_PERSIST_MERIT_001_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	cycle := testCycle(t, "cycle-recovery")
	if err := meritstore.New(appConn(t, db)).Save(context.Background(), tenant.String(), cycle); err != nil {
		t.Fatal(err)
	}
	got, err := meritstore.New(appConn(t, db)).Load(context.Background(), tenant.String(), cycle.CycleID, cycle.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != cycle.CanonicalDigest || got.Population.SnapshotID != cycle.Population.SnapshotID {
		t.Fatalf("recovered cycle = %+v, want digest %s and snapshot %s", got, cycle.CanonicalDigest, cycle.Population.SnapshotID)
	}
}

func TestTodo_PERSIST_MERIT_001_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db)
	conn := appConn(t, db)
	cycle := testCycle(t, "cycle-mutation")
	if err := meritstore.New(conn).Save(context.Background(), tenant.String(), cycle); err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`UPDATE merit_population_snapshot SET frozen=false WHERE tenant_id=$1`,
		`DELETE FROM merit_population_snapshot WHERE tenant_id=$1`,
		`UPDATE merit_cycle_revision SET state='OPEN' WHERE tenant_id=$1`,
		`DELETE FROM merit_cycle_revision WHERE tenant_id=$1`,
		`UPDATE merit_recommendation SET state='ADJUSTED' WHERE tenant_id=$1`,
		`DELETE FROM merit_recommendation WHERE tenant_id=$1`,
	}
	for _, statement := range statements {
		err := tenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(context.Background(), statement, tenant)
			return err
		})
		if err == nil {
			t.Fatalf("mutation accepted: %s", statement)
		}
	}
}

var _ dbport.Beginner = (*pgxadapter.Conn)(nil)
