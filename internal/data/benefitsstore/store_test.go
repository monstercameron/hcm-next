package benefitsstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/benefits"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 72); err != nil {
		t.Fatalf("apply migrations through 00072: %v", err)
	}
	return db
}

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
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set role: %v", err)
	}
	return conn
}

func tenantTx(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
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

func interval(t *testing.T) values.EffectiveInterval {
	t.Helper()
	start, err := values.NewLocalDate(2026, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2027, time.January, 1)
	if err != nil {
		t.Fatal(err)
	}
	out, err := values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func ref(tenantID uuid.UUID, kind string, id uuid.UUID) values.EntityRef {
	return values.EntityRef{Tenant: values.TenantId(tenantID.String()), Kind: values.Kind(kind), Id: id.String()}
}

func revision(t *testing.T, tenantID uuid.UUID, sequence uint64, rowID uuid.UUID, supersedes uuid.UUID) benefits.PlanRevision {
	t.Helper()
	planID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	planRef := ref(tenantID, "benefit_plan", planID)
	token, err := values.NewSequenceRevision("benefits.plan", sequence)
	if err != nil {
		t.Fatal(err)
	}
	out := benefits.PlanRevision{
		RevisionID: ref(tenantID, "benefit_plan_revision", rowID), PlanID: planRef, Revision: token,
		PlanYear: benefits.PlanYear{PlanID: planRef, Year: 2026, Effective: interval(t)},
		Name:     "Medical PPO", Jurisdiction: "US-NY", Currency: "USD",
		CoverageTiers: []string{"employee", "family"}, Options: []string{"hsa", "telehealth"}, Effective: interval(t),
		Carrier:              ref(tenantID, "carrier", uuid.MustParse("30000000-0000-4000-8000-000000000001")),
		Provider:             ref(tenantID, "provider", uuid.MustParse("30000000-0000-4000-8000-000000000002")),
		Sponsor:              ref(tenantID, "sponsor", uuid.MustParse("30000000-0000-4000-8000-000000000003")),
		RateScheduleRef:      ref(tenantID, "rate_schedule", uuid.MustParse("40000000-0000-4000-8000-000000000001")),
		EligibilityRulesRef:  ref(tenantID, "eligibility_rules", uuid.MustParse("40000000-0000-4000-8000-000000000002")),
		EnrollmentRulesRef:   ref(tenantID, "enrollment_rules", uuid.MustParse("40000000-0000-4000-8000-000000000003")),
		ContributionRulesRef: ref(tenantID, "contribution_rules", uuid.MustParse("40000000-0000-4000-8000-000000000004")),
		Authority:            ref(tenantID, "authority", uuid.MustParse("50000000-0000-4000-8000-000000000001")),
	}
	if supersedes != uuid.Nil {
		out.Supersedes = ref(tenantID, "benefit_plan_revision", supersedes)
	}
	out, err = benefits.NewPlanRevision(out)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTodo_PERSIST_BENEFITS_001(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "benefits-primary")
	store := New(conn)
	first := revision(t, tenantID, 1, uuid.MustParse("20000000-0000-4000-8000-000000000001"), uuid.Nil)
	ctx := context.Background()
	if err := store.Save(ctx, tenantID.String(), first, ""); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(ctx, tenantID.String(), first.PlanID.Id, first.Revision.String())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CanonicalDigest != first.CanonicalDigest || loaded.PlanYear.Effective.String() != first.PlanYear.Effective.String() || loaded.RevisionID != first.RevisionID {
		t.Fatalf("loaded revision = %+v, want digest %q and row %s", loaded, first.CanonicalDigest, first.RevisionID)
	}
	if got := CodeOf(store.Save(ctx, tenantID.String(), first, "")); got != CodeDuplicateRevision {
		t.Fatalf("duplicate code = %q", got)
	}
}

func TestTodo_PERSIST_BENEFITS_001_Fault(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "benefits-fault")
	store := New(conn)
	firstID := uuid.MustParse("20000000-0000-4000-8000-000000000011")
	first := revision(t, tenantID, 1, firstID, uuid.Nil)
	if err := store.Save(context.Background(), tenantID.String(), first, ""); err != nil {
		t.Fatal(err)
	}
	if got := CodeOf(store.Save(context.Background(), tenantID.String(), first, "")); got != CodeDuplicateRevision {
		t.Fatalf("duplicate revision code = %q", got)
	}
	wrong, _ := values.NewSequenceRevision("benefits.plan", 99)
	next := revision(t, tenantID, 2, uuid.MustParse("20000000-0000-4000-8000-000000000012"), firstID)
	if got := CodeOf(store.Save(context.Background(), tenantID.String(), next, wrong.String())); got != CodeStaleCAS {
		t.Fatalf("stale CAS code = %q", got)
	}
}

func TestTodo_PERSIST_BENEFITS_001_Integration(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "benefits-integration")
	store := New(conn)
	firstID := uuid.MustParse("20000000-0000-4000-8000-000000000021")
	first := revision(t, tenantID, 1, firstID, uuid.Nil)
	if err := store.Save(context.Background(), tenantID.String(), first, ""); err != nil {
		t.Fatal(err)
	}
	next := revision(t, tenantID, 2, uuid.MustParse("20000000-0000-4000-8000-000000000022"), firstID)
	if err := store.Save(context.Background(), tenantID.String(), next, first.Revision.String()); err != nil {
		t.Fatal(err)
	}
	current, err := store.Current(context.Background(), tenantID.String(), first.PlanID.Id)
	if err != nil || current.Revision.String() != next.Revision.String() {
		t.Fatalf("current = %s, err=%v", current.Revision, err)
	}
	all, err := store.List(context.Background(), tenantID.String(), first.PlanID.Id)
	if err != nil || len(all) != 2 || all[0].Revision.String() != first.Revision.String() {
		t.Fatalf("history = %v, err=%v", all, err)
	}
}

