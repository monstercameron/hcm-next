// Package libfirewall implements the LIB-002 third-party semantic firewall
// and the module-specific qualification checks that depend on it
// (LIB-003/004/006/007/008): only manifest-approved implementation roots
// (definitions/architecture/dependency-roles.yaml's allowed_import_roots)
// may import a given third-party module, and the packages that do own that
// module's mechanics must not leak its concrete types back out through
// their own exported API.
//
// See firewall.go (import-edge boundary) and leakscan.go (exported-type
// exposure scan).
package libfirewall

import (
	"strings"

	"github.com/monstercameron/hcm-next/tools/policy/depmanifest"
)

// Violation names one forbidden import: importer (a module-relative HCM
// Next package path) directly imports importedPath (a full Go import path,
// classified by the dependency-roles manifest as Module/Role), and
// importer's root is not listed in AllowedRoots.
type Violation struct {
	Importer     string
	ImportedPath string
	Module       string
	Role         string
	AllowedRoots []string
}

func trimModule(module, importPath string) (string, bool) {
	if importPath == module {
		return "", true
	}
	prefix := module + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return "", false
	}
	return strings.TrimPrefix(importPath, prefix), true
}

func withinAllowedRoot(rel string, roots []string) bool {
	for _, root := range roots {
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

// classify resolves importedPath against manifest, matching not only exact
// module paths and family_rules prefixes (which depmanifest.Classify
// already does on whatever string it is given) but also a subpackage of an
// exact module row, e.g. "github.com/jackc/pgx/v5/pgxpool" under the
// "github.com/jackc/pgx/v5" row. depmanifest.Classify alone only matches a
// module row by exact string equality, which is correct for its own LIB-001
// go.mod-require-list job but too narrow for classifying arbitrary package
// import paths pulled from a real `go list` import graph.
func classify(manifest *depmanifest.Manifest, importedPath string) depmanifest.Classification {
	var best *depmanifest.ModuleRow
	for i, row := range manifest.Modules {
		if importedPath == row.Path || strings.HasPrefix(importedPath, row.Path+"/") {
			if best == nil || len(row.Path) > len(best.Path) {
				best = &manifest.Modules[i]
			}
		}
	}
	if best != nil {
		return depmanifest.Classification{Found: true, Row: *best, Exact: true}
	}
	return manifest.Classify(importedPath)
}

// CheckImport evaluates one direct import edge: a package at
// importerImportPath (full Go import path) importing importedPath (full Go
// import path, may be a subpackage of a classified module). It returns nil
// when: importer is not part of manifest's module (nothing for this
// firewall to say), importedPath is not classified by the manifest at all
// (LIB-001's job, not this firewall's), the row is PROJECT_CORE (no
// third-party boundary applies to HCM Next's own reserved core), or
// importer's root is inside the classification's allowed_import_roots.
// Otherwise it names the exact forbidden edge.
func CheckImport(manifest *depmanifest.Manifest, importerImportPath, importedPath string) *Violation {
	rel, ok := trimModule(manifest.Module, importerImportPath)
	if !ok {
		return nil
	}

	class := classify(manifest, importedPath)
	if !class.Found {
		return nil
	}
	if class.Row.Role == depmanifest.RoleProjectCore {
		return nil
	}
	if withinAllowedRoot(rel, class.Row.AllowedImportRoots) {
		return nil
	}

	return &Violation{
		Importer:     rel,
		ImportedPath: importedPath,
		Module:       class.Row.Path,
		Role:         class.Row.Role,
		AllowedRoots: class.Row.AllowedImportRoots,
	}
}

// CheckImportAgainstRoots evaluates one import edge against an explicit
// module-match list and allowed-roots list, independent of
// dependency-roles.yaml's own row for importedPath. It backs the
// module-specific qualification checks (LIB-003/007/008) whose todo defines
// a boundary that is narrower, wider, or simply not yet reflected at row
// granularity in dependency-roles.yaml (e.g. LIB-003's grant of
// internal/intent/protomap as a business-side Protobuf translation seam).
func CheckImportAgainstRoots(hcmModule, importerImportPath, importedPath string, matchModules, allowedRoots []string) *Violation {
	rel, ok := trimModule(hcmModule, importerImportPath)
	if !ok {
		return nil
	}
	if !matchesAnyModule(importedPath, matchModules) {
		return nil
	}
	if withinAllowedRoot(rel, allowedRoots) {
		return nil
	}
	return &Violation{Importer: rel, ImportedPath: importedPath, AllowedRoots: allowedRoots}
}

func matchesAnyModule(importPath string, modules []string) bool {
	for _, m := range modules {
		if importPath == m || strings.HasPrefix(importPath, m+"/") {
			return true
		}
	}
	return false
}

// CheckPackage runs CheckImport for every import of one repopath.Package
// against manifest, returning every violation found (an importer commonly
// has more than one forbidden import in a synthetic fixture, though in
// practice a real offending package usually has exactly one).
func CheckPackage(manifest *depmanifest.Manifest, importPath string, imports []string) []Violation {
	var out []Violation
	for _, imp := range imports {
		if v := CheckImport(manifest, importPath, imp); v != nil {
			out = append(out, *v)
		}
	}
	return out
}
