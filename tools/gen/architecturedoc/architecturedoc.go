// Package architecturedoc renders the repository's observed Go architecture.
//
// The manifests remain normative; this package only produces an explanatory,
// deterministic snapshot of the package/import graph and its declared owners.
package architecturedoc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depedge"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/importgraph"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
	"gopkg.in/yaml.v3"
)

const DefaultOutputName = "architecture.md"

type processManifest struct {
	Processes []process `yaml:"processes"`
}
type process struct {
	Command          string   `yaml:"command"`
	Status           string   `yaml:"status"`
	SemanticPackages []string `yaml:"semantic_packages"`
}

// Package describes one observed package and its declared architecture root.
type Package struct{ ImportPath, Root, Owner, Layer, Phase string }

// Edge is a sorted direct within-module dependency.
type Edge struct{ Importer, Imported string }

// Snapshot is the complete input-derived architecture document model.
type Snapshot struct {
	Module    string
	Digest    string
	Packages  []Package
	Edges     []Edge
	Processes map[string][]string // semantic package root -> process names
	// PortAdapters records the declared port roots and the observed packages
	// beneath each root. It is explanatory output; the dependency manifest
	// remains the authority for whether an edge is allowed.
	PortAdapters []Boundary
}

// Boundary describes one declared port and its observed implementation
// packages. Keeping the port name separate from paths makes the generated
// document useful when a package is renamed without changing ownership.
type Boundary struct {
	Name     string
	Root     string
	Packages []string
}

