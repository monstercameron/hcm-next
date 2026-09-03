// Package phaseonepackages enforces ARCH-GO-018's physical Phase 1 package
// ceiling. The repository-layout manifest is the source of truth for roots;
// this check only observes manifests and the tree and never mutates either.
package phaseonepackages

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const manifestPath = "definitions/architecture/repository-layout.yaml"

type Finding struct{ Code, Path, Detail string }

func (f Finding) String() string { return fmt.Sprintf("%s: %s (%s)", f.Path, f.Detail, f.Code) }

type Manifest struct {
	InternalPackageRoots []struct {
		Name  string `yaml:"name"`
		Phase string `yaml:"phase"`
	} `yaml:"internal_package_roots"`
	ApprovedCommands struct {
		Initial []string `yaml:"initial"`
	} `yaml:"approved_commands"`
}

// LoadManifest reads the canonical repository layout manifest.
func LoadManifest(root string) (Manifest, error) {
	b, err := os.ReadFile(filepath.Join(root, manifestPath))
	if err != nil {
		return Manifest{}, fmt.Errorf("read repository layout: %w", err)
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse repository layout: %w", err)
	}
	return m, nil
}

// Check scans physical Go package directories and reports packages outside
// the exact P1A subset. P1A includes only roots marked P1A and initial command
// directories; P1B/deferred roots, later commands, generated output and the
// legacy module are intentionally outside this product ceiling.
func Check(root string) []Finding {
	m, err := LoadManifest(root)
	if err != nil {
		return []Finding{{Code: "manifest-error", Path: manifestPath, Detail: err.Error()}}
	}
	return CheckWithManifest(root, m)
}

func CheckWithManifest(root string, m Manifest) []Finding {
	allowed := map[string]bool{}
	for _, r := range m.InternalPackageRoots {
		if strings.EqualFold(strings.TrimSpace(r.Phase), "P1A") {
			allowed[r.Name] = true
		}
	}
	commands := map[string]bool{}
	for _, c := range m.ApprovedCommands.Initial {
		commands[c] = true
	}
	var out []Finding
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if info.IsDir() {
			if rel == "." {
				return nil
			}
			for _, p := range []string{".git", "node_modules", "gen", "src/blocks/go", "planning", "definitions", "migrations"} {
				if rel == p || strings.HasPrefix(rel, p+"/") {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.HasPrefix(rel, "internal/") {
			parts := strings.Split(rel, "/")
			if len(parts) >= 2 && !allowed[parts[1]] {
				out = append(out, Finding{"deferred-root", rel, "internal root is not marked P1A in repository-layout.yaml"})
			}
		} else if strings.HasPrefix(rel, "cmd/") {
			parts := strings.Split(rel, "/")
			if len(parts) >= 2 && !commands[parts[1]] {
				out = append(out, Finding{"non-phase1-command", rel, "command is not in approved_commands.initial"})
			}
		}
		return nil
	})
	if err != nil {
		out = append(out, Finding{"walk-error", ".", err.Error()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Code < out[j].Code
	})
	return out
}
