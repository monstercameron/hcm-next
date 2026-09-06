package accessstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/data/accessstore"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	"github.com/monstercameron/hcm-next/internal/data/pgxadapter"
	"github.com/monstercameron/hcm-next/internal/domains/access"
	"github.com/monstercameron/hcm-next/internal/domains/evidence"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func instant(s string) time.Time {
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return at.UTC()
}

func fixture(t *testing.T, tenant values.TenantId, suffix string) (access.WorkforceIdentity, access.AccountLink, access.EntitlementDefinition) {
	t.Helper()
	from := values.NewInstant(instant("2026-01-01T00:00:00Z"))
	to := values.NewInstant(instant("2027-01-01T00:00:00Z"))
	effective, err := values.NewInstantInterval(from, to)
	if err != nil {
		t.Fatal(err)
	}
	known, err := values.NewKnownAt(values.NewInstant(instant("2026-01-02T00:00:00Z")))
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(instant("2026-01-02T00:00:01Z")))
	if err != nil {
		t.Fatal(err)
	}
	prov := evidence.Provenance{Source: "access-test", EvidenceRef: "evidence-" + suffix, RecordedAt: recorded}
	revision, err := values.NewSequenceRevision("access."+suffix, 1)
	if err != nil {
		t.Fatal(err)
	}
	worker := uuid.New()
	identity := access.WorkforceIdentity{ID: "identity-" + suffix, Tenant: tenant, Subject: "subject-" + suffix, System: "github", WorkerRef: values.EntityRef{Tenant: tenant, Kind: "worker", Id: worker.String()}, Revision: revision, Authority: access.AuthorityNative, Effective: effective, KnownAt: known, Provenance: prov, Lifecycle: access.LifecycleActive}
	account := access.AccountLink{ID: "link-" + suffix, Tenant: tenant, Subject: "subject-" + suffix, System: "github", Source: "directory", WorkforceIdentityID: identity.ID, Application: "github", AccountID: "account-" + suffix, Revision: revision, Authority: access.AuthorityNative, Effective: effective, KnownAt: known, Provenance: prov, Lifecycle: access.LifecycleActive}
	entitlement := access.EntitlementDefinition{ID: "entitlement-" + suffix, Tenant: tenant, Subject: "entitlement-subject-" + suffix, System: "github", Application: "github", Code: "repo-read-" + suffix, Version: "v1", RiskClass: access.RiskLow, Owner: "security", Revision: revision, Authority: access.AuthorityNative, Effective: effective, KnownAt: known, Provenance: prov, Lifecycle: access.LifecycleActive}
	return identity, account, entitlement
}

