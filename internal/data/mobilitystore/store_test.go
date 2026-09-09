package mobilitystore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/mobilitystore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/mobility"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var storeNow = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 70); err != nil {
		t.Fatalf("apply stable migrations through 00070: %v", err)
	}
	raw, err := migrations.FS.ReadFile("00102_mobility.sql")
	if err != nil {
		t.Fatalf("read migration 00102: %v", err)
	}
	parts := strings.Split(string(raw), "-- +goose Down")
	if len(parts) != 2 {
		t.Fatal("migration 00102 has no goose Down marker")
	}
	up := strings.SplitN(parts[0], "-- +goose Up", 2)
	if len(up) != 2 {
		t.Fatal("migration 00102 has no goose Up marker")
	}
	if _, err := db.Conn.Exec(context.Background(), up[1]); err != nil {
		t.Fatalf("apply migration 00102: %v", err)
	}
	return db
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',$4)`, id, key, key, storeNow)
	return id
}

func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("set application role: %v", err)
	}
	return conn
}

func inTenant(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) {
	t.Helper()
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		t.Fatal(err)
	}
	if err := fn(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func inTenantErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	tx, err := conn.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(context.Background())
}

func localInterval(t *testing.T, start, end int) values.EffectiveInterval {
	t.Helper()
	a, err := values.NewLocalDate(2026, time.January, start)
	if err != nil {
		t.Fatal(err)
	}
	b, err := values.NewLocalDate(2026, time.January, end)
	if err != nil {
		t.Fatal(err)
	}
	iv, err := values.NewLocalDateInterval(a, b, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	return iv
}

func decimal(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func assignment(t *testing.T, id string, role mobility.AssignmentRole, start, end int) mobility.AssignmentRevision {
	t.Helper()
	a, err := mobility.NewAssignmentRevision(mobility.AssignmentRevision{AssignmentID: id, Revision: 1, Role: role, Type: mobility.LongTerm, EntityRef: uuid.NewString(), EmploymentRef: "employment:001", LocationRef: "location:" + id, Jurisdiction: "US-CA", PayrollRef: "payroll:" + id, PayrollModel: mobility.PayrollSplit, Effective: localInterval(t, start, end), CostAllocationSplit: decimal(t, "0.50"), SourceAuthority: "hr.authority/v1"})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func testPlan(t *testing.T, id, worker string) mobility.MobilityPlan {
	t.Helper()
	relocation, err := mobility.NewRelocationPackage(mobility.RelocationPackage{PackageID: "relocation:" + id, MobilityID: id, WorkerRef: worker, OwnerRef: "mobility-owner", Milestones: []mobility.RelocationMilestone{{MilestoneID: "travel", Kind: mobility.RelocationTravel, Effective: localInterval(t, 3, 4), OwnerRef: "mobility-owner", EvidenceRef: "evidence:travel"}}})
	if err != nil {
		t.Fatal(err)
	}
	kinds := []mobility.ImmigrationMilestoneKind{mobility.ImmigrationPetition, mobility.ImmigrationApproval, mobility.ImmigrationExpiry, mobility.ImmigrationReverification}
	immigration := make([]mobility.ImmigrationMilestone, len(kinds))
	for i, kind := range kinds {
		date, err := values.NewLocalDate(2026, time.March, i+1)
		if err != nil {
			t.Fatal(err)
		}
		immigration[i], err = mobility.NewImmigrationMilestone(mobility.ImmigrationMilestone{MilestoneID: string(kind), ProcessRef: "process:" + id, Kind: kind, Jurisdiction: "DE-BE", At: date, SourceRef: "immigration.source/v1", Disclosure: mobility.DisclosureScope{Jurisdiction: "DE-BE", Allowed: true}})
		if err != nil {
			t.Fatal(err)
		}
	}
	obligations := []mobility.MobilityObligation{{Kind: mobility.ObligationPayroll, Ref: "payroll-review", Status: mobility.ObligationReady, OwnerRef: "payroll", EvidenceRef: "evidence:payroll"}, {Kind: mobility.ObligationTax, Ref: "tax-review", Status: mobility.ObligationReady, OwnerRef: "tax", EvidenceRef: "evidence:tax"}, {Kind: mobility.ObligationPE, Ref: "pe-review", Status: mobility.ObligationReady, OwnerRef: "legal", EvidenceRef: "evidence:pe"}, {Kind: mobility.ObligationPrivacy, Ref: "privacy-review", Status: mobility.ObligationReady, OwnerRef: "privacy", EvidenceRef: "evidence:privacy"}}
	plan, err := mobility.NewMobilityPlan(mobility.MobilityPlan{MobilityID: id, Revision: 1, WorkerRef: worker, HomeAssignment: assignment(t, "home:"+id, mobility.AssignmentHome, 1, 20), HostAssignment: assignment(t, "host:"+id, mobility.AssignmentHost, 5, 15), Legs: []mobility.MobilityLeg{{LegID: "leg:" + id, OriginJurisdiction: "US-CA", DestinationJurisdiction: "DE-BE", Effective: localInterval(t, 5, 15), Purpose: "assignment", WorkPresence: true, SourceRef: "travel:" + id}}, Relocation: relocation, Immigration: immigration, Obligations: obligations})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func successor(t *testing.T, p mobility.MobilityPlan) mobility.MobilityPlan {
	t.Helper()
	next, err := p.Successor(mobility.MobilityPlan{HomeAssignment: p.HomeAssignment, HostAssignment: p.HostAssignment, Legs: p.Legs, Relocation: p.Relocation, Immigration: p.Immigration, Obligations: p.Obligations})
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func TestTodo_PERSIST_MOBILITY_001(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "mobility-primary")
	store := mobilitystore.New(appConn(t, db))
	p := testPlan(t, "mobility:primary", uuid.NewString())
	if err := store.Put(context.Background(), values.TenantId(tenantID.String()), p); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), values.TenantId(tenantID.String()), p.MobilityID, 1)
	if err != nil || got.CanonicalDigest != p.CanonicalDigest || got.WorkerRef != p.WorkerRef {
		t.Fatalf("Get = %#v, %v", got, err)
	}
	if err := store.AppendImmigrationMilestone(context.Background(), values.TenantId(tenantID.String()), p.Immigration[0], 1); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListImmigrationMilestones(context.Background(), values.TenantId(tenantID.String()), p.Immigration[0].ProcessRef)
	if err != nil || len(events) != 1 || events[0].CanonicalDigest != p.Immigration[0].CanonicalDigest {
		t.Fatalf("milestones = %#v, %v", events, err)
	}
}

func TestTodo_PERSIST_MOBILITY_001_Fault(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "mobility-fault")
	store := mobilitystore.New(appConn(t, db))
	tenant := values.TenantId(tenantID.String())
	p := testPlan(t, "mobility:fault", uuid.NewString())
	if err := store.Put(context.Background(), tenant, p); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), tenant, p); !errors.Is(err, mobilitystore.ErrDuplicateRevision) {
		t.Fatalf("duplicate plan = %v", err)
	}
	next := successor(t, p)
	if err := store.Put(context.Background(), tenant, next); err != nil {
		t.Fatal(err)
	}
	stale := successor(t, next)
	stale.ParentRevision, stale.ParentDigest = p.Revision, p.CanonicalDigest
	stale, err := mobility.NewMobilityPlan(stale)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Put(context.Background(), tenant, stale)
	if !errors.Is(err, mobilitystore.ErrVersionConflict) {
		t.Fatalf("stale plan = %v", err)
	}
	if err := store.AppendImmigrationMilestone(context.Background(), tenant, p.Immigration[0], 1); err != nil {
		t.Fatal(err)
	}
	err = store.AppendImmigrationMilestone(context.Background(), tenant, p.Immigration[0], 1)
	if !errors.Is(err, mobilitystore.ErrDuplicateMilestone) {
		t.Fatalf("duplicate milestone = %v", err)
	}
	var classified *mobilitystore.Error
	if !errors.As(err, &classified) || classified.Code != mobilitystore.CodeDuplicateMilestone {
		t.Fatalf("milestone code = %v", err)
	}
}

