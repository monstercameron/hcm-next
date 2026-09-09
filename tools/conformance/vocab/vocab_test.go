package vocab

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/conformance/internal/reporoot"
)

func runtimeSpecPath(t *testing.T) string {
	t.Helper()
	root, err := reporoot.Find()
	if err != nil {
		t.Fatalf("reporoot.Find: %v", err)
	}
	return filepath.Join(root, "planning", "specs", "workflow-runtime.md")
}

// TestLoadFromAuthoritativeSpec proves the table parser correctly recovers
// the ten core primitives, three structural primitives, and four retired
// names from the actual planning/specs/workflow-runtime.md at HEAD. If this
// test starts failing because the spec's vocabulary changed shape, that is
// a real signal: CONF-001's checks must be re-derived, not patched around.
func TestLoadFromAuthoritativeSpec(t *testing.T) {
	v, err := Load(runtimeSpecPath(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got, want := len(v.Core), 10; got != want {
		t.Errorf("len(Core) = %d, want %d (names: %v)", got, want, v.CoreNames())
	}
	if got, want := len(v.Structural), 3; got != want {
		t.Errorf("len(Structural) = %d, want %d (names: %v)", got, want, v.StructuralNames())
	}
	if got, want := len(v.Retired), 4; got != want {
		t.Errorf("len(Retired) = %d, want %d", got, want)
	}

	wantCore := []string{"APPROVAL", "CAPABILITY", "COMPENSATE", "DECISION", "END", "OBSERVE", "SIGNAL", "TASK", "TRANSFORM", "WAIT"}
	assertStringSlice(t, "CoreNames", v.CoreNames(), wantCore)

	wantStructural := []string{"JOIN", "PARALLEL", "SUBWORKFLOW"}
	assertStringSlice(t, "StructuralNames", v.StructuralNames(), wantStructural)

	wantRetired := []string{"AGENT", "CHECKPOINT", "DOCUMENT", "RULE"}
	assertStringSlice(t, "RetiredNames", v.RetiredNames(), wantRetired)

	if !v.IsCore("WAIT") {
		t.Error("IsCore(WAIT) = false, want true")
	}
	if v.IsCore("PARALLEL") {
		t.Error("IsCore(PARALLEL) = true, want false (it is structural)")
	}
	if !v.IsStructural("SUBWORKFLOW") {
		t.Error("IsStructural(SUBWORKFLOW) = false, want true")
	}
	if got := v.ExpressedAs("CHECKPOINT"); got == "" {
		t.Error("ExpressedAs(CHECKPOINT) = \"\", want the safe_point mapping text")
	}
	if got := v.ExpressedAs("WAIT"); got != "" {
		t.Errorf("ExpressedAs(WAIT) = %q, want \"\" (WAIT is not retired)", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist.md")); err == nil {
		t.Fatal("Load(missing file): got nil error, want non-nil")
	}
}

func TestLoadMissingTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.md")
	writeFile(t, path, "# Spec\n\nNo tables here.\n")
	if _, err := Load(path); err == nil {
		t.Fatal("Load(no tables): got nil error, want non-nil")
	}
}

func assertStringSlice(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", label, got, want)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s = %v, want %v", label, got, want)
			return
		}
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
