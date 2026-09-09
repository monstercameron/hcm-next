package assetstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/assetstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/asset"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func newDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	if _, err := db.Provider(t).UpTo(context.Background(), 71); err != nil {
		t.Fatalf("apply migrations through asset schema: %v", err)
	}
	return db
}

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func revision(t *testing.T, stream string, number uint64) values.RevisionToken {
	t.Helper()
	token, err := values.NewSequenceRevision(stream, number)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func assetReference(tenant values.TenantId) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: "asset", Id: uuid.NewString()}
}

func workerReference(tenant values.TenantId) values.EntityRef {
	return values.EntityRef{Tenant: tenant, Kind: "worker", Id: uuid.NewString()}
}

func receipt(tenant values.TenantId, verified bool) asset.Receipt {
	return asset.Receipt{
		ID:       values.EntityRef{Tenant: tenant, Kind: "receipt", Id: uuid.NewString()},
		Issuer:   values.EntityRef{Tenant: tenant, Kind: "system", Id: uuid.NewString()},
		IssuedAt: time.Unix(10, 0).UTC(), Verified: verified, Evidence: "receipt-evidence",
	}
}

func fixture(t *testing.T, tenant values.TenantId, number uint64) (asset.InventoryRevision, asset.CustodyRevision) {
	t.Helper()
	assetRef := assetReference(tenant)
	inventory := asset.InventoryRevision{
		InventoryID:    assetRef,
		Owner:          values.EntityRef{Tenant: tenant, Kind: "organization", Id: uuid.NewString()},
		Classification: "LAPTOP", SerialNumber: "SN-" + assetRef.Id[:8],
		Revision: revision(t, "asset.inventory", number), EffectiveAt: time.Unix(100, 0).UTC(), Status: asset.Available,
	}
	custody := asset.CustodyRevision{
		Asset: assetRef, Worker: workerReference(tenant), Location: "HQ", Condition: "GOOD",
		AssigneeReceipt: receipt(tenant, true), IssuerReceipt: receipt(tenant, true),
		Revision: revision(t, "asset.custody", number), EffectiveAt: time.Unix(100+int64(number), 0).UTC(), Status: asset.Assigned,
	}
	return inventory, custody
}

func TestTodo_PERSIST_ASSET_001(t *testing.T) {
	db := newDB(t)
	insertTenant(t, db, "tenant-primary")
	tenant := values.TenantId("tenant-primary")
	inventory, custody := fixture(t, tenant, 1)
	store := assetstore.New(db.Conn)
	if err := store.RegisterInventory(inventory); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendCustody(inventory.InventoryID, values.UnspecifiedRevision(), custody); err != nil {
		t.Fatal(err)
	}
	var inventoryCount, custodyCount int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM asset_inventory`).Scan(&inventoryCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM asset_custody_event`).Scan(&custodyCount); err != nil {
		t.Fatal(err)
	}
	if inventoryCount != 1 || custodyCount != 1 {
		t.Fatalf("stored rows = %d/%d, want 1/1", inventoryCount, custodyCount)
	}
}

func TestTodo_PERSIST_ASSET_001_Fault(t *testing.T) {
	db := newDB(t)
	insertTenant(t, db, "tenant-fault")
	tenant := values.TenantId("tenant-fault")
	inventory, custody := fixture(t, tenant, 1)
	store := assetstore.New(db.Conn)
	if err := store.RegisterInventory(inventory); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterInventory(inventory); assetstore.CodeOf(err) != assetstore.CodeDuplicate || !errors.Is(err, assetstore.ErrDuplicate) {
		t.Fatalf("duplicate inventory = %v, code=%q", err, assetstore.CodeOf(err))
	}
	if err := store.AppendCustody(inventory.InventoryID, values.UnspecifiedRevision(), custody); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendCustody(inventory.InventoryID, custody.Revision, custody); assetstore.CodeOf(err) != assetstore.CodeDuplicate {
		t.Fatalf("duplicate custody = %v, code=%q", err, assetstore.CodeOf(err))
	}
	stale := custody
	stale.Revision = revision(t, "asset.custody", 2)
	if err := store.AppendCustody(inventory.InventoryID, revision(t, "asset.custody", 0), stale); assetstore.CodeOf(err) != assetstore.CodeStaleCAS {
		t.Fatalf("stale custody = %v, code=%q", err, assetstore.CodeOf(err))
	}
}

