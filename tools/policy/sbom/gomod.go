package sbom

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/mod/modfile"
)

// Require is one module in root's go.mod require block, after Go's module
// graph pruning has already resolved it to the single version this module
// actually builds against (MVS-selected, exactly what `go build ./...` and
// `go list -m -json all` would report for it — see doc.go for why this
// package reads go.mod directly instead of shelling out to `go list`).
type Require struct {
	Path     string
	Version  string
	Indirect bool
}

// ModulePath returns root's own module path (the "module" directive).
func ModulePath(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("sbom: reading go.mod: %w", err)
	}
	path := modfile.ModulePath(data)
	if path == "" {
		return "", fmt.Errorf("sbom: go.mod has no module directive")
	}
	return path, nil
}

// ParseRequires reads root/go.mod and returns every module in its require
// block, sorted by path for deterministic output. This mirrors
// tools/policy/depmanifest.ParseGoModRequires's own rationale: go.mod's
// require list (after Go 1.17+ module graph pruning) already holds exactly
// the modules this module's own build depends on, at the version actually
// selected — the same information `go list -m -json all` would report for
// them, without that command's tendency to walk the *entire* transitive
// module graph (including modules no package here ever imports, and whose
// own go.mod files this repository does not control).
func ParseRequires(root string) ([]Require, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("sbom: reading go.mod: %w", err)
	}
	f, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		return nil, fmt.Errorf("sbom: parsing go.mod: %w", err)
	}

	requires := make([]Require, 0, len(f.Require))
	for _, r := range f.Require {
		requires = append(requires, Require{
			Path:     r.Mod.Path,
			Version:  r.Mod.Version,
			Indirect: r.Indirect,
		})
	}
	sort.Slice(requires, func(i, j int) bool { return requires[i].Path < requires[j].Path })
	return requires, nil
}
