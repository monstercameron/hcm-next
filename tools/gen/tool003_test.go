package gen

import (
	"path/filepath"
	"testing"
)

// TestGeneratedTreeIsReproducible is the TOOL-003 primary test. It runs
// `buf generate` twice against the same buf.gen.yaml template into two
// independent temp output directories and asserts the resulting trees are
// byte-identical, ruling out timestamp, path or Go map/iteration-order
// drift between clean runs.
func TestGeneratedTreeIsReproducible(t *testing.T) {
	repoRoot := findRepoRoot(t)

	out1 := t.TempDir()
	out2 := t.TempDir()

	runBuf(t, repoRoot, "generate", "--template", "buf.gen.yaml", "-o", out1)
	runBuf(t, repoRoot, "generate", "--template", "buf.gen.yaml", "-o", out2)

	diffTreesByteIdentical(t, filepath.Join(out1, "gen", "go"), filepath.Join(out2, "gen", "go"))
}
