package librarystrategy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			t.Fatal("repository root not found")
		}
		d = p
	}
}

// TestReadmeLibraryStrategyMatchesManifest is LIB-015's primary check. It
// compares the checked-in README with a fresh, manifest-derived rendering.
func TestReadmeLibraryStrategyMatchesManifest(t *testing.T) {
	root := repoRoot(t)
	if err := Check(root); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_LIB_015_Golden pins the stable semantic claims and candidate
// names. Versions and tables are intentionally checked through generation,
// not repeated as a second hand-maintained golden source.
func TestTodo_LIB_015_Golden(t *testing.T) {
	m, err := Load(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	got := Render(m)
	for _, want := range []string{
		BeginMarker, EndMarker,
		"Go", "GWC / GoWebComponents", "grpcbridge", "SchemaFlux",
		"PROJECT CORE; product language/toolchain",
		"replaceable mechanics", "Prohibited semantic frameworks",
		"github.com/monstercameron/human-capital-management-suite",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("generated README inventory does not contain %q", want)
		}
	}
}

// TestTodo_LIB_015_Integration proves the write boundary can update an
// isolated README and does not require changing the shared checkout.
func TestTodo_LIB_015_Integration(t *testing.T) {
	root := repoRoot(t)
	m, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(string(original), "<!-- END GENERATED LIBRARY STRATEGY -->", "stale\n<!-- END GENERATED LIBRARY STRATEGY -->", 1)
	updated, err := Replace([]byte(mutated), []byte(Render(m)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "stale\n<!-- END GENERATED LIBRARY STRATEGY -->") {
		t.Fatal("stale generated content survived replacement")
	}
	if string(mutated) == string(updated) {
		t.Fatal("fixture mutation was not repaired")
	}
}

// TestTodo_LIB_015_Conformance ensures generated documentation preserves the
// semantic firewall: no prohibited framework is presented as an approved
// dependency and no JavaScript runtime is implied by the generated region.
func TestTodo_LIB_015_Conformance(t *testing.T) {
	m, err := Load(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	got := Render(m)
	// Prohibited prefixes must be visible as prohibited rows (omitting one is
	// the LIB-015 RED case), but never appear in the preferred-candidate table.
	for _, banned := range []string{"gorm.io/gorm", "entgo.io/ent", "go.temporal.io/sdk", "go.starlark.net", "gopher-lua"} {
		if !strings.Contains(got, banned) {
			t.Errorf("generated inventory omitted prohibited framework %q", banned)
		}
	}
	preferred := got[:strings.Index(got, "### Package and dependency shape")]
	for _, banned := range []string{"gorm.io/gorm", "entgo.io/ent", "go.temporal.io/sdk", "go.starlark.net", "gopher-lua", "Node API", "TypeScript application runtime"} {
		if strings.Contains(preferred, banned) {
			t.Errorf("generated preferred-candidate table promotes prohibited/runtime text %q", banned)
		}
	}
	for _, d := range m.Dependencies {
		if d.Role == "PROJECT_CORE" && d.Path != "" {
			t.Errorf("module %q is incorrectly labelled PROJECT_CORE", d.Path)
		}
	}
}

// TestTodo_LIB_015_Browser checks the generated region remains valid markdown
// for the browser-rendered README: fenced blocks are balanced and marker
// delimiters occur exactly once.
func TestTodo_LIB_015_Browser(t *testing.T) {
	m, err := Load(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	got := Render(m)
	if strings.Count(got, "```") != 2 {
		t.Fatalf("generated diagram has %d fence delimiters, want 2", strings.Count(got, "```"))
	}
	if strings.Count(got, BeginMarker) != 1 || strings.Count(got, EndMarker) != 1 {
		t.Fatal("generated marker pair is not total")
	}
}
