// Package layout implements the ARCH-GO-001 repository-layout policy:
// classifying a Go import path against the manifest at
// definitions/architecture/repository-layout.yaml.
package layout

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest is the parsed form of repository-layout.yaml.
type Manifest struct {
	Version int    `yaml:"version"`
	Module  string `yaml:"module"`

	AllowedRoots []struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	} `yaml:"allowed_roots"`

	InternalPackageRoots []struct {
		Name  string `yaml:"name"`
		Owner string `yaml:"owner"`
		Layer string `yaml:"layer"`
		Phase string `yaml:"phase"`
	} `yaml:"internal_package_roots"`

	ApprovedCommands struct {
		Initial []string `yaml:"initial"`
		Later   []string `yaml:"later"`
	} `yaml:"approved_commands"`

	LegacyModuleExemptions []struct {
		Path   string `yaml:"path"`
		Module string `yaml:"module"`
		Reason string `yaml:"reason"`
		Owner  string `yaml:"owner"`
		Expiry string `yaml:"expiry"`
	} `yaml:"legacy_module_exemptions"`

	Waivers []struct {
		Path       string `yaml:"path"`
		PathPrefix string `yaml:"path_prefix"`
		Kind       string `yaml:"kind"`
		Observed   string `yaml:"observed"`
		Owner      string `yaml:"owner"`
		Rationale  string `yaml:"rationale"`
		Expiry     string `yaml:"expiry"`
	} `yaml:"waivers"`
}

// Verdict is the result of classifying one import path against the manifest.
type Verdict struct {
	Allowed bool
	// Waived is true when Allowed is true only because a waiver in the
	// manifest covers this path. Callers should surface waived paths
	// (e.g. via t.Logf) rather than treating them as clean.
	Waived bool
	Reason string
}

// Load reads and parses the manifest at path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("layout: reading manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("layout: parsing manifest: %w", err)
	}
	if m.Module == "" {
		return nil, fmt.Errorf("layout: manifest has no module")
	}
	return &m, nil
}

func (m *Manifest) hasAllowedRoot(name string) bool {
	for _, r := range m.AllowedRoots {
		if r.Name == name {
			return true
		}
	}
	return false
}

func (m *Manifest) hasInternalRoot(name string) bool {
	for _, r := range m.InternalPackageRoots {
		if r.Name == name {
			return true
		}
	}
	return false
}

func (m *Manifest) isApprovedCommand(name string) bool {
	for _, c := range m.ApprovedCommands.Initial {
		if c == name {
			return true
		}
	}
	return false
}

// matchWaiver returns the reason string of the first waiver matching rel
// (a module-relative, slash-separated path), or "" if none matches.
func (m *Manifest) matchWaiver(rel string) (string, bool) {
	for _, w := range m.Waivers {
		if w.Path != "" && w.Path == rel {
			return fmt.Sprintf("waived (%s): %s", w.Kind, w.Rationale), true
		}
		if w.PathPrefix != "" && strings.HasPrefix(rel, w.PathPrefix) {
			return fmt.Sprintf("waived (%s): %s", w.Kind, w.Rationale), true
		}
	}
	return "", false
}

// ClassifyImportPath decides whether importPath is allowed under the
// repository-layout policy. importPath is a full Go import path; it need
// not currently exist on disk.
func (m *Manifest) ClassifyImportPath(importPath string) Verdict {
	if importPath == m.Module {
		return Verdict{Allowed: true, Reason: "module root package"}
	}

	prefix := m.Module + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return Verdict{Allowed: false, Reason: fmt.Sprintf("import path %q is not part of module %q", importPath, m.Module)}
	}
	rel := strings.TrimPrefix(importPath, prefix)

	if reason, ok := m.matchWaiver(rel); ok {
		return Verdict{Allowed: true, Waived: true, Reason: reason}
	}

	segments := strings.Split(rel, "/")
	root := segments[0]

	if !m.hasAllowedRoot(root) {
		return Verdict{Allowed: false, Reason: fmt.Sprintf("root %q is not one of the allowed_roots in the repository-layout manifest", root)}
	}

	switch root {
	case "cmd":
		if len(segments) < 2 || segments[1] == "" {
			return Verdict{Allowed: false, Reason: "cmd/ requires a named command directory"}
		}
		cmdName := segments[1]
		if !m.isApprovedCommand(cmdName) {
			return Verdict{Allowed: false, Reason: fmt.Sprintf("command %q is not in approved_commands.initial", cmdName)}
		}
	case "internal":
		if len(segments) < 2 || segments[1] == "" {
			return Verdict{Allowed: false, Reason: "internal/ requires a declared package root"}
		}
		pkgRoot := segments[1]
		if !m.hasInternalRoot(pkgRoot) {
			return Verdict{Allowed: false, Reason: fmt.Sprintf("internal package root %q is not declared in internal_package_roots", pkgRoot)}
		}
	}

	return Verdict{Allowed: true}
}
