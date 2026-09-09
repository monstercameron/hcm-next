package customstore_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/customstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/custom"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	// Migration 00082 currently has an unrelated unterminated dollar-quoted
	// function. Apply the known-good substrate through 00070, then execute this
	// lane's numbered migration directly so the custom tests stay scoped to
	// migration 00086.
	if _, err := db.Provider(t).UpTo(context.Background(), 70); err != nil {
		t.Fatalf("apply migrations through 00070: %v", err)
	}
	body, err := migrations.FS.ReadFile("00086_custom.sql")
	if err != nil {
		t.Fatalf("read migration 00086: %v", err)
	}
	up, _, ok := strings.Cut(string(body), "-- +goose Down")
	if !ok {
		t.Fatal("migration 00086 has no down marker")
	}
	if _, err := db.SQL.ExecContext(context.Background(), up); err != nil {
		t.Fatalf("apply migration 00086: %v", err)
	}
	return db
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

func objectDefinition(version uint64) custom.CustomObjectDefinition {
	return custom.CustomObjectDefinition{
		Kind: "Asset", Namespace: "inventory", Version: version,
		Fields: map[string]custom.FieldDefinition{
			"serial": {
				Type: "string",
				Classification: custom.FieldClassification{
					AuthZDomain: "worker.core", Classification: "INTERNAL",
					ResidencyRef: "NO_CONSTRAINT", RetentionClass: "PERMANENT",
				},
			},
		},
	}
}

func relationshipDefinition(version uint64) custom.CustomRelationshipDefinition {
	return custom.CustomRelationshipDefinition{
		Name: "AssetOwner", Namespace: "inventory", Version: version,
		SourceKind: "Asset", TargetKind: "Worker",
		Cardinality: custom.CardinalityManyToMany, EffectiveDateRule: "INSTANT_INTERVAL",
	}
}

func recordDefinitionVersion(t *testing.T, version uint64) custom.CustomRecordRevision {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	end := values.NewInstant(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	effective, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(start)
	if err != nil {
		t.Fatal(err)
	}
	return custom.CustomRecordRevision{
		ObjectID: "asset-1", ObjectKind: "Asset", Namespace: "inventory",
		DefinitionVersion: version, Effective: effective, Known: known,
		FieldValues: map[string]custom.TypedValue{
			"serial": {FieldName: "serial", Type: "string", Value: "SN-1"},
		},
	}
}

func saveInitial(t *testing.T, store *customstore.Store, tenant uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := store.SaveObjectDefinition(ctx, tenant.String(), objectDefinition(1), 0); err != nil {
		t.Fatalf("SaveObjectDefinition: %v", err)
	}
	if err := store.SaveRelationshipDefinition(ctx, tenant.String(), relationshipDefinition(1), 0); err != nil {
		t.Fatalf("SaveRelationshipDefinition: %v", err)
	}
	if err := store.SaveRecordRevision(ctx, tenant.String(), recordDefinitionVersion(t, 1)); err != nil {
		t.Fatalf("SaveRecordRevision: %v", err)
	}
}

func TestTodo_PERSIST_CUSTOM_001(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "custom-primary")
	store := customstore.New(appConn(t, db))
	saveInitial(t, store, tenant)

	definition, err := store.LoadObjectDefinition(context.Background(), tenant.String(), "Asset", "inventory", 1)
	if err != nil {
		t.Fatalf("LoadObjectDefinition: %v", err)
	}
	if definition.Fields["serial"].Classification.AuthZDomain != "worker.core" {
		t.Fatalf("definition lost field policy: %#v", definition)
	}
	records, err := store.ListRecordRevisions(context.Background(), tenant.String(), "asset-1")
	if err != nil {
		t.Fatalf("ListRecordRevisions: %v", err)
	}
	if len(records) != 1 || records[0].FieldValues["serial"].Value != "SN-1" {
		t.Fatalf("records=%#v, want one durable record", records)
	}
	for _, table := range []string{"custom_object_definition", "custom_relationship_definition", "custom_record_revision"} {
		var count int
		if err := db.QueryRow(context.Background(), "SELECT count(*) FROM "+table+" WHERE tenant_id=$1", tenant).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("%s count=%d, want 1", table, count)
		}
	}
}

