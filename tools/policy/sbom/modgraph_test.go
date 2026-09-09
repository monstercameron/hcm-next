package sbom_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/sbom"
)

// TestModGraph_Live runs `go mod graph` against this repository's real
// root. This is the deviation TOOL-017's doc.go documents: `go list -m
// -json all` fails outright here (a malformed module path reachable
// elsewhere in the graph), while `go mod graph` succeeds, which is exactly
// why Generate uses it for the dependency graph instead.
func TestModGraph_Live(t *testing.T) {
	root := repopath.RootDir()
	modulePath := repopath.ModulePath(root)

	edges, err := sbom.ModGraph(root)
	if err != nil {
		t.Fatalf("ModGraph(%s): %v", root, err)
	}
	if len(edges) == 0 {
		t.Fatal("ModGraph returned no edges for the live repository")
	}

	foundRootEdge := false
	for _, e := range edges {
		if e.FromPath == modulePath {
			foundRootEdge = true
			if e.FromVersion != "" {
				t.Errorf("edge from the main module carries a version (%q); go mod graph never prints one for it", e.FromVersion)
			}
			if e.ToPath == "" || e.ToVersion == "" {
				t.Errorf("edge %+v has an empty target path/version", e)
			}
		}
	}
	if !foundRootEdge {
		t.Errorf("no edge in the graph originates from the root module %q", modulePath)
	}
}

func TestModGraphRejectsARootWithoutAModule(t *testing.T) {
	if _, err := sbom.ModGraph(t.TempDir()); err == nil {
		t.Fatal("ModGraph unexpectedly succeeded outside a Go module")
	}
}
