package bindingcheck

import (
	"path/filepath"
	"sync"
	"testing"

	sfx "github.com/monstercameron/human-capital-management-suite/tools/gen/schemaflux"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	d, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func sourceCatalog(t *testing.T) *sfx.Catalog {
	t.Helper()
	root := repoRoot(t)
	defs, err := sfx.LoadDefinitions(filepath.Join(root, "schema", "schemaflux", "business_intents", "v1"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := sfx.LoadCapabilityManifest(filepath.Join(root, "tools", "gen", "schemaflux", "testdata", "capability_manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	c, errs := sfx.Compile(defs, manifest)
	if len(errs) != 0 {
		t.Fatalf("compile: %v", errs)
	}
	return c
}

// TestTodo_MSRC_009 is the primary drafted-definition-to-generated-model
// binding check: exactly fourteen rows and no dangling references.
func TestTodo_MSRC_009(t *testing.T) {
	r := Check(sourceCatalog(t))
	if !r.Valid {
		t.Fatalf("MSRC-009 report invalid: %s", r.Summary())
	}
	if len(r.Rows) != 14 {
		t.Fatalf("rows=%d, want 14", len(r.Rows))
	}
	for _, row := range r.Rows {
		if row.SourceRef == "" || row.InputSchema == "" || row.ResultSchema == "" || row.AggregateRoots == 0 || row.ReadProperties == 0 {
			t.Fatalf("incomplete row: %+v", row)
		}
	}
}

func TestTodo_MSRC_009_Golden(t *testing.T) {
	r := Check(sourceCatalog(t))
	if !r.Valid {
		t.Fatal(r.Summary())
	}
	for i, row := range r.Rows {
		t.Logf("%02d %s roots=%d reads=%d writes=%d", i+1, row.Definition, row.AggregateRoots, row.ReadProperties, row.WriteProperties)
	}
}

func TestTodo_MSRC_009_Race(t *testing.T) {
	c := sourceCatalog(t)
	want := Check(c).Summary()
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := Check(c).Summary(); got != want {
				t.Errorf("summary=%q, want %q", got, want)
			}
		}()
	}
	wg.Wait()
}
