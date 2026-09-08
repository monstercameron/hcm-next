package privacymeta_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/hcm-next/internal/data/dbport"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/privacymeta"
)

func certificationFixture(t *testing.T, db *pgtest.DB, key string, inventoryAt, verifiedAt time.Time, scope string) (uuid.UUID, privacymeta.DataCopyInventory) {
	t.Helper()
	tenant := uuid.NewSHA1(uuid.NameSpaceOID, []byte(key))
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenant, key, "tenant "+key)
	inventory := privacymeta.DataCopyInventory{TenantID: tenant, InventoryID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(key+":inventory")), CanonicalAssetKey: "asset:test", AsOf: inventoryAt, SourceWatermark: "ledger:42", ExpectedSources: []byte(`["cache-catalog"]`), SourceWatermarks: []byte(`{"cache-catalog":"42"}`), Completeness: "COMPLETE", ContentDigest: digestOf("discovery-manifest"), CreatedAt: inventoryAt}
	if key == "records-copy-missing-source" {
		inventory.ExpectedSources = []byte(`["backup-catalog","cache-catalog"]`)
		inventory.SourceWatermarks = []byte(`{"backup-catalog":"9","cache-catalog":"42"}`)
	}
	copyRow := privacymeta.DataCopy{TenantID: tenant, CopyID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(key+":copy")), InventoryID: inventory.InventoryID, CanonicalAssetKey: inventory.CanonicalAssetKey, CopyType: "CACHE", StoreRef: "cache:1", DiscoverySource: "cache-catalog", SubjectRef: "worker:1", DataCategory: "HR", ProcessorRef: "processor:cache", Region: "us-east", FieldScope: []byte(scope), EncryptionKeyRef: "key:tenant", RetentionScheduleKey: "retention:test", HoldState: "NONE", DeletionCapability: "DELETE", RestorePolicy: "REAPPLY_TOMBSTONES", LastVerifiedAt: &verifiedAt, CreatedAt: inventoryAt}
	conn := appConn(t, db)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := privacymeta.InsertDataCopyInventory(context.Background(), tx, inventory); err != nil {
			return err
		}
		return privacymeta.InsertDataCopy(context.Background(), tx, copyRow)
	})
	return tenant, inventory
}

func certifyFixture(t *testing.T, key string) privacymeta.CopyInventoryCertificate {
	t.Helper()
	db := pgtest.New(t)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	tenant, inventory := certificationFixture(t, db, key, now.Add(-time.Hour), now.Add(-30*time.Minute), `{"subject_ref":"worker:1","category":"HR","restore_policy":"REAPPLY_TOMBSTONES"}`)
	var certificate privacymeta.CopyInventoryCertificate
	conn := appConn(t, db)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		certificate, err = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		return err
	})
	return certificate
}

func TestTodo_RECORDS_COPY_001(t *testing.T) {
	certificate := certifyFixture(t, "records-copy-primary")
	if certificate.Digest == "" || certificate.CopyCount != 1 {
		t.Fatalf("certificate=%+v", certificate)
	}
}

func TestTodo_RECORDS_COPY_001_Golden(t *testing.T) {
	certificate := certifyFixture(t, "records-copy-golden")
	const want = "sha256:b6a8cb57dcdd931fced9b1d8a38bd13b5b9dce96e64e0bd54b1e670b6284586a"
	if certificate.Digest != want {
		t.Fatalf("certificate digest changed: got %q", certificate.Digest)
	}
}

func TestTodo_RECORDS_COPY_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	tenant, inventory := certificationFixture(t, db, "records-copy-integration", now.Add(-time.Hour), now.Add(-30*time.Minute), `{"subject_ref":"worker:1","category":"HR","restore_policy":"REAPPLY_TOMBSTONES"}`)
	conn := appConn(t, db)
	var first, second privacymeta.CopyInventoryCertificate
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		first, err = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(context.Background(), `UPDATE data_copy SET hold_state='HELD' WHERE tenant_id=$1 AND inventory_id=$2`, tenant, inventory.InventoryID); err != nil {
			return err
		}
		second, err = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		return err
	})
	if first.Digest == second.Digest {
		t.Fatal("certificate did not bind persisted hold metadata")
	}
	var canonicalFirst, canonicalSecond privacymeta.CopyInventoryCertificate
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		canonicalFirst, err = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(context.Background(), `UPDATE data_copy SET field_scope='{"restore_policy":"REAPPLY_TOMBSTONES", "category":"HR", "subject_ref":"worker:1"}'::jsonb WHERE tenant_id=$1 AND inventory_id=$2`, tenant, inventory.InventoryID); err != nil {
			return err
		}
		canonicalSecond, err = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		return err
	})
	if canonicalFirst.Digest != canonicalSecond.Digest {
		t.Fatal("certificate digest changed for semantically identical field-scope JSON")
	}
}

