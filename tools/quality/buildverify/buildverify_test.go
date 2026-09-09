package buildverify_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/buildverify"
)

// repoRoot walks up from this test file's own location to find go.mod.
// tools/quality/quality_test.go documents why every package under
// tools/quality repeats this small search rather than sharing a helper
// package: it keeps each check runnable in isolation.
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

// fixtureSource returns the TOOL-016 build fixture: a small, dependency-
// free main package used instead of a real `cmd/*` binary so this suite
// never depends on the state of packages other agents may be mid-editing
// (see tools/quality/testdata/buildfixture/main.go's own doc comment for
// why the fixture proves the same thing a real command would).
func fixtureSource(t *testing.T) []byte {
	t.Helper()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "tools", "quality", "testdata", "buildfixture", "main.go"))
	if err != nil {
		t.Fatalf("reading buildfixture source: %v", err)
	}
	return data
}

// TestTodo_TOOL_016 is the TOOL-016 primary test. It proves both halves of
// the todo directly against the real `go build` toolchain (no mocked
// comparison): building the fixture from two independently created
// temporary directories without -trimpath produces different digests
// (RED - the nondeterminism -trimpath exists to remove), and building it
// the same way under the pinned flag set (-trimpath, -buildvcs=false,
// CGO_ENABLED=0) produces byte-identical binaries and identical embedded
// build-settings output (GREEN).
func TestTodo_TOOL_016(t *testing.T) {
	source := fixtureSource(t)

	t.Run("RED_isolated_builds_without_trimpath_are_not_reproducible", func(t *testing.T) {
		opts := buildverify.Options{Trimpath: false, BuildVCS: false, CGOEnabled: false}
		cmp, err := buildverify.CompareIsolated(source, "main.go", opts)
		if err != nil {
			t.Fatalf("CompareIsolated: %v", err)
		}
		if cmp.DigestsMatch {
			t.Fatal("expected two isolated builds without -trimpath to embed differing absolute source directories and produce different digests, got identical digests")
		}
	})

	t.Run("GREEN_pinned_flags_produce_byte_identical_isolated_builds", func(t *testing.T) {
		cmp, err := buildverify.CompareIsolated(source, "main.go", buildverify.DefaultOptions())
		if err != nil {
			t.Fatalf("CompareIsolated: %v", err)
		}
		if !cmp.DigestsMatch {
			t.Fatalf("digests differ across isolated directories under the pinned flag set:\n  first:  %s\n  second: %s", cmp.First.Digest, cmp.Second.Digest)
		}
		if !cmp.VersionInfoMatch {
			t.Fatalf("go version -m output differs across isolated directories:\n--- first ---\n%s\n--- second ---\n%s", cmp.First.VersionInfo, cmp.Second.VersionInfo)
		}
		if !cmp.Reproducible() {
			t.Fatal("Reproducible() must be true when both digest and version info match")
		}
	})

	t.Run("GREEN_version_info_embeds_the_pinned_flags", func(t *testing.T) {
		cmp, err := buildverify.CompareIsolated(source, "main.go", buildverify.DefaultOptions())
		if err != nil {
			t.Fatalf("CompareIsolated: %v", err)
		}
		for _, want := range []string{"-trimpath=true", "CGO_ENABLED=0"} {
			if !strings.Contains(cmp.First.VersionInfo, want) {
				t.Errorf("go version -m output missing %q:\n%s", want, cmp.First.VersionInfo)
			}
		}
		if strings.Contains(cmp.First.VersionInfo, "vcs.revision") {
			t.Errorf("go version -m output embeds vcs.revision despite -buildvcs=false:\n%s", cmp.First.VersionInfo)
		}
	})
}

// TestTodo_TOOL_016_Golden pins the exact shape (not the machine-specific
// values) of the `go version -m` build settings a default-options build
// must carry, and proves ParseBuildSettings is a pure, deterministic
// function of that text.
func TestTodo_TOOL_016_Golden(t *testing.T) {
	source := fixtureSource(t)

	res, dir, err := buildverify.BuildIsolated(source, "main.go", buildverify.DefaultOptions())
	if err != nil {
		t.Fatalf("BuildIsolated: %v", err)
	}
	defer os.RemoveAll(dir)

	path, settings := buildverify.ParseBuildSettings(res.VersionInfo)
	if path != "command-line-arguments" {
		t.Fatalf("path = %q, want %q (the fixture is built as a bare file, not a package pattern)", path, "command-line-arguments")
	}

	got := make(map[string]string, len(settings))
	for _, s := range settings {
		if _, dup := got[s.Key]; dup {
			t.Fatalf("build setting %q reported more than once: %v", s.Key, settings)
		}
		got[s.Key] = s.Value
	}

	for _, key := range []string{"-buildmode", "-compiler", "-trimpath", "CGO_ENABLED", "GOOS", "GOARCH"} {
		if _, ok := got[key]; !ok {
			t.Errorf("go version -m build settings missing %q, got %v", key, got)
		}
	}
	if got["-trimpath"] != "true" {
		t.Errorf("-trimpath = %q, want %q", got["-trimpath"], "true")
	}
	if got["CGO_ENABLED"] != "0" {
		t.Errorf("CGO_ENABLED = %q, want %q", got["CGO_ENABLED"], "0")
	}
	if _, ok := got["vcs.revision"]; ok {
		t.Error("vcs.revision present despite -buildvcs=false")
	}

	// The parse is a pure function of the text: parsing the same
	// VersionInfo twice must yield an identical ordered settings slice.
	_, again := buildverify.ParseBuildSettings(res.VersionInfo)
	if !reflect.DeepEqual(settings, again) {
		t.Fatalf("ParseBuildSettings is not deterministic:\nfirst: %v\nsecond: %v", settings, again)
	}
}
