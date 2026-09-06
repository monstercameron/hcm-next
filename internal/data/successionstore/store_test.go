package successionstore_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/data/successionstore"
	"github.com/monstercameron/hcm-next/internal/data/tenancy"
	"github.com/monstercameron/hcm-next/internal/domains/succession"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var successionFixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	// The repository currently has unrelated later migration failures and a
	// heavily contended shared test cluster. This lane needs only the platform
	// control trigger and tenant primitives before its exact Up section.
	if _, err := db.Provider(t).UpTo(context.Background(), 2); err != nil {
		t.Fatalf("apply migrations through 00002: %v", err)
	}
	if _, err := db.SQL.ExecContext(context.Background(), `
		DO $$
		BEGIN
			CREATE ROLE hcmnext_app
				NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT
				NOLOGIN NOREPLICATION NOBYPASSRLS;
		EXCEPTION
			WHEN duplicate_object OR unique_violation THEN NULL;
		END
		$$`); err != nil {
		t.Fatalf("create app role: %v", err)
	}
	if _, err := db.SQL.ExecContext(context.Background(), fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO hcmnext_app", db.Schema)); err != nil {
		t.Fatalf("grant schema usage: %v", err)
	}
	body, err := migrations.FS.ReadFile("00120_succession.sql")
	if err != nil {
		t.Fatalf("read succession migration: %v", err)
	}
	upStart := strings.Index(string(body), "-- +goose Up")
	downStart := strings.Index(string(body), "-- +goose Down")
	if upStart < 0 || downStart < 0 || downStart <= upStart {
		t.Fatal("succession migration is missing ordered Up/Down sections")
	}
	if _, err := db.SQL.ExecContext(context.Background(), string(body[upStart:downStart])); err != nil {
		t.Fatalf("apply migration 00120: %v", err)
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

func successionInstant() values.Instant {
	return values.NewInstant(successionFixedInstant)
}

func roleFixture(t *testing.T, id string, revision uint64, parent succession.CriticalRole) succession.CriticalRole {
	t.Helper()
	in := succession.CriticalRole{
		RoleID: id, Revision: revision, PositionRef: "position-" + id, JobRevisionRef: "job-1",
		OwnerRef: "owner-1", AuthorityRef: "authority-1", EffectiveAt: successionInstant(), KnownAt: successionInstant(),
		EvidenceRefs: []string{"evidence-role-" + id},
	}
	if revision > 1 {
		in.ParentRevision = parent.Revision
		in.ParentDigest = parent.CanonicalDigest
	}
	out, err := succession.NewCriticalRole(in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func readinessFixture(t *testing.T, id string, revision uint64, parent succession.SuccessorReadinessRevision) succession.SuccessorReadinessRevision {
	t.Helper()
	in := succession.SuccessorReadinessRevision{
		SuccessorID: id, Revision: revision, Readiness: succession.ReadinessReadyNow, VacancyRisk: succession.VacancyRiskMedium,
		AssessedBy: "assessor-1", AssessmentSource: "assessment-1", EvidenceRefs: []string{"evidence-" + id},
		EffectiveAt: successionInstant(), KnownAt: successionInstant(),
	}
	if revision > 1 {
		in.ParentRevision = parent.Revision
		in.ParentDigest = parent.CanonicalDigest
	}
	out, err := succession.NewSuccessorReadinessRevision(in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func slateFixture(t *testing.T, role succession.CriticalRole, candidate succession.SuccessorReadinessRevision) succession.SuccessionSlate {
	t.Helper()
	out, err := succession.NewSuccessionSlate(succession.SuccessionSlate{
		SlateID: "slate-1", Revision: 1, CriticalRoleID: role.RoleID, CriticalRoleRevision: role.Revision, CriticalRoleDigest: role.CanonicalDigest,
		PositionRef: role.PositionRef, JobRevisionRef: role.JobRevisionRef, NominatorID: "manager-1", NominatorRole: "ROLE_MANAGER",
		NominatorAuthorizationRef: "authorization-1", NominatorAuthorized: true, Visibility: succession.DisclosureScoped,
		DeclaredScopes: []string{"talent.read"}, Candidates: []succession.SuccessorReadinessRevision{candidate}, EffectiveAt: successionInstant(), KnownAt: successionInstant(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func saveInitial(t *testing.T, store *successionstore.Store, tenant uuid.UUID) (succession.CriticalRole, succession.SuccessorReadinessRevision, succession.SuccessionSlate) {
	t.Helper()
	role := roleFixture(t, "role-1", 1, succession.CriticalRole{})
	readiness := readinessFixture(t, "worker-1", 1, succession.SuccessorReadinessRevision{})
	slate := slateFixture(t, role, readiness)
	ctx := context.Background()
	if err := store.SaveCriticalRole(ctx, tenant, role); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveReadiness(ctx, tenant, readiness); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSlate(ctx, tenant, slate); err != nil {
		t.Fatal(err)
	}
	return role, readiness, slate
}

func TestTodo_PERSIST_SUCCESSION_001(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "succession-primary")
	store := successionstore.New(appConn(t, db))
	role, readiness, slate := saveInitial(t, store, tenant)

	gotRole, err := store.LoadCriticalRole(context.Background(), tenant, role.RoleID, role.Revision)
	if err != nil || gotRole.CanonicalDigest != role.CanonicalDigest {
		t.Fatalf("LoadCriticalRole = %+v, %v", gotRole, err)
	}
	gotReadiness, err := store.LoadReadiness(context.Background(), tenant, readiness.SuccessorID, readiness.Revision)
	if err != nil || gotReadiness.CanonicalDigest != readiness.CanonicalDigest {
		t.Fatalf("LoadReadiness = %+v, %v", gotReadiness, err)
	}
	gotSlate, err := store.LoadSlate(context.Background(), tenant, slate.SlateID, slate.Revision)
	if err != nil || gotSlate.CanonicalDigest != slate.CanonicalDigest || len(gotSlate.Candidates) != 1 {
		t.Fatalf("LoadSlate = %+v, %v", gotSlate, err)
	}
	for _, table := range []string{"succession_critical_role", "succession_readiness_revision", "succession_slate"} {
		var count int
		if err := db.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", tenant).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s count=%d, want 1", table, count)
		}
	}
}

func TestTodo_PERSIST_SUCCESSION_001_Fault(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "succession-fault")
	store := successionstore.New(appConn(t, db))
	role, _, _ := saveInitial(t, store, tenant)

	duplicateErr := store.SaveCriticalRole(context.Background(), tenant, role)
	var duplicate *succession.StoreError
	if !errors.As(duplicateErr, &duplicate) || duplicate.Code != succession.StoreDuplicateCode {
		t.Fatalf("duplicate SaveCriticalRole=%v, want typed %s", duplicateErr, succession.StoreDuplicateCode)
	}
	stale := roleFixture(t, role.RoleID, 2, role)
	stale.ParentDigest = "sha256:" + "0" + stale.CanonicalDigest[len("sha256:")+1:]
	stale.CanonicalDigest = ""
	stale, err := succession.NewCriticalRole(stale)
	if err != nil {
		t.Fatal(err)
	}
	staleErr := store.SaveCriticalRole(context.Background(), tenant, stale)
	var cas *succession.StoreError
	if !errors.As(staleErr, &cas) || cas.Code != succession.StoreStaleCASCode {
		t.Fatalf("stale SaveCriticalRole=%v, want typed %s", staleErr, succession.StoreStaleCASCode)
	}
}

func TestTodo_PERSIST_SUCCESSION_001_Integration(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "succession-integration")
	store := successionstore.New(appConn(t, db))
	role, readiness, slate := saveInitial(t, store, tenant)
	role2 := roleFixture(t, role.RoleID, 2, role)
	if err := store.SaveCriticalRole(context.Background(), tenant, role2); err != nil {
		t.Fatal(err)
	}
	readiness2 := readinessFixture(t, readiness.SuccessorID, 2, readiness)
	if err := store.SaveReadiness(context.Background(), tenant, readiness2); err != nil {
		t.Fatal(err)
	}
	slate2, err := slate.Revise([]succession.SuccessorReadinessRevision{readiness2}, "manager-2", "authorization-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSlate(context.Background(), tenant, slate2); err != nil {
		t.Fatal(err)
	}
	current, err := store.CurrentSlate(context.Background(), tenant, slate.SlateID)
	if err != nil || current.Revision != 2 || current.ParentDigest != slate.CanonicalDigest {
		t.Fatalf("CurrentSlate = %+v, %v", current, err)
	}
}

func TestTodo_PERSIST_SUCCESSION_001_Security(t *testing.T) {
	db := newDB(t)
	alpha, beta := insertTenant(t, db, "succession-alpha"), insertTenant(t, db, "succession-beta")
	store := successionstore.New(appConn(t, db))
	saveInitial(t, store, alpha)
	saveInitial(t, store, beta)
	conn := appConn(t, db)
	for _, table := range []string{"succession_critical_role", "succession_readiness_revision", "succession_slate"} {
		var count int
		err := tenantTxErr(conn, alpha, func(tx dbport.Tx) error {
			return tx.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", beta).Scan(&count)
		})
		if err != nil {
			t.Fatalf("cross-tenant query %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("tenant alpha saw %d beta rows in %s", count, table)
		}
	}
}

func TestTodo_PERSIST_SUCCESSION_001_Recovery(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "succession-recovery")
	role, _, _ := saveInitial(t, successionstore.New(appConn(t, db)), tenant)
	got, err := successionstore.New(appConn(t, db)).LoadCriticalRole(context.Background(), tenant, role.RoleID, role.Revision)
	if err != nil || got.CanonicalDigest != role.CanonicalDigest {
		t.Fatalf("LoadCriticalRole after fresh connection = %+v, %v", got, err)
	}
}

func TestTodo_PERSIST_SUCCESSION_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "succession-mutation")
	role, _, _ := saveInitial(t, successionstore.New(appConn(t, db)), tenant)
	if err := db.ExecErr(`UPDATE succession_critical_role SET owner_ref='changed' WHERE tenant_id=$1 AND role_id=$2 AND revision=$3`, tenant, role.RoleID, role.Revision); err == nil {
		t.Fatal("UPDATE of immutable critical role succeeded")
	}
	if err := db.ExecErr(`DELETE FROM succession_slate WHERE tenant_id=$1`, tenant); err == nil {
		t.Fatal("DELETE of immutable slate succeeded")
	}
}
