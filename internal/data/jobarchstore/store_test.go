package jobarchstore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/jobarchstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/jobarch"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 48); err != nil {
		t.Fatalf("apply migrations through 00048: %v", err)
	}
	return db
}

func digestOf(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
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

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(dbport.Tx) error) error {
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

func save(t *testing.T, store *jobarchstore.Store, tenant uuid.UUID, architecture jobarch.ArchitectureRevision, expected string) {
	t.Helper()
	if err := store.Save(context.Background(), tenant.String(), architecture, expected); err != nil {
		t.Fatalf("Save %s/%s: %v", architecture.ID, architecture.Revision, err)
	}
}

func architectureFixture(t *testing.T, id string) jobarch.ArchitectureRevision {
	t.Helper()
	at := func(day int) time.Time { return time.Date(2026, time.January, day, 0, 0, 0, 0, time.UTC) }
	a, err := jobarch.NewArchitectureRevision(jobarch.ArchitectureRevision{
		ID: id, Revision: "r1",
		Families: []jobarch.JobFamilyRevision{{ID: "family-1", FamilyID: "family-1", Revision: "f1", Code: "ENG", Name: "Engineering", Lifecycle: jobarch.LifecyclePublished, EffectiveFrom: at(1), KnownFrom: at(1), Lineage: jobarch.RevisionLineage{RootID: "family-1"}}},
		Levels:   []jobarch.JobLevelRevision{{ID: "level-1", LevelID: "level-1", Revision: "l1", FamilyID: "family-1", Code: "IC1", Title: "Individual Contributor 1", Rank: 1, Lifecycle: jobarch.LifecyclePublished, EffectiveFrom: at(1), KnownFrom: at(1), Lineage: jobarch.RevisionLineage{RootID: "level-1"}}},
		Grades:   []jobarch.JobGradeRevision{{ID: "grade-1", GradeID: "grade-1", Revision: "g1", LevelID: "level-1", Code: "G1", Name: "Grade 1", Lifecycle: jobarch.LifecyclePublished, EffectiveFrom: at(1), KnownFrom: at(1), Lineage: jobarch.RevisionLineage{RootID: "grade-1"}}},
		Profiles: []jobarch.JobProfileRevision{{ID: "profile-1", ProfileID: "profile-1", Revision: "p1", FamilyID: "family-1", LevelID: "level-1", GradeID: "grade-1", JobCode: "ENG", Title: "Engineer", Description: "sensitive responsibilities", Lifecycle: jobarch.LifecyclePublished, EffectiveFrom: at(1), KnownFrom: at(1), Lineage: jobarch.RevisionLineage{RootID: "profile-1"}}},
	})
	if err != nil {
		t.Fatalf("architecture fixture: %v", err)
	}
	return a
}

func successor(t *testing.T, architecture jobarch.ArchitectureRevision) jobarch.ArchitectureRevision {
	t.Helper()
	next := architecture
	next.Revision = "r2"
	next.SupersedesRevision = architecture.Revision
	next, err := jobarch.NewArchitectureRevision(next)
	if err != nil {
		t.Fatalf("successor: %v", err)
	}
	return next
}

func TestTodo_PERSIST_JOBARCH_001(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "jobarch-primary")
	conn := appConn(t, db)
	store := jobarchstore.New(conn)
	architecture := architectureFixture(t, "architecture-primary")
	save(t, store, tenant, architecture, "")

	got, err := store.Load(context.Background(), tenant.String(), architecture.ID, architecture.Revision)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.CanonicalDigest != architecture.CanonicalDigest || got.Profiles[0].Description != architecture.Profiles[0].Description {
		t.Fatalf("Load lost the persisted revision: got=%+v want=%+v", got, architecture)
	}
	for _, table := range []string{"job_family_revision", "job_level_revision", "job_grade_revision", "job_profile_revision", "job_architecture_revision"} {
		var count int
		if err := db.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", tenant).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s count=%d, want 1", table, count)
		}
	}
}

