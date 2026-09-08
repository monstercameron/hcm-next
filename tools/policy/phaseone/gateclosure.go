package phaseone

import (
	"fmt"
	"sort"
	"strings"
)

const (
	GateA    = "GATE_A"
	GateB    = "GATE_B"
	GateC    = "GATE_C"
	Phase2   = "PHASE_2"
	Contract = "CONTRACT"
	Vector   = "VECTOR"
)

const (
	TodoNode           = "TODO"
	TestNode           = "TEST"
	EvidenceNode       = "EVIDENCE"
	ImplementationNode = "IMPLEMENTATION"
	AssuranceNode      = "ASSURANCE"
	RestoreNode        = "RESTORE"
)

type GateNode struct {
	ID              string `json:"id"`
	Gate            string `json:"gate"`
	Kind            string `json:"kind"`
	Package         string `json:"package,omitempty"`
	Selected        bool   `json:"selected"`
	Final           bool   `json:"final,omitempty"`
	Independent     bool   `json:"independent,omitempty"`
	RestoreVerified bool   `json:"restore_verified,omitempty"`
	Assures         string `json:"assures,omitempty"`
}

type GateEdge struct {
	From             string `json:"from"`
	To               string `json:"to"`
	Kind             string `json:"kind"`
	ContractOrVector bool   `json:"contract_or_vector,omitempty"`
}

type GateDependencyGraph struct {
	Nodes []GateNode `json:"nodes"`
	Edges []GateEdge `json:"edges"`
}
type GateViolation struct {
	Kind    string   `json:"kind"`
	From    string   `json:"from,omitempty"`
	To      string   `json:"to,omitempty"`
	Detail  string   `json:"detail"`
	Witness []string `json:"witness,omitempty"`
}
type AssuranceClosure struct {
	Gate       string          `json:"gate"`
	Final      string          `json:"final"`
	Order      []string        `json:"order,omitempty"`
	Violations []GateViolation `json:"violations,omitempty"`
}

// CheckGateDependencyGraph checks a graph already expanded from an exact signed
// manifest. It never discovers todos or enlarges the caller's selection.
func CheckGateDependencyGraph(graph GateDependencyGraph) []GateViolation {
	nodes := make(map[string]GateNode, len(graph.Nodes))
	var out []GateViolation
	if len(graph.Nodes) == 0 {
		out = append(out, violation("empty-selection", "", "", "gate graph must contain exact selected nodes", nil))
	}
	for _, n := range graph.Nodes {
		if n.ID == "" {
			out = append(out, violation("invalid-node", "", "", "node has no stable id", nil))
			continue
		}
		if _, exists := nodes[n.ID]; exists {
			out = append(out, violation("duplicate-node", n.ID, "", "selected node appears more than once", nil))
			continue
		}
		if !n.Selected {
			out = append(out, violation("unselected-node", n.ID, "", "unselected node cannot enter an exact closure", nil))
			continue
		}
		if _, ok := gateRank(n.Gate); !ok {
			out = append(out, violation("invalid-gate", n.ID, "", fmt.Sprintf("unknown gate %q", n.Gate), nil))
		}
		if !validNodeKind(n.Kind) {
			out = append(out, violation("invalid-node-kind", n.ID, "", fmt.Sprintf("unknown node kind %q", n.Kind), nil))
		}
		nodes[n.ID] = n
	}
	adj := make(map[string][]string, len(nodes))
	for _, e := range graph.Edges {
		from, fromOK := nodes[e.From]
		to, toOK := nodes[e.To]
		if !fromOK || !toOK {
			out = append(out, violation("out-of-selection", e.From, e.To, "dependency must name two exact selected nodes", nil))
			continue
		}
		adj[e.From] = append(adj[e.From], e.To)
		if !validNodeKind(e.Kind) {
			out = append(out, violation("invalid-edge-kind", e.From, e.To, fmt.Sprintf("unknown edge kind %q", e.Kind), []string{e.From, e.To}))
		}
		exception := (e.Kind == Contract || e.Kind == Vector) && e.Kind == to.Kind
		if e.ContractOrVector && !exception {
			out = append(out, violation("invalid-phase-exception", e.From, e.To, "contract/vector exception must point to a node of the same explicit kind", []string{e.From, e.To}))
		}
		fromRank, fromKnown := gateRank(from.Gate)
		toRank, toKnown := gateRank(to.Gate)
		if fromKnown && toKnown && fromRank < toRank && !exception {
			out = append(out, violation("later-phase-dependency", e.From, e.To, "earlier gate depends on later-phase work", []string{e.From, e.To}))
		}
	}
	for id := range adj {
		sort.Strings(adj[id])
		adj[id] = uniqueGateStrings(adj[id])
	}
	for _, from := range sortedGateNodes(nodes) {
		fromRank, known := gateRank(from.Gate)
		if !known {
			continue
		}
		path := shortestPath(adj, from.ID, func(id string) bool {
			to := nodes[id]
			rank, ok := gateRank(to.Gate)
			return id != from.ID && ok && fromRank < rank && to.Kind == ImplementationNode
		})
		if len(path) > 2 {
			out = append(out, violation("later-phase-dependency", from.ID, path[len(path)-1], "earlier gate transitively depends on later-phase implementation", path))
		}
	}
	for _, cycle := range graphCycles(nodes, adj) {
		out = append(out, violation("cycle", cycle[0], cycle[len(cycle)-1], "gate dependency graph contains a cycle", cycle))
	}
	for _, n := range sortedGateNodes(nodes) {
		if n.Kind != AssuranceNode || n.Assures == "" {
			continue
		}
		if _, ok := nodes[n.Assures]; !ok {
			out = append(out, violation("invalid-assurance-target", n.ID, n.Assures, "assurance target is not an exact selected node", nil))
			continue
		}
		path := shortestPath(adj, n.ID, func(id string) bool { return id == n.Assures })
		if len(path) > 1 {
			out = append(out, violation("evidence-self-certification", n.ID, n.Assures, "assurance depends on the evidence package it informs", path))
		}
	}
	return sortAndUnique(out)
}

