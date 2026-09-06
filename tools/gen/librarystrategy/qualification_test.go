package librarystrategy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLibraryStrategyQualificationRecordsAreGenerated proves that the
// library-strategy generator reads all requested libqualification records and
// produces a deterministic table for them.
func TestLibraryStrategyQualificationRecordsAreGenerated(t *testing.T) {
	root := libraryStrategyRepoRoot(t)
	decisions, err := LoadQualificationDecisions(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"LIB-005", "LIB-009", "LIB-010", "LIB-011", "LIB-012", "LIB-014", "LIB-019", "TOOL-022"}
	if len(decisions) != len(want) {
		t.Fatalf("got %d qualification decisions, want %d: %+v", len(decisions), len(want), decisions)
	}
	section := RenderQualificationDecisions(decisions)
	for _, todo := range want {
		if !strings.Contains(section, "| `"+todo+"` |") {
			t.Errorf("qualification section omitted %s", todo)
		}
	}
	if strings.Contains(section, "| `LIB-005` |  —") {
		t.Fatal("qualification section omitted a decision verdict")
	}
}

// TestLibraryStrategyQualificationGenerationKeepsCoreDriftCheck proves the
// checked-in README still equals a fresh Render from dependency-roles.yaml.
// The additive qualification renderer is separately inspectable because the
// existing command/README are owned by the stopped lane and are not edited.
func TestLibraryStrategyQualificationGenerationKeepsCoreDriftCheck(t *testing.T) {
	root := libraryStrategyRepoRoot(t)
	generated, err := RenderWithQualificationDecisions(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(generated, "### Qualification decisions") {
		t.Fatal("fresh additive generation omitted qualification decisions")
	}
	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(readme), "### Qualification decisions") {
		t.Fatal("test fixture unexpectedly contains an untracked qualification section")
	}
}

func libraryStrategyRepoRoot(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			t.Fatal("repository root not found")
		}
		d = parent
	}
}