func TestTodo_PERSIST_BENEFITS_001_Security(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	a := insertTenant(t, db, "benefits-security-a")
	b := insertTenant(t, db, "benefits-security-b")
	store := New(conn)
	if err := store.Save(context.Background(), a.String(), revision(t, a, 1, uuid.MustParse("20000000-0000-4000-8000-000000000031"), uuid.Nil), ""); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), b.String(), revision(t, b, 1, uuid.MustParse("20000000-0000-4000-8000-000000000032"), uuid.Nil), ""); err != nil {
		t.Fatal(err)
	}
	var count int
	tenantTx(t, conn, a, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM benefit_plan_revision WHERE tenant_id=$1`, b).Scan(&count)
	})
	if count != 0 {
		t.Fatalf("tenant A saw %d rows from tenant B", count)
	}
}

func TestTodo_PERSIST_BENEFITS_001_Recovery(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "benefits-recovery")
	first := revision(t, tenantID, 1, uuid.MustParse("20000000-0000-4000-8000-000000000041"), uuid.Nil)
	if err := New(appConn(t, db)).Save(context.Background(), tenantID.String(), first, ""); err != nil {
		t.Fatal(err)
	}
	loaded, err := New(appConn(t, db)).Load(context.Background(), tenantID.String(), first.PlanID.Id, first.Revision.String())
	if err != nil || loaded.CanonicalDigest != first.CanonicalDigest {
		t.Fatalf("fresh connection load = %q, err=%v", loaded.CanonicalDigest, err)
	}
}

func TestTodo_PERSIST_BENEFITS_001_Mutation(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "benefits-mutation")
	store := New(conn)
	firstID := uuid.MustParse("20000000-0000-4000-8000-000000000051")
	first := revision(t, tenantID, 1, firstID, uuid.Nil)
	if err := store.Save(context.Background(), tenantID.String(), first, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE benefit_plan_revision SET name='rewritten' WHERE tenant_id=$1`, tenantID); err == nil {
		t.Fatal("immutable revision accepted UPDATE")
	}
	if err := db.ExecErr(`DELETE FROM benefit_plan_revision WHERE tenant_id=$1`, tenantID); err == nil {
		t.Fatal("immutable revision accepted DELETE")
	}
	otherPlan := uuid.MustParse("10000000-0000-4000-8000-000000000099")
	wrongLineage := db.ExecErr(`
		INSERT INTO benefit_plan_revision
		(tenant_id,row_id,plan_id,revision,supersedes,plan_year,name,jurisdiction,currency,options,effective_from,canonical_digest)
		VALUES ($1,$2,$3,2,$4,2026,'wrong','US-NY','USD','{}',now(),repeat('a',64))`,
		tenantID, uuid.MustParse("20000000-0000-4000-8000-000000000052"), otherPlan, firstID)
	if wrongLineage == nil {
		t.Fatal("cross-plan supersedes was accepted")
	}
	if !errors.Is(store.Save(context.Background(), tenantID.String(), first, ""), benefits.ErrStoreDuplicate) {
		t.Fatal("duplicate revision did not retain its typed domain cause")
	}
}
