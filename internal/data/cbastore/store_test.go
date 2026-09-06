package cbastore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/cbastore"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/cba"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var cbaTestNow = time.Date(2026, time.September, 5, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 76); err != nil {
		t.Fatalf("apply migrations through 00076: %v", err)
	}
	return db
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-cba',$3,'ACTIVE',$4)`, id, key, key, cbaTestNow)
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

func agreement(revision string, retired bool) cba.AgreementRevision {
	return cba.AgreementRevision{
		ID: "agreement-row-" + revision, AgreementID: "agreement-1", Revision: revision,
		Version: "2026." + revision, Title: "Engineers", Representative: "union-1", Source: "cba-register",
		EffectiveFrom: cbaTestNow, KnownFrom: cbaTestNow, Precedence: 10, Retired: retired,
	}
}

func unit(revision string, retired bool) cba.BargainingUnitRevision {
	return cba.BargainingUnitRevision{
		ID: "unit-row-" + revision, UnitID: "unit-1", Revision: revision, AgreementID: "agreement-1",
		Name: "Engineering", Representative: "union-1", Source: "unit-register",
		EffectiveFrom: cbaTestNow, KnownFrom: cbaTestNow, Retired: retired,
	}
}

func membership(revision string, retired bool) cba.MembershipRevision {
	return cba.MembershipRevision{
		ID: "membership-row-" + revision, MembershipID: "membership-1", Revision: revision,
		WorkerID: "worker-1", UnitID: "unit-1", Source: "membership-register",
		EffectiveFrom: cbaTestNow, KnownFrom: cbaTestNow, Retired: retired,
	}
}

func clause(revision string, retired bool) cba.AgreementClauseRevision {
	return cba.AgreementClauseRevision{
		ID: "clause-1", AgreementID: "agreement-1", AgreementRevision: "1", Revision: revision,
		Kind: "OVERTIME", Source: "clause-register", JobCodes: []string{"ENG"}, LocationIDs: []string{"NYC"},
		EffectiveFrom: cbaTestNow, KnownFrom: cbaTestNow, Retired: retired,
	}
}

func savePrimarySet(t *testing.T, store *cbastore.Store, tenant uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := store.SaveAgreement(ctx, tenant.String(), agreement("1", false), ""); err != nil {
		t.Fatalf("SaveAgreement: %v", err)
	}
	if err := store.SaveBargainingUnit(ctx, tenant.String(), unit("1", false), ""); err != nil {
		t.Fatalf("SaveBargainingUnit: %v", err)
	}
	if err := store.SaveMembership(ctx, tenant.String(), membership("1", false), ""); err != nil {
		t.Fatalf("SaveMembership: %v", err)
	}
	if err := store.SaveAgreementClause(ctx, tenant.String(), clause("1", false), ""); err != nil {
		t.Fatalf("SaveAgreementClause: %v", err)
	}
}

func TestTodo_PERSIST_CBA_001(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "cba-primary")
	store := cbastore.New(appConn(t, db))
	savePrimarySet(t, store, tenant)

	agreementRow, err := store.LoadAgreement(context.Background(), tenant.String(), "agreement-1", "1")
	if err != nil {
		t.Fatalf("LoadAgreement: %v", err)
	}
	if agreementRow.AgreementID != "agreement-1" || agreementRow.Title != "Engineers" || agreementRow.Precedence != 10 {
		t.Fatalf("agreement = %+v", agreementRow)
	}
	unitRow, err := store.LoadBargainingUnit(context.Background(), tenant.String(), "unit-1", "1")
	if err != nil || unitRow.AgreementID != "agreement-1" {
		t.Fatalf("unit = %+v, err=%v", unitRow, err)
	}
	membershipRow, err := store.LoadMembership(context.Background(), tenant.String(), "membership-1", "1")
	if err != nil || membershipRow.WorkerID != "worker-1" {
		t.Fatalf("membership = %+v, err=%v", membershipRow, err)
	}
	clauseRow, err := store.LoadAgreementClause(context.Background(), tenant.String(), "clause-1", "1")
	if err != nil || clauseRow.Kind != "OVERTIME" || len(clauseRow.JobCodes) != 1 {
		t.Fatalf("clause = %+v, err=%v", clauseRow, err)
	}
	for _, table := range []string{"cba_agreement_revision", "cba_bargaining_unit_revision", "cba_membership_revision", "cba_agreement_clause_revision"} {
		var count int
		if err := db.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", tenant).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s count=%d, want 1", table, count)
		}
	}
}

