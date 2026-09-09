// Package importgraph builds the real Go import graph for the root module
// (ARCH-GO-003) and evaluates it against the layout and dependency-edge
// policies.
package importgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depedge"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
)

// Edge is one direct, within-module import edge.
type Edge struct {
	Importer string
	Imported string
}

// Graph is the real import graph plus the policy findings computed over it.
type Graph struct {
	Module           string
	Packages         []repopath.Package
	Edges            []Edge
	LayoutViolations []string
	PolicyViolations []depedge.Violation
	Cycle            []string // non-nil when an import cycle was found
	Digest           string
}

// Build lists the root module's packages, extracts direct within-module
// import edges, and evaluates both the layout manifest and the dependency
// policy over every package and edge.
func Build(root string, layoutManifest *layout.Manifest, policy *depedge.Policy) (*Graph, error) {
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		return nil, err
	}

	mod := repopath.ModulePath(root)
	g := &Graph{Module: mod, Packages: pkgs}

	for _, pkg := range pkgs {
		if v := layoutManifest.ClassifyImportPath(pkg.ImportPath); !v.Allowed {
			g.LayoutViolations = append(g.LayoutViolations,
				fmt.Sprintf("%s: %s", pkg.ImportPath, v.Reason))
		}

		for _, imp := range pkg.Imports {
			if imp == pkg.ImportPath {
				continue
			}
			if !strings.HasPrefix(imp, mod+"/") && imp != mod {
				continue // std lib / third-party: not this graph's concern
			}
			g.Edges = append(g.Edges, Edge{Importer: pkg.ImportPath, Imported: imp})

			if violation := policy.CheckEdge(pkg.ImportPath, imp); violation != nil {
				g.PolicyViolations = append(g.PolicyViolations, *violation)
			}
		}
	}

	g.Cycle = findCycle(g.Edges)
	g.Digest = computeDigest(g.Edges)

	return g, nil
}

// findCycle runs a DFS over edges and returns one cycle (as a slice of
// import paths, first element repeated at the end) if one exists, or nil.
// Successfully `go list`-ed Go packages cannot actually contain an import
// cycle (the compiler forbids it), so this is a defensive sanity check.
func findCycle(edges []Edge) []string {
	adjacency := map[string][]string{}
	for _, e := range edges {
		adjacency[e.Importer] = append(adjacency[e.Importer], e.Imported)
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var path []string

	var visit func(node string) []string
	visit = func(node string) []string {
		color[node] = gray
		path = append(path, node)

		for _, next := range adjacency[node] {
			switch color[next] {
			case white:
				if cyc := visit(next); cyc != nil {
					return cyc
				}
			case gray:
				// Found the back edge; build the cycle starting at `next`.
				for i, n := range path {
					if n == next {
						cyc := append([]string{}, path[i:]...)
						return append(cyc, next)
					}
				}
			}
		}

		path = path[:len(path)-1]
		color[node] = black
		return nil
	}

	// Sort node names for a deterministic traversal order.
	nodes := make([]string, 0, len(adjacency))
	for n := range adjacency {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	for _, n := range nodes {
		if color[n] == white {
			if cyc := visit(n); cyc != nil {
				return cyc
			}
		}
	}
	return nil
}

// computeDigest returns a stable sha256 hex digest of the sorted edge list,
// so it changes only when the graph's shape actually changes (not with
// listing order).
func computeDigest(edges []Edge) string {
	lines := make([]string, 0, len(edges))
	for _, e := range edges {
		lines = append(lines, e.Importer+" -> "+e.Imported)
	}
	sort.Strings(lines)

	h := sha256.New()
	for _, l := range lines {
		h.Write([]byte(l))
		h.Write([]byte("\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}