func TestTodo_PERSIST_CUSTOM_001_Fault(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "custom-fault")
	store := customstore.New(appConn(t, db))
	if err := store.SaveObjectDefinition(context.Background(), tenant.String(), objectDefinition(1), 0); err != nil {
		t.Fatal(err)
	}
	duplicateErr := store.SaveObjectDefinition(context.Background(), tenant.String(), objectDefinition(1), 0)
	var duplicate *custom.StoreError
	if !errors.As(duplicateErr, &duplicate) || duplicate.Code != custom.StoreDuplicateCode {
		t.Fatalf("duplicate definition=%v, want typed %s", duplicateErr, custom.StoreDuplicateCode)
	}
	staleErr := store.SaveObjectDefinition(context.Background(), tenant.String(), objectDefinition(3), 1)
	var stale *custom.StoreError
	if !errors.As(staleErr, &stale) || stale.Code != custom.StoreStaleCASCode {
		t.Fatalf("stale definition=%v, want typed %s", staleErr, custom.StoreStaleCASCode)
	}
	missing := recordDefinitionVersion(t, 9)
	missingErr := store.SaveRecordRevision(context.Background(), tenant.String(), missing)
	var reference *custom.StoreError
	if !errors.As(missingErr, &reference) || reference.Code != custom.StoreReferenceCode {
		t.Fatalf("missing definition=%v, want typed %s", missingErr, custom.StoreReferenceCode)
	}
}

func TestTodo_PERSIST_CUSTOM_001_Integration(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "custom-integration")
	store := customstore.New(appConn(t, db))
	if err := store.SaveObjectDefinition(context.Background(), tenant.String(), objectDefinition(1), 0); err != nil {
		t.Fatal(err)
	}
	next := objectDefinition(2)
	next.Fields["serial"] = objectDefinition(1).Fields["serial"]
	if err := store.SaveObjectDefinition(context.Background(), tenant.String(), next, 1); err != nil {
		t.Fatalf("save successor: %v", err)
	}
	record := recordDefinitionVersion(t, 2)
	record.ObjectID = "asset-2"
	if err := store.SaveRecordRevision(context.Background(), tenant.String(), record); err != nil {
		t.Fatalf("save successor record: %v", err)
	}
	got, err := store.ListRecordRevisions(context.Background(), tenant.String(), "asset-2")
	if err != nil || len(got) != 1 || got[0].DefinitionVersion != 2 {
		t.Fatalf("successor records=%#v err=%v, want definition version 2", got, err)
	}
}

func TestTodo_PERSIST_CUSTOM_001_Security(t *testing.T) {
	db := newDB(t)
	alpha, beta := insertTenant(t, db, "custom-alpha"), insertTenant(t, db, "custom-beta")
	store := customstore.New(appConn(t, db))
	if err := store.SaveObjectDefinition(context.Background(), alpha.String(), objectDefinition(1), 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveObjectDefinition(context.Background(), beta.String(), objectDefinition(1), 0); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRecordRevision(context.Background(), alpha.String(), recordDefinitionVersion(t, 1)); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRecordRevision(context.Background(), beta.String(), recordDefinitionVersion(t, 1)); err != nil {
		t.Fatal(err)
	}
	conn := appConn(t, db)
	tx, err := conn.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if err := tenancy.WithTenant(context.Background(), tx, alpha); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM custom_record_revision WHERE tenant_id=$1`, beta).Scan(&count); err != nil {
		t.Fatalf("cross-tenant query: %v", err)
	}
	if count != 0 {
		t.Fatalf("alpha read %d beta records under RLS", count)
	}
}

func TestTodo_PERSIST_CUSTOM_001_Recovery(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "custom-recovery")
	first := customstore.New(appConn(t, db))
	saveInitial(t, first, tenant)
	fresh := customstore.New(appConn(t, db))
	definition, err := fresh.LoadObjectDefinition(context.Background(), tenant.String(), "Asset", "inventory", 1)
	if err != nil {
		t.Fatalf("fresh LoadObjectDefinition: %v", err)
	}
	if definition.Digest() == "" {
		t.Fatal("fresh connection returned an invalid definition")
	}
	records, err := fresh.ListRecordRevisions(context.Background(), tenant.String(), "asset-1")
	if err != nil || len(records) != 1 {
		t.Fatalf("fresh records=%#v err=%v, want one row", records, err)
	}
}

func TestTodo_PERSIST_CUSTOM_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenant := insertTenant(t, db, "custom-mutation")
	store := customstore.New(appConn(t, db))
	saveInitial(t, store, tenant)
	conn := appConn(t, db)
	for _, table := range []string{"custom_object_definition", "custom_relationship_definition", "custom_record_revision"} {
		tx, err := conn.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatal(err)
		}
		_, updateErr := tx.Exec(context.Background(), "UPDATE "+table+" SET tenant_id=$1 WHERE tenant_id=$1", tenant)
		if updateErr == nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("UPDATE %s succeeded on immutable rows", table)
		}
		_ = tx.Rollback(context.Background())

		tx, err = conn.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if err := tenancy.WithTenant(context.Background(), tx, tenant); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatal(err)
		}
		_, deleteErr := tx.Exec(context.Background(), "DELETE FROM "+table+" WHERE tenant_id=$1", tenant)
		if deleteErr == nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("DELETE %s succeeded on immutable rows", table)
		}
		_ = tx.Rollback(context.Background())
	}
}