func TestTodo_PERSIST_CBA_001_Fault(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "cba-fault")
	store := cbastore.New(appConn(t, db))
	if err := store.SaveAgreement(context.Background(), tenant.String(), agreement("1", false), ""); err != nil {
		t.Fatal(err)
	}
	duplicateErr := store.SaveAgreement(context.Background(), tenant.String(), agreement("1", false), "1")
	var duplicate *cba.StoreError
	if !errors.As(duplicateErr, &duplicate) || duplicate.Code != cba.StoreDuplicateCode {
		t.Fatalf("duplicate error=%v, want typed %s", duplicateErr, cba.StoreDuplicateCode)
	}
	staleErr := store.SaveAgreement(context.Background(), tenant.String(), agreement("2", false), "0")
	var stale *cba.StoreError
	if !errors.As(staleErr, &stale) || stale.Code != cba.StoreStaleCASCode || stale.Expected != "0" || stale.Actual != "1" {
		t.Fatalf("stale error=%v, want typed stale CAS", staleErr)
	}
	bad := agreement("3", false)
	bad.KnownTo = cbaTestNow.Add(-time.Minute)
	if err := store.SaveAgreement(context.Background(), tenant.String(), bad, "1"); err == nil {
		t.Fatal("known_to before known_from was accepted")
	}
}

func TestTodo_PERSIST_CBA_001_Integration(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "cba-integration")
	store := cbastore.New(appConn(t, db))
	if err := store.SaveAgreement(context.Background(), tenant.String(), agreement("1", false), ""); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAgreement(context.Background(), tenant.String(), agreement("2", true), "1"); err != nil {
		t.Fatal(err)
	}
	current, err := store.CurrentAgreement(context.Background(), tenant.String(), "agreement-1")
	if err != nil || current.Revision != "2" || !current.Retired {
		t.Fatalf("current=%+v err=%v", current, err)
	}
	history, err := store.ListAgreementRevisions(context.Background(), tenant.String(), "agreement-1")
	if err != nil || len(history) != 2 || history[0].Revision != "1" || history[1].Revision != "2" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
}

func TestTodo_PERSIST_CBA_001_Security(t *testing.T) {
	db := newDB(t)
	alpha, beta := insertTenant(t, db, "cba-alpha"), insertTenant(t, db, "cba-beta")
	store := cbastore.New(appConn(t, db))
	savePrimarySet(t, store, alpha)
	if err := store.SaveAgreement(context.Background(), beta.String(), agreement("1", false), ""); err != nil {
		t.Fatal(err)
	}
	conn := appConn(t, db)
	err := inTenantTx(t, conn, alpha, func(tx dbport.Tx) error {
		for _, table := range []string{"cba_agreement_revision", "cba_bargaining_unit_revision", "cba_membership_revision", "cba_agreement_clause_revision"} {
			var count int
			if err := tx.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", beta).Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return errors.New("cross-tenant rows visible in " + table)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTodo_PERSIST_CBA_001_Recovery(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "cba-recovery")
	first := cbastore.New(appConn(t, db))
	if err := first.SaveAgreement(context.Background(), tenant.String(), agreement("1", false), ""); err != nil {
		t.Fatal(err)
	}
	fresh := cbastore.New(appConn(t, db))
	got, err := fresh.LoadAgreement(context.Background(), tenant.String(), "agreement-1", "1")
	if err != nil {
		t.Fatalf("load after fresh connection: %v", err)
	}
	if got.Title != "Engineers" || got.KnownFrom.IsZero() {
		t.Fatalf("fresh row=%+v", got)
	}
}

func TestTodo_PERSIST_CBA_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "cba-mutation")
	store := cbastore.New(appConn(t, db))
	savePrimarySet(t, store, tenant)
	for _, table := range []string{"cba_agreement_revision", "cba_bargaining_unit_revision", "cba_membership_revision", "cba_agreement_clause_revision"} {
		if err := db.ExecErr("UPDATE "+table+" SET retired=retired WHERE tenant_id=$1", tenant); err == nil {
			t.Fatalf("UPDATE %s succeeded on immutable revision", table)
		}
		if err := db.ExecErr("DELETE FROM "+table+" WHERE tenant_id=$1", tenant); err == nil {
			t.Fatalf("DELETE %s succeeded on immutable revision", table)
		}
	}
}
