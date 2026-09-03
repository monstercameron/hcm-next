package crosscut

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var crossCuttingRoots = []string{
	"internal/trust",
	// Governance and safety overlays are cross-cutting even when their
	// implementation packages are introduced incrementally. Keep the policy
	// explicit so a newly-created overlay cannot accidentally bypass it.
	"internal/privacy",
	"internal/governance/privacy",
	"internal/dlp",
	"internal/secrets",
	"internal/admission",
	"internal/rollout",
	"internal/recovery",
	// Intelligence and agents are optional overlays in Phase 1. Keeping their
	// roots here means introducing either package cannot silently acquire a
	// synchronous domain/store dependency.
	"internal/intelligence",
	"internal/agent",
	"internal/platform/telemetry",
	"internal/platform/bootstrap",
	"internal/operations",
}

var forbiddenTargets = []string{
	"internal/domains",
	"internal/data/postgres",
	"internal/data/ledger",
	"internal/connectivity",
}

func isCrossCutting(importerRel string) bool {
	for _, r := range crossCuttingRoots {
		if importerRel == r || strings.HasPrefix(importerRel, r+"/") {
			return true
		}
	}
	return false
}

func isForbiddenTarget(importedRel string) bool {
	for _, f := range forbiddenTargets {
		if importedRel == f || strings.HasPrefix(importedRel, f+"/") {
			return true
		}
		if strings.Contains(f, "*") {
			parts := strings.Split(f, "/")
			impParts := strings.Split(importedRel, "/")
			if len(impParts) < len(parts) {
				continue
			}
			match := true
			for i, p := range parts {
				if p == "*" {
					continue
				}
				if p != impParts[i] {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}
	}
	return false
}

type Violation struct {
	File     string `json:",omitempty"`
	Importer string
	Imported string
	Rule     string
}

func CheckEdge(module, importer, imported string) *Violation {
	prefix := module + "/"
	if !strings.HasPrefix(importer, prefix) || !strings.HasPrefix(imported, prefix) {
		return nil
	}
	impRel := strings.TrimPrefix(importer, prefix)
	tgtRel := strings.TrimPrefix(imported, prefix)
	if !isCrossCutting(impRel) {
		return nil
	}
	if isForbiddenTarget(tgtRel) {
		return &Violation{Importer: importer, Imported: imported, Rule: "crosscut-must-not-import-domain"}
	}
	if tgtRel == "internal/intelligence" || strings.HasPrefix(tgtRel, "internal/intelligence/") {
		return &Violation{Importer: importer, Imported: imported, Rule: "crosscut-must-not-require-intelligence"}
	}
	if tgtRel == "internal/agent" || strings.HasPrefix(tgtRel, "internal/agent/") {
		return &Violation{Importer: importer, Imported: imported, Rule: "crosscut-must-not-require-agent"}
	}
	return nil
}

func CheckGraph(module string, edges [][2]string) []Violation {
	var out []Violation
	for _, e := range edges {
		if v := CheckEdge(module, e[0], e[1]); v != nil {
			out = append(out, *v)
		}
	}
	return out
}

// ScanDir parses production Go files below root and checks each direct import
// made by a cross-cutting package. Tests and testdata are deliberately
// excluded: they may use fixtures or adapters without changing the compiled
// dependency graph. Imports are parsed rather than loaded, so this checker
// remains useful while a package is being developed or is platform-specific.
func ScanDir(root, module string) ([]Violation, error) {
	var violations []Violation
	fset := token.NewFileSet()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "vendor" || info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		pkg := strings.TrimSuffix(filepath.ToSlash(rel), "/")
		if pkg == "." {
			pkg = ""
		}
		importer := module
		if pkg != "" {
			importer += "/" + pkg
		}
		for _, spec := range file.Imports {
			imported := strings.Trim(spec.Path.Value, `"`)
			if v := CheckEdge(module, importer, imported); v != nil {
				v.File = path
				violations = append(violations, *v)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		if violations[i].Imported != violations[j].Imported {
			return violations[i].Imported < violations[j].Imported
		}
		return violations[i].Rule < violations[j].Rule
	})
	return violations, nil
}

func AllowedPorts() []string {
	return []string{
		"internal/capability",
		"internal/ledger",
		"internal/data",
		"internal/platform/telemetry",
	}
}
