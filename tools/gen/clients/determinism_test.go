package clientsgen

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// repoRoot locates the repository root from this test file's own path,
// which is stable regardless of the working directory `go test` is invoked
// from.
func repoRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("determinism_test.go: runtime.Caller failed")
	}
	// this file is tools/gen/clients/determinism_test.go
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// TestRenderIsDeterministic proves that generating the client package twice,
// from the same checked-in manifest and the same linked descriptors,
// produces byte-identical output. This is the "generation is deterministic"
// guarantee TOOL-007 requires: a caller who reruns the generator with no
// input change must see git report no diff.
func TestRenderIsDeterministic(t *testing.T) {
	manifestPath := filepath.Join(repoRoot(t), DefaultManifestPath)

	first, err := Render(manifestPath)
	if err != nil {
		t.Fatalf("Render (first run): %v", err)
	}
	second, err := Render(manifestPath)
	if err != nil {
		t.Fatalf("Render (second run): %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("file count differs: first=%d second=%d", len(first), len(second))
	}
	for name, want := range first {
		got, ok := second[name]
		if !ok {
			t.Fatalf("second run did not produce %s", name)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%s differs between two runs of Render", name)
		}
	}
}

// TestGeneratedClientsCurrent regenerates the client package into a temp
// directory and diffs it against the committed
// internal/transport/clients tree, the same drift check TOOL-010's
// TestGeneratedArtifactsCurrent runs for the buf-generated gen/go tree. A
// hand-edit under internal/transport/clients, or a manifest change that was
// never regenerated, fails here.
func TestGeneratedClientsCurrent(t *testing.T) {
	root := repoRoot(t)
	manifestPath := filepath.Join(root, DefaultManifestPath)
	committedDir := filepath.Join(root, DefaultOutputDir)

	files, err := Render(manifestPath)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// internal/transport/clients also holds hand-written *_test.go files
	// (the TOOL-007/PROTO-006 parity suites) alongside the generated
	// non-test sources; only the latter are this generator's output, so
	// the drift check ignores anything ending in _test.go rather than
	// requiring an exact directory listing.
	committed, err := os.ReadDir(committedDir)
	if err != nil {
		t.Fatalf("reading %s: %v", committedDir, err)
	}
	var committedNames []string
	for _, entry := range committed {
		if entry.IsDir() || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		committedNames = append(committedNames, entry.Name())
	}
	sort.Strings(committedNames)

	renderedNames := sortedFileNames(files)
	if len(committedNames) != len(renderedNames) {
		t.Fatalf("file set differs: committed=%v rendered=%v", committedNames, renderedNames)
	}
	for i := range committedNames {
		if committedNames[i] != renderedNames[i] {
			t.Fatalf("file set differs: committed=%v rendered=%v", committedNames, renderedNames)
		}
	}

	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(committedDir, name))
		if err != nil {
			t.Fatalf("reading committed %s: %v", name, err)
		}
		if !bytes.Equal(want, got) {
			t.Errorf("%s has drifted from what the generator currently produces; run "+
				"`go run ./tools/gen/clients/cmd/generateclients`", name)
		}
	}
}
