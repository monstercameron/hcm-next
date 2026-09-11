// Package librarystrategy generates the README library-strategy inventory.
//
// The prose around the inventory is intentionally authored in README.md. The
// volatile part (versions, roles, package roots, candidate decisions and
// prohibited framework families) is rendered from the repository's manifests
// so documentation cannot quietly drift from the architecture.
package librarystrategy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

const (
	// BeginMarker and EndMarker delimit the generated region in README.md.
	BeginMarker = "<!-- BEGIN GENERATED LIBRARY STRATEGY -->"
	EndMarker   = "<!-- END GENERATED LIBRARY STRATEGY -->"
)

type roleManifest struct {
	Module              string        `yaml:"module"`
	ProjectCoreReserved []projectCore `yaml:"project_core_reserved"`
	FamilyRules         []familyRule  `yaml:"family_rules"`
	Modules             []dependency  `yaml:"modules"`
}

type projectCore struct {
	Name string `yaml:"name"`
	Note string `yaml:"note"`
}

type dependency struct {
	Path          string   `yaml:"path"`
	Version       string   `yaml:"version"`
	Role          string   `yaml:"role"`
	SemanticOwner string   `yaml:"semantic_owner"`
	AllowedRoots  []string `yaml:"allowed_import_roots"`
}

type familyRule struct {
	Prefix        string `yaml:"prefix"`
	Role          string `yaml:"role"`
	SemanticOwner string `yaml:"semantic_owner"`
}

type layoutManifest struct {
	InternalRoots []packageRoot `yaml:"internal_package_roots"`
}

type packageRoot struct {
	Name        string `yaml:"name"`
	Owner       string `yaml:"owner"`
	Layer       string `yaml:"layer"`
	Phase       string `yaml:"phase"`
	Description string `yaml:"description"`
}

type prohibitedManifest struct {
	Rules []prohibitedRule `yaml:"rules"`
}

type prohibitedRule struct {
	ID             string   `yaml:"id"`
	Category       string   `yaml:"category"`
	Disposition    string   `yaml:"disposition"`
	ImportPrefixes []string `yaml:"import_prefixes"`
}

type rendererDecision struct {
	SelectedRenderer string `yaml:"selected_renderer"`
}

type generatorDecision struct {
	Decision string `yaml:"decision"`
	Fallback struct {
		Selected    bool   `yaml:"selected"`
		Kind        string `yaml:"kind"`
		Description string `yaml:"description"`
	} `yaml:"fallback"`
}

// Dependency is one module in go.mod joined to its dependency-role row.
type Dependency struct {
	Path          string
	Version       string
	Role          string
	Direct        bool
	SemanticOwner string
}

// PackageRoot is a declared internal package root from repository-layout.yaml.
type PackageRoot struct {
	Name        string
	Owner       string
	Layer       string
	Phase       string
	Description string
}

// Framework is a prohibited semantic framework family from
// prohibited-frameworks.yaml.
type Framework struct {
	ID          string
	Category    string
	Disposition string
	Prefixes    []string
}

// Candidate is the human-facing status of a preferred house library. Its
// status is derived from the role and qualification manifests, not inferred
// from whether an indirect go.mod row happens to exist.
type Candidate struct {
	Name     string
	Status   string
	Fallback string
}

// Manifest is the complete input-derived README inventory.
type Manifest struct {
	Module       string
	Dependencies []Dependency
	Packages     []PackageRoot
	Frameworks   []Framework
	Candidates   []Candidate
}

