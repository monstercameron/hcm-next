package phaseonepackages_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/tools/quality/phaseonepackages"
)

func TestPhaseOnePackageAllowlist(t *testing.T) {
	root := t.TempDir()
	m := phaseonepackages.Manifest{}
	m.InternalPackageRoots = append(m.InternalPackageRoots,
		struct {
			Name  string `yaml:"name"`
			Phase string `yaml:"phase"`
		}{"kernel", "P1A"},
		struct {
			Name  string `yaml:"name"`
			Phase string `yaml:"phase"`
		}{"workflow", "P1B"})
	m.ApprovedCommands.Initial = []string{"hcmnext"}
	for _, p := range []string{"internal/kernel/ok.go", "internal/workflow/deferred.go", "cmd/hcmnext/main.go", "cmd/scheduler/main.go"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte("package fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := phaseonepackages.CheckWithManifest(root, m)
	if len(got) != 2 {
		t.Fatalf("findings = %#v, want deferred root and later command", got)
	}
}

func TestPhaseOnePackageManifestErrors(t *testing.T) {
	if got := phaseonepackages.Check(t.TempDir()); len(got) != 1 || got[0].Code != "manifest-error" {
		t.Fatalf("got %#v", got)
	}
}
