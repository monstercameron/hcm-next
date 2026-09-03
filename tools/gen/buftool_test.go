package gen

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

// findRepoRoot walks upward from the current working directory until it
// finds buf.yaml, which lives at the repository root. Tests run with a
// working directory under tools/gen, so this is normally two levels up, but
// walking rather than hardcoding the depth keeps the test robust to how it
// is invoked (`go test ./...` from the root, an IDE runner, etc).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "buf.yaml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root: no buf.yaml found in any parent directory")
		}
		dir = parent
	}
}

// runBuf runs the buf CLI (must be on PATH) with the given arguments and cwd
// set to repoRoot, failing the test with combined stdout/stderr on error.
func runBuf(t *testing.T, repoRoot string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("buf", args...)
	cmd.Dir = repoRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("buf %v: %v\noutput:\n%s", args, err, out.String())
	}
	return out.Bytes()
}

// bufVersion returns the buf CLI's own version string, used to pin it in
// gen/TOOLS.lock.
func bufVersion(t *testing.T, repoRoot string) string {
	t.Helper()
	out := runBuf(t, repoRoot, "--version")
	return string(bytes.TrimSpace(out))
}

// listTreeFiles returns the sorted, slash-normalized relative paths of every
// regular file under root.
func listTreeFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	sort.Strings(files)
	return files
}

// diffTreesByteIdentical fails the test with a precise reason unless dir1
// and dir2 contain exactly the same set of relative file paths with
// byte-identical content.
func diffTreesByteIdentical(t *testing.T, dir1, dir2 string) {
	t.Helper()

	files1 := listTreeFiles(t, dir1)
	files2 := listTreeFiles(t, dir2)

	if fmt.Sprint(files1) != fmt.Sprint(files2) {
		t.Fatalf("generated file sets differ:\n%v\nvs\n%v", files1, files2)
	}
	if len(files1) == 0 {
		t.Fatal("generated tree is empty; nothing to compare")
	}

	for _, rel := range files1 {
		b1, err := os.ReadFile(filepath.Join(dir1, rel))
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Join(dir1, rel), err)
		}
		b2, err := os.ReadFile(filepath.Join(dir2, rel))
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Join(dir2, rel), err)
		}
		if !bytes.Equal(b1, b2) {
			t.Fatalf("file %s differs between %s and %s", rel, dir1, dir2)
		}
	}
}