// Load reads architecture/dependency manifests and go.mod under root.
func Load(root string) (Manifest, error) {
	roles, err := loadYAML[roleManifest](filepath.Join(root, "definitions", "architecture", "dependency-roles.yaml"))
	if err != nil {
		return Manifest{}, err
	}
	layout, err := loadYAML[layoutManifest](filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
	if err != nil {
		return Manifest{}, err
	}
	prohibited, err := loadYAML[prohibitedManifest](filepath.Join(root, "definitions", "architecture", "prohibited-frameworks.yaml"))
	if err != nil {
		return Manifest{}, err
	}

	goMod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return Manifest{}, fmt.Errorf("librarystrategy: read go.mod: %w", err)
	}
	f, err := modfile.Parse("go.mod", goMod, nil)
	if err != nil {
		return Manifest{}, fmt.Errorf("librarystrategy: parse go.mod: %w", err)
	}
	versions := make(map[string]struct {
		version string
		direct  bool
	}, len(f.Require))
	for _, req := range f.Require {
		versions[req.Mod.Path] = struct {
			version string
			direct  bool
		}{req.Mod.Version, !req.Indirect}
	}

	deps := make([]Dependency, 0, len(versions))
	for _, row := range roles.Modules {
		if row.Role == "PROJECT_CORE" {
			return Manifest{}, fmt.Errorf("librarystrategy: dependency-role row %q mislabels a third-party module as PROJECT_CORE", row.Path)
		}
		v, ok := versions[row.Path]
		if !ok {
			continue
		}
		deps = append(deps, Dependency{Path: row.Path, Version: v.version, Role: row.Role, Direct: v.direct, SemanticOwner: row.SemanticOwner})
	}
	for path := range versions {
		if _, ok := findDependency(deps, path); !ok {
			matched := false
			for _, family := range roles.FamilyRules {
				if strings.HasPrefix(path, family.Prefix) {
					v := versions[path]
					deps = append(deps, Dependency{Path: path, Version: v.version, Role: family.Role, Direct: v.direct, SemanticOwner: family.SemanticOwner})
					matched = true
					break
				}
			}
			if !matched {
				return Manifest{}, fmt.Errorf("librarystrategy: go.mod module %q has no dependency-role row or family rule", path)
			}
		}
	}
	sort.Slice(deps, func(i, j int) bool { return deps[i].Path < deps[j].Path })

	packages := make([]PackageRoot, 0, len(layout.InternalRoots))
	for _, p := range layout.InternalRoots {
		packages = append(packages, PackageRoot(p))
	}
	frameworks := make([]Framework, 0, len(prohibited.Rules))
	for _, r := range prohibited.Rules {
		frameworks = append(frameworks, Framework{r.ID, r.Category, r.Disposition, append([]string(nil), r.ImportPrefixes...)})
	}

	// These three candidates are constitutionally reserved even when their
	// module rows are indirect or absent. The selected fallbacks are kept
	// explicit so a README reader cannot mistake a preference for admission.
	gwc := "PREFERRED; qualified renderer (UX-QUAL-001)"
	if d, e := loadYAML[rendererDecision](filepath.Join(root, "definitions", "ux", "workspace-renderer-decision.yaml")); e == nil && strings.EqualFold(d.SelectedRenderer, "gwc") {
		gwc = "QUALIFIED; selected renderer (UX-QUAL-001)"
	}
	genStatus := "PREFERRED; qualification pending"
	genFallback := "protoc-style deterministic Go compiler"
	if d, e := loadYAML[generatorDecision](filepath.Join(root, "definitions", "generation", "schemaflux-qualification.yaml")); e == nil {
		if d.Decision != "" {
			genStatus = "DISQUALIFIED; deterministic fallback selected"
		}
		if d.Fallback.Kind != "" {
			genFallback = strings.ReplaceAll(d.Fallback.Kind, "_", " ")
		}
	}
	connectStatus := "PREFERRED; not admitted; current edge uses connect-go"
	connectFallback := "connect-go (connectrpc.com/connect)"
	if d, ok := findDependency(deps, "connectrpc.com/connect"); ok {
		connectStatus = fmt.Sprintf("PREFERRED; not admitted; current edge uses connect-go %s", d.Version)
		connectFallback = fmt.Sprintf("connect-go (%s)", d.Version)
	}
	return Manifest{
		Module: roles.Module, Dependencies: deps, Packages: packages, Frameworks: frameworks,
		Candidates: []Candidate{
			{Name: "Go", Status: "PROJECT CORE; product language/toolchain", Fallback: "Go standard library"},
			{Name: "GWC / GoWebComponents", Status: gwc, Fallback: "Go server-rendered HTML with progressive enhancement"},
			{Name: "grpcbridge", Status: connectStatus, Fallback: connectFallback},
			{Name: "SchemaFlux", Status: genStatus, Fallback: genFallback},
		},
	}, nil
}

func findDependency(deps []Dependency, path string) (Dependency, bool) {
	for _, d := range deps {
		if d.Path == path {
			return d, true
		}
	}
	return Dependency{}, false
}

func loadYAML[T any](path string) (T, error) {
	var out T
	b, err := os.ReadFile(path)
	if err != nil {
		return out, fmt.Errorf("librarystrategy: read %s: %w", path, err)
	}
	// The generator intentionally projects only the fields it documents. The
	// source manifests carry additional policy fields owned by other checkers;
	// rejecting those here would make this README generator an accidental
	// second schema owner.
	if err := yaml.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("librarystrategy: parse %s: %w", path, err)
	}
	return out, nil
}

