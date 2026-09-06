package safetystore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/safetystore"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/safety"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var safetyAt = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 70); err != nil {
		t.Fatalf("apply migrations through 00070: %v", err)
	}
	body, err := migrations.FS.ReadFile("00112_safety.sql")
	if err != nil {
		t.Fatalf("read 00112 migration: %v", err)
	}
	up := strings.SplitN(string(body), "-- +goose Down", 2)[0]
	up = strings.Replace(up, "-- +goose Up", "", 1)
	if _, err := db.Conn.Exec(context.Background(), up); err != nil {
		t.Fatalf("apply owned 00112 migration: %v", err)
	}
	return db
}

func id(name string) string { return uuid.NewSHA1(uuid.Nil, []byte("hcm-next/safety/"+name)).String() }

func insertTenant(t *testing.T, db *pgtest.DB, name string) uuid.UUID {
	t.Helper()
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-safety',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenant, name, name)
	return tenant
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
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
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

func instant() values.Instant { return values.NewInstant(safetyAt) }

func incident(t *testing.T, revision uint64, parent string, description string) safety.IncidentRevision {
	t.Helper()
	var parentRevision uint64
	if revision > 1 {
		parentRevision = revision - 1
	}
	r, err := safety.NewIncidentRevision(safety.IncidentRevision{ID: id("incident"), CaseRef: id("case"), CompartmentRef: id("operational-compartment"), Revision: revision, ParentRevision: parentRevision, ParentDigest: parent, IncidentAt: instant(), Kind: safety.IncidentInjury, WorkerRef: id("worker"), ReporterRef: id("reporter"), LocationRef: id("location"), Description: description, Status: safety.IncidentOpen})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func allRecords(t *testing.T) (safety.IncidentRevision, safety.InjuryRevision, safety.ReportabilityDeterminationRevision, safety.ClaimRevision, safety.WorkRestrictionRevision, safety.CorrectiveActionRevision) {
	t.Helper()
	i := incident(t, 1, "", "incident description")
	rule, _ := safety.RuleFor(safety.OSHAReportableSevere)
	injury, err := safety.NewInjuryRevision(safety.InjuryRevision{ID: id("injury"), CaseRef: i.CaseRef, CompartmentRef: id("medical-compartment"), Revision: 1, IncidentRef: i.ID, WorkerRef: id("worker"), Kind: safety.InjuryPhysical, MedicalEvidenceRef: id("medical-evidence"), Severity: "restricted"})
	if err != nil {
		t.Fatal(err)
	}
	reportability, err := safety.NewReportabilityDeterminationRevision(safety.ReportabilityDeterminationRevision{ID: id("reportability"), CaseRef: i.CaseRef, CompartmentRef: id("regulatory-compartment"), Revision: 1, IncidentRef: i.ID, IncidentAt: i.IncidentAt, Class: rule.Class, Clock: rule.Clock, RuleCitation: rule.Citation, Deadline: values.NewInstant(i.IncidentAt.Time().Add(rule.Duration)), Rationale: "derived from declared regulatory clock"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := safety.NewClaimRevision(safety.ClaimRevision{ID: id("claim"), CaseRef: i.CaseRef, CompartmentRef: id("claims-compartment"), Revision: 1, IncidentRef: i.ID, WorkerRef: id("worker"), ClaimRef: "claim-ref", AuthorityRef: id("claims-authority"), Status: safety.ClaimSubmitted})
	if err != nil {
		t.Fatal(err)
	}
	restriction, err := safety.NewWorkRestrictionRevision(safety.WorkRestrictionRevision{ID: id("restriction"), CaseRef: i.CaseRef, CompartmentRef: id("medical-compartment"), Revision: 1, IncidentRef: i.ID, WorkerRef: id("worker"), Kind: safety.RestrictionModifiedDuty, MedicalEvidenceRef: id("medical-evidence"), Status: safety.RestrictionActive})
	if err != nil {
		t.Fatal(err)
	}
	action, err := safety.NewCorrectiveActionRevision(safety.CorrectiveActionRevision{ID: id("action"), CaseRef: i.CaseRef, CompartmentRef: id("operational-compartment"), Revision: 1, IncidentRef: i.ID, OwnerRef: id("owner"), DueRule: "verify-before-close", Action: "repair guard", Status: safety.CorrectiveActionOpen})
	if err != nil {
		t.Fatal(err)
	}
	return i, injury, reportability, claim, restriction, action
}

func saveAll(t *testing.T, tx dbport.Tx, tenant uuid.UUID) {
	t.Helper()
	i, injury, reportability, claim, restriction, action := allRecords(t)
	store := safetystore.New(tx, tenant)
	if err := store.SaveIncident(i); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveInjury(injury); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReportability(reportability); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveClaim(claim); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRestriction(restriction); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCorrectiveAction(action); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PERSIST_SAFETY_001(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "safety-primary")
	var want [6]string
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		i, injury, reportability, claim, restriction, action := allRecords(t)
		store := safetystore.New(tx, tenant)
		for _, err := range []error{store.SaveIncident(i), store.SaveInjury(injury), store.SaveReportability(reportability), store.SaveClaim(claim), store.SaveRestriction(restriction), store.SaveCorrectiveAction(action)} {
			if err != nil {
				return err
			}
		}
		want = [6]string{i.CanonicalDigest, injury.CanonicalDigest, reportability.CanonicalDigest, claim.CanonicalDigest, restriction.CanonicalDigest, action.CanonicalDigest}
		return nil
	})
	fresh := appConn(t, db)
	tenantTx(t, fresh, tenant, func(tx dbport.Tx) error {
		i, injury, reportability, claim, restriction, action := allRecords(t)
		store := safetystore.New(tx, tenant)
		gotI, ok := store.GetIncident(i.ID, 1)
		if !ok || gotI.CanonicalDigest != want[0] {
			t.Fatalf("incident was not reloaded")
		}
		gotInjury, ok := store.GetInjury(injury.ID, 1)
		if !ok || gotInjury.CanonicalDigest != want[1] {
			t.Fatalf("injury was not reloaded")
		}
		gotReportability, ok := store.GetReportability(reportability.ID, 1)
		if !ok || gotReportability.Deadline.Time() != reportability.Deadline.Time() || gotReportability.CanonicalDigest != want[2] {
			t.Fatalf("reportability was not reloaded")
		}
		gotClaim, ok := store.GetClaim(claim.ID, 1)
		if !ok || gotClaim.CanonicalDigest != want[3] {
			t.Fatalf("claim was not reloaded")
		}
		gotRestriction, ok := store.GetRestriction(restriction.ID, 1)
		if !ok || gotRestriction.CanonicalDigest != want[4] {
			t.Fatalf("restriction was not reloaded")
		}
		gotAction, ok := store.GetCorrectiveAction(action.ID, 1)
		if !ok || gotAction.CanonicalDigest != want[5] {
			t.Fatalf("corrective action was not reloaded")
		}
		return nil
	})
}

func TestTodo_PERSIST_SAFETY_001_Integration(t *testing.T) {
	db := newDB(t)
	writer, reader := appConn(t, db), appConn(t, db)
	tenant := insertTenant(t, db, "safety-integration")
	want := incident(t, 1, "", "durable incident")
	tenantTx(t, writer, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(want) })
	tenantTx(t, reader, tenant, func(tx dbport.Tx) error {
		got, ok := safetystore.New(tx, tenant).GetIncident(want.ID, 1)
		if !ok || got.CanonicalDigest != want.CanonicalDigest {
			t.Fatalf("fresh connection lost incident")
		}
		return nil
	})
}

func TestTodo_PERSIST_SAFETY_001_Recovery(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "safety-recovery")
	want := incident(t, 1, "", "recovery incident")
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(want) })
	fresh := appConn(t, db)
	tenantTx(t, fresh, tenant, func(tx dbport.Tx) error {
		got, ok := safetystore.New(tx, tenant).GetIncident(want.ID, 1)
		if !ok || got.Description != want.Description {
			t.Fatalf("row did not survive a fresh connection")
		}
		return nil
	})
}

