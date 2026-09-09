package privacymeta_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/privacymeta"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, "tenant "+key)
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

func inTenantTx(t *testing.T, conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) {
	t.Helper()
	if err := inTenantTxErr(conn, tenant, fn); err != nil {
		t.Fatalf("tenant transaction: %v", err)
	}
}

func inTenantTxErr(conn *pgxadapter.Conn, tenant uuid.UUID, fn func(tx dbport.Tx) error) error {
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

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func newPurpose(tenant uuid.UUID) privacymeta.ProcessingPurposeDeclaration {
	return privacymeta.ProcessingPurposeDeclaration{
		TenantID:             tenant,
		DeclarationID:        uuid.New(),
		PurposeKey:           "purpose:" + uuid.NewString(),
		DeclarationVersion:   1,
		LawfulBasis:          "CONTRACT",
		DataCategories:       json.RawMessage(`{"identity":true}`),
		AllowedOperations:    json.RawMessage(`{"read":true}`),
		Recipients:           json.RawMessage(`{"processor:adp":true}`),
		RetentionScheduleKey: "schedule:hr-100",
		ContentDigest:        digestOf("purpose-" + uuid.NewString()),
		EffectiveFrom:        fixedInstant,
		Status:               "ACTIVE",
		CreatedAt:            fixedInstant,
	}
}

func newInventory(tenant uuid.UUID, completeness string, unknowns int32) privacymeta.DataCopyInventory {
	return privacymeta.DataCopyInventory{
		TenantID:          tenant,
		InventoryID:       uuid.New(),
		CanonicalAssetKey: "asset:" + uuid.NewString(),
		AsOf:              fixedInstant,
		SourceWatermark:   "wm-1",
		ExpectedSources:   json.RawMessage(`["catalog"]`),
		SourceWatermarks:  json.RawMessage(`{"catalog":"wm-1"}`),
		Completeness:      completeness,
		UnknownCount:      unknowns,
		ContentDigest:     digestOf("inventory-" + uuid.NewString()),
		CreatedAt:         fixedInstant,
	}
}

func newCopy(tenant, inventoryID uuid.UUID, assetKey, kind, store string) privacymeta.DataCopy {
	c := privacymeta.DataCopy{
		TenantID:             tenant,
		CopyID:               uuid.New(),
		InventoryID:          inventoryID,
		CanonicalAssetKey:    assetKey,
		CopyType:             kind,
		StoreRef:             store,
		DiscoverySource:      "catalog",
		SubjectRef:           "worker:1",
		DataCategory:         "HR",
		ProcessorRef:         "processor:internal",
		Region:               "us-east",
		FieldScope:           json.RawMessage(`{"fields":["name"]}`),
		EncryptionKeyRef:     "kms://key/1",
		RetentionScheduleKey: "schedule:hr-100",
		HoldState:            "NONE",
		DeletionCapability:   "DELETE",
		RestorePolicy:        "REAPPLY_TOMBSTONES",
		CreatedAt:            fixedInstant,
	}
	if kind == "PROVIDER" {
		c.ProcessorRef = "processor:adp"
	}
	return c
}

// TestTodo_DB_015_Privacy is DB-015's privacy-side test. The matrix names
// live in internal/data/recordsmeta (the lane's primary package for DB-015);
// this suite proves the same clauses for the three privacy tables migration
// 00032 creates.
func TestTodo_DB_015_Privacy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenant := insertTenant(t, db, "db015-privacy")
	other := insertTenant(t, db, "db015-privacy-other")

	t.Run("the privacy table set is exactly what 00032 declares", func(t *testing.T) {
		want := []string{"data_copy", "data_copy_inventory", "processing_purpose_declaration"}
		if !slices.Equal(privacymeta.PrivacyTables, want) {
			t.Fatalf("PrivacyTables=%v, want %v", privacymeta.PrivacyTables, want)
		}
		for _, table := range want {
			var found string
			if err := db.Conn.QueryRow(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, table).Scan(&found); err != nil {
				t.Errorf("table %s missing from the live schema: %v", table, err)
			}
		}
	})

	t.Run("a census with unknowns is never COMPLETE", func(t *testing.T) {
		lying := newInventory(tenant, "COMPLETE", 3)
		if err := lying.Validate(); !errors.Is(err, privacymeta.ErrIncompleteCensus) {
			t.Fatalf("a COMPLETE census with 3 unknowns validated as %v", err)
		}
		if err := db.ExecErr(`INSERT INTO data_copy_inventory (tenant_id, inventory_id, canonical_asset_key, as_of, source_watermark, completeness, unknown_count, content_digest, created_at) VALUES ($1,$2,'asset:x',now(),'wm','COMPLETE',3,$3,now())`,
			tenant, uuid.New(), digestOf("x")); err == nil {
			t.Fatal("the schema accepted a COMPLETE census with 3 unknowns")
		}
		honest := newInventory(tenant, "PARTIAL", 3)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return privacymeta.InsertDataCopyInventory(ctx, tx, honest)
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := privacymeta.LoadDataCopyInventory(ctx, tx, tenant, honest.InventoryID)
			if err != nil {
				return err
			}
			if loaded.Completeness != "PARTIAL" || loaded.UnknownCount != 3 || loaded.SourceWatermark == "" {
				return fmt.Errorf("census %+v lost its completeness, unknowns or watermark", loaded)
			}
			return nil
		})
	})

	t.Run("every copy carries its scope, retention and deletion capability", func(t *testing.T) {
		inventory := newInventory(tenant, "COMPLETE", 0)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return privacymeta.InsertDataCopyInventory(ctx, tx, inventory)
		})
		for i, kind := range privacymeta.CopyTypes {
			c := newCopy(tenant, inventory.InventoryID, inventory.CanonicalAssetKey, kind, fmt.Sprintf("store:%d", i))
			inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
				return privacymeta.InsertDataCopy(ctx, tx, c)
			})
		}
		kinds, err := func() ([]string, error) {
			var out []string
			e := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
				var err error
				out, err = privacymeta.ListCopyTypes(ctx, tx, tenant, inventory.InventoryID)
				return err
			})
			return out, e
		}()
		if err != nil {
			t.Fatalf("list copy types: %v", err)
		}
		want := slices.Clone(privacymeta.CopyTypes)
		slices.Sort(want)
		if !slices.Equal(kinds, want) {
			t.Fatalf("inventory holds %v, want %v", kinds, want)
		}

		noRetention := newCopy(tenant, inventory.InventoryID, inventory.CanonicalAssetKey, "CACHE", "store:no-retention")
		noRetention.RetentionScheduleKey = ""
		if err := noRetention.Validate(); !errors.Is(err, privacymeta.ErrMissingScope) {
			t.Errorf("a copy with no retention schedule validated as %v", err)
		}
	})

	t.Run("a processor copy always names its processor", func(t *testing.T) {
		inventory := newInventory(tenant, "COMPLETE", 0)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return privacymeta.InsertDataCopyInventory(ctx, tx, inventory)
		})
		anonymous := newCopy(tenant, inventory.InventoryID, inventory.CanonicalAssetKey, "PROVIDER", "store:mystery")
		anonymous.ProcessorRef = ""
		if err := anonymous.Validate(); !errors.Is(err, privacymeta.ErrMissingScope) {
			t.Fatalf("a PROVIDER copy with no processor validated as %v", err)
		}
		if err := db.ExecErr(`INSERT INTO data_copy (tenant_id, copy_id, inventory_id, canonical_asset_key, copy_type, store_ref, region, retention_schedule_key, deletion_capability, created_at) VALUES ($1,$2,$3,'asset:x','PROVIDER','store:mystery','us-east','schedule:x','DELETE',now())`,
			tenant, uuid.New(), inventory.InventoryID); err == nil {
			t.Fatal("the schema accepted a PROVIDER copy with no processor")
		}
	})

	t.Run("one store holds one copy of a kind per census", func(t *testing.T) {
		inventory := newInventory(tenant, "COMPLETE", 0)
		first := newCopy(tenant, inventory.InventoryID, inventory.CanonicalAssetKey, "SEARCH", "store:search")
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			if err := privacymeta.InsertDataCopyInventory(ctx, tx, inventory); err != nil {
				return err
			}
			return privacymeta.InsertDataCopy(ctx, tx, first)
		})
		duplicate := newCopy(tenant, inventory.InventoryID, inventory.CanonicalAssetKey, "SEARCH", "store:search")
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return privacymeta.InsertDataCopy(ctx, tx, duplicate)
		}); err == nil {
			t.Fatal("the same store was counted twice in one census")
		}
	})

	t.Run("a purpose declaration never loses its version or lawful basis", func(t *testing.T) {
		p := newPurpose(tenant)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return privacymeta.InsertProcessingPurposeDeclaration(ctx, tx, p)
		})
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			loaded, err := privacymeta.LoadProcessingPurposeDeclaration(ctx, tx, tenant, p.DeclarationID)
			if err != nil {
				return err
			}
			if loaded.DeclarationVersion != 1 || loaded.LawfulBasis != "CONTRACT" || loaded.RetentionScheduleKey == "" {
				return fmt.Errorf("declaration %+v lost its version, basis or retention schedule", loaded)
			}
			return nil
		})
		unversioned := newPurpose(tenant)
		unversioned.DeclarationVersion = 0
		if err := unversioned.Validate(); !errors.Is(err, privacymeta.ErrMissingVersion) {
			t.Errorf("an unversioned declaration validated as %v", err)
		}
		invalidBasis := newPurpose(tenant)
		invalidBasis.LawfulBasis = "BECAUSE_WE_WANT_TO"
		if err := invalidBasis.Validate(); !errors.Is(err, privacymeta.ErrInvalidEnum) {
			t.Errorf("an invented lawful basis validated as %v", err)
		}
		duplicate := newPurpose(tenant)
		duplicate.PurposeKey = p.PurposeKey
		duplicate.DeclarationVersion = p.DeclarationVersion
		if err := inTenantTxErr(conn, tenant, func(tx dbport.Tx) error {
			return privacymeta.InsertProcessingPurposeDeclaration(ctx, tx, duplicate)
		}); err == nil {
			t.Fatal("two declarations claimed the same purpose version")
		}
	})

	t.Run("a census is append-only", func(t *testing.T) {
		inventory := newInventory(tenant, "PARTIAL", 1)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return privacymeta.InsertDataCopyInventory(ctx, tx, inventory)
		})
		if err := db.ExecErr(`UPDATE data_copy_inventory SET completeness='COMPLETE', unknown_count=0 WHERE tenant_id=$1 AND inventory_id=$2`, tenant, inventory.InventoryID); err == nil {
			t.Fatal("a census was rewritten into a complete one")
		}
		if err := db.ExecErr(`DELETE FROM data_copy_inventory WHERE tenant_id=$1 AND inventory_id=$2`, tenant, inventory.InventoryID); err == nil {
			t.Fatal("a census was deleted")
		}
	})

	t.Run("one tenant cannot reach another tenant's copies", func(t *testing.T) {
		inventory := newInventory(tenant, "COMPLETE", 0)
		inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
			return privacymeta.InsertDataCopyInventory(ctx, tx, inventory)
		})
		if err := inTenantTxErr(conn, other, func(tx dbport.Tx) error {
			_, err := privacymeta.LoadDataCopyInventory(ctx, tx, tenant, inventory.InventoryID)
			return err
		}); !errors.Is(err, dbport.ErrNoRows) {
			t.Fatalf("another tenant read this tenant's census: %v", err)
		}
		crossTenant := newCopy(other, inventory.InventoryID, inventory.CanonicalAssetKey, "CACHE", "store:x")
		if err := inTenantTxErr(conn, other, func(tx dbport.Tx) error {
			return privacymeta.InsertDataCopy(ctx, tx, crossTenant)
		}); err == nil {
			t.Fatal("another tenant added a copy to this tenant's census")
		}
	})
}