func TestTodo_PERSIST_JOBARCH_001_Fault(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "jobarch-fault")
	store := jobarchstore.New(appConn(t, db))
	architecture := architectureFixture(t, "architecture-fault")
	save(t, store, tenant, architecture, "")

	duplicateErr := store.Save(context.Background(), tenant.String(), architecture, architecture.Revision)
	var duplicate *jobarch.StoreError
	if !errors.As(duplicateErr, &duplicate) || duplicate.Code != jobarch.StoreDuplicateCode {
		t.Fatalf("duplicate Save=%v, want typed %s", duplicateErr, jobarch.StoreDuplicateCode)
	}
	next := successor(t, architecture)
	staleErr := store.Save(context.Background(), tenant.String(), next, "stale-r0")
	var stale *jobarch.StoreError
	if !errors.As(staleErr, &stale) || stale.Code != jobarch.StoreStaleCASCode {
		t.Fatalf("stale Save=%v, want typed %s", staleErr, jobarch.StoreStaleCASCode)
	}
}

func TestTodo_PERSIST_JOBARCH_001_Integration(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "jobarch-integration")
	store := jobarchstore.New(appConn(t, db))
	first := architectureFixture(t, "architecture-integration")
	second := successor(t, first)
	save(t, store, tenant, first, "")
	save(t, store, tenant, second, first.Revision)

	current, err := store.Current(context.Background(), tenant.String(), first.ID)
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current.Revision != second.Revision {
		t.Fatalf("Current revision=%s, want %s", current.Revision, second.Revision)
	}
	history, err := store.List(context.Background(), tenant.String(), first.ID)
	if err != nil || len(history) != 2 || history[0].Revision != "r1" || history[1].Revision != "r2" {
		t.Fatalf("List=%v err=%v, want r1 then r2", history, err)
	}
}

func TestTodo_PERSIST_JOBARCH_001_Security(t *testing.T) {
	db := newDB(t)
	alpha, beta := insertTenant(t, db, "jobarch-alpha"), insertTenant(t, db, "jobarch-beta")
	store := jobarchstore.New(appConn(t, db))
	save(t, store, alpha, architectureFixture(t, "architecture-alpha"), "")
	save(t, store, beta, architectureFixture(t, "architecture-beta"), "")

	conn := appConn(t, db)
	var count int
	err := inTenantTxErr(conn, alpha, func(tx dbport.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM job_architecture_revision WHERE tenant_id=$1`, beta).Scan(&count)
	})
	if err != nil {
		t.Fatalf("cross-tenant query: %v", err)
	}
	if count != 0 {
		t.Fatalf("tenant alpha read %d beta rows under RLS", count)
	}
}

func TestTodo_PERSIST_JOBARCH_001_Recovery(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "jobarch-recovery")
	firstConn := appConn(t, db)
	firstStore := jobarchstore.New(firstConn)
	architecture := architectureFixture(t, "architecture-recovery")
	save(t, firstStore, tenant, architecture, "")

	freshStore := jobarchstore.New(appConn(t, db))
	got, err := freshStore.Load(context.Background(), tenant.String(), architecture.ID, architecture.Revision)
	if err != nil {
		t.Fatalf("Load after fresh connection: %v", err)
	}
	if got.CanonicalDigest != architecture.CanonicalDigest {
		t.Fatalf("fresh connection digest=%s, want %s", got.CanonicalDigest, architecture.CanonicalDigest)
	}
}

func TestTodo_PERSIST_JOBARCH_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "jobarch-mutation")
	store := jobarchstore.New(appConn(t, db))
	architecture := architectureFixture(t, "architecture-mutation")
	save(t, store, tenant, architecture, "")

	conn := appConn(t, db)
	for _, table := range []string{"job_family_revision", "job_level_revision", "job_grade_revision", "job_profile_revision", "job_architecture_revision"} {
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(context.Background(), "UPDATE "+table+" SET tenant_id=$1 WHERE tenant_id=$1", tenant)
			return err
		}); err == nil {
			t.Fatalf("UPDATE %s succeeded on an immutable revision", table)
		}
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			_, err := tx.Exec(context.Background(), "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
			return err
		}); err == nil {
			t.Fatalf("DELETE %s succeeded on an immutable revision", table)
		}
	}
}

func TestStoreRejectsNilTenantBeforeOpeningDatabase(t *testing.T) {
	store := jobarchstore.New(nil)
	err := store.Save(context.Background(), "", jobarch.ArchitectureRevision{}, "")
	if err == nil || !errors.Is(err, jobarch.ErrStoreInvalid) {
		t.Fatalf("nil tenant Save=%v, want ErrStoreInvalid", err)
	}
}

func TestDigestFixtureIsStable(t *testing.T) {
	if len(digestOf("jobarch")) != 64 {
		t.Fatal("fixture digest is not sha256")
	}
}