func TestTodo_RECORDS_COPY_001_Security(t *testing.T) {
	db := pgtest.New(t)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	tenant, inventory := certificationFixture(t, db, "records-copy-security", now.Add(-time.Hour), now.Add(-30*time.Minute), `{"subject_ref":"worker:1","category":"HR","restore_policy":"REAPPLY_TOMBSTONES"}`)
	foreign := insertTenant(t, db, "records-copy-foreign")
	conn := appConn(t, db)
	var gotErr error
	inTenantTx(t, conn, foreign, func(tx dbport.Tx) error {
		_, gotErr = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		return nil
	})
	if !errors.Is(gotErr, privacymeta.ErrCertificationBlocked) {
		t.Fatalf("cross-tenant certification err=%v", gotErr)
	}
	emptyTenant := insertTenant(t, db, "records-copy-empty")
	empty := privacymeta.DataCopyInventory{TenantID: emptyTenant, InventoryID: uuid.New(), CanonicalAssetKey: "asset:empty", AsOf: now.Add(-time.Minute), SourceWatermark: "caller:complete", ExpectedSources: []byte(`["catalog"]`), SourceWatermarks: []byte(`{"catalog":"1"}`), Completeness: "COMPLETE", ContentDigest: digestOf("empty"), CreatedAt: now.Add(-time.Minute)}
	emptyConn := appConn(t, db)
	inTenantTx(t, emptyConn, emptyTenant, func(tx dbport.Tx) error {
		if err := privacymeta.InsertDataCopyInventory(context.Background(), tx, empty); err != nil {
			return err
		}
		_, gotErr = privacymeta.CertifyCopyInventory(context.Background(), tx, emptyTenant, empty.InventoryID, now, 2*time.Hour)
		return nil
	})
	if !errors.Is(gotErr, privacymeta.ErrCertificationBlocked) {
		t.Fatalf("caller COMPLETE without discovered copies err=%v", gotErr)
	}
	tenant, inventory = certificationFixture(t, db, "records-copy-missing-source", now.Add(-time.Hour), now.Add(-30*time.Minute), `{"fields":["name"]}`)
	conn = appConn(t, db)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, gotErr = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		return nil
	})
	if !errors.Is(gotErr, privacymeta.ErrCertificationBlocked) {
		t.Fatalf("COMPLETE inventory missing an expected source certified: %v", gotErr)
	}
}

func TestTodo_RECORDS_COPY_001_Recovery(t *testing.T) {
	db := pgtest.New(t)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	tenant, inventory := certificationFixture(t, db, "records-copy-recovery", now.Add(-3*time.Hour), now.Add(-30*time.Minute), `{"subject_ref":"worker:1","category":"HR","restore_policy":"REAPPLY_TOMBSTONES"}`)
	conn := appConn(t, db)
	var gotErr error
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, gotErr = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		return nil
	})
	if !errors.Is(gotErr, privacymeta.ErrStaleCopy) {
		t.Fatalf("stale inventory watermark err=%v", gotErr)
	}
	tenant, inventory = certificationFixture(t, db, "records-copy-no-restore", now.Add(-time.Hour), now.Add(-30*time.Minute), `{"subject_ref":"worker:1","category":"HR"}`)
	if err := db.ExecErr(`UPDATE data_copy SET restore_policy='' WHERE tenant_id=$1 AND inventory_id=$2`, tenant, inventory.InventoryID); err != nil {
		t.Fatalf("arrange legacy incomplete metadata: %v", err)
	}
	conn = appConn(t, db)
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, gotErr = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		return nil
	})
	if !errors.Is(gotErr, privacymeta.ErrCertificationBlocked) {
		t.Fatalf("missing restore policy certified: %v", gotErr)
	}
}

func TestTodo_RECORDS_COPY_001_Mutation(t *testing.T) {
	db := pgtest.New(t)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	tenant, inventory := certificationFixture(t, db, "records-copy-digest-binding", now.Add(-time.Hour), now.Add(-30*time.Minute), `{"fields":["name"]}`)
	conn := appConn(t, db)
	var baseline, differentTime, differentWindow, largeIntegerA, largeIntegerB privacymeta.CopyInventoryCertificate
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		var err error
		baseline, err = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		if err != nil {
			return err
		}
		differentTime, err = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now.Add(time.Minute), 2*time.Hour)
		if err != nil {
			return err
		}
		differentWindow, err = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 90*time.Minute)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(context.Background(), `UPDATE data_copy SET field_scope='{"subject":{"id":9007199254740992},"fields":["name"]}'::jsonb WHERE tenant_id=$1 AND inventory_id=$2`, tenant, inventory.InventoryID); err != nil {
			return err
		}
		largeIntegerA, err = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(context.Background(), `UPDATE data_copy SET field_scope='{"fields":["name"],"subject":{"id":9007199254740993}}'::jsonb WHERE tenant_id=$1 AND inventory_id=$2`, tenant, inventory.InventoryID); err != nil {
			return err
		}
		largeIntegerB, err = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventory.InventoryID, now, 2*time.Hour)
		return err
	})
	if baseline.Digest == differentTime.Digest || baseline.Digest == differentWindow.Digest {
		t.Fatal("certificate digest did not bind issued_at and maxAge")
	}
	if largeIntegerA.Digest == largeIntegerB.Digest {
		t.Fatal("certificate digest collapsed distinct large integer field scopes")
	}
}

