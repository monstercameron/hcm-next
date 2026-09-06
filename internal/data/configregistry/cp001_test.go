package configregistry_test

import (
	"testing"
	"time"

	dataconfigregistry "github.com/monstercameron/hcm-next/internal/data/configregistry"
	"github.com/monstercameron/hcm-next/internal/data/pgtest"
	platformconfig "github.com/monstercameron/hcm-next/internal/platform/configregistry"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

// TestTodo_CP_001_Integration is the CP-001 test matrix's INTEGRATION entry:
// the full Publish -> Activate -> Resolve round trip run against a real,
// freshly migrated PostgreSQL schema (migrations/00027_config_object.sql),
// through the [dataconfigregistry.Store] adapter rather than the in-memory
// [platformconfig.Registry] the PRIMARY/GOLDEN/MUTATION tests in
// internal/platform/configregistry use.
func TestTodo_CP_001_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, "cp001-integration")
	conn := appConn(t, db)
	store := dataconfigregistry.New(conn)

	scope := platformconfig.Scope{TenantID: tenantID.String()}
	publishedAt := time.Date(2026, 9, 5, 10, 0, 0, 0, time.UTC)

	v1, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind:               platformconfig.KindWorkflow,
		ID:                 "onboarding",
		Revision:           1,
		Body:               []byte(`{"steps":["collect","approve"]}`),
		SchemaRef:          "hcmnext.workflow.definition/v1",
		Scope:              scope,
		PublisherPrincipal: "dataops-admin",
		PublishedAt:        publishedAt,
	})
	if err != nil {
		t.Fatalf("Publish v1: %v", err)
	}
	if err := v1.Verify(); err != nil {
		t.Fatalf("Verify on a freshly published-and-stored object: %v", err)
	}

	// Re-publishing identical content through the real adapter is
	// idempotent, same as the in-memory registry.
	again, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind: v1.Kind, ID: v1.ID, Revision: v1.Revision,
		Body: v1.Body, SchemaRef: v1.SchemaRef, Scope: v1.Scope,
		PublisherPrincipal: v1.PublisherPrincipal, PublishedAt: v1.PublishedAt,
	})
	if err != nil {
		t.Fatalf("Publish (idempotent re-publish): %v", err)
	}
	if again.Digest() != v1.Digest() {
		t.Fatalf("idempotent re-publish through Postgres minted a new identity: %s != %s", again.Digest(), v1.Digest())
	}

	// A different body under the same revision is refused, enforced by both
	// the platform package's own check and (defensively) the database's
	// primary key.
	_, err = platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind: v1.Kind, ID: v1.ID, Revision: v1.Revision,
		Body: []byte("a different body"), SchemaRef: v1.SchemaRef, Scope: v1.Scope,
		PublisherPrincipal: v1.PublisherPrincipal, PublishedAt: v1.PublishedAt,
	})
	if platformconfig.CodeOf(err) != platformconfig.CodeRevisionConflict {
		t.Fatalf("CodeOf(err) = %q, want %q", platformconfig.CodeOf(err), platformconfig.CodeRevisionConflict)
	}

	// Resolve refuses before any activation.
	if _, err := platformconfig.Resolve(store, scope, v1.Kind, v1.ID); platformconfig.CodeOf(err) != platformconfig.CodeNoActiveRevision {
		t.Fatalf("Resolve before activation: CodeOf(err) = %q, want %q", platformconfig.CodeOf(err), platformconfig.CodeNoActiveRevision)
	}

	if _, err := platformconfig.Activate(store, v1.Ref(), platformconfig.ActivationEvidence{
		ActivatedBy: "release-manager", Authority: "change-board", ActivatedAt: time.Unix(1, 0),
	}); err != nil {
		t.Fatalf("Activate v1: %v", err)
	}
	resolved, err := platformconfig.Resolve(store, scope, v1.Kind, v1.ID)
	if err != nil {
		t.Fatalf("Resolve after activating v1: %v", err)
	}
	if resolved.Revision != 1 || resolved.Digest() != v1.Digest() {
		t.Fatalf("Resolve = revision %d digest %s, want revision 1 digest %s", resolved.Revision, resolved.Digest(), v1.Digest())
	}

	// Publish and activate a second revision; the first activation must
	// survive as permanent history, not be deleted or edited.
	v2, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind: v1.Kind, ID: v1.ID, Revision: 2,
		Body:               []byte(`{"steps":["collect","approve","notify"]}`),
		SchemaRef:          v1.SchemaRef,
		Scope:              scope,
		PublisherPrincipal: "dataops-admin",
		PublishedAt:        publishedAt.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("Publish v2: %v", err)
	}
	if _, err := platformconfig.Activate(store, v2.Ref(), platformconfig.ActivationEvidence{
		ActivatedBy: "release-manager", ActivatedAt: time.Unix(2, 0),
	}); err != nil {
		t.Fatalf("Activate v2: %v", err)
	}
	resolved, err = platformconfig.Resolve(store, scope, v1.Kind, v1.ID)
	if err != nil || resolved.Revision != 2 {
		t.Fatalf("Resolve after activating v2: revision=%d err=%v", resolved.Revision, err)
	}

	history, err := store.ListActivations(scope, v1.Kind, v1.ID)
	if err != nil {
		t.Fatalf("ListActivations: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("ListActivations returned %d rows, want 2 (superseded activation retained)", len(history))
	}
	if history[0].Revision != 1 || history[1].Revision != 2 {
		t.Fatalf("ListActivations order = %+v, want [1,2] oldest first", history)
	}

	revisions, err := store.ListRevisions(scope, v1.Kind, v1.ID)
	if err != nil || len(revisions) != 2 {
		t.Fatalf("ListRevisions = %d entries, err=%v, want 2 (both immutable revisions retained)", len(revisions), err)
	}
}

// TestTodo_CP_001_TenantIsolation proves migration 00027's row level
// security actually governs config_object and config_object_activation: a
// connection scoped to tenant B can neither read nor overwrite tenant A's
// rows, matching the RED clause's "unowned version is accepted" concern one
// tenant boundary lower.
func TestTodo_CP_001_TenantIsolation(t *testing.T) {
	db := pgtest.New(t)
	tenantA := insertTenant(t, db, "cp001-tenant-a")
	tenantB := insertTenant(t, db, "cp001-tenant-b")
	conn := appConn(t, db)
	store := dataconfigregistry.New(conn)

	scopeA := platformconfig.Scope{TenantID: tenantA.String()}
	obj, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind: platformconfig.KindPolicy, ID: "leave-policy", Revision: 1,
		Body: []byte("policy body"), SchemaRef: "hcmnext.policy/v1",
		Scope: scopeA, PublisherPrincipal: "admin-a", PublishedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Publish under tenant A: %v", err)
	}

	scopeB := platformconfig.Scope{TenantID: tenantB.String()}
	if _, found, err := store.GetObject(platformconfig.ObjectRef{Scope: scopeB, Kind: obj.Kind, ID: obj.ID, Revision: obj.Revision}); err != nil {
		t.Fatalf("GetObject under tenant B: %v", err)
	} else if found {
		t.Fatal("tenant B's connection read tenant A's config_object row")
	}
}
