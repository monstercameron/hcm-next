package payrollstore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/payrollstore"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/payroll"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const (
	digestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	digestC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id) VALUES ($1)`, id)
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

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	db.Exec(t, `
		CREATE DOMAIN tenant_ref AS uuid;
		CREATE DOMAIN semantic_key AS text CHECK (VALUE <> '' AND VALUE = btrim(VALUE));
		CREATE DOMAIN cas_version AS bigint CHECK (VALUE >= 1);
		CREATE DOMAIN content_digest AS text CHECK (VALUE ~ '^[0-9a-f]{64}$');
		CREATE TABLE tenant (tenant_id tenant_ref PRIMARY KEY);
		CREATE OR REPLACE FUNCTION forbid_mutation() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = 'restrict_violation';
		END;
		$$;`)
	db.Exec(t, `
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'hcmnext_app') THEN
				CREATE ROLE hcmnext_app NOLOGIN NOSUPERUSER NOBYPASSRLS;
			END IF;
		END
		$$;
		ALTER ROLE hcmnext_app NOSUPERUSER NOBYPASSRLS;`)
	db.Exec(t, `GRANT USAGE ON SCHEMA "`+db.Schema+`" TO hcmnext_app; GRANT SELECT ON tenant TO hcmnext_app;`)
	migration, err := migrations.FS.ReadFile("00046_payroll_run.sql")
	if err != nil {
		t.Fatalf("read payroll migration: %v", err)
	}
	text := string(migration)
	up := strings.SplitN(text, "-- +goose Up", 2)[1]
	up = strings.SplitN(up, "-- +goose Down", 2)[0]
	db.Exec(t, up)
	return db
}

func newRun(t *testing.T) payroll.PayrollRun {
	t.Helper()
	run, err := payroll.NewPayrollRun(
		"run-001", "weekly", payroll.PeriodRef{ID: "period-2026-09", Version: "1", Digest: digestA},
		payroll.PopulationBindingRef{DefinitionID: "population-001", RevisionVersion: "1", Digest: digestB}, digestC)
	if err != nil {
		t.Fatalf("NewPayrollRun: %v", err)
	}
	return run
}

func newPopulation(t *testing.T, run payroll.PayrollRun) payroll.FrozenPopulation {
	t.Helper()
	asOf := values.NewInstant(time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
	population, err := payroll.FreezePopulation(run, asOf, []payroll.PopulationMember{{
		WorkerRef: "worker-001", EmploymentRef: "employment-001", PayGroupRef: run.PayGroupRef,
	}}, payroll.LateEntryPolicyExplicitAmendment)
	if err != nil {
		t.Fatalf("FreezePopulation: %v", err)
	}
	return population
}

func saveBase(t *testing.T, store *payrollstore.Store, tenant uuid.UUID) (payroll.PayrollRun, payroll.FrozenPopulation, payroll.PopulationAmendment) {
	t.Helper()
	ctx := context.Background()
	run := newRun(t)
	if err := store.SaveRun(ctx, tenant, run); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	population := newPopulation(t, run)
	if err := store.SavePopulation(ctx, tenant, population); err != nil {
		t.Fatalf("SavePopulation: %v", err)
	}
	amendment, err := payroll.NewPopulationAmendment(
		payroll.PopulationAmendmentLateEntry,
		payroll.PopulationMember{WorkerRef: "worker-002", EmploymentRef: "employment-002", PayGroupRef: run.PayGroupRef},
		"late hire received after the freeze", values.NewInstant(time.Date(2026, 9, 5, 13, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("NewPopulationAmendment: %v", err)
	}
	if err := store.AppendAmendment(ctx, tenant, run.RunID, 1, amendment); err != nil {
		t.Fatalf("AppendAmendment: %v", err)
	}
	return run, population, amendment
}

func TestTodo_PERSIST_PAYROLL_001(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "payroll-primary")
	store := payrollstore.New(appConn(t, db))
	run, population, amendment := saveBase(t, store, tenant)

	gotRun, err := store.LoadRun(context.Background(), tenant, run.RunID, run.Revision)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	if gotRun != run {
		t.Fatalf("run round trip = %+v, want %+v", gotRun, run)
	}
	gotPopulation, err := store.LoadPopulation(context.Background(), tenant, population.RunID, population.Revision)
	if err != nil {
		t.Fatalf("LoadPopulation: %v", err)
	}
	if gotPopulation.RunID != population.RunID || gotPopulation.Digest != population.Digest || len(gotPopulation.Members) != 1 {
		t.Fatalf("population round trip = %+v, want %+v", gotPopulation, population)
	}
	amendments, err := store.ListAmendments(context.Background(), tenant, run.RunID)
	if err != nil {
		t.Fatalf("ListAmendments: %v", err)
	}
	if len(amendments) != 1 || amendments[0].Digest != amendment.Digest {
		t.Fatalf("amendments = %+v, want one amendment %s", amendments, amendment.Digest)
	}
}

func TestTodo_PERSIST_PAYROLL_001_Fault(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "payroll-fault")
	store := payrollstore.New(appConn(t, db))
	run, _, amendment := saveBase(t, store, tenant)
	if err := store.SaveRun(context.Background(), tenant, run); !errors.Is(err, payroll.ErrDuplicateRevision) {
		t.Fatalf("duplicate run = %v, want ErrDuplicateRevision", err)
	}
	next, err := run.Calculate(digestB)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	next.SupersedesRevision = 0
	next.CanonicalDigest = ""
	if err := store.SaveRun(context.Background(), tenant, next); !errors.Is(err, payroll.ErrStaleRevision) {
		t.Fatalf("stale run = %v, want ErrStaleRevision", err)
	}
	if err := store.AppendAmendment(context.Background(), tenant, run.RunID, 1, amendment); !errors.Is(err, payroll.ErrDuplicateAmendment) {
		t.Fatalf("duplicate amendment = %v, want ErrDuplicateAmendment", err)
	}
}

func TestTodo_PERSIST_PAYROLL_001_Integration(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "payroll-integration")
	store := payrollstore.New(appConn(t, db))
	run, population, amendment := saveBase(t, store, tenant)
	nextRun, err := run.Calculate(digestB)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if err := store.SaveRun(context.Background(), tenant, nextRun); err != nil {
		t.Fatalf("SaveRun revision 2: %v", err)
	}
	nextPopulation, err := population.Amend(amendment)
	if err != nil {
		t.Fatalf("Amend population: %v", err)
	}
	if err := store.SavePopulation(context.Background(), tenant, nextPopulation); err != nil {
		t.Fatalf("SavePopulation revision 2: %v", err)
	}
	loaded, err := store.LoadPopulation(context.Background(), tenant, run.RunID, 2)
	if err != nil {
		t.Fatalf("LoadPopulation revision 2: %v", err)
	}
	if loaded.SupersedesDigest != population.Digest || loaded.AmendmentDigest != amendment.Digest {
		t.Fatalf("successor links = %+v, want supersedes=%s amendment=%s", loaded, population.Digest, amendment.Digest)
	}
}

func TestTodo_PERSIST_PAYROLL_001_Security(t *testing.T) {
	db := newDB(t)
	alpha := insertTenant(t, db, "payroll-alpha")
	beta := insertTenant(t, db, "payroll-beta")
	alphaStore := payrollstore.New(appConn(t, db))
	run := newRun(t)
	if err := alphaStore.SaveRun(context.Background(), alpha, run); err != nil {
		t.Fatalf("SaveRun alpha: %v", err)
	}
	if _, err := alphaStore.LoadRun(context.Background(), beta, run.RunID, run.Revision); !errors.Is(err, payroll.ErrNotFound) {
		t.Fatalf("cross-tenant LoadRun = %v, want ErrNotFound", err)
	}
	otherConn := appConn(t, db)
	otherStore := payrollstore.New(otherConn)
	if _, err := otherStore.LoadRun(context.Background(), beta, run.RunID, run.Revision); !errors.Is(err, payroll.ErrNotFound) {
		t.Fatalf("fresh beta connection cross-tenant LoadRun = %v, want ErrNotFound", err)
	}
}

func TestTodo_PERSIST_PAYROLL_001_Recovery(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "payroll-recovery")
	writer := payrollstore.New(appConn(t, db))
	run, population, _ := saveBase(t, writer, tenant)
	reader := payrollstore.New(appConn(t, db))
	gotRun, err := reader.LoadRun(context.Background(), tenant, run.RunID, run.Revision)
	if err != nil {
		t.Fatalf("fresh connection LoadRun: %v", err)
	}
	gotPopulation, err := reader.LoadPopulation(context.Background(), tenant, population.RunID, population.Revision)
	if err != nil {
		t.Fatalf("fresh connection LoadPopulation: %v", err)
	}
	if gotRun.RunID != run.RunID || gotPopulation.Digest != population.Digest {
		t.Fatalf("recovered values = run %q population %q", gotRun.RunID, gotPopulation.Digest)
	}
}

func TestTodo_PERSIST_PAYROLL_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "payroll-mutation")
	store := payrollstore.New(appConn(t, db))
	run, population, _ := saveBase(t, store, tenant)
	conn := appConn(t, db)
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
		t.Fatalf("WithTenant: %v", err)
	}
	for name, statement := range map[string]string{
		"run update":        `UPDATE payroll_run SET state = 'RELEASED' WHERE tenant_id = $1 AND run_id = $2`,
		"population delete": `DELETE FROM payroll_frozen_population WHERE tenant_id = $1 AND run_id = $2`,
		"amendment update":  `UPDATE payroll_population_amendment SET reason = 'rewritten' WHERE tenant_id = $1 AND run_id = $2`,
		"amendment delete":  `DELETE FROM payroll_population_amendment WHERE tenant_id = $1 AND run_id = $2`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := tx.Exec(context.Background(), statement, tenant, run.RunID); err == nil {
				t.Fatalf("mutation was accepted")
			}
		})
	}
	if strings.TrimSpace(population.Digest) == "" {
		t.Fatal("fixture population has no digest")
	}
}

var _ dbport.Execer = (*pgxadapter.Conn)(nil)
