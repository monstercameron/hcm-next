// Package lineage explains one deterministic transformation execution without
// exposing the values that were transformed.  The graph is derived beside the
// taint interpreter, so source metadata and operation provenance are checked by
// the XFORM-004 implementation before an explanation can be published.
package lineage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/hcm-next/internal/engines/transformation"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/ir"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/taint"
)

const contractVersion = 1

// Version reports this package's contract version (ARCH-GO-009).
func Version() int { return contractVersion }

var (
	ErrInvalidGraph  = errors.New("transformation/lineage: invalid graph")
	ErrUnknownOutput = errors.New("transformation/lineage: unknown output")
	ErrMissingSource = errors.New("transformation/lineage: output has no source")
)

// NodeKind is the closed vocabulary used in a lineage graph.
type NodeKind string

const (
	NodeSource    NodeKind = "SOURCE"
	NodeConstant  NodeKind = "CONSTANT"
	NodeOperation NodeKind = "OPERATION"
	NodeOutput    NodeKind = "OUTPUT"
)

// Node is an audit-safe graph node.  It contains field names and semantic
// metadata, never a source or output value.
type Node struct {
	ID              string               `json:"id"`
	Kind            NodeKind             `json:"kind"`
	Field           string               `json:"field,omitempty"`
	Operation       string               `json:"operation,omitempty"`
	Function        string               `json:"function,omitempty"`
	TargetType      transformation.Type  `json:"target_type,omitempty"`
	LiteralDeclared bool                 `json:"literal_declared,omitempty"`
	Classification  taint.Classification `json:"classification,omitempty"`
	Presence        taint.PresenceState  `json:"presence,omitempty"`
	Taint           []string             `json:"taint,omitempty"`
	Reason          string               `json:"reason,omitempty"`
}

// Edge is a directed dependency edge.  Edges point from a source/value node
// through an operation to its output node.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Graph is the canonical explanation of one executed program.  Nodes and
// edges are returned in canonical order and are defensively copied by the
// methods on Graph.
type Graph struct {
	Nodes   []Node   `json:"nodes"`
	Edges   []Edge   `json:"edges"`
	Outputs []string `json:"outputs"`
}

// Execution combines the taint-checked output metadata and its graph.  Output
// values are intentionally not part of this type; callers that need values can
// use taint.Execute with the same input and program.
type Execution struct {
	Graph  Graph
	Digest string
}

// Limits is the same bounded execution contract used by transformation/exec.
type Limits = taint.Limits

// Execute runs one record through taint (and therefore transformation/exec)
// and returns its value-free source-to-output graph.  The optional limit keeps
// the common single-record call concise while allowing callers to declare the
// same row, step, and output bounds as the underlying interpreter.
func Execute(program ir.Program, input taint.Record, limits ...Limits) (Execution, error) {
	var limit Limits
	if len(limits) > 1 {
		return Execution{}, fmt.Errorf("%w: more than one execution limit", ErrInvalidGraph)
	}
	if len(limits) == 1 {
		limit = limits[0]
	}
	outputs, err := taint.Execute(program, []taint.Record{input}, limit)
	if err != nil {
		return Execution{}, err
	}
	if len(outputs) != 1 {
		return Execution{}, fmt.Errorf("%w: taint execution returned %d rows", ErrInvalidGraph, len(outputs))
	}
	graph, err := build(program, input, outputs[0])
	if err != nil {
		return Execution{}, err
	}
	digest, err := graph.DigestChecked()
	if err != nil {
		return Execution{}, err
	}
	return Execution{Graph: graph, Digest: digest}, nil
}

// Build is an explicit alias for Execute for callers that prefer the graph
// construction vocabulary.
func Build(program ir.Program, input taint.Record, limits ...Limits) (Graph, error) {
	execution, err := Execute(program, input, limits...)
	if err != nil {
		return Graph{}, err
	}
	return execution.Graph, nil
}

