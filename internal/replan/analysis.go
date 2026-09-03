// Package replan determines the material proposal subgraph affected by drift.
// It is deliberately independent of workflow execution: callers can use the
// result to decide whether to revalidate, re-simulate, or block a proposal.
package replan

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Kind identifies the material proposal artifact represented by a node.
type Kind string

const (
	Fact        Kind = "FACT"
	Calculation Kind = "CALCULATION"
	Write       Kind = "WRITE"
	Effect      Kind = "EFFECT"
	Obligation  Kind = "OBLIGATION"
	Decision    Kind = "DECISION"
)

// Status is the action required for a node under the current inputs.
type Status string

const (
	Unchanged  Status = "UNCHANGED"
	Recompute  Status = "RECOMPUTE"
	Invalidate Status = "INVALIDATE"
	Unknown    Status = "UNKNOWN"
)

// Node is a proposal component. Dependencies must point from a node to the
// nodes it consumes. IDs are stable within a proposal revision.
type Node struct {
	ID           string
	Kind         Kind
	Dependencies []string
}

// Snapshot contains canonical, versioned values for material inputs. Values
// are opaque to this package; callers should provide canonical bytes/strings.
type Snapshot map[string]string

// Result is deterministic and safe to persist as evidence.
type Result struct {
	Digest  string
	Changed []string
	Nodes   []Finding
}

// Finding gives the classification and explainable reason for one component.
type Finding struct {
	ID     string
	Kind   Kind
	Status Status
	Reason string
}

// Analyze computes the dependency closure rooted at changed snapshot keys.
// A changed key must identify a graph node. Unknown roots, missing dependency
// references, duplicate IDs, and dependency cycles fail closed with UNKNOWN.
func Analyze(old, current Snapshot, nodes []Node) Result {
	result := Result{}
	byID := make(map[string]Node, len(nodes))
	invalid := make(map[string]string)
	for _, n := range nodes {
		if n.ID == "" {
			continue
		}
		if _, exists := byID[n.ID]; exists {
			invalid[n.ID] = "duplicate node id"
			continue
		}
		byID[n.ID] = n
	}
	changed := make(map[string]string)
	keys := make(map[string]struct{}, len(old)+len(current))
	for k := range old {
		keys[k] = struct{}{}
	}
	for k := range current {
		keys[k] = struct{}{}
	}
	for k := range keys {
		ov, ook := old[k]
		cv, cok := current[k]
		if !ook || !cok || ov != cv {
			changed[k] = "input changed"
			result.Changed = append(result.Changed, k)
		}
	}
	sort.Strings(result.Changed)

	// Reverse edges make closure traversal explicit and deterministic.
	reverse := make(map[string][]string, len(byID))
	for id, n := range byID {
		for _, dep := range n.Dependencies {
			if _, ok := byID[dep]; !ok {
				invalid[id] = fmt.Sprintf("missing dependency %q", dep)
				continue
			}
			reverse[dep] = append(reverse[dep], id)
		}
	}
	affected := make(map[string]string)
	queue := append([]string(nil), result.Changed...)
	for _, root := range result.Changed {
		if _, ok := byID[root]; !ok {
			affected[root] = "changed input has no lineage"
		} else {
			affected[root] = "input changed"
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, child := range reverse[id] {
			if _, seen := affected[child]; seen {
				continue
			}
			affected[child] = "depends on changed input"
			queue = append(queue, child)
		}
	}
	// Detect cycles over the whole declared graph; allowing one would make the
	// closure incomplete even when the cycle is not currently changed.
	state := make(map[string]uint8, len(byID))
	path := make([]string, 0, len(byID))
	pathIndex := make(map[string]int, len(byID))
	var visit func(string)
	visit = func(id string) {
		if state[id] == 1 {
			// Mark every member of the back-edge cycle, rather than only the
			// node where DFS happened to encounter the back edge.
			for _, member := range path[pathIndex[id]:] {
				invalid[member] = "dependency cycle"
			}
			return
		}
		if state[id] == 2 {
			return
		}
		state[id] = 1
		pathIndex[id] = len(path)
		path = append(path, id)
		for _, dep := range byID[id].Dependencies {
			if _, ok := byID[dep]; ok {
				visit(dep)
			}
		}
		path = path[:len(path)-1]
		delete(pathIndex, id)
		state[id] = 2
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		visit(id)
	}
	unknownQueue := make([]string, 0, len(invalid))
	for id := range invalid {
		unknownQueue = append(unknownQueue, id)
	}
	for len(unknownQueue) > 0 {
		id := unknownQueue[0]
		unknownQueue = unknownQueue[1:]
		for _, child := range reverse[id] {
			if _, exists := invalid[child]; !exists {
				invalid[child] = "depends on unknown lineage"
				unknownQueue = append(unknownQueue, child)
			}
		}
	}

	for _, id := range ids {
		f := Finding{ID: id, Kind: byID[id].Kind, Status: Unchanged, Reason: "no material input changed"}
		if reason, ok := invalid[id]; ok {
			f.Status, f.Reason = Unknown, reason
		} else if reason, ok := affected[id]; ok {
			// Facts and calculations can be regenerated from the current
			// snapshot. A write/effect/obligation/decision is a material
			// proposal artifact and must be invalidated before it can be
			// reconsidered.
			if byID[id].Kind == Fact || byID[id].Kind == Calculation {
				f.Status = Recompute
			} else {
				f.Status = Invalidate
			}
			f.Reason = reason
		}
		result.Nodes = append(result.Nodes, f)
	}
	for _, id := range result.Changed {
		if _, ok := byID[id]; !ok {
			result.Nodes = append(result.Nodes, Finding{ID: id, Kind: Fact, Status: Unknown, Reason: affected[id]})
		}
	}
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].ID < result.Nodes[j].ID })
	result.Digest = digest(result, old, current)
	return result
}

// AnalyzeMaterialSubgraph is the descriptive alias for Analyze.
func AnalyzeMaterialSubgraph(old, current Snapshot, nodes []Node) Result {
	return Analyze(old, current, nodes)
}

func digest(r Result, old, current Snapshot) string {
	var b strings.Builder
	for _, id := range r.Changed {
		ov, ook := old[id]
		cv, cok := current[id]
		fmt.Fprintf(&b, "C:%s|%t:%s|%t:%s\n", id, ook, ov, cok, cv)
	}
	for _, n := range r.Nodes {
		fmt.Fprintf(&b, "N:%s|%s|%s|%s\n", n.ID, n.Kind, n.Status, n.Reason)
	}
	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:])
}