func TestTodo_PERSIST_ASSET_001_Integration(t *testing.T) {
	db := newDB(t)
	insertTenant(t, db, "tenant-integration")
	tenant := values.TenantId("tenant-integration")
	inventory, assigned := fixture(t, tenant, 1)
	store := assetstore.New(db.Conn)
	if err := store.RegisterInventory(inventory); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendCustody(inventory.InventoryID, values.UnspecifiedRevision(), assigned); err != nil {
		t.Fatal(err)
	}
	pending := assigned
	pending.Status = asset.ReturnPending
	pending.Worker = assigned.Worker
	pending.AssigneeReceipt = asset.Receipt{}
	pending.IssuerReceipt = asset.Receipt{}
	pending.Revision = revision(t, "asset.custody", 2)
	pending.EffectiveAt = time.Unix(102, 0).UTC()
	if err := store.AppendCustody(inventory.InventoryID, assigned.Revision, pending); err != nil {
		t.Fatal(err)
	}
	returned := pending
	returned.Status = asset.Returned
	returned.Worker = values.EntityRef{}
	returned.AssigneeReceipt = receipt(tenant, true)
	returned.IssuerReceipt = receipt(tenant, true)
	returned.Revision = revision(t, "asset.custody", 3)
	returned.EffectiveAt = time.Unix(103, 0).UTC()
	if err := store.AppendCustody(inventory.InventoryID, pending.Revision, returned); err != nil {
		t.Fatal(err)
	}
	current, found, err := store.CurrentCustody(inventory.InventoryID)
	if err != nil || !found || current.Status != asset.Returned {
		t.Fatalf("current = %+v, found=%v, err=%v", current, found, err)
	}
	history, err := store.HistoryCustody(inventory.InventoryID)
	if err != nil || len(history) != 3 {
		t.Fatalf("history = %d, err=%v", len(history), err)
	}
}

func asAppRole(t *testing.T, db *pgtest.DB, tenant uuid.UUID) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	ctx := context.Background()
	if _, err := conn.Exec(ctx, "SET ROLE hcmnext_app"); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `SELECT set_config('app.tenant_id',$1,false)`, tenant.String()); err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestTodo_PERSIST_ASSET_001_Security(t *testing.T) {
	db := newDB(t)
	tenantA := insertTenant(t, db, "tenant-security-a")
	tenantB := insertTenant(t, db, "tenant-security-b")
	store := assetstore.New(db.Conn)
	a, ac := fixture(t, values.TenantId("tenant-security-a"), 1)
	b, bc := fixture(t, values.TenantId("tenant-security-b"), 1)
	if err := store.RegisterInventory(a); err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterInventory(b); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendCustody(a.InventoryID, values.UnspecifiedRevision(), ac); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendCustody(b.InventoryID, values.UnspecifiedRevision(), bc); err != nil {
		t.Fatal(err)
	}
	conn := asAppRole(t, db, tenantA)
	var inventoryCount, custodyCount int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM asset_inventory`).Scan(&inventoryCount); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM asset_custody_event`).Scan(&custodyCount); err != nil {
		t.Fatal(err)
	}
	if inventoryCount != 1 || custodyCount != 1 {
		t.Fatalf("tenant A sees rows %d/%d, want 1/1", inventoryCount, custodyCount)
	}
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM asset_inventory WHERE owner_ref=$1`, b.Owner.Id).Scan(&inventoryCount); err != nil {
		t.Fatal(err)
	}
	if inventoryCount != 0 {
		t.Fatal("tenant A read tenant B inventory")
	}
	_ = tenantB
}

func TestTodo_PERSIST_ASSET_001_Recovery(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "tenant-recovery")
	tenant := values.TenantId(tenantID.String())
	inventory, custody := fixture(t, tenant, 1)
	if err := assetstore.New(db.Conn).RegisterInventory(inventory); err != nil {
		t.Fatal(err)
	}
	if err := assetstore.New(db.Conn).AppendCustody(inventory.InventoryID, values.UnspecifiedRevision(), custody); err != nil {
		t.Fatal(err)
	}
	fresh := db.NewConn(t)
	current, found, err := assetstore.New(fresh).CurrentCustody(inventory.InventoryID)
	if err != nil || !found || current.Revision.String() != custody.Revision.String() {
		t.Fatalf("fresh current = %+v, found=%v, err=%v", current, found, err)
	}
	history, err := assetstore.New(fresh).HistoryCustody(inventory.InventoryID)
	if err != nil || len(history) != 1 {
		t.Fatalf("fresh history = %d, err=%v", len(history), err)
	}
}

func TestTodo_PERSIST_ASSET_001_Mutation(t *testing.T) {
	db := newDB(t)
	tenantID := insertTenant(t, db, "tenant-mutation")
	tenant := values.TenantId("tenant-mutation")
	inventory, custody := fixture(t, tenant, 1)
	store := assetstore.New(db.Conn)
	if err := store.RegisterInventory(inventory); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendCustody(inventory.InventoryID, values.UnspecifiedRevision(), custody); err != nil {
		t.Fatal(err)
	}
	var inventoryRow, custodyRow uuid.UUID
	if err := db.QueryRow(context.Background(), `SELECT row_id FROM asset_inventory WHERE tenant_id=$1`, tenantID).Scan(&inventoryRow); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), `SELECT row_id FROM asset_custody_event WHERE tenant_id=$1`, tenantID).Scan(&custodyRow); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE asset_inventory SET status='RETIRED' WHERE row_id=$1`, inventoryRow); err == nil {
		t.Fatal("inventory revision update was accepted")
	}
	if err := db.ExecErr(`DELETE FROM asset_inventory WHERE row_id=$1`, inventoryRow); err == nil {
		t.Fatal("inventory revision delete was accepted")
	}
	if err := db.ExecErr(`UPDATE asset_custody_event SET status='LOST' WHERE row_id=$1`, custodyRow); err == nil {
		t.Fatal("custody event update was accepted")
	}
	if err := db.ExecErr(`DELETE FROM asset_custody_event WHERE row_id=$1`, custodyRow); err == nil {
		t.Fatal("custody event delete was accepted")
	}
}