func build(program ir.Program, input, output taint.Record) (Graph, error) {
	if err := program.Validate(); err != nil {
		return Graph{}, err
	}
	nodes := make(map[string]Node)
	edges := make(map[Edge]struct{})
	produced := make(map[string]ir.Instruction, len(program.Instructions))
	for _, instruction := range program.Instructions {
		produced[pathKey(instruction.Destination)] = instruction
	}

	for _, instruction := range program.Instructions {
		opID := operationID(instruction)
		opNode := Node{
			ID:              opID,
			Kind:            NodeOperation,
			Field:           pathKey(instruction.Destination),
			Operation:       string(instruction.Op),
			Function:        string(instruction.Function),
			TargetType:      instruction.TargetType,
			LiteralDeclared: len(instruction.Sources) == 0 && instruction.Literal != "",
		}
		if value, ok := output[pathKey(instruction.Destination)]; ok {
			opNode.Classification = value.Classification
			opNode.Presence = value.Presence
			opNode.Taint = append([]string(nil), value.Taint...)
			opNode.Reason = value.Reason
		}
		nodes[opID] = opNode

		for _, source := range instruction.Sources {
			key := pathKey(source)
			id := fieldID(key, produced[key])
			if _, ok := nodes[id]; !ok {
				node, err := fieldNode(id, key, source.Type, input, output, produced[key])
				if err != nil {
					return Graph{}, err
				}
				nodes[id] = node
			}
			edges[Edge{From: id, To: opID}] = struct{}{}
		}
		if len(instruction.Sources) == 0 && instruction.Op == ir.OpMap && instruction.Literal != "" {
			constantID := "constant:" + pathKey(instruction.Destination)
			nodes[constantID] = Node{ID: constantID, Kind: NodeConstant, Field: pathKey(instruction.Destination), Presence: taint.PresencePresent}
			edges[Edge{From: constantID, To: opID}] = struct{}{}
		}

		outputKey := pathKey(instruction.Destination)
		outputID := "output:" + outputKey
		value, ok := output[outputKey]
		if !ok {
			return Graph{}, fmt.Errorf("%w: %s", ErrInvalidGraph, outputKey)
		}
		nodes[outputID] = Node{
			ID:             outputID,
			Kind:           NodeOutput,
			Field:          outputKey,
			Classification: value.Classification,
			Presence:       value.Presence,
			Taint:          append([]string(nil), value.Taint...),
			Reason:         value.Reason,
		}
		edges[Edge{From: opID, To: outputID}] = struct{}{}
	}

	graph := Graph{Nodes: make([]Node, 0, len(nodes)), Edges: make([]Edge, 0, len(edges))}
	for _, node := range nodes {
		node.Taint = sortedUnique(node.Taint)
		graph.Nodes = append(graph.Nodes, node)
	}
	for edge := range edges {
		graph.Edges = append(graph.Edges, edge)
	}
	for _, instruction := range program.Instructions {
		graph.Outputs = append(graph.Outputs, "output:"+pathKey(instruction.Destination))
	}
	canonicalize(&graph)
	if err := graph.Validate(); err != nil {
		return Graph{}, err
	}
	return graph, nil
}

func fieldNode(id, key string, typ transformation.Type, input, output taint.Record, produced ir.Instruction) (Node, error) {
	if produced.Destination.Field != "" {
		value, ok := output[key]
		if !ok {
			return Node{}, fmt.Errorf("%w: intermediate output %s", ErrInvalidGraph, key)
		}
		return Node{ID: id, Kind: NodeOutput, Field: key, Classification: value.Classification, Presence: value.Presence, Taint: append([]string(nil), value.Taint...), Reason: value.Reason}, nil
	}
	value, ok := input[key]
	if !ok {
		return Node{ID: id, Kind: NodeSource, Field: key, Presence: taint.PresenceAbsent, Classification: taint.ClassificationPublic, Taint: []string{"source:" + key}}, nil
	}
	if err := value.Validate(); err != nil {
		return Node{}, fmt.Errorf("%s: %w", key, err)
	}
	if value.Type != typ {
		return Node{}, fmt.Errorf("%w: source %s has type %s, want %s", ErrInvalidGraph, key, value.Type, typ)
	}
	return Node{ID: id, Kind: NodeSource, Field: key, Classification: value.Classification, Presence: value.Presence, Taint: append([]string(nil), value.Taint...), Reason: value.Reason}, nil
}