func CompileAssuranceClosure(graph GateDependencyGraph, gate string) AssuranceClosure {
	closure := AssuranceClosure{Gate: gate, Violations: CheckGateDependencyGraph(graph)}
	if _, ok := gateRank(gate); !ok {
		closure.Violations = append(closure.Violations, violation("invalid-closure-gate", "", gate, "closure gate is unknown", nil))
		closure.Violations = sortAndUnique(closure.Violations)
		return closure
	}
	var finals []GateNode
	for _, n := range graph.Nodes {
		if n.Selected && n.Gate == gate && n.Final {
			finals = append(finals, n)
		}
	}
	if len(finals) != 1 {
		closure.Violations = append(closure.Violations, violation("invalid-final-count", "", gate, fmt.Sprintf("gate requires exactly one final evidence node; got %d", len(finals)), nil))
		closure.Violations = sortAndUnique(closure.Violations)
		return closure
	}
	final := finals[0]
	closure.Final = final.ID
	if final.Kind != EvidenceNode {
		closure.Violations = append(closure.Violations, violation("invalid-final-kind", final.ID, "", "final gate node must be evidence", nil))
	}
	if len(closure.Violations) != 0 {
		closure.Violations = sortAndUnique(closure.Violations)
		return closure
	}
	closure.Order = assuranceOrder(graph, final.ID)
	if gate != GateB {
		return closure
	}
	reachable := make(map[string]bool, len(closure.Order))
	for _, id := range closure.Order {
		reachable[id] = true
	}
	var independent, restore bool
	for _, n := range graph.Nodes {
		if !reachable[n.ID] || n.ID == final.ID {
			continue
		}
		if n.Kind == AssuranceNode && n.Independent && n.Assures == final.ID && n.Package != final.Package {
			independent = true
		}
		if n.Kind == RestoreNode {
			if n.RestoreVerified {
				restore = true
			} else {
				closure.Violations = append(closure.Violations, violation("restore-not-verified", n.ID, "", "backup readability is not cross-store restore evidence", nil))
			}
		}
	}
	if !independent {
		closure.Violations = append(closure.Violations, violation("missing-independent-assurance", "", final.ID, "Gate B requires independent assurance before final evidence", nil))
	}
	if !restore {
		closure.Violations = append(closure.Violations, violation("missing-cross-store-restore", "", final.ID, "Gate B requires verified cross-store restore before final evidence", nil))
	}
	closure.Violations = sortAndUnique(closure.Violations)
	return closure
}

