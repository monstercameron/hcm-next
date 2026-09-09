package configregistry_test

import (
	"testing"
	"time"

	dataconfigregistry "github.com/monstercameron/human-capital-management-suite/internal/data/configregistry"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
)

func TestStorePutObjectAndGetObjectRoundTrip(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "object-roundtrip")
	store := dataconfigregistry.New(appConn(t, db))

	scope := platformconfig.Scope{TenantID: tenantID.String()}
	obj, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind: platformconfig.KindSchema, ID: "person", Revision: 1,
		Body: []byte(`{"fields":["legal_name"]}`), SchemaRef: "hcmnext.schema.meta/v1",
		Scope: scope, PublisherPrincipal: "admin", PublishedAt: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	got, found, err := store.GetObject(obj.Ref())
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if !found {
		t.Fatal("GetObject: not found immediately after Publish")
	}
	if got.Digest() != obj.Digest() {
		t.Fatalf("GetObject returned a different identity: %s != %s", got.Digest(), obj.Digest())
	}
	if string(got.Body) != `{"fields":["legal_name"]}` {
		t.Fatalf("GetObject body = %q", got.Body)
	}
	if err := got.Verify(); err != nil {
		t.Fatalf("Verify on a round-tripped object: %v", err)
	}
}

func TestStoreGetObjectUnknownKeyIsNotFoundNotError(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "object-unknown")
	store := dataconfigregistry.New(appConn(t, db))

	_, found, err := store.GetObject(platformconfig.ObjectRef{
		Scope: platformconfig.Scope{TenantID: tenantID.String()}, Kind: platformconfig.KindSchema, ID: "never-published", Revision: 1,
	})
	if err != nil {
		t.Fatalf("GetObject: %v", err)
	}
	if found {
		t.Fatal("GetObject reported found for a key never published")
	}
}

func TestStoreListRevisionsOldestFirst(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "object-list-revisions")
	store := dataconfigregistry.New(appConn(t, db))
	scope := platformconfig.Scope{TenantID: tenantID.String()}

	for rev := uint32(1); rev <= 3; rev++ {
		if _, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
			Kind: platformconfig.KindRule, ID: "eligibility", Revision: rev,
			Body: []byte{byte('a' + rev)}, SchemaRef: "hcmnext.rule/v1",
			Scope: scope, PublisherPrincipal: "admin", PublishedAt: time.Now(),
		}); err != nil {
			t.Fatalf("Publish revision %d: %v", rev, err)
		}
	}

	revisions, err := store.ListRevisions(scope, platformconfig.KindRule, "eligibility")
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revisions) != 3 {
		t.Fatalf("ListRevisions returned %d entries, want 3", len(revisions))
	}
	for i, rev := range revisions {
		if rev.Revision != uint32(i+1) {
			t.Fatalf("ListRevisions[%d].Revision = %d, want %d", i, rev.Revision, i+1)
		}
	}
}

func TestStoreListRevisionsUnknownGroupIsEmpty(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "object-list-empty")
	store := dataconfigregistry.New(appConn(t, db))

	revisions, err := store.ListRevisions(platformconfig.Scope{TenantID: tenantID.String()}, platformconfig.KindRule, "never-published")
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if len(revisions) != 0 {
		t.Fatalf("ListRevisions = %v, want empty", revisions)
	}
}