func TestTodo_PERSIST_MOBILITY_001_Integration(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "mobility-integration")
	tenant := values.TenantId(tenantID.String())
	p := testPlan(t, "mobility:integration", uuid.NewString())
	writer := mobilitystore.New(appConn(t, db))
	if err := writer.Put(context.Background(), tenant, p); err != nil {
		t.Fatal(err)
	}
	reader := mobilitystore.New(appConn(t, db))
	got, err := reader.Load(context.Background(), tenant, p.MobilityID, p.Revision)
	if err != nil || got.CanonicalDigest != p.CanonicalDigest {
		t.Fatalf("fresh reader = %#v, %v", got, err)
	}
}

func TestTodo_PERSIST_MOBILITY_001_Security(t *testing.T) {
	db := newDB(t)
	alpha, beta := insertTenant(t, db, "mobility-security-a"), insertTenant(t, db, "mobility-security-b")
	p := testPlan(t, "mobility:security", uuid.NewString())
	store := mobilitystore.New(appConn(t, db))
	if err := store.Put(context.Background(), values.TenantId(alpha.String()), p); err != nil {
		t.Fatal(err)
	}
	other := mobilitystore.New(appConn(t, db))
	if _, err := other.Get(context.Background(), values.TenantId(beta.String()), p.MobilityID, 1); !errors.Is(err, mobilitystore.ErrNotFound) {
		t.Fatalf("cross-tenant Get = %v", err)
	}
	var count int
	inTenant(t, appConn(t, db), beta, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM mobility_plan`).Scan(&count)
	})
	if count != 0 {
		t.Fatalf("tenant beta saw %d plan rows", count)
	}
}

func TestTodo_PERSIST_MOBILITY_001_Recovery(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "mobility-recovery")
	tenant := values.TenantId(tenantID.String())
	p := testPlan(t, "mobility:recovery", uuid.NewString())
	if err := mobilitystore.New(appConn(t, db)).Put(context.Background(), tenant, p); err != nil {
		t.Fatal(err)
	}
	fresh := mobilitystore.New(appConn(t, db))
	got, err := fresh.Get(context.Background(), tenant, p.MobilityID, 1)
	if err != nil || got.CanonicalDigest != p.CanonicalDigest || len(got.Immigration) != 4 {
		t.Fatalf("recovered plan = %#v, %v", got, err)
	}
}

func TestTodo_PERSIST_MOBILITY_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "mobility-mutation")
	tenant := values.TenantId(tenantID.String())
	p := testPlan(t, "mobility:mutation", uuid.NewString())
	if err := mobilitystore.New(appConn(t, db)).Put(context.Background(), tenant, p); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE mobility_plan SET status='BLOCKED' WHERE tenant_id=$1`, tenantID); err == nil {
		t.Fatal("plan update was accepted")
	}
	if err := db.ExecErr(`DELETE FROM mobility_plan WHERE tenant_id=$1`, tenantID); err == nil {
		t.Fatal("plan delete was accepted")
	}
	app := appConn(t, db)
	if err := inTenantErr(app, tenantID, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE mobility_plan SET status='BLOCKED' WHERE tenant_id=$1`, tenantID)
		return err
	}); err == nil {
		t.Fatal("application plan update was accepted")
	}
}
