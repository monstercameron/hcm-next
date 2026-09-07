package layout_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
	"github.com/monstercameron/hcm-next/tools/policy/layout"
)

func loadManifest(t *testing.T) (*layout.Manifest, string) {
	t.Helper()
	root := repopath.RootDir()
	manifestPath := filepath.Join(root, "definitions", "architecture", "repository-layout.yaml")
	m, err := layout.Load(manifestPath)
	if err != nil {
		t.Fatalf("loading repository-layout manifest: %v", err)
	}
	return m, root
}

// TestRepositoryLayoutRejectsUnownedOrMisplacedPackage is the ARCH-GO-001
// primary test. It has two halves: a table-driven check of the
// classification policy over synthetic import paths (the RED/GREEN cases
// the manifest must get right regardless of what exists on disk today), and
// a real-tree check that every package `go list ./...` currently reports is
// classified as allowed (explicitly including any path only allowed via a
// documented waiver, which is logged rather than hidden).
func TestRepositoryLayoutRejectsUnownedOrMisplacedPackage(t *testing.T) {
	m, root := loadManifest(t)

	t.Run("synthetic fixtures", func(t *testing.T) {
		mod := m.Module

		cases := []struct {
			name           string
			importPath     string
			wantAllowed    bool
			reasonContains string
		}{
			{"approved initial command", mod + "/cmd/hcmnext", true, ""},
			{"another approved initial command", mod + "/cmd/migrate", true, ""},
			{"declared internal root", mod + "/internal/kernel/values", true, ""},
			{"nested declared internal root", mod + "/internal/domains/people/aggregate", true, ""},
			{"tools root has no internal structure constraint", mod + "/tools/quality/fuzzkit", true, ""},
			{"gen root has no internal structure constraint", mod + "/gen/go/hcmnext/intents/v1", true, ""},
			{"definitions root is allowed", mod + "/definitions/architecture", true, ""},
			{"migrations root is allowed", mod + "/migrations/seed", true, ""},
			{"test root is allowed", mod + "/test/conformance/promotion", true, ""},
			{"module root package", mod, true, ""},

			{"production package outside allowed roots", mod + "/src/newthing", false, "not one of the allowed_roots"},
			{"another outside-root package", mod + "/pkg/helpers", false, "not one of the allowed_roots"},
			{"undeclared internal root", mod + "/internal/somethingnew/foo", false, "is not declared in internal_package_roots"},
			{"bare internal with no root", mod + "/internal", false, "requires a declared package root"},
			{"unapproved command: not in approved_commands.initial", mod + "/cmd/unapproved", false, "is not in approved_commands.initial"},
			{"unapproved command: admin not yet approved", mod + "/cmd/admin", false, "is not in approved_commands.initial"},
			{"unapproved command: arbitrary dev tool", mod + "/cmd/toolbox", false, "is not in approved_commands.initial"},
			{"bare cmd with no command name", mod + "/cmd", false, "requires a named command directory"},

			{"unapproved command after waiver expiry: gen-todos", mod + "/cmd/gen-todos", false, "is not in approved_commands.initial"},
			{"waived: stray npm package go source", mod + "/node_modules/flatted/golang/pkg/flatted", true, ""},

			{"foreign module is never classified", "example.com/other/pkg", false, "is not part of module"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				v := m.ClassifyImportPath(tc.importPath)
				if v.Allowed != tc.wantAllowed {
					t.Fatalf("ClassifyImportPath(%q) = Allowed:%v Reason:%q, want Allowed:%v", tc.importPath, v.Allowed, v.Reason, tc.wantAllowed)
				}
				if tc.reasonContains != "" && !strings.Contains(v.Reason, tc.reasonContains) {
					t.Fatalf("ClassifyImportPath(%q) reason = %q, want it to contain %q", tc.importPath, v.Reason, tc.reasonContains)
				}
			})
		}
	})

	t.Run("current tree via go list", func(t *testing.T) {
		packages, err := repopath.ListPackages(root)
		if err != nil {
			t.Fatalf("listing packages: %v", err)
		}
		if len(packages) == 0 {
			t.Fatalf("go list ./... returned no packages")
		}

		for _, pkg := range packages {
			v := m.ClassifyImportPath(pkg.ImportPath)
			if !v.Allowed {
				t.Errorf("package %s violates the repository-layout policy: %s", pkg.ImportPath, v.Reason)
				continue
			}
			if v.Waived {
				t.Logf("package %s is allowed only via a documented waiver: %s", pkg.ImportPath, v.Reason)
			}
		}
	})
}
