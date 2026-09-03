package gen

import (
	"path/filepath"
	"testing"
)

// TestGeneratedArtifactsCurrent is the TOOL-010 primary test. It regenerates
// the Go bindings into a temp directory from the checked-in buf.yaml/
// buf.gen.yaml/schema and diffs the result against the committed gen/go
// tree. Any drift (an edited generated file, or a schema change that was
// never regenerated) fails the test, which is what CI uses to block drift.
func TestGeneratedArtifactsCurrent(t *testing.T) {
	repoRoot := findRepoRoot(t)

	freshOut := t.TempDir()
	runBuf(t, repoRoot, "generate", "--template", "buf.gen.yaml", "-o", freshOut)

	diffTreesByteIdentical(t, filepath.Join(freshOut, "gen", "go"), filepath.Join(repoRoot, "gen", "go"))
}
