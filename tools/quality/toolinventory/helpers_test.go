package toolinventory_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/toolinventory"
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

// validEntry returns a fully-populated Entry. Tests mutate a copy of it
// to exercise one missing field at a time.
func validEntry() toolinventory.Entry {
	return toolinventory.Entry{
		Name:            "example-tool",
		Category:        "example_category",
		Version:         "v1.2.3",
		Digest:          "sha256:deadbeef",
		Source:          "https://example.test/example-tool",
		License:         "MIT",
		CVEStatus:       "PENDING_MANUAL_REVIEW",
		Owner:           "platform-toolchain",
		UpdateSLA:       "reviewed on every bump",
		ReplacementPath: "none recorded",
		DerivedFrom:     "example.lock",
	}
}

// blankField zeroes exactly the named required field on e, by its YAML
// key, so callers can drive Entry.MissingFields() one field at a time.
func blankField(e *toolinventory.Entry, field string) {
	switch field {
	case "version":
		e.Version = ""
	case "source":
		e.Source = ""
	case "license":
		e.License = ""
	case "cve_status":
		e.CVEStatus = ""
	case "owner":
		e.Owner = ""
	case "update_sla":
		e.UpdateSLA = ""
	case "replacement_path":
		e.ReplacementPath = ""
	}
}