func gateRank(gate string) (int, bool) {
	switch strings.ToUpper(gate) {
	case GateA:
		return 1, true
	case GateB:
		return 2, true
	case GateC:
		return 3, true
	case Phase2:
		return 4, true
	default:
		return 0, false
	}
}
func validNodeKind(kind string) bool {
	switch kind {
	case TodoNode, TestNode, EvidenceNode, ImplementationNode, AssuranceNode, RestoreNode, Contract, Vector:
		return true
	default:
		return false
	}
}
func sortedGateNodes(nodes map[string]GateNode) []GateNode {
	out := make([]GateNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func uniqueGateStrings(in []string) []string {
	out := in[:0]
	for _, value := range in {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func shortestPath(adj map[string][]string, start string, match func(string) bool) []string {
	queue := [][]string{{start}}
	seen := map[string]bool{start: true}
	for len(queue) > 0 {
		path := queue[0]
		queue = queue[1:]
		for _, next := range adj[path[len(path)-1]] {
			candidate := append(append([]string(nil), path...), next)
			if match(next) {
				return candidate
			}
			if !seen[next] {
				seen[next] = true
				queue = append(queue, candidate)
			}
		}
	}
	return nil
}

func graphCycles(nodes map[string]GateNode, adj map[string][]string) [][]string {
	found := map[string][]string{}
	for _, start := range sortedGateNodes(nodes) {
		for _, next := range adj[start.ID] {
			path := shortestPath(adj, next, func(id string) bool { return id == start.ID })
			if len(path) == 0 {
				continue
			}
			cycle := canonicalCycle(append([]string{start.ID}, path...))
			found[strings.Join(cycle, "\x00")] = cycle
		}
	}
	keys := make([]string, 0, len(found))
	for key := range found {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([][]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, found[key])
	}
	return out
}
func canonicalCycle(cycle []string) []string {
	base := cycle[:len(cycle)-1]
	best := append([]string(nil), base...)
	for i := 1; i < len(base); i++ {
		rotated := append(append([]string(nil), base[i:]...), base[:i]...)
		if strings.Join(rotated, "\x00") < strings.Join(best, "\x00") {
			best = rotated
		}
	}
	return append(best, best[0])
}

func assuranceOrder(graph GateDependencyGraph, final string) []string {
	seen := map[string]bool{}
	var order []string
	byFrom := map[string][]string{}
	for _, e := range graph.Edges {
		byFrom[e.From] = append(byFrom[e.From], e.To)
	}
	for id := range byFrom {
		sort.Strings(byFrom[id])
	}
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, dep := range byFrom[id] {
			visit(dep)
		}
		order = append(order, id)
	}
	visit(final)
	return order
}
func violation(kind, from, to, detail string, witness []string) GateViolation {
	return GateViolation{Kind: kind, From: from, To: to, Detail: detail, Witness: witness}
}
func sortAndUnique(in []GateViolation) []GateViolation {
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Kind != in[j].Kind {
			return in[i].Kind < in[j].Kind
		}
		if in[i].From != in[j].From {
			return in[i].From < in[j].From
		}
		if in[i].To != in[j].To {
			return in[i].To < in[j].To
		}
		return strings.Join(in[i].Witness, "\x00") < strings.Join(in[j].Witness, "\x00")
	})
	out := in[:0]
	seen := map[string]bool{}
	for _, v := range in {
		key := fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s", v.Kind, v.From, v.To, v.Detail, strings.Join(v.Witness, "\x00"))
		if !seen[key] {
			seen[key] = true
			out = append(out, v)
		}
	}
	return out
}
