package employeerelationsstore_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/employeerelationsstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/employeerelations"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type fixture struct {
	tenant, other, caseRef, otherCase, compartment                       uuid.UUID
	allegation, investigation, interview, finding, discipline, grievance uuid.UUID
	reporter, subject, investigator, authority, reviewer, representative uuid.UUID
}

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 70); err != nil {
		t.Fatalf("apply migrations through 00070: %v", err)
	}
	migration, err := migrations.FS.ReadFile("00088_employeerelations.sql")
	if err != nil {
		t.Fatalf("read 00088 migration: %v", err)
	}
	up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
	up = strings.Replace(up, "-- +goose Up", "", 1)
	if _, err := db.Conn.Exec(context.Background(), up); err != nil {
		t.Fatalf("apply owned 00088 migration: %v", err)
	}
	return db
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
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
	t.Helper()
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

func makeFixture(t *testing.T, db *pgtest.DB) fixture {
	t.Helper()
	f := fixture{
		tenant: uuid.New(), other: uuid.New(), caseRef: uuid.New(), otherCase: uuid.New(), compartment: uuid.New(),
		allegation: uuid.New(), investigation: uuid.New(), interview: uuid.New(), finding: uuid.New(), discipline: uuid.New(), grievance: uuid.New(),
		reporter: uuid.New(), subject: uuid.New(), investigator: uuid.New(), authority: uuid.New(), reviewer: uuid.New(), representative: uuid.New(),
	}
	for id, key := range map[uuid.UUID]string{f.tenant: "er-primary", f.other: "er-other"} {
		db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
            VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	}
	return f
}

func participants(f fixture) []employeerelations.Participant {
	return []employeerelations.Participant{
		{Role: employeerelations.RoleReporter, Ref: f.reporter.String()},
		{Role: employeerelations.RoleSubject, Ref: f.subject.String()},
		{Role: employeerelations.RoleInvestigator, Ref: f.investigator.String()},
		{Role: employeerelations.RoleReviewer, Ref: f.reviewer.String()},
	}
}

