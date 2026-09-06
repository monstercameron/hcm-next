package cleancheckout

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func writeFixture(t *testing.T, root, rel, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_CICD_001 proves the clean-copy boundary: a working-tree Go input
// is diagnosed when it is not tracked, while a declared build-time asset is
// visible but does not become a new gap.
func TestTodo_CICD_001(t *testing.T) {
	working := t.TempDir()
	clean := t.TempDir()
	writeFixture(t, working, "go.mod", "module example.com/clean\n\ngo 1.26.3\n")
	writeFixture(t, working, "main.go", "package main\nfunc main() {}\n")
	writeFixture(t, working, "needed.go", "package main\nvar generated = true\n")
	writeFixture(t, working, "assets/.keep", "marker\n")
	writeFixture(t, working, "internal/humanwork/workspace/assets/journey.wasm", "wasm\n")
	writeFixture(t, working, "embed.go", "package main\n\nimport _ \"embed\"\n\n//go:embed internal/humanwork/workspace/assets/journey.wasm\nvar journey []byte\n")

	tracked := []string{"go.mod", "main.go", "assets/.keep", "embed.go"}
	exported, err := ExportPaths(working, clean, tracked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(clean, "needed.go")); !os.IsNotExist(err) {
		t.Fatalf("untracked needed.go was copied into clean tree: err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(clean, "internal/humanwork/workspace/assets/journey.wasm")); !os.IsNotExist(err) {
		t.Fatalf("untracked build artifact was copied into clean tree: err=%v", err)
	}

	findings, err := FindWorkingTreeGaps(working, exported.Paths)
	if err != nil {
		t.Fatal(err)
	}
	var missing, artifact *Finding
	for i := range findings {
		switch findings[i].Path {
		case "needed.go":
			missing = &findings[i]
		case "internal/humanwork/workspace/assets/journey.wasm":
			artifact = &findings[i]
		}
	}
	if missing == nil || missing.Code != "missing-tracked-file" || missing.Allowlisted {
		t.Fatalf("missing tracked input finding = %+v, want an unallowlisted missing-tracked-file", missing)
	}
	if artifact == nil || artifact.Code != "expected-build-artifact" || !artifact.Allowlisted {
		t.Fatalf("artifact finding = %+v, want an allowlisted expected-build-artifact", artifact)
	}
}

// TestTodo_CICD_001_Property checks that every path exported into a clean
// tree is tracked and every untracked Go input remains absent from it.
func TestTodo_CICD_001_Property(t *testing.T) {
	working := t.TempDir()
	clean := t.TempDir()
	writeFixture(t, working, "go.mod", "module example.com/property\n\ngo 1.26.3\n")
	paths := []string{"go.mod"}
	for i := 0; i < 8; i++ {
		rel := filepath.ToSlash(filepath.Join("pkg", "tracked"+string(rune('a'+i))+".go"))
		writeFixture(t, working, rel, "package pkg\n")
		paths = append(paths, rel)
	}
	writeFixture(t, working, "pkg/untracked.go", "package pkg\n")

	exported, err := ExportPaths(working, clean, paths)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(exported.Paths, append([]string{"go.mod"}, paths[1:]...)) {
		t.Fatalf("exported paths = %v, want sorted tracked paths", exported.Paths)
	}
	for _, rel := range paths {
		if _, err := os.Stat(filepath.Join(clean, filepath.FromSlash(rel))); err != nil {
			t.Errorf("tracked path %s missing from clean tree: %v", rel, err)
		}
	}
	findings, err := FindWorkingTreeGaps(working, exported.Paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Path != "pkg/untracked.go" {
		t.Fatalf("findings = %+v, want exactly pkg/untracked.go", findings)
	}
}

// TestTodo_CICD_001_Golden pins the clean-checkout artifact and command
// declarations so regeneration instructions do not drift silently.
func TestTodo_CICD_001_Golden(t *testing.T) {
	var paths []string
	for _, artifact := range BuildTimeArtifacts {
		paths = append(paths, artifact.Path)
		if artifact.Command == "" || artifact.Owner == "" {
			t.Fatalf("incomplete artifact declaration: %+v", artifact)
		}
	}
	want := []string{
		"internal/humanwork/workspace/assets/journey.wasm",
		"internal/humanwork/workspace/assets/uxqual.wasm",
		"internal/humanwork/workspace/assets/wasm_exec.js",
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("artifact paths = %v, want %v", paths, want)
	}
	if len(PolicyCommands) != 3 || RacePolicyCommand.Name != "racepolicy" {
		t.Fatalf("policy commands = %+v plus %+v, want policy trio plus racepolicy", PolicyCommands, RacePolicyCommand)
	}
	if got := (Report{}).Digest(); got == "" || got != (Report{}).Digest() {
		t.Fatalf("report digest is not stable: %q", got)
	}
}

// TestTodo_CICD_001_Race exercises the pure classifier concurrently. It must
// not mutate the tracked set or the declared artifact/owner tables.
func TestTodo_CICD_001_Race(t *testing.T) {
	working := t.TempDir()
	writeFixture(t, working, "needed.go", "package needed\n")
	tracked := []string{}
	const workers = 16
	results := make(chan string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			findings, err := FindWorkingTreeGaps(working, tracked)
			if err != nil {
				results <- err.Error()
				return
			}
			if len(findings) == 1 {
				results <- findings[0].Code + ":" + findings[0].Path
				return
			}
			results <- strings.Join([]string{"unexpected", string(rune(len(findings)))}, ":")
		}()
	}
	wg.Wait()
	close(results)
	for result := range results {
		if result != "missing-tracked-file:needed.go" {
			t.Fatalf("concurrent classifier result = %q", result)
		}
	}
}
