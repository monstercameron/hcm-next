package configbundle

import (
	"errors"
	"fmt"
	"testing"
	"time"

	platformconfig "github.com/monstercameron/human-capital-management-suite/internal/platform/configregistry"
)

func publishObject(t *testing.T, store *platformconfig.Registry, scope Scope, kind Kind, id string, revision uint32, body string) platformconfig.ConfigurationObject {
	t.Helper()
	obj, err := platformconfig.Publish(store, platformconfig.ConfigurationObject{
		Kind: kind, ID: id, Revision: revision, Body: []byte(body),
		SchemaRef: "hcmnext.configbundle.test/v1", Scope: scope,
		PublisherPrincipal: "config-publisher", PublishedAt: time.Unix(int64(revision), 0).UTC(),
	})
	if err != nil {
		t.Fatalf("publish %s/%s@%d: %v", kind, id, revision, err)
	}
	if _, err := platformconfig.Activate(store, obj.Ref(), platformconfig.ActivationEvidence{ActivatedBy: "release-manager", ActivatedAt: time.Unix(int64(revision)+100, 0).UTC()}); err != nil {
		t.Fatalf("activate %s/%s@%d: %v", kind, id, revision, err)
	}
	return obj
}

func compileOptions(scope Scope, refs ...ObjectRef) CompileOptions {
	return CompileOptions{BundleID: "bundle-test", Roots: refs, TargetScope: scope, MinimumRuntimeVersion: "go1.26.3"}
}

// TestTodo_CP_002 proves that compilation resolves a complete, exact,
// active dependency closure and records every included object digest.
func TestTodo_CP_002(t *testing.T) {
	store := platformconfig.NewRegistry()
	scope := Scope{TenantID: "tenant-cp002"}
	rule := publishObject(t, store, scope, KindRule, "eligibility", 1, `{"kind":"rule"}`)
	mapping := publishObject(t, store, scope, KindMapping, "hris", 1, `{"kind":"mapping"}`)
	workflowBody := fmt.Sprintf(`{"dependencies":[{"kind":"RULE","id":"eligibility","revision":1,"digest":%q},{"kind":"MAPPING","id":"hris","revision":1,"digest":%q}]}`, rule.Digest(), mapping.Digest())
	workflow := publishObject(t, store, scope, KindWorkflow, "onboarding", 1, workflowBody)
	bundle, err := Compile(store, compileOptions(scope, workflow.Ref()))
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Objects) != 3 {
		t.Fatalf("closure contains %d objects, want workflow, rule, and mapping", len(bundle.Objects))
	}
	if bundle.Digest == "" || bundle.TargetScope != scope || bundle.MinimumRuntimeVersion != "go1.26.3" {
		t.Fatalf("bundle identity is incomplete: %+v", bundle)
	}
	if err := bundle.Verify(); err != nil {
		t.Fatalf("fresh bundle failed Verify: %v", err)
	}
}

func TestTodo_CP_002_Golden(t *testing.T) {
	store := platformconfig.NewRegistry()
	scope := Scope{TenantID: "tenant-cp002-golden", CellID: "cell-a"}
	a := publishObject(t, store, scope, KindRule, "a", 1, `{"value":"a"}`)
	b := publishObject(t, store, scope, KindRule, "b", 1, `{"value":"b"}`)
	first, err := Compile(store, compileOptions(scope, a.Ref(), b.Ref()))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compile(store, compileOptions(scope, b.Ref(), a.Ref()))
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("root order changed bundle digest: %s != %s", first.Digest, second.Digest)
	}
	if len(first.Canonical()) == 0 {
		t.Fatal("canonical bundle manifest is empty")
	}
}