func fieldID(key string, produced ir.Instruction) string {
	if produced.Destination.Field != "" {
		return "output:" + key
	}
	return "source:" + key
}

func operationID(instruction ir.Instruction) string {
	return "operation:" + string(instruction.Op) + ":" + pathKey(instruction.Destination)
}

func pathKey(path transformation.Path) string { return path.Schema + "." + path.Field }

// Validate checks graph shape, edge endpoints, and the source/constant
// invariant required by XFORM-007.
func (g Graph) Validate() error {
	byID := make(map[string]Node, len(g.Nodes))
	for _, node := range g.Nodes {
		if node.ID == "" || node.Kind == "" || node.Field == "" {
			return fmt.Errorf("%w: malformed node", ErrInvalidGraph)
		}
		if _, exists := byID[node.ID]; exists {
			return fmt.Errorf("%w: duplicate node %s", ErrInvalidGraph, node.ID)
		}
		if node.Kind != NodeSource && node.Kind != NodeConstant && node.Kind != NodeOperation && node.Kind != NodeOutput {
			return fmt.Errorf("%w: unknown node kind %s", ErrInvalidGraph, node.Kind)
		}
		byID[node.ID] = node
	}
	for _, edge := range g.Edges {
		if _, ok := byID[edge.From]; !ok {
			return fmt.Errorf("%w: edge source %s is missing", ErrInvalidGraph, edge.From)
		}
		if _, ok := byID[edge.To]; !ok {
			return fmt.Errorf("%w: edge destination %s is missing", ErrInvalidGraph, edge.To)
		}
	}
	for _, outputID := range g.Outputs {
		node, ok := byID[outputID]
		if !ok || node.Kind != NodeOutput {
			return fmt.Errorf("%w: declared output %s is missing", ErrInvalidGraph, outputID)
		}
		if !hasSource(outputID, byID, reverseEdges(g.Edges), make(map[string]bool)) {
			return fmt.Errorf("%w: %s", ErrMissingSource, node.Field)
		}
	}
	return nil
}

func hasSource(id string, nodes map[string]Node, parents map[string][]string, seen map[string]bool) bool {
	if seen[id] {
		return false
	}
	seen[id] = true
	node := nodes[id]
	if node.Kind == NodeSource || node.Kind == NodeConstant {
		return true
	}
	for _, parent := range parents[id] {
		if hasSource(parent, nodes, parents, seen) {
			return true
		}
	}
	return false
}

// SourcePath returns one deterministic, flattened path from an output back to
// its source fields.  For a multi-source operation the operation is followed
// by sources in lexical order.  Values are never returned.
func (g Graph) SourcePath(output string) ([]string, error) {
	outputID := output
	if !strings.HasPrefix(outputID, "output:") {
		outputID = "output:" + outputID
	}
	byID := make(map[string]Node, len(g.Nodes))
	for _, node := range g.Nodes {
		byID[node.ID] = node
	}
	if node, ok := byID[outputID]; !ok || node.Kind != NodeOutput {
		return nil, fmt.Errorf("%w: %s", ErrUnknownOutput, output)
	}
	parents := reverseEdges(g.Edges)
	path := []string{outputID}
	seen := map[string]bool{outputID: true}
	queue := append([]string(nil), parents[outputID]...)
	sort.Strings(queue)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		path = append(path, id)
		if byID[id].Kind == NodeSource || byID[id].Kind == NodeConstant {
			continue
		}
		parentsOf := append([]string(nil), parents[id]...)
		sort.Strings(parentsOf)
		queue = append(queue, parentsOf...)
	}
	return path, nil
}