func TestTodo_RECORDS_COPY_001_Upgrade(t *testing.T) {
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 273); err != nil {
		t.Fatalf("apply predecessor migrations: %v", err)
	}
	tenant := uuid.New()
	inventoryID, copyID := uuid.New(), uuid.New()
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from) VALUES ($1,'records-copy-upgrade','cell-local','upgrade tenant','ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenant)
	db.Exec(t, `INSERT INTO data_copy_inventory (tenant_id, inventory_id, canonical_asset_key, as_of, source_watermark, completeness, unknown_count, content_digest, created_at) VALUES ($1,$2,'asset:legacy',$3,'legacy:7','COMPLETE',0,$4,$3)`, tenant, inventoryID, now.Add(-time.Hour), digestOf("legacy-inventory"))
	db.Exec(t, `INSERT INTO data_copy (tenant_id, copy_id, inventory_id, canonical_asset_key, copy_type, store_ref, processor_ref, region, field_scope, encryption_key_ref, retention_schedule_key, hold_state, deletion_capability, last_verified_at, created_at) VALUES ($1,$2,$3,'asset:legacy','CACHE','cache:legacy','processor:legacy','us-east','{"fields":["name"]}','key:legacy','retention:legacy','NONE','DELETE',$4,$4)`, tenant, copyID, inventoryID, now.Add(-30*time.Minute))
	if _, err := db.Provider(t).UpTo(context.Background(), 274); err != nil {
		t.Fatalf("migration 00274 rejected populated predecessor schema: %v", err)
	}
	var expected, watermarks []byte
	var discoverySource, subject, category, restore string
	if err := db.QueryRow(context.Background(), `SELECT i.expected_sources, i.source_watermarks, c.discovery_source, c.subject_ref, c.data_category, c.restore_policy FROM data_copy_inventory i JOIN data_copy c ON c.tenant_id=i.tenant_id AND c.inventory_id=i.inventory_id WHERE i.tenant_id=$1 AND i.inventory_id=$2`, tenant, inventoryID).Scan(&expected, &watermarks, &discoverySource, &subject, &category, &restore); err != nil {
		t.Fatalf("read upgraded legacy row: %v", err)
	}
	if string(expected) != "[]" || string(watermarks) != "{}" || discoverySource != "" || subject != "" || category != "" || restore != "" {
		t.Fatalf("migration fabricated discovery evidence: expected=%s watermarks=%s source=%q subject=%q category=%q restore=%q", expected, watermarks, discoverySource, subject, category, restore)
	}
	conn := appConn(t, db)
	var gotErr error
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		_, gotErr = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, inventoryID, now, 2*time.Hour)
		return nil
	})
	if !errors.Is(gotErr, privacymeta.ErrCertificationBlocked) {
		t.Fatalf("legacy unknown discovery evidence certified: %v", gotErr)
	}
	verifiedAt := now.Add(-time.Minute)
	complete := privacymeta.DataCopyInventory{TenantID: tenant, InventoryID: uuid.New(), CanonicalAssetKey: "asset:post-upgrade", AsOf: now.Add(-time.Minute), SourceWatermark: "catalog:2", ExpectedSources: []byte(`["catalog"]`), SourceWatermarks: []byte(`{"catalog":"2"}`), Completeness: "COMPLETE", ContentDigest: digestOf("post-upgrade-inventory"), CreatedAt: now}
	completeCopy := privacymeta.DataCopy{TenantID: tenant, CopyID: uuid.New(), InventoryID: complete.InventoryID, CanonicalAssetKey: complete.CanonicalAssetKey, CopyType: "CACHE", StoreRef: "cache:post-upgrade", DiscoverySource: "catalog", SubjectRef: "worker:1", DataCategory: "HR", ProcessorRef: "processor:cache", Region: "us-east", FieldScope: []byte(`{"fields":["name"]}`), EncryptionKeyRef: "key:tenant", RetentionScheduleKey: "retention:test", HoldState: "NONE", DeletionCapability: "DELETE", RestorePolicy: "REAPPLY_TOMBSTONES", LastVerifiedAt: &verifiedAt, CreatedAt: now}
	var certificate privacymeta.CopyInventoryCertificate
	inTenantTx(t, conn, tenant, func(tx dbport.Tx) error {
		if err := privacymeta.InsertDataCopyInventory(context.Background(), tx, complete); err != nil {
			return err
		}
		if err := privacymeta.InsertDataCopy(context.Background(), tx, completeCopy); err != nil {
			return err
		}
		var err error
		certificate, err = privacymeta.CertifyCopyInventory(context.Background(), tx, tenant, complete.InventoryID, now, 2*time.Hour)
		return err
	})
	if certificate.Digest == "" || certificate.CopyCount != 1 {
		t.Fatalf("fully evidenced post-upgrade inventory did not certify: %+v", certificate)
	}
}