func TestTodo_PERSIST_SAFETY_001_Fault(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "safety-fault")
	first := incident(t, 1, "", "first")
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(first) })
	err := tenantTxErr(conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(first) })
	if !errors.Is(err, safetystore.ErrDuplicate) {
		t.Fatalf("duplicate = %v", err)
	}
	var duplicate *safetystore.Error
	if !errors.As(err, &duplicate) || duplicate.Code != safetystore.CodeDuplicateRevision {
		t.Fatalf("duplicate code = %v", err)
	}
	nextA := incident(t, 2, first.CanonicalDigest, "successor A")
	nextB := incident(t, 2, first.CanonicalDigest, "successor B")
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(nextA) })
	err = tenantTxErr(conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveIncident(nextB) })
	if !errors.Is(err, safetystore.ErrVersionConflict) {
		t.Fatalf("stale CAS = %v", err)
	}
	var stale *safetystore.Error
	if !errors.As(err, &stale) || stale.Code != safetystore.CodeVersionConflict {
		t.Fatalf("stale code = %v", err)
	}
	wrongCase := nextA
	wrongCase.ID = id("wrong-case-claim")
	wrongCase.CaseRef = id("other-case")
	_, _, _, claim, _, _ := allRecords(t)
	claim.CaseRef = wrongCase.CaseRef
	claim.IncidentRef = first.ID
	err = tenantTxErr(conn, tenant, func(tx dbport.Tx) error { return safetystore.New(tx, tenant).SaveClaim(claim) })
	if !errors.Is(err, safetystore.ErrReferenceConflict) {
		t.Fatalf("cross-case incident = %v", err)
	}
}

func TestTodo_PERSIST_SAFETY_001_Security(t *testing.T) {
	db := newDB(t)
	alpha, beta := insertTenant(t, db, "safety-alpha"), insertTenant(t, db, "safety-beta")
	conn := appConn(t, db)
	want := incident(t, 1, "", "alpha-only")
	tenantTx(t, conn, alpha, func(tx dbport.Tx) error { return safetystore.New(tx, alpha).SaveIncident(want) })
	tenantTx(t, conn, beta, func(tx dbport.Tx) error {
		var count int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM safety_incident_revision`).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("beta saw %d alpha rows", count)
		}
		if _, ok := safetystore.New(tx, beta).GetIncident(want.ID, 1); ok {
			t.Fatal("cross-tenant read succeeded")
		}
		return nil
	})
}

func TestTodo_PERSIST_SAFETY_001_Mutation(t *testing.T) {
	db := newDB(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "safety-mutation")
	tenantTx(t, conn, tenant, func(tx dbport.Tx) error { saveAll(t, tx, tenant); return nil })
	for _, table := range []string{"safety_incident_revision", "safety_injury_revision", "safety_reportability_revision", "safety_claim_revision", "safety_work_restriction_revision", "safety_corrective_action_revision"} {
		for _, operation := range []string{"UPDATE " + table + " SET canonical_digest=canonical_digest WHERE tenant_id=$1", "DELETE FROM " + table + " WHERE tenant_id=$1"} {
			err := tenantTxErr(conn, tenant, func(tx dbport.Tx) error { _, err := tx.Exec(context.Background(), operation, tenant); return err })
			if err == nil {
				t.Fatalf("%s accepted mutation", table)
			}
		}
	}
}
