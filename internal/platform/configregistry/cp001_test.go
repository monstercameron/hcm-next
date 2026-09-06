// cp001_test.go carries the CP-001 test matrix's PRIMARY, GOLDEN and
// MUTATION entries (planning/todos.md section 30). TestTodo_CP_001_Integration
// lives in internal/data/configregistry, exercised against the PostgreSQL
// adapter and migrations/00025_config_object.sql, since an integration test
// with no database is not one.
package configregistry_test

import (
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/platform/configregistry"
)

// TestTodo_CP_001 is the PRIMARY test the todo names. RED: "mutable
// workflow/policy/schema/rule/connector/agent/reference definition or
// unowned version is accepted." GREEN: "typed objects are content-addressed,
// ... owner/phase/scope/effective-dated ...; updates always create
// versions." This proves both halves against the public API only, using the
// in-memory [configregistry.Registry] — no database.
func TestTodo_CP_001(t *testing.T) {
	store := configregistry.NewRegistry()
	scope := configregistry.Scope{TenantID: "tenant-cp001"}

	// RED, part 1: an object with no owning publisher is rejected outright —
	// an "unowned version" cannot be accepted.
	_, err := configregistry.Publish(store, configregistry.ConfigurationObject{
		Kind:        configregistry.KindWorkflow,
		ID:          "onboarding",
		Revision:    1,
		Body:        []byte(`{"steps":["collect","approve"]}`),
		SchemaRef:   "hcmnext.workflow.definition/v1",
		Scope:       scope,
		PublishedAt: time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC),
		// PublisherPrincipal deliberately omitted.
	})
	if err == nil {
		t.Fatal("RED: publishing an unowned (no publisher principal) object was accepted")
	}
	if configregistry.CodeOf(err) != configregistry.CodeMissingPublisher {
		t.Fatalf("RED: CodeOf(err) = %q, want %q", configregistry.CodeOf(err), configregistry.CodeMissingPublisher)
	}

	// GREEN: a fully-formed object across every declared kind is
	// content-addressed, owner/scope/effective-dated, and stored immutably.
	kinds := []configregistry.Kind{
		configregistry.KindWorkflow, configregistry.KindPolicy, configregistry.KindSchema,
		configregistry.KindRule, configregistry.KindConnector, configregistry.KindAgent,
		configregistry.KindReference, configregistry.KindMapping, configregistry.KindCapability,
	}
	published := make(map[configregistry.Kind]configregistry.ConfigurationObject, len(kinds))
	for _, k := range kinds {
		obj, err := configregistry.Publish(store, configregistry.ConfigurationObject{
			Kind:               k,
			ID:                 "obj-" + string(k),
			Revision:           1,
			Body:               []byte("body for " + string(k)),
			SchemaRef:          "hcmnext.test.schema/v1",
			Scope:              scope,
			PublisherPrincipal: "dataops-admin",
			PublishedAt:        time.Date(2026, 9, 5, 9, 0, 0, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("GREEN: Publish(%s) failed: %v", k, err)
		}
		if obj.CanonicalBodyDigest == "" {
			t.Fatalf("GREEN: %s has no canonical body digest — not content-addressed", k)
		}
		if obj.Digest() == "" {
			t.Fatalf("GREEN: %s has no record digest", k)
		}
		if obj.Scope != scope {
			t.Fatalf("GREEN: %s scope = %+v, want %+v", k, obj.Scope, scope)
		}
		if obj.PublisherPrincipal != "dataops-admin" {
			t.Fatalf("GREEN: %s lost its publisher", k)
		}
		if obj.PublishedAt.IsZero() {
			t.Fatalf("GREEN: %s has no effective/published date", k)
		}
		published[k] = obj
	}

	// RED, part 2: a mutable in-place edit is not how this package models
	// change — publishing new content under the SAME revision key is
	// refused; a real update must mint a new revision.
	tampered := published[configregistry.KindWorkflow]
	_, err = configregistry.Publish(store, configregistry.ConfigurationObject{
		Kind: tampered.Kind, ID: tampered.ID, Revision: tampered.Revision,
		Body:               []byte("a mutated body for the same revision"),
		SchemaRef:          tampered.SchemaRef,
		Scope:              tampered.Scope,
		PublisherPrincipal: tampered.PublisherPrincipal,
		PublishedAt:        tampered.PublishedAt,
	})
	if configregistry.CodeOf(err) != configregistry.CodeRevisionConflict {
		t.Fatalf("RED: mutating revision %d of %s in place: CodeOf(err) = %q, want %q",
			tampered.Revision, tampered.ID, configregistry.CodeOf(err), configregistry.CodeRevisionConflict)
	}

	// GREEN: "updates always create versions" — a genuine update publishes
	// revision 2, and both revisions remain retrievable.
	updated, err := configregistry.Publish(store, configregistry.ConfigurationObject{
		Kind: tampered.Kind, ID: tampered.ID, Revision: 2,
		Body:               []byte("legitimately updated body"),
		SchemaRef:          tampered.SchemaRef,
		Scope:              tampered.Scope,
		PublisherPrincipal: tampered.PublisherPrincipal,
		PublishedAt:        tampered.PublishedAt.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("GREEN: publishing a genuine new revision: %v", err)
	}
	if updated.Digest() == tampered.Digest() {
		t.Fatal("GREEN: an updated revision must carry a different identity than the one it updates")
	}
	revisions, err := store.ListRevisions(scope, tampered.Kind, tampered.ID)
	if err != nil || len(revisions) != 2 {
		t.Fatalf("GREEN: ListRevisions = %v entries, err=%v, want 2 (both versions retained)", len(revisions), err)
	}
}

// TestTodo_CP_001_Golden pins the exact canonical digests this package's
// canonicalization profiles produce for a fixed input. A change to the
// encoding (field order, added/removed field, profile string) changes these
// values — deliberately, since [configregistry.ConfigurationObject.Digest]
// is meant to be a stable cross-process identity, not an implementation
// detail that can drift silently between releases.
func TestTodo_CP_001_Golden(t *testing.T) {
	store := configregistry.NewRegistry()
	scope := configregistry.Scope{TenantID: "golden-tenant", CellID: "golden-cell"}
	obj, err := configregistry.Publish(store, configregistry.ConfigurationObject{
		Kind:               configregistry.KindSchema,
		ID:                 "person",
		Revision:           1,
		Body:               []byte(`{"fields":["legal_name","worker_id"]}`),
		SchemaRef:          "hcmnext.schema.meta/v1",
		Scope:              scope,
		PublisherPrincipal: "golden-publisher",
		PublishedAt:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// These two constants were captured from this exact test's own first
	// green run (t.Logf below), matching how a golden test in this codebase
	// is normally seeded (see internal/workflow/version's own
	// wfcomp006_golden_test.go): a fixed input's digest is pinned so a
	// silent change to the canonicalization (field order, an added or
	// removed field, a changed profile string) is caught here rather than
	// discovered later as a cross-process identity mismatch.
	const wantBodyDigest = "972ee05d56c52bd621de12ad24ee50eb2bded3373f63b4332fe8db0565cca528"
	const wantRecordDigest = "6e20856afdb1adcbe496afebbbb76e9d37d8ec8584a118f75ab69b71067f7888"
	if obj.CanonicalBodyDigest != wantBodyDigest {
		t.Fatalf("canonical body digest = %s, want pinned golden %s", obj.CanonicalBodyDigest, wantBodyDigest)
	}
	if obj.Digest() != wantRecordDigest {
		t.Fatalf("record digest = %s, want pinned golden %s", obj.Digest(), wantRecordDigest)
	}

	// Re-publishing byte-identical content from a second, independent
	// Registry must reproduce the exact same digests — the golden property
	// that actually matters: the digest names the content, not the process
	// that computed it.
	secondStore := configregistry.NewRegistry()
	again, err := configregistry.Publish(secondStore, configregistry.ConfigurationObject{
		Kind:               configregistry.KindSchema,
		ID:                 "person",
		Revision:           1,
		Body:               []byte(`{"fields":["legal_name","worker_id"]}`),
		SchemaRef:          "hcmnext.schema.meta/v1",
		Scope:              scope,
		PublisherPrincipal: "golden-publisher",
		PublishedAt:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Publish (second store): %v", err)
	}
	if again.CanonicalBodyDigest != obj.CanonicalBodyDigest {
		t.Fatalf("body digest is not process-independent: %s != %s", again.CanonicalBodyDigest, obj.CanonicalBodyDigest)
	}
	if again.Digest() != obj.Digest() {
		t.Fatalf("record digest is not process-independent: %s != %s", again.Digest(), obj.Digest())
	}
}

// TestTodo_CP_001_Mutation proves a record read back from the store, then
// mutated by a caller, is DETECTABLY mutated: [ConfigurationObject.Verify]
// must fail, and the mutation must never have reached the store's own
// backing data (a second, independent read is unaffected). This is the
// "typed objects are content-addressed" GREEN clause's teeth: a digest that
// nobody ever checks is not an invariant.
func TestTodo_CP_001_Mutation(t *testing.T) {
	store := configregistry.NewRegistry()
	scope := configregistry.Scope{TenantID: "mutation-tenant"}
	published, err := configregistry.Publish(store, configregistry.ConfigurationObject{
		Kind:               configregistry.KindRule,
		ID:                 "eligibility",
		Revision:           1,
		Body:               []byte("original rule body"),
		SchemaRef:          "hcmnext.rule.definition/v1",
		Scope:              scope,
		PublisherPrincipal: "rules-admin",
		PublishedAt:        time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	readBack, found, err := store.GetObject(published.Ref())
	if err != nil || !found {
		t.Fatalf("GetObject: found=%v err=%v", found, err)
	}

	// Mutate the caller's own copy of the body in place.
	readBack.Body[0] = 'X'
	if err := readBack.Verify(); err == nil {
		t.Fatal("Verify() on a body-mutated copy: want error, got nil")
	} else if configregistry.CodeOf(err) != configregistry.CodeRecordMutated {
		t.Fatalf("CodeOf(err) = %q, want %q", configregistry.CodeOf(err), configregistry.CodeRecordMutated)
	}

	// The mutation must be local to readBack's own slice — a second,
	// independent read from the store proves the store's own bytes were
	// never touched.
	again, found, err := store.GetObject(published.Ref())
	if err != nil || !found {
		t.Fatalf("GetObject (again): found=%v err=%v", found, err)
	}
	if err := again.Verify(); err != nil {
		t.Fatalf("Verify() on a fresh read after a caller mutated its own earlier copy: %v", err)
	}
	if string(again.Body) != "original rule body" {
		t.Fatalf("store's own content changed: %q", again.Body)
	}

	// Metadata mutation (not just body) is caught the same way.
	relabeled := published
	relabeled.SchemaRef = "hcmnext.rule.definition/v2-not-really-published"
	if err := relabeled.Verify(); configregistry.CodeOf(err) != configregistry.CodeRecordMutated {
		t.Fatalf("Verify() on a metadata-mutated copy: CodeOf(err) = %q, want %q", configregistry.CodeOf(err), configregistry.CodeRecordMutated)
	}
}