// Path is a concise alias for SourcePath.
func (g Graph) Path(output string) ([]string, error) { return g.SourcePath(output) }

// SourcePathEdges reports the graph edges reachable backwards from an output.
// It is primarily useful to validators and remains value-free.
func (g Graph) SourcePathEdges(output string) []Edge {
	parents := reverseEdges(g.Edges)
	seen := map[string]bool{}
	queue := []string{output}
	var result []Edge
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		for _, parent := range parents[id] {
			result = append(result, Edge{From: parent, To: id})
			queue = append(queue, parent)
		}
	}
	return result
}

func reverseEdges(edges []Edge) map[string][]string {
	parents := make(map[string][]string)
	for _, edge := range edges {
		parents[edge.To] = append(parents[edge.To], edge.From)
	}
	return parents
}

// Digest returns the canonical graph digest.  Invalid graphs return the empty
// string; DigestChecked is available when the caller needs the error.
func (g Graph) Digest() string {
	digest, _ := g.DigestChecked()
	return digest
}

// DigestChecked returns the canonical sha256 graph digest.
func (g Graph) DigestChecked() (string, error) {
	if err := g.Validate(); err != nil {
		return "", err
	}
	c := cloneGraph(g)
	canonicalize(&c)
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(h[:]), nil
}

// Digest is the package-level form for callers that prefer functional APIs.
func Digest(g Graph) (string, error) { return g.DigestChecked() }

// Explain renders a stable, value-free explanation of a graph.
func Explain(g Graph) string { return g.Explain() }

// Explain renders the graph as a readable source-to-operation-to-output plan.
func (g Graph) Explain() string {
	var b strings.Builder
	fmt.Fprintf(&b, "transformation lineage v%d (%s)", Version(), g.Digest())
	for _, output := range g.Outputs {
		path, err := g.SourcePath(output)
		if err != nil {
			fmt.Fprintf(&b, "\n  output %s: invalid (%v)", output, err)
			continue
		}
		fmt.Fprintf(&b, "\n  %s <- %s", output, strings.Join(path[1:], " <- "))
	}
	for _, node := range g.Nodes {
		if node.Kind != NodeOperation {
			continue
		}
		fmt.Fprintf(&b, "\n  %s: %s", node.ID, node.Operation)
		if node.Function != "" {
			fmt.Fprintf(&b, " function=%s", node.Function)
		}
		if node.TargetType != "" {
			fmt.Fprintf(&b, " target=%s", node.TargetType)
		}
		if node.LiteralDeclared {
			b.WriteString(" literal=declared")
		}
		if node.Classification != "" {
			fmt.Fprintf(&b, " classification=%s", node.Classification)
		}
		if node.Reason != "" {
			fmt.Fprintf(&b, " decision=%s", node.Reason)
		}
	}
	return b.String()
}

func canonicalize(g *Graph) {
	for i := range g.Nodes {
		g.Nodes[i].Taint = sortedUnique(g.Nodes[i].Taint)
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool {
		if g.Edges[i].From != g.Edges[j].From {
			return g.Edges[i].From < g.Edges[j].From
		}
		return g.Edges[i].To < g.Edges[j].To
	})
	sort.Strings(g.Outputs)
}

func cloneGraph(g Graph) Graph {
	c := Graph{Nodes: append([]Node(nil), g.Nodes...), Edges: append([]Edge(nil), g.Edges...), Outputs: append([]string(nil), g.Outputs...)}
	for i := range c.Nodes {
		c.Nodes[i].Taint = append([]string(nil), c.Nodes[i].Taint...)
	}
	return c
}

func sortedUnique(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := append([]string(nil), items...)
	sort.Strings(out)
	result := out[:0]
	for _, item := range out {
		if len(result) == 0 || result[len(result)-1] != item {
			result = append(result, item)
		}
	}
	return result
}
