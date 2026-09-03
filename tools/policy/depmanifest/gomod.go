package depmanifest

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

// ParseGoModRequires reads root/go.mod and returns every module in its
// require block (LIB-001's "direct and the notable indirect ones").
//
// This deliberately reads go.mod rather than `go list -m all`: Go's module
// graph pruning (Go 1.17+) already keeps go.mod's own require list to the
// modules actually needed to build packages this module imports, while `go
// list -m all` additionally enumerates every module reachable through a
// dependency's own go.mod (including modules no package here ever builds
// against). See the "Scope note" at the top of dependency-roles.yaml.
func ParseGoModRequires(root string) ([]GoModRequire, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("depmanifest: reading go.mod: %w", err)
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("depmanifest: parsing go.mod: %w", err)
	}

	requires := make([]GoModRequire, 0, len(f.Require))
	for _, r := range f.Require {
		requires = append(requires, GoModRequire{
			Path:     r.Mod.Path,
			Version:  r.Mod.Version,
			Indirect: r.Indirect,
		})
	}
	return requires, nil
}
