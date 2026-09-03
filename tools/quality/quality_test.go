package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repoRoot walks up from this test file's own location to find go.mod. It
// cannot import tools/policy/internal/repopath (that package is Go-internal
// to tools/policy), so it repeats the same small upward search main.go
// uses at runtime.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found walking up from %s", thisFile)
		}
		dir = parent
	}
}

// TestTodo_TOOL_011 is the TOOL-011 primary test. It proves each of the
// four fixture categories (formatting drift, unchecked error, impossible
// branch, unsafe conversion) is rejected by the same tools tools/quality
// wires together (gofmt, go vet's default analyzers), and that a
// correctly-written fixture package is accepted by all of them.
func TestTodo_TOOL_011(t *testing.T) {
	root := repoRoot(t)
	fixtures := filepath.Join("tools", "quality", "testdata", "fixtures")

	t.Run("formatting drift is rejected by gofmt", func(t *testing.T) {
		// The drifted file is written at test time rather than checked in:
		// the repository's commit hook runs gofmt -w over every staged .go
		// file, so a committed unformatted fixture silently self-heals and
		// the check stops proving anything.
		drift := t.TempDir()
		src := "package fmtdrift\n\nfunc Add(a int,b int) int {\nreturn a+b\n}\n"
		if err := os.WriteFile(filepath.Join(drift, "fmtdrift.go"), []byte(src), 0o644); err != nil {
			t.Fatalf("writing drift fixture: %v", err)
		}
		violations, err := gofmtViolations(drift)
		if err != nil {
			t.Fatalf("gofmtViolations: %v", err)
		}
		if len(violations) == 0 {
			t.Fatalf("expected gofmt to flag the fmtdrift fixture, found no violations")
		}
	})

	t.Run("unchecked error is rejected by go vet", func(t *testing.T) {
		pattern := "./" + filepath.ToSlash(filepath.Join(fixtures, "uncheckederror")) + "/..."
		out, passed := runGoVet(root, pattern)
		if passed {
			t.Fatalf("expected go vet to reject the uncheckederror fixture, it passed:\n%s", out)
		}
		if !strings.Contains(out, "not used") {
			t.Errorf("expected go vet output to mention an unused result, got:\n%s", out)
		}
	})

	t.Run("impossible branch is rejected by go vet", func(t *testing.T) {
		pattern := "./" + filepath.ToSlash(filepath.Join(fixtures, "impossiblebranch")) + "/..."
		out, passed := runGoVet(root, pattern)
		if passed {
			t.Fatalf("expected go vet to reject the impossiblebranch fixture, it passed:\n%s", out)
		}
		if !strings.Contains(out, "unreachable") {
			t.Errorf("expected go vet output to mention unreachable code, got:\n%s", out)
		}
	})

	t.Run("unsafe conversion is rejected by go vet", func(t *testing.T) {
		pattern := "./" + filepath.ToSlash(filepath.Join(fixtures, "unsafeconversion")) + "/..."
		out, passed := runGoVet(root, pattern)
		if passed {
			t.Fatalf("expected go vet to reject the unsafeconversion fixture, it passed:\n%s", out)
		}
		if !strings.Contains(out, "unsafe.Pointer") {
			t.Errorf("expected go vet output to mention unsafe.Pointer misuse, got:\n%s", out)
		}
	})

	t.Run("a clean fixture passes gofmt, vet and staticcheck", func(t *testing.T) {
		cleanDir := filepath.Join(root, fixtures, "clean")

		violations, err := gofmtViolations(cleanDir)
		if err != nil {
			t.Fatalf("gofmtViolations: %v", err)
		}
		if len(violations) != 0 {
			t.Errorf("expected the clean fixture to have no formatting drift, got: %v", violations)
		}

		pattern := "./" + filepath.ToSlash(filepath.Join(fixtures, "clean")) + "/..."
		if out, passed := runGoVet(root, pattern); !passed {
			t.Errorf("expected go vet to accept the clean fixture, got:\n%s", out)
		}
		if out, passed := runStaticcheck(root, pattern); !passed {
			t.Errorf("expected staticcheck to accept the clean fixture, got:\n%s", out)
		}
	})
}
