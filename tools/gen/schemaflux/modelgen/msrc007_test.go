package modelgen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux/sources"
)

func msrc007Manifest(t *testing.T) *sources.Manifest {
	t.Helper()
	root := repoRoot(t)
	metamodel, err := sources.LoadMetamodel(filepath.Join(root, "schema/schemaflux/metamodel/v1/metamodel.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	auth, ret, err := sources.LoadRegistries(filepath.Join(root, "schema/schemaflux/registries/v1/registries.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	ents, rels, err := sources.LoadEntityFamilies(filepath.Join(root, "schema/schemaflux/entities/v1"))
	if err != nil {
		t.Fatal(err)
	}
	m, errs := sources.Compile(sources.Bundle{Metamodel: metamodel, Authorities: auth, Retentions: ret, Entities: ents, Relationships: rels})
	if len(errs) != 0 {
		t.Fatalf("source compile: %v", errs)
	}
	return m
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("repository root not found")
		}
		dir = next
	}
}

// TestTodo_MSRC_007 is the primary generation contract: all source entities
// become typed Go structs, with no generic modeled payload escape hatch, and
// every relation is represented in the generated registry.
func TestTodo_MSRC_007(t *testing.T) {
	m := msrc007Manifest(t)
	a, err := Generate(m, "schemaflux")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entities) != 114 || len(m.Relationships) != 5 {
		t.Fatalf("source counts = %d entities, %d relationships; want 114 and 5", len(m.Entities), len(m.Relationships))
	}
	if !bytes.Contains(a.Go, []byte("type Person struct")) || !bytes.Contains(a.Go, []byte("func (v Person) Validate() error")) {
		t.Fatal("generated Person type/validator missing")
	}
	if bytes.Contains(a.Go, []byte("map[string]any")) {
		t.Fatal("generated model contains map[string]any")
	}
	if a.SourceDigest == "" || a.GeneratedDigest == "" {
		t.Fatal("generation did not publish source and generated digests")
	}
	if !bytes.Contains(a.Go, []byte("CanonicalDigest")) || !bytes.Contains(a.Go, []byte("RelationDescriptor")) {
		t.Fatal("generated digest or relation metadata helper missing")
	}
}

// TestTodo_MSRC_007_Golden pins the source and generated identities. A source
// edit must intentionally change these values and therefore the generated
// artifact, while repeated runs remain byte-identical.
func TestTodo_MSRC_007_Golden(t *testing.T) {
	a, err := Generate(msrc007Manifest(t), "schemaflux")
	if err != nil {
		t.Fatal(err)
	}
	wantSource := "sha256:0477e12b252685066eeb2274dd867023ee3cff01b87df05c7c19f15c7ea2c555"
	wantGenerated := "sha256:f795a0118aff185db8a74657fd4798c4a777ec6ffdf9a95d64a3fc347299deb5"
	if a.SourceDigest != wantSource {
		t.Fatalf("source digest = %s, want %s", a.SourceDigest, wantSource)
	}
	if a.GeneratedDigest != wantGenerated {
		t.Fatalf("generated digest = %s, want %s", a.GeneratedDigest, wantGenerated)
	}
	if len(a.Go) == 0 || !strings.Contains(string(a.Go), a.GeneratedDigest) {
		t.Fatal("generated digest is absent from generated header/source")
	}
	b, err := Generate(msrc007Manifest(t), "schemaflux")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Go, b.Go) || a.SourceDigest != b.SourceDigest || a.GeneratedDigest != b.GeneratedDigest {
		t.Fatal("identical source builds differ")
	}
}

// TestTodo_MSRC_007_Race builds independent artifacts concurrently. The
// generator only reads immutable source values and must not expose shared
// mutable state or map-order drift.
func TestTodo_MSRC_007_Race(t *testing.T) {
	m := msrc007Manifest(t)
	const n = 12
	results := make([]Artifact, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			var err error
			results[i], err = Generate(m, "schemaflux")
			if err != nil {
				t.Errorf("build %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	for i := 1; i < n; i++ {
		if !bytes.Equal(results[0].Go, results[i].Go) {
			t.Fatalf("build %d differs from build 0", i)
		}
	}
}