func TestTodo_PERSIST_ACCESS_001(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "tenant-primary")
	tenant := values.TenantId("tenant-primary")
	identity, account, entitlement := fixture(t, tenant, "primary")
	store := accessstore.New(db.Conn)
	ctx := context.Background()
	if err := store.Add(ctx, identity); err != nil {
		t.Fatalf("add identity: %v", err)
	}
	if err := store.Add(ctx, account); err != nil {
		t.Fatalf("add account: %v", err)
	}
	if err := store.Add(ctx, entitlement); err != nil {
		t.Fatalf("add entitlement: %v", err)
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM workforce_identity WHERE tenant_id=$1`, tenantID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("workforce_identity count = %d, want 1", count)
	}
}

func TestTodo_PERSIST_ACCESS_001_Fault(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "tenant-fault")
	tenant := values.TenantId("tenant-fault")
	identity, _, _ := fixture(t, tenant, "fault")
	store := accessstore.New(db.Conn)
	ctx := context.Background()
	if err := store.Add(ctx, identity); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, identity); !errors.Is(err, accessstore.ErrDuplicate) {
		t.Fatalf("duplicate = %v, want ErrDuplicate", err)
	}
	conn := db.NewConn(t)
	if _, err := conn.Exec(ctx, `INSERT INTO workforce_identity (row_id,tenant_id,identity_id,subject,system,worker_ref,revision,authority_class,effective_from,known_at,lifecycle,digest) VALUES ($1,$2,'identity-fault', 'x','github',$3,0,'NATIVE',now(),now(),'ACTIVE',repeat('0',64))`, uuid.New(), tenantID, uuid.New()); err == nil {
		t.Fatal("cas_version zero was accepted")
	}
}

func TestTodo_PERSIST_ACCESS_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "tenant-integration")
	tenant := values.TenantId("tenant-integration")
	identity, account, entitlement := fixture(t, tenant, "integration")
	store := accessstore.New(db.Conn)
	ctx := context.Background()
	for _, record := range []access.Record{identity, account, entitlement} {
		if err := store.Add(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := store.Snapshot(ctx, tenant)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(graph.Identities) != 1 || len(graph.Accounts) != 1 || len(graph.Entitlements) != 1 {
		t.Fatalf("snapshot sizes = %d/%d/%d", len(graph.Identities), len(graph.Accounts), len(graph.Entitlements))
	}
	if graph.Tenant != tenant || graph.Identities[0].Tenant != tenant {
		t.Fatalf("snapshot tenant = %q/%q", graph.Tenant, graph.Identities[0].Tenant)
	}
	_ = tenantID
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

func TestTodo_PERSIST_ACCESS_001_Security(t *testing.T) {
	db := pgtest.New(t)
	tenantA := insertTenant(t, db, "tenant-security-a")
	tenantB := insertTenant(t, db, "tenant-security-b")
	a, _, _ := fixture(t, values.TenantId("tenant-security-a"), "security-a")
	b, _, _ := fixture(t, values.TenantId("tenant-security-b"), "security-b")
	store := accessstore.New(db.Conn)
	ctx := context.Background()
	if err := store.Add(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, b); err != nil {
		t.Fatal(err)
	}
	conn := asAppRole(t, db, tenantA)
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM workforce_identity`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tenant A sees %d identity rows, want 1", count)
	}
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM workforce_identity WHERE worker_ref=$1`, b.WorkerRef.Id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("tenant A saw tenant B identity")
	}
	_ = tenantB
}

func TestTodo_PERSIST_ACCESS_001_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "tenant-recovery")
	tenant := values.TenantId(tenantID.String())
	identity, _, _ := fixture(t, tenant, "recovery")
	store := accessstore.New(db.Conn)
	ctx := context.Background()
	if err := store.Add(ctx, identity); err != nil {
		t.Fatal(err)
	}
	fresh := db.NewConn(t)
	recovered, err := accessstore.New(fresh).Snapshot(ctx, tenant)
	if err != nil {
		t.Fatalf("fresh snapshot: %v", err)
	}
	if len(recovered.Identities) != 1 || recovered.Identities[0].ID != identity.ID {
		t.Fatalf("recovered identities = %+v", recovered.Identities)
	}
}

func TestTodo_PERSIST_ACCESS_001_Mutation(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "tenant-mutation")
	tenant := values.TenantId("tenant-mutation")
	identity, _, _ := fixture(t, tenant, "mutation")
	if err := accessstore.New(db.Conn).Add(context.Background(), identity); err != nil {
		t.Fatal(err)
	}
	var rowID uuid.UUID
	if err := db.QueryRow(context.Background(), `SELECT row_id FROM workforce_identity WHERE tenant_id=$1`, tenantID).Scan(&rowID); err != nil {
		t.Fatal(err)
	}
	if err := db.ExecErr(`UPDATE workforce_identity SET subject='forged' WHERE row_id=$1`, rowID); err == nil {
		t.Fatal("revision update was accepted")
	}
	if err := db.ExecErr(`DELETE FROM workforce_identity WHERE row_id=$1`, rowID); err == nil {
		t.Fatal("revision delete was accepted")
	}
}

func TestAccessStore_ObserveAndLoadAliases(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "tenant-observe")
	tenant := values.TenantId("tenant-observe")
	identity, account, _ := fixture(t, tenant, "observe")
	store := accessstore.New(db.Conn)
	ctx := context.Background()
	if err := store.Add(ctx, identity); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(ctx, account); err != nil {
		t.Fatal(err)
	}
	from := values.NewInstant(instant("2026-02-01T00:00:00Z"))
	known, err := values.NewKnownAt(values.NewInstant(instant("2026-02-02T00:00:00Z")))
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := values.NewRecordedAt(values.NewInstant(instant("2026-02-02T00:00:01Z")))
	if err != nil {
		t.Fatal(err)
	}
	effective, err := values.NewOpenInstantInterval(from)
	if err != nil {
		t.Fatal(err)
	}
	observation := access.ExternalAccessObservation{ID: identity.ID, Tenant: tenant, Subject: identity.Subject, System: identity.System, Application: account.Application, AccountID: account.AccountID, ProviderVersion: "provider-v1", ObservedState: "GRANTED", Revision: mustRevision(t, "observe", 1), Authority: access.AuthorityExternalObservation, Effective: effective, KnownAt: known, Provenance: evidence.Provenance{Source: "provider", EvidenceRef: "observation-1", RecordedAt: recorded}, Lifecycle: access.LifecycleActive}
	if err := store.Observe(ctx, observation); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM external_access_observation WHERE tenant_id=$1`, tenantID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("observation count = %d, want 1", count)
	}
	loaded, err := store.Load(ctx, tenant)
	if err != nil || loaded.Tenant != tenant || len(loaded.Identities) != 1 {
		t.Fatalf("loaded graph = %+v, err=%v", loaded, err)
	}
}

func mustRevision(t *testing.T, key string, sequence uint64) values.RevisionToken {
	t.Helper()
	revision, err := values.NewSequenceRevision(key, sequence)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}
