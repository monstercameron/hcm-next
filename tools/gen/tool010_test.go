package gen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

	// model_generated.go is produced by the independent tools/gen/modelgen
	// generator, not buf. Compare the buf-owned tree while leaving that
	// separately generated artifact in the committed tree.
	diffBufTreesByteIdentical(t, filepath.Join(freshOut, "gen", "go"), filepath.Join(repoRoot, "gen", "go"))
}

func diffBufTreesByteIdentical(t *testing.T, freshDir, committedDir string) {
	t.Helper()
	fresh := listTreeFiles(t, freshDir)
	committed := listTreeFiles(t, committedDir)
	const modelGenerated = "hcmnext/model/model_generated.go"
	filtered := committed[:0]
	for _, name := range committed {
		if name != modelGenerated {
			filtered = append(filtered, name)
		}
	}
	committed = filtered
	if fmt.Sprint(fresh) != fmt.Sprint(committed) {
		t.Fatalf("buf-generated file sets differ:\n%v\nvs\n%v", fresh, committed)
	}
	if len(fresh) == 0 {
		t.Fatal("buf-generated tree is empty; nothing to compare")
	}
	sort.Strings(fresh)
	for _, rel := range fresh {
		want, err := os.ReadFile(filepath.Join(freshDir, rel))
		if err != nil {
			t.Fatalf("read fresh %s: %v", rel, err)
		}
		got, err := os.ReadFile(filepath.Join(committedDir, rel))
		if err != nil {
			t.Fatalf("read committed %s: %v", rel, err)
		}
		if string(want) != string(got) {
			t.Fatalf("buf-generated file %s differs", rel)
		}
	}
}