func records(t *testing.T, f fixture) (employeerelations.AllegationRevision, employeerelations.InvestigationRevision, employeerelations.InterviewRevision, employeerelations.FindingRevision, employeerelations.DisciplineRevision, employeerelations.GrievanceRevision) {
	t.Helper()
	p := participants(f)
	allegation, err := employeerelations.NewAllegationRevision(employeerelations.AllegationRevision{ID: f.allegation.String(), CaseRef: f.caseRef.String(), CompartmentRef: f.compartment.String(), Participants: p, ReporterRef: f.reporter.String(), SubjectRef: f.subject.String(), Summary: "protected allegation", Status: employeerelations.AllegationOpen, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	investigation, err := employeerelations.NewInvestigationRevision(employeerelations.InvestigationRevision{ID: f.investigation.String(), CaseRef: f.caseRef.String(), CompartmentRef: f.compartment.String(), Participants: p, AllegationRef: f.allegation.String(), InvestigatorRef: f.investigator.String(), AuthorityRef: f.authority.String(), Purpose: "establish facts", Scope: "reported conduct", Status: employeerelations.InvestigationActive, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	interview, err := employeerelations.NewInterviewRevision(employeerelations.InterviewRevision{ID: f.interview.String(), CaseRef: f.caseRef.String(), CompartmentRef: f.compartment.String(), Participants: p, InvestigationRef: f.investigation.String(), InterviewerRef: f.investigator.String(), IntervieweeRef: f.subject.String(), Statement: "sealed statement", Status: employeerelations.InterviewSealed, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	finding, err := employeerelations.NewFindingRevision(employeerelations.FindingRevision{ID: f.finding.String(), CaseRef: f.caseRef.String(), CompartmentRef: f.compartment.String(), Participants: p, InvestigationRef: f.investigation.String(), InvestigatorRef: f.investigator.String(), SubjectRef: f.subject.String(), ReporterRef: f.reporter.String(), EvidenceStandard: employeerelations.EvidenceMoreLikelyThanNot, EvidenceRefs: []string{"evidence-1"}, Disposition: employeerelations.FindingSubstantiated, Rationale: "evidence supports finding", Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	discipline, err := employeerelations.NewDisciplineRevision(employeerelations.DisciplineRevision{ID: f.discipline.String(), CaseRef: f.caseRef.String(), CompartmentRef: f.compartment.String(), Participants: p, FindingRef: f.finding.String(), SubjectRef: f.subject.String(), Action: "written warning", LegalReviewRef: f.reviewer.String(), RepresentationReviewRef: f.representative.String(), Status: employeerelations.DisciplineApproved, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	grievance, err := employeerelations.NewGrievanceRevision(employeerelations.GrievanceRevision{ID: f.grievance.String(), CaseRef: f.caseRef.String(), CompartmentRef: f.compartment.String(), Participants: p, DecisionRef: f.discipline.String(), GrievantRef: f.subject.String(), Grounds: "decision is contested", Outcome: "under review", Status: employeerelations.GrievanceOpen, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	return allegation, investigation, interview, finding, discipline, grievance
}

func TestTodo_PERSIST_EMPLOYEERELATIONS_001(t *testing.T) {
	db := newDB(t)
	f := makeFixture(t, db)
	a, i, v, finding, discipline, grievance := records(t, f)
	store := employeerelationsstore.New(appConn(t, db))
	ctx := context.Background()
	if err := store.SaveAllegation(ctx, f.tenant.String(), a, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInvestigation(ctx, f.tenant.String(), i, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInterview(ctx, f.tenant.String(), v, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFinding(ctx, f.tenant.String(), finding, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDiscipline(ctx, f.tenant.String(), discipline, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGrievance(ctx, f.tenant.String(), grievance, 0); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadFinding(ctx, f.tenant.String(), finding.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CanonicalDigest != finding.CanonicalDigest || loaded.EvidenceRefs[0] != "evidence-1" {
		t.Fatalf("loaded finding lost data: %+v", loaded)
	}
	for _, table := range []string{"er_allegation_revision", "er_investigation_revision", "er_interview_revision", "er_finding_revision", "er_discipline_revision", "er_grievance_revision"} {
		var count int
		if err := db.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", f.tenant).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s count=%d, want 1", table, count)
		}
	}
}

func TestTodo_PERSIST_EMPLOYEERELATIONS_001_Fault(t *testing.T) {
	db := newDB(t)
	f := makeFixture(t, db)
	a, _, _, _, _, _ := records(t, f)
	store := employeerelationsstore.New(appConn(t, db))
	ctx := context.Background()
	if err := store.SaveAllegation(ctx, f.tenant.String(), a, 0); err != nil {
		t.Fatal(err)
	}
	duplicateErr := store.SaveAllegation(ctx, f.tenant.String(), a, 0)
	var duplicate *employeerelations.StoreError
	if !errors.As(duplicateErr, &duplicate) || duplicate.Code != employeerelations.StoreDuplicateCode {
		t.Fatalf("duplicate=%v, want typed duplicate", duplicateErr)
	}
	next := a
	next.Revision = 2
	next.ParentRevision = 1
	next.ParentDigest = a.CanonicalDigest
	next.Summary = "successor"
	staleErr := store.SaveAllegation(ctx, f.tenant.String(), next, 99)
	var stale *employeerelations.StoreError
	if !errors.As(staleErr, &stale) || stale.Code != employeerelations.StoreStaleCASCode {
		t.Fatalf("stale=%v, want typed stale CAS", staleErr)
	}
}

func TestTodo_PERSIST_EMPLOYEERELATIONS_001_Integration(t *testing.T) {
	db := newDB(t)
	f := makeFixture(t, db)
	a, _, _, finding, _, _ := records(t, f)
	store := employeerelationsstore.New(appConn(t, db))
	ctx := context.Background()
	if err := store.SaveAllegation(ctx, f.tenant.String(), a, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFinding(ctx, f.tenant.String(), finding, 0); err != nil {
		t.Fatal(err)
	}
	wrongCase := finding
	wrongCase.ID = uuid.New().String()
	wrongCase.CaseRef = f.otherCase.String()
	wrongCase, err := employeerelations.NewFindingRevision(wrongCase)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFinding(ctx, f.tenant.String(), wrongCase, 0); err != nil {
		t.Fatal(err)
	}
	discipline := employeerelations.DisciplineRevision{ID: uuid.New().String(), CaseRef: f.caseRef.String(), CompartmentRef: f.compartment.String(), Participants: participants(f), FindingRef: wrongCase.ID, SubjectRef: f.subject.String(), Action: "warning", LegalReviewRef: f.reviewer.String(), RepresentationReviewRef: f.representative.String(), Status: employeerelations.DisciplineProposed, Revision: 1}
	if err := store.SaveDiscipline(ctx, f.tenant.String(), discipline, 0); err == nil {
		t.Fatal("cross-case finding reference was accepted")
	}
}

func TestTodo_PERSIST_EMPLOYEERELATIONS_001_Security(t *testing.T) {
	db := newDB(t)
	f := makeFixture(t, db)
	a, _, _, _, _, _ := records(t, f)
	store := employeerelationsstore.New(appConn(t, db))
	ctx := context.Background()
	if err := store.SaveAllegation(ctx, f.tenant.String(), a, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadAllegation(ctx, f.other.String(), a.ID, 1); err == nil || !errors.Is(err, employeerelations.ErrStoreNotFound) {
		t.Fatalf("foreign tenant load=%v, want not found", err)
	}
	var count int
	if err := inTenantTx(t, appConn(t, db), f.other, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM er_allegation_revision WHERE tenant_id=$1`, f.tenant).Scan(&count)
	}); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("foreign tenant saw %d rows under RLS", count)
	}
}

func TestTodo_PERSIST_EMPLOYEERELATIONS_001_Recovery(t *testing.T) {
	db := newDB(t)
	f := makeFixture(t, db)
	a, _, _, _, _, _ := records(t, f)
	if err := employeerelationsstore.New(appConn(t, db)).SaveAllegation(context.Background(), f.tenant.String(), a, 0); err != nil {
		t.Fatal(err)
	}
	got, err := employeerelationsstore.New(appConn(t, db)).LoadAllegation(context.Background(), f.tenant.String(), a.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.CanonicalDigest != a.CanonicalDigest {
		t.Fatalf("fresh connection digest=%s, want %s", got.CanonicalDigest, a.CanonicalDigest)
	}
}

func TestTodo_PERSIST_EMPLOYEERELATIONS_001_Mutation(t *testing.T) {
	db := newDB(t)
	f := makeFixture(t, db)
	a, i, v, finding, discipline, grievance := records(t, f)
	store := employeerelationsstore.New(appConn(t, db))
	ctx := context.Background()
	if err := store.SaveAllegation(ctx, f.tenant.String(), a, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInvestigation(ctx, f.tenant.String(), i, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInterview(ctx, f.tenant.String(), v, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveFinding(ctx, f.tenant.String(), finding, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDiscipline(ctx, f.tenant.String(), discipline, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveGrievance(ctx, f.tenant.String(), grievance, 0); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"er_allegation_revision", "er_investigation_revision", "er_interview_revision", "er_finding_revision", "er_discipline_revision", "er_grievance_revision"} {
		if err := inTenantTx(t, appConn(t, db), f.tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, "UPDATE "+table+" SET case_ref=case_ref WHERE tenant_id=$1", f.tenant)
			return err
		}); err == nil {
			t.Fatalf("UPDATE %s succeeded", table)
		}
		if err := inTenantTx(t, appConn(t, db), f.tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE tenant_id=$1", f.tenant)
			return err
		}); err == nil {
			t.Fatalf("DELETE %s succeeded", table)
		}
	}
}