func TestTodo_CP_002_Integration(t *testing.T) {
	store := platformconfig.NewRegistry()
	scope := Scope{TenantID: "tenant-cp002-integration"}
	v1 := publishObject(t, store, scope, KindWorkflow, "workflow", 1, `{"steps":["a"]}`)
	_ = publishObject(t, store, scope, KindWorkflow, "workflow", 2, `{"steps":["b"]}`)
	if _, err := Compile(store, compileOptions(scope, v1.Ref())); !errors.Is(err, ErrSupersededObject) {
		t.Fatalf("superseded root error = %v, want ErrSupersededObject", err)
	}
	missing := ObjectRef{Scope: scope, Kind: KindRule, ID: "never-published", Revision: 1}
	if _, err := Compile(store, compileOptions(scope, missing)); !errors.Is(err, ErrUnpublishedObject) {
		t.Fatalf("unpublished root error = %v, want ErrUnpublishedObject", err)
	}
}

func TestTodo_CP_002_Conformance(t *testing.T) {
	store := platformconfig.NewRegistry()
	scope := Scope{TenantID: "tenant-cp002-conformance"}
	root := publishObject(t, store, scope, KindRule, "root", 1, `{"value":"root"}`)
	for _, tc := range []struct {
		name string
		ref  ObjectRef
		want error
	}{
		{name: "un pinned root", ref: ObjectRef{Scope: scope, Kind: root.Kind, ID: root.ID}, want: ErrUnpinnedReference},
		{name: "wrong scope", ref: ObjectRef{Scope: Scope{TenantID: "other"}, Kind: KindRule, ID: "root", Revision: 1}, want: ErrScopeMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(store, compileOptions(scope, tc.ref))
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}

	cycleStore := platformconfig.NewRegistry()
	cycleScope := Scope{TenantID: "tenant-cp002-cycle"}
	a := publishObject(t, cycleStore, cycleScope, KindRule, "a", 1, `{"dependencies":[{"kind":"RULE","id":"b","revision":1}]}`)
	b := publishObject(t, cycleStore, cycleScope, KindRule, "b", 1, `{"dependencies":[{"kind":"RULE","id":"a","revision":1}]}`)
	if _, err := Compile(cycleStore, compileOptions(cycleScope, a.Ref(), b.Ref())); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("cycle error = %v, want ErrDependencyCycle", err)
	}
}

func TestDiffNamesAddedRemovedAndReversionedObjects(t *testing.T) {
	scope := Scope{TenantID: "diff-tenant"}
	a := Bundle{BundleID: "a", ManifestVersion: Version(), TargetScope: scope, Scope: scope, MinimumRuntimeVersion: "go1.26", Objects: []IncludedObject{{Ref: ObjectRef{Scope: scope, Kind: KindRule, ID: "same", Revision: 1}, Digest: "digest-a"}, {Ref: ObjectRef{Scope: scope, Kind: KindRule, ID: "removed", Revision: 1}, Digest: "digest-r"}}}
	b := Bundle{BundleID: "b", ManifestVersion: Version(), TargetScope: scope, Scope: scope, MinimumRuntimeVersion: "go1.26", Objects: []IncludedObject{{Ref: ObjectRef{Scope: scope, Kind: KindRule, ID: "same", Revision: 2}, Digest: "digest-b"}, {Ref: ObjectRef{Scope: scope, Kind: KindRule, ID: "added", Revision: 1}, Digest: "digest-n"}}}
	diff := Diff(a, b)
	if len(diff.Added) != 1 || diff.Added[0].After.ID != "added" {
		t.Fatalf("added = %+v", diff.Added)
	}
	if len(diff.Removed) != 1 || diff.Removed[0].Before.ID != "removed" {
		t.Fatalf("removed = %+v", diff.Removed)
	}
	if len(diff.Reversioned) != 1 || diff.Reversioned[0].Before.Revision != 1 || diff.Reversioned[0].After.Revision != 2 {
		t.Fatalf("reversioned = %+v", diff.Reversioned)
	}
	if diff.Digest == "" || len(diff.Changed) != 1 {
		t.Fatalf("diff digest/compatibility change missing: %+v", diff)
	}
}