// Render produces only the generated region, including its markers.
func Render(m Manifest) string {
	var b strings.Builder
	b.WriteString(BeginMarker + "\n")
	b.WriteString("<!-- This region is generated by tools/gen/librarystrategy. DO NOT EDIT. -->\n\n")
	b.WriteString("### Preferred house libraries\n\n")
	b.WriteString("| Candidate | Current status | Named fallback |\n| --- | --- | --- |\n")
	for _, c := range m.Candidates {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", c.Name, c.Status, c.Fallback)
	}
	b.WriteString("\n### Package and dependency shape\n\n")
	b.WriteString("```text\nGo product core (" + m.Module + ")\n")
	b.WriteString("├── package roots\n")
	for i, p := range m.Packages {
		branch := "│   ├──"
		if i == len(m.Packages)-1 {
			branch = "│   └──"
		}
		fmt.Fprintf(&b, "%s internal/%s [%s; %s; owner=%s]\n", branch, p.Name, p.Layer, p.Phase, p.Owner)
	}
	b.WriteString("└── replaceable mechanics (versions and roles below)\n```\n\n")
	b.WriteString("| Module | Version | Role | Direct | Semantic owner |\n| --- | --- | --- | --- | --- |\n")
	for _, d := range m.Dependencies {
		direct := "indirect"
		if d.Direct {
			direct = "direct"
		}
		fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | %s | %s |\n", d.Path, d.Version, d.Role, direct, d.SemanticOwner)
	}
	b.WriteString("\n### Prohibited semantic frameworks\n\n")
	b.WriteString("These families are generated from `definitions/architecture/prohibited-frameworks.yaml`; infrastructure mechanics may not become semantic authority.\n\n")
	b.WriteString("| Rule | Category | Disposition | Import prefixes |\n| --- | --- | --- | --- |\n")
	for _, r := range m.Frameworks {
		fmt.Fprintf(&b, "| `%s` | %s | %s | `%s` |\n", r.ID, r.Category, r.Disposition, strings.Join(r.Prefixes, "`, `"))
	}
	b.WriteString("\n" + EndMarker + "\n")
	return b.String()
}

// Replace substitutes the generated region in readme. It refuses missing or
// duplicate markers so a typo cannot silently append a second inventory.
func Replace(readme, generated []byte) ([]byte, error) {
	s := string(readme)
	if strings.Count(s, BeginMarker) != 1 || strings.Count(s, EndMarker) != 1 {
		return nil, fmt.Errorf("librarystrategy: README must contain exactly one generated marker pair")
	}
	start := strings.Index(s, BeginMarker)
	end := strings.Index(s, EndMarker)
	if end < start {
		return nil, fmt.Errorf("librarystrategy: generated end marker precedes begin marker")
	}
	end += len(EndMarker)
	return []byte(s[:start] + strings.TrimRight(string(generated), "\r\n") + s[end:]), nil
}

// UpdateREADME regenerates README.md in place. All manifest reads and output
// are deterministic; it is the command-facing write boundary.
func UpdateREADME(root string) error {
	m, err := Load(root)
	if err != nil {
		return err
	}
	path := filepath.Join(root, "README.md")
	old, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("librarystrategy: read README.md: %w", err)
	}
	updated, err := Replace(old, []byte(Render(m)))
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("librarystrategy: write README.md: %w", err)
	}
	return nil
}

// Check verifies that README.md is exactly what a fresh generation produces.
func Check(root string) error {
	m, err := Load(root)
	if err != nil {
		return err
	}
	path := filepath.Join(root, "README.md")
	old, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("librarystrategy: read README.md: %w", err)
	}
	expected, err := Replace(old, []byte(Render(m)))
	if err != nil {
		return err
	}
	if !bytes.Equal(normalizeTables(old), normalizeTables(expected)) {
		return fmt.Errorf("librarystrategy: README.md library-strategy region is stale; run go run ./tools/gen/librarystrategy/cmd/generatelibrarystrategy")
	}
	return nil
}

// tableCellPadding matches the padding prettier adds inside Markdown table
// cells (runs of spaces around pipes) and the dash runs it stretches in
// separator rows. The checked-in README is prettier-formatted by the commit
// hook, while Render emits minimal tables; the two must compare equal on
// content, not on padding.
var tableCellPadding = regexp.MustCompile(`[ 	]*\|[ 	]*`)

// tableSeparatorRun matches a Markdown table separator run of any width.
var tableSeparatorRun = regexp.MustCompile(`-{3,}`)

// normalizeTables collapses prettier's table padding so Check compares the
// generated region by content.
func normalizeTables(b []byte) []byte {
	b = tableCellPadding.ReplaceAll(b, []byte("|"))
	return tableSeparatorRun.ReplaceAll(b, []byte("---"))
}

// Verify is a compatibility name for callers that use checker terminology.
// It is intentionally an alias of Check so there is one drift policy.
func Verify(root string) error { return Check(root) }

// Generate loads all current architecture inputs. Render is kept separate so
// callers can inspect or test the deterministic model without touching disk.
func Generate(root string) (Manifest, error) { return Load(root) }

// WriteAll follows the naming used by the other generators in tools/gen.
// This generator has one output, README.md, so it delegates to UpdateREADME.
func WriteAll(root string) error { return UpdateREADME(root) }

// RenderSection is an explicit spelling for callers that distinguish the
// generated section from a complete README document.
func RenderSection(m Manifest) string { return Render(m) }
