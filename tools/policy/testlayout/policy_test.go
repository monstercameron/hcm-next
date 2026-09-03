package testlayout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
)

func fixtureRoot(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func rendered(findings []Violation) string {
	var lines []string
	for _, finding := range findings {
		lines = append(lines, finding.String())
	}
	return strings.Join(lines, "\n") + "\n"
}

// TestTestLayoutPolicy is ARCH-GO-016's primary test. It proves that package
// tests stay beside production code, while a documented cross-system harness
// is allowed beneath the top-level test tree.
func TestTestLayoutPolicy(t *testing.T) {
	findings, err := Check(fixtureRoot(t, "clean"))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("clean fixture findings = %v, want none", findings)
	}

	findings, err = Check(fixtureRoot(t, "faults"))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"detached_test", "production_test_dependency", "invalid_fixture_metadata"} {
		if !hasKind(findings, kind) {
			t.Errorf("fault fixture findings = %v, want kind %q", findings, kind)
		}
	}
}

// TestTodo_ARCH_GO_016_Golden pins stable, path-relative diagnostics for the
// fixture corpus. This makes a policy change reviewable without OS path noise.
func TestTodo_ARCH_GO_016_Golden(t *testing.T) {
	findings, err := Check(fixtureRoot(t, "faults"))
	if err != nil {
		t.Fatal(err)
	}
	wantBytes, err := os.ReadFile(filepath.Join("testdata", "golden", "faults.golden.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rendered(findings), string(wantBytes); got != want {
		t.Fatalf("diagnostics mismatch\n--- got\n%s--- want\n%s", got, want)
	}
}

// TestTodo_ARCH_GO_016_Fault checks that malformed ownership metadata is a
// finding and cannot suppress unrelated detached-test or dependency failures.
func TestTodo_ARCH_GO_016_Fault(t *testing.T) {
	findings, err := Check(fixtureRoot(t, "faults"))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 3 {
		t.Fatalf("fault fixture returned %d findings, want 3: %v", len(findings), findings)
	}
}

// TestTodo_ARCH_GO_016_Conformance applies the policy to this checkout. The
// scan is filesystem-only and deterministic: it neither starts services nor
// changes repository state.
func TestTodo_ARCH_GO_016_Conformance(t *testing.T) {
	findings, err := Check(repopath.RootDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		t.Errorf("ARCH-GO-016 violation: %s", finding)
	}
}

func hasKind(findings []Violation, want string) bool {
	for _, finding := range findings {
		if finding.Kind == want {
			return true
		}
	}
	return false
}
