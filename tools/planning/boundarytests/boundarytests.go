// Package boundarytests implements the GOV-013 architecture-boundary
// dependency tests: it reuses tools/policy/depedge and
// tools/policy/importgraph as libraries to assert that the real Go import
// graph obeys definitions/architecture/package-dependency-policy.yaml's
// direction rules, and reduces that graph to a coarse, diffable
// layer-to-layer edge set so an accidental new dependency direction shows
// up as a text diff against a committed golden, never as a silent pass.
package boundarytests

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/tools/policy/depedge"
	"github.com/monstercameron/hcm-next/tools/policy/importgraph"
)

// ClassifyRel resolves a module-relative package path to the name of the
// ranked layer or port/adapter grouping that owns it, using only
// depedge.Policy's exported fields (Policy's own layerOf/portAdapterOf are
// unexported). isPort reports whether the match was a port/adapter rather
// than a ranked layer.
func ClassifyRel(policy *depedge.Policy, rel string) (name string, isPort bool, ok bool) {
	for _, l := range policy.Layers {
		for _, root := range l.Roots {
			if rel == root || strings.HasPrefix(rel, root+"/") {
				return l.Name, false, true
			}
		}
	}
	for _, pa := range policy.PortsAndAdapters {
		for _, root := range pa.Roots {
			if rel == root || strings.HasPrefix(rel, root+"/") {
				return pa.Name, true, true
			}
		}
	}
	return "", false, false
}

// LayerEdge is one distinct layer/port-to-layer/port dependency direction
// observed in the real import graph.
type LayerEdge struct {
	From string
	To   string
}

func (e LayerEdge) String() string { return e.From + " -> " + e.To }

// LayerGraph reduces a full package-level import graph to the distinct set
// of layer/port-to-layer/port edges it contains, sorted for determinism.
// A package outside every declared layer/port root contributes no edge:
// layout.Manifest (ARCH-GO-001/GOV-013's sibling package check) is what
// catches an unrooted package, not this coarse plane summary. A self-edge
// (a layer importing itself) is also omitted: only cross-layer direction
// is this graph's concern.
func LayerGraph(g *importgraph.Graph, policy *depedge.Policy) []LayerEdge {
	module := policy.Module
	seen := make(map[LayerEdge]bool)

	for _, e := range g.Edges {
		importerRel, ok1 := trimModule(module, e.Importer)
		importedRel, ok2 := trimModule(module, e.Imported)
		if !ok1 || !ok2 {
			continue
		}
		fromName, _, fromOK := ClassifyRel(policy, importerRel)
		toName, _, toOK := ClassifyRel(policy, importedRel)
		if !fromOK || !toOK || fromName == toName {
			continue
		}
		seen[LayerEdge{From: fromName, To: toName}] = true
	}

	edges := make([]LayerEdge, 0, len(seen))
	for e := range seen {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		return edges[i].To < edges[j].To
	})
	return edges
}

func trimModule(module, importPath string) (string, bool) {
	prefix := module + "/"
	if !strings.HasPrefix(importPath, prefix) {
		return "", false
	}
	return strings.TrimPrefix(importPath, prefix), true
}

// RenderGolden renders edges as a deterministic, line-oriented, diffable
// text golden: one "From -> To" line per edge in edges' given order
// (callers pass already-sorted output from LayerGraph), LF-terminated.
func RenderGolden(edges []LayerEdge) string {
	var b strings.Builder
	for _, e := range edges {
		b.WriteString(e.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// normalizeLineEndings collapses CRLF to LF before a golden comparison, so
// a Windows checkout with autocrlf-mangled line endings never produces a
// false-positive diff (a known repo hazard: hcm-next's own pre-commit hook
// flags CRLF worktree files).
func normalizeLineEndings(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// GoldenDiff reports whether want and got render identically after
// line-ending normalization. When they differ it returns a human-readable
// report naming every edge unique to one side, ok=false, and a non-empty
// diff. An empty want (no golden file recorded yet) is never silently
// treated as a match: it is reported as "no golden recorded" so a first run
// fails loudly instead of passing.
func GoldenDiff(want, got string) (diff string, ok bool) {
	if strings.TrimSpace(want) == "" {
		return "no golden layer graph recorded yet; run with HCMNEXT_UPDATE_GOLDEN=1 to create one", false
	}

	wantLines := splitNonEmpty(normalizeLineEndings(want))
	gotLines := splitNonEmpty(normalizeLineEndings(got))

	wantSet := toSet(wantLines)
	gotSet := toSet(gotLines)

	var added, removed []string
	for _, l := range gotLines {
		if !wantSet[l] {
			added = append(added, l)
		}
	}
	for _, l := range wantLines {
		if !gotSet[l] {
			removed = append(removed, l)
		}
	}

	if len(added) == 0 && len(removed) == 0 {
		return "", true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "layer graph changed (%d new edge(s), %d removed edge(s)):\n", len(added), len(removed))
	sort.Strings(added)
	sort.Strings(removed)
	for _, l := range added {
		fmt.Fprintf(&b, "  + %s\n", l)
	}
	for _, l := range removed {
		fmt.Fprintf(&b, "  - %s\n", l)
	}
	return b.String(), false
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func toSet(lines []string) map[string]bool {
	m := make(map[string]bool, len(lines))
	for _, l := range lines {
		m[l] = true
	}
	return m
}

// HasEdge reports whether edges contains a From->To edge, ignoring order.
func HasEdge(edges []LayerEdge, from, to string) bool {
	for _, e := range edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

// ImportsSubtree reports whether the real import graph contains any edge
// from a package rooted at fromPrefix to a package rooted at toPrefix
// (module-relative prefixes, e.g. "internal/workflow", "internal/intelligence").
// Used to guard against a dependency direction the current policy manifest
// does not yet model at all (no declared layer exists for it), by asserting
// the edge is simply absent rather than merely unclassified.
func ImportsSubtree(g *importgraph.Graph, module, fromPrefix, toPrefix string) bool {
	for _, e := range g.Edges {
		importerRel, ok1 := trimModule(module, e.Importer)
		importedRel, ok2 := trimModule(module, e.Imported)
		if !ok1 || !ok2 {
			continue
		}
		if withinRoot(importerRel, fromPrefix) && withinRoot(importedRel, toPrefix) {
			return true
		}
	}
	return false
}

func withinRoot(rel, root string) bool {
	return rel == root || strings.HasPrefix(rel, root+"/")
}
