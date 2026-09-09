package importgraph_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depedge"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/importgraph"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
)

// TestGoImportGraphPolicy is the ARCH-GO-003 primary test. It builds the
// real import graph from `go list -json ./...`, asserts it is acyclic,
// asserts every package maps to a declared repository-layout root, asserts
// no edge violates the package-dependency policy, and emits a stable digest
// of the package/dependency list. The digest is only written to
// definitions/architecture/import-graph.digest when
// HCMNEXT_UPDATE_GOLDEN=1 is set; otherwise it is only computed and logged,
// because sibling packages under internal/, tools/ and gen/ are still
// landing from other agents this wave and a committed golden file would
// need to change on every one of those unrelated commits.
func TestGoImportGraphPolicy(t *testing.T) {
	root := repopath.RootDir()

	layoutManifest, err := layout.Load(filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
	if err != nil {
		t.Fatalf("loading repository-layout manifest: %v", err)
	}
	policy, err := depedge.Load(filepath.Join(root, "definitions", "architecture", "package-dependency-policy.yaml"))
	if err != nil {
		t.Fatalf("loading package-dependency-policy manifest: %v", err)
	}

	graph, err := importgraph.Build(root, layoutManifest, policy)
	if err != nil {
		t.Fatalf("building import graph: %v", err)
	}

	if len(graph.Packages) == 0 {
		t.Fatalf("import graph has no packages")
	}

	for _, v := range graph.LayoutViolations {
		// Waived paths (cmd/gen-todos, node_modules/...) classify as
		// Allowed by layout.ClassifyImportPath, so anything reaching this
		// slice is a genuine, undocumented layout violation.
		t.Errorf("package not mapped to a declared layout root: %s", v)
	}

	for _, v := range graph.PolicyViolations {
		t.Errorf("forbidden dependency edge: %s -> %s violates %s", v.Importer, v.Imported, v.Rule)
	}

	if graph.Cycle != nil {
		t.Errorf("import cycle detected: %s", strings.Join(graph.Cycle, " -> "))
	}

	t.Logf("import graph digest: %s (packages=%d edges=%d)", graph.Digest, len(graph.Packages), len(graph.Edges))

	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") == "1" {
		digestPath := filepath.Join(root, "definitions", "architecture", "import-graph.digest")
		content := graph.Digest + "\n"
		if err := os.WriteFile(digestPath, []byte(content), 0o644); err != nil {
			t.Fatalf("writing import-graph digest: %v", err)
		}
		t.Logf("wrote updated digest to %s", digestPath)
	}
}
