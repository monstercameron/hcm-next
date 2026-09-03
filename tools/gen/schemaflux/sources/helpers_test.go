package sources_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/tools/gen/schemaflux/sources"
)

// findRepoRoot walks up from the working directory to the first ancestor
// containing go.mod, mirroring tools/gen/schemaflux's own copy of this
// helper (unexported there, so this package keeps its own rather than
// reaching into a frozen sibling).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root: no go.mod found in any parent directory")
		}
		dir = parent
	}
}

// loadValidBundle loads the real, checked-in metamodel, registries and
// entity-family sources and returns them as a [sources.Bundle], failing the
// test on any load error. Every test in this package that needs the real
// source tree starts here rather than re-implementing the three Load calls.
func loadValidBundle(t *testing.T) sources.Bundle {
	t.Helper()
	root := findRepoRoot(t)

	mm, err := sources.LoadMetamodel(filepath.Join(root, "schema", "schemaflux", "metamodel", "v1", "metamodel.yaml"))
	if err != nil {
		t.Fatalf("LoadMetamodel: %v", err)
	}
	auths, rets, err := sources.LoadRegistries(filepath.Join(root, "schema", "schemaflux", "registries", "v1", "registries.yaml"))
	if err != nil {
		t.Fatalf("LoadRegistries: %v", err)
	}
	ents, rels, err := sources.LoadEntityFamilies(filepath.Join(root, "schema", "schemaflux", "entities", "v1"))
	if err != nil {
		t.Fatalf("LoadEntityFamilies: %v", err)
	}
	return sources.Bundle{Metamodel: mm, Authorities: auths, Retentions: rets, Entities: ents, Relationships: rels}
}

// compileValid loads and compiles the real source tree and fails the test if
// Compile reports any unresolved reference: every test that needs a clean
// starting [sources.Manifest] to mutate or inspect starts here.
func compileValid(t *testing.T) (*sources.Manifest, sources.Bundle) {
	t.Helper()
	bundle := loadValidBundle(t)
	manifest, errs := sources.Compile(bundle)
	if len(errs) != 0 {
		t.Fatalf("Compile of the checked-in sources returned %d error(s), want 0: %v", len(errs), errs)
	}
	return manifest, bundle
}

// entitiesInFamily filters a bundle's entities to one family.
func entitiesInFamily(bundle sources.Bundle, family string) []sources.EntitySource {
	var out []sources.EntitySource
	for _, e := range bundle.Entities {
		if e.Family == family {
			out = append(out, e)
		}
	}
	return out
}

// findEntity returns the entity named name (any version) from bundle, or
// fails the test.
func findEntity(t *testing.T, bundle sources.Bundle, name string) sources.EntitySource {
	t.Helper()
	for _, e := range bundle.Entities {
		if e.Name == name {
			return e
		}
	}
	t.Fatalf("no entity named %q in bundle", name)
	return sources.EntitySource{}
}

// assertStringSlicesEqual fails the test with an index-by-index diff unless
// got and want are the same length and content, mirroring
// tools/gen/schemaflux's own golden-comparison helper.
func assertStringSlicesEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d entries %v, want %d entries %v", label, len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d] = %q, want %q", label, i, got[i], want[i])
		}
	}
}
