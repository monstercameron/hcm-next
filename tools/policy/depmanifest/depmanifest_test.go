package depmanifest_test

import (
	"path/filepath"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/depmanifest"
	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
)

func loadManifest(t *testing.T) *depmanifest.Manifest {
	t.Helper()
	root := repopath.RootDir()
	m, err := depmanifest.Load(filepath.Join(root, "definitions", "architecture", "dependency-roles.yaml"))
	if err != nil {
		t.Fatalf("loading dependency-roles manifest: %v", err)
	}
	return m
}

// TestDependencyRoleManifestRejectsUnclassifiedModule is the LIB-001
// primary test.
func TestDependencyRoleManifestRejectsUnclassifiedModule(t *testing.T) {
	m := loadManifest(t)

	t.Run("synthetic fixtures", func(t *testing.T) {
		cases := []struct {
			name         string
			modulePath   string
			wantFound    bool
			wantRole     string
			wantExactRow bool
		}{
			{"exact row: jackc/pgx", "github.com/jackc/pgx/v5", true, depmanifest.RoleInfrastructureMechanic, true},
			{"exact row: goose", "github.com/pressly/goose/v3", true, depmanifest.RoleInfrastructureMechanic, true},
			{"exact row: staticcheck is dev-only", "honnef.co/go/tools", true, depmanifest.RoleDevTestOnly, true},
			{"exact row: grpc is not PROJECT_CORE", "google.golang.org/grpc", true, depmanifest.RoleInfrastructureMechanic, true},
			{"family rule: unlisted golang.org/x module", "golang.org/x/crypto", true, depmanifest.RoleInfrastructureMechanic, false},
			{"family rule: unlisted google.golang.org module", "google.golang.org/appengine", true, depmanifest.RoleInfrastructureMechanic, false},
			{"unclassified module", "github.com/example/unclassified-thing", false, "", false},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				c := m.Classify(tc.modulePath)
				if c.Found != tc.wantFound {
					t.Fatalf("Classify(%q).Found = %v, want %v", tc.modulePath, c.Found, tc.wantFound)
				}
				if !tc.wantFound {
					return
				}
				if c.Row.Role != tc.wantRole {
					t.Errorf("Classify(%q).Row.Role = %q, want %q", tc.modulePath, c.Row.Role, tc.wantRole)
				}
				if c.Exact != tc.wantExactRow {
					t.Errorf("Classify(%q).Exact = %v, want %v", tc.modulePath, c.Exact, tc.wantExactRow)
				}
				if c.Row.Role == depmanifest.RoleProjectCore && !m.IsProjectCoreEligible(tc.modulePath) {
					t.Errorf("Classify(%q) returned PROJECT_CORE but %q is not in project_core_reserved", tc.modulePath, tc.modulePath)
				}
			})
		}
	})

	t.Run("no module row is PROJECT_CORE unless reserved", func(t *testing.T) {
		for _, row := range m.Modules {
			if row.Role == depmanifest.RoleProjectCore && !m.IsProjectCoreEligible(row.Path) {
				t.Errorf("module %s is classified PROJECT_CORE but is not one of project_core_reserved (Go, GWC, grpcbridge, SchemaFlux)", row.Path)
			}
		}
	})

	t.Run("every manifest row is complete", func(t *testing.T) {
		for _, row := range m.Modules {
			if missing := depmanifest.RowIsComplete(row); len(missing) > 0 {
				t.Errorf("module %s manifest row is missing fields: %v", row.Path, missing)
			}
		}
	})

	t.Run("every go.mod require is classified", func(t *testing.T) {
		root := repopath.RootDir()
		requires, err := depmanifest.ParseGoModRequires(root)
		if err != nil {
			t.Fatalf("parsing go.mod requires: %v", err)
		}
		if len(requires) == 0 {
			t.Fatalf("go.mod has no require block")
		}

		for _, req := range requires {
			c := m.Classify(req.Path)
			if !c.Found {
				t.Errorf("go.mod requires %s (%s) but dependency-roles.yaml classifies neither an exact row nor a matching family_rules prefix for it", req.Path, req.Version)
				continue
			}
			if c.Row.Role == depmanifest.RoleProjectCore && !m.IsProjectCoreEligible(req.Path) {
				t.Errorf("go.mod dependency %s is classified PROJECT_CORE but is not project_core_reserved", req.Path)
			}
			if c.Exact {
				if missing := depmanifest.RowIsComplete(c.Row); len(missing) > 0 {
					t.Errorf("go.mod dependency %s manifest row is missing fields: %v", req.Path, missing)
				}
			}
		}
	})
}