// Load builds a snapshot from repository-layout, package-dependency-policy,
// process-roles and the real `go list` import graph.
func Load(root string) (Snapshot, error) {
	l, err := layout.Load(filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
	if err != nil {
		return Snapshot{}, err
	}
	p, err := depedge.Load(filepath.Join(root, "definitions", "architecture", "package-dependency-policy.yaml"))
	if err != nil {
		return Snapshot{}, err
	}
	g, err := importgraph.Build(root, l, p)
	if err != nil {
		return Snapshot{}, fmt.Errorf("architecturedoc: build graph: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "definitions", "architecture", "process-roles.yaml"))
	if err != nil {
		return Snapshot{}, err
	}
	var pm processManifest
	if err := yaml.Unmarshal(data, &pm); err != nil {
		return Snapshot{}, fmt.Errorf("architecturedoc: parse process roles: %w", err)
	}
	owners := map[string]struct{ owner, layer, phase string }{}
	for _, r := range l.InternalPackageRoots {
		owners[r.Name] = struct{ owner, layer, phase string }{r.Owner, r.Layer, r.Phase}
	}
	pkgs := make([]Package, 0, len(g.Packages))
	for _, x := range g.Packages {
		rel := strings.TrimPrefix(x.ImportPath, g.Module+"/")
		parts := strings.Split(rel, "/")
		rootName := ""
		if len(parts) >= 2 && parts[0] == "internal" {
			rootName = parts[1]
		}
		v := owners[rootName]
		pkgs = append(pkgs, Package{x.ImportPath, rootName, v.owner, v.layer, v.phase})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })
	edges := make([]Edge, len(g.Edges))
	for i, e := range g.Edges {
		edges[i] = Edge{e.Importer, e.Imported}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Importer == edges[j].Importer {
			return edges[i].Imported < edges[j].Imported
		}
		return edges[i].Importer < edges[j].Importer
	})
	proc := map[string][]string{}
	for _, row := range pm.Processes {
		for _, sp := range row.SemanticPackages {
			parts := strings.Split(strings.Trim(sp, "/"), "/")
			key := sp
			if len(parts) >= 2 && parts[0] == "internal" {
				key = parts[1]
			}
			proc[key] = append(proc[key], row.Command+" ("+row.Status+")")
		}
	}
	for k := range proc {
		sort.Strings(proc[k])
	}
	boundaries := make([]Boundary, 0, len(p.PortsAndAdapters))
	for _, pa := range p.PortsAndAdapters {
		for _, root := range pa.Roots {
			b := Boundary{Name: pa.Name, Root: root}
			for _, pkg := range pkgs {
				if pkg.ImportPath == g.Module+"/"+root || strings.HasPrefix(pkg.ImportPath, g.Module+"/"+root+"/") {
					b.Packages = append(b.Packages, pkg.ImportPath)
				}
			}
			sort.Strings(b.Packages)
			boundaries = append(boundaries, b)
		}
	}
	sort.Slice(boundaries, func(i, j int) bool {
		if boundaries[i].Name == boundaries[j].Name {
			return boundaries[i].Root < boundaries[j].Root
		}
		return boundaries[i].Name < boundaries[j].Name
	})
	return Snapshot{Module: g.Module, Digest: g.Digest, Packages: pkgs, Edges: edges, Processes: proc, PortAdapters: boundaries}, nil
}

// Render emits stable Markdown. It is pure and never writes to disk.
func Render(s Snapshot) []byte {
	packages := append([]Package(nil), s.Packages...)
	sort.Slice(packages, func(i, j int) bool { return packages[i].ImportPath < packages[j].ImportPath })
	edges := append([]Edge(nil), s.Edges...)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Importer == edges[j].Importer {
			return edges[i].Imported < edges[j].Imported
		}
		return edges[i].Importer < edges[j].Importer
	})
	var b bytes.Buffer
	fmt.Fprintf(&b, "# Repository architecture\n\n<!-- generated by tools/gen/architecturedoc; source manifests are normative -->\n\n")
	fmt.Fprintf(&b, "- Module: `%s`\n- Import-graph digest: `%s`\n- Packages: %d\n- Direct internal edges: %d\n\n", s.Module, s.Digest, len(packages), len(edges))
	b.WriteString("## Package tree\n\n| Package | Root | Owner | Layer | Phase |\n|---|---|---|---|---|\n")
	for _, p := range packages {
		fmt.Fprintf(&b, "| `%s` | `%s` | `%s` | `%s` | `%s` |\n", p.ImportPath, p.Root, p.Owner, p.Layer, p.Phase)
	}
	b.WriteString("\n## Dependency diagram\n\n```text\n")
	for _, e := range edges {
		fmt.Fprintf(&b, "%s -> %s\n", e.Importer, e.Imported)
	}
	b.WriteString("```\n\n## Semantic owner to process role\n\n| Package root | Processes |\n|---|---|\n")
	keys := make([]string, 0, len(s.Processes))
	for k := range s.Processes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "| `%s` | %s |\n", k, strings.Join(s.Processes[k], ", "))
	}
	b.WriteString("\n## Ports and adapters\n\n| Port | Root | Observed packages |\n|---|---|---|\n")
	boundaries := append([]Boundary(nil), s.PortAdapters...)
	sort.Slice(boundaries, func(i, j int) bool {
		if boundaries[i].Name == boundaries[j].Name {
			return boundaries[i].Root < boundaries[j].Root
		}
		return boundaries[i].Name < boundaries[j].Name
	})
	for _, boundary := range boundaries {
		fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", boundary.Name, boundary.Root, strings.Join(boundary.Packages, ", "))
	}
	b.WriteString("\n## Phase 1 and deferred decomposition\n\n")
	for _, p := range packages {
		if p.Phase == "P1A" {
			fmt.Fprintf(&b, "- `%s` — `%s` (%s)\n", p.ImportPath, p.Owner, p.Layer)
		}
	}
	b.WriteString("\n## Sources and deferred decisions\n\n")
	b.WriteString("- [`repository-layout.yaml`](../../definitions/architecture/repository-layout.yaml) — package ownership, layers and phase decisions\n")
	b.WriteString("- [`package-dependency-policy.yaml`](../../definitions/architecture/package-dependency-policy.yaml) — allowed dependency and port/adapter boundaries\n")
	b.WriteString("- [`process-roles.yaml`](../../definitions/architecture/process-roles.yaml) — semantic-owner to process placement\n")
	b.WriteString("\nDeferred packages remain declared architecture, not implemented code; consult `repository-layout.yaml` for their descriptions and phase decisions.\n")
	return b.Bytes()
}

// Check compares path with the current rendered snapshot and reports drift.
func Check(root, path string) error {
	want, err := func() ([]byte, error) {
		s, e := Load(root)
		if e != nil {
			return nil, e
		}
		return Render(s), nil
	}()
	if err != nil {
		return err
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("architecturedoc: read %s: %w", path, err)
	}
	if !bytes.Equal(want, got) {
		return fmt.Errorf("architecturedoc: %s is stale", path)
	}
	return nil
}

// Write renders to exactly path. Callers choose the destination; generation
// never writes a repository file implicitly.
func Write(root, path string) error {
	s, err := Load(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, Render(s), 0644)
}

// Digest returns the stable digest of rendered bytes, useful for artifact
// metadata and tests independent of filesystem timestamps.
func Digest(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
