// Package effectgraph compiles and checks the semantic dependency graph for
// external effects. It is deliberately pure: compiling a graph performs no
// I/O and does not know about connector queues or provider transports.
package effectgraph

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

type OrderingClass string

const (
	Strict              OrderingClass = "STRICT"
	Commutative         OrderingClass = "COMMUTATIVE"
	Independent         OrderingClass = "INDEPENDENT"
	ProviderConditional OrderingClass = "PROVIDER_CONDITIONAL"
)

type ObservationContract struct {
	Required bool
	Profile  string
}

type EffectNode struct {
	ID                   string
	ProposalRef          string
	TransactionRef       string
	CapabilityRef        string
	Prerequisites        []string
	ResourceKey          string
	OrderingKey          string
	ExpectedVersion      string
	Ordering             OrderingClass
	IdempotencyKey       string
	Critical             bool
	Irreversible         bool
	DispatchCondition    string
	Observation          ObservationContract
	Deadline             string
	FailurePolicy        string
	CompensationPolicy   string
	RepairPolicy         string
	TerminalContribution string
}

type Edge struct{ From, To string }

type Graph struct {
	Nodes []EffectNode
	Edges []Edge
}

type Diagnostic struct{ NodeID, Code, Detail string }

func (d Diagnostic) Error() string {
	if d.NodeID == "" {
		return fmt.Sprintf("effectgraph: %s: %s", d.Code, d.Detail)
	}
	return fmt.Sprintf("effectgraph: %s (%s): %s", d.Code, d.NodeID, d.Detail)
}

// Unwrap lets callers classify a failed compilation without parsing the
// human-readable diagnostic.  A cycle is the one graph-wide sentinel; all
// other diagnostics are malformed graph input.
func (d Diagnostic) Unwrap() error {
	if d.Code == "CYCLE" {
		return ErrCycle
	}
	return ErrInvalidGraph
}

var (
	ErrInvalidGraph = errors.New("effectgraph: invalid graph")
	ErrCycle        = errors.New("effectgraph: dependency cycle")
)

// CompiledGraph is a canonical, immutable graph plus its deterministic digest.
type CompiledGraph struct {
	Nodes  []EffectNode
	Edges  []Edge
	Order  []string
	Digest string
}

func cloneNode(n EffectNode) EffectNode {
	n.Prerequisites = append([]string(nil), n.Prerequisites...)
	return n
}

// Compile validates and canonicalizes g. The returned graph owns all slices.
func Compile(g Graph) (CompiledGraph, error) {
	if len(g.Nodes) == 0 {
		return CompiledGraph{}, Diagnostic{Code: "EMPTY_GRAPH", Detail: "at least one effect is required"}
	}
	nodes := make([]EffectNode, len(g.Nodes))
	ids := make(map[string]bool, len(g.Nodes))
	idempotency := make(map[string]string, len(g.Nodes))
	for i, n := range g.Nodes {
		nodes[i] = cloneNode(n)
		if n.ID == "" {
			return CompiledGraph{}, Diagnostic{Code: "MISSING_NODE_ID", Detail: "effect node has no id"}
		}
		if ids[n.ID] {
			return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: "DUPLICATE_NODE", Detail: "node id is not unique"}
		}
		ids[n.ID] = true
		if n.ProposalRef == "" || n.TransactionRef == "" || n.CapabilityRef == "" {
			return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: "MISSING_LINEAGE", Detail: "proposal, transaction and capability references are required"}
		}
		if n.ResourceKey == "" {
			return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: "MISSING_RESOURCE_KEY", Detail: "external resource key is required"}
		}
		if n.IdempotencyKey == "" {
			return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: "MISSING_IDEMPOTENCY", Detail: "stable idempotency key is required"}
		}
		if prior, ok := idempotency[n.IdempotencyKey]; ok {
			return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: "DUPLICATE_EFFECT_KEY", Detail: fmt.Sprintf("idempotency key is already used by %s", prior)}
		}
		idempotency[n.IdempotencyKey] = n.ID
		if n.DispatchCondition == "" || n.Deadline == "" || n.FailurePolicy == "" || n.CompensationPolicy == "" || n.RepairPolicy == "" || n.TerminalContribution == "" {
			return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: "INCOMPLETE_POLICY", Detail: "dispatch condition, deadline, failure, compensation, repair and terminal policies are required"}
		}
		if !validOrdering(n.Ordering) {
			return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: "INVALID_ORDERING", Detail: string(n.Ordering)}
		}
		if n.Ordering == Strict && n.OrderingKey == "" {
			return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: "MISSING_ORDERING_KEY", Detail: "strict effects require a resource ordering key"}
		}
		if n.Ordering == ProviderConditional && (n.OrderingKey == "" || n.ExpectedVersion == "") {
			return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: "MISSING_PROVIDER_PRECONDITION", Detail: "provider-conditional effects require an expected external version"}
		}
		if !n.Observation.Required || n.Observation.Profile == "" {
			code := "MISSING_OBSERVATION"
			if n.Irreversible {
				code = "IRREVERSIBLE_NEEDS_OBSERVATION"
			}
			return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: code, Detail: "effects require an observation contract"}
		}
		if n.Irreversible && n.RepairPolicy == "NONE" {
			return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: "IRREVERSIBLE_NEEDS_REPAIR", Detail: "irreversible effects require a repair route"}
		}
	}
	edges := make([]Edge, 0, len(g.Edges))
	seenEdge := map[string]bool{}
	for _, n := range nodes {
		for _, p := range n.Prerequisites {
			if !ids[p] {
				return CompiledGraph{}, Diagnostic{NodeID: n.ID, Code: "UNDECLARED_DEPENDENCY", Detail: p}
			}
			k := p + "\x00" + n.ID
			if !seenEdge[k] {
				edges = append(edges, Edge{p, n.ID})
				seenEdge[k] = true
			}
		}
	}
	for _, e := range g.Edges {
		if !ids[e.From] || !ids[e.To] {
			return CompiledGraph{}, Diagnostic{Code: "UNDECLARED_EDGE", Detail: fmt.Sprintf("%s -> %s", e.From, e.To)}
		}
		k := e.From + "\x00" + e.To
		if !seenEdge[k] {
			edges = append(edges, e)
			seenEdge[k] = true
		}
	}
	if err := checkOrdering(nodes); err != nil {
		return CompiledGraph{}, err
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From == edges[j].From {
			return edges[i].To < edges[j].To
		}
		return edges[i].From < edges[j].From
	})
	order, err := topo(nodes, edges)
	if err != nil {
		return CompiledGraph{}, err
	}
	c := CompiledGraph{Nodes: nodes, Edges: edges, Order: order}
	raw, _ := json.Marshal(struct {
		Nodes []EffectNode
		Edges []Edge
		Order []string
	}{nodes, edges, order})
	sum := sha256.Sum256(raw)
	c.Digest = hex.EncodeToString(sum[:])
	return c, nil
}

func validOrdering(o OrderingClass) bool {
	return o == Strict || o == Commutative || o == Independent || o == ProviderConditional
}
func checkOrdering(ns []EffectNode) error {
	for i := range ns {
		for j := i + 1; j < len(ns); j++ {
			a, b := ns[i], ns[j]
			if a.ResourceKey == b.ResourceKey && (a.Ordering == Independent || b.Ordering == Independent) && a.Ordering != b.Ordering {
				return Diagnostic{NodeID: b.ID, Code: "INCOMPATIBLE_ORDERING", Detail: "same resource mixes INDEPENDENT with ordered operation"}
			}
			if a.ResourceKey == b.ResourceKey && a.Ordering == Strict && b.Ordering == Strict && a.OrderingKey != "" && b.OrderingKey != "" && a.OrderingKey != b.OrderingKey {
				return Diagnostic{NodeID: b.ID, Code: "INCOMPATIBLE_ORDERING_KEY", Detail: "strict operations for one resource use different ordering keys"}
			}
			if a.ResourceKey == b.ResourceKey && a.Ordering == ProviderConditional && b.Ordering == ProviderConditional && (a.IdempotencyKey == b.IdempotencyKey) {
				return Diagnostic{NodeID: b.ID, Code: "DUPLICATE_EFFECT_KEY", Detail: "same resource has duplicate idempotency key"}
			}
		}
	}
	return nil
}
func topo(ns []EffectNode, es []Edge) ([]string, error) {
	indeg := map[string]int{}
	out := map[string][]string{}
	for _, n := range ns {
		indeg[n.ID] = 0
	}
	for _, e := range es {
		out[e.From] = append(out[e.From], e.To)
		indeg[e.To]++
	}
	q := []string{}
	for id, d := range indeg {
		if d == 0 {
			q = append(q, id)
		}
	}
	sort.Strings(q)
	order := []string{}
	for len(q) > 0 {
		id := q[0]
		q = q[1:]
		order = append(order, id)
		for _, to := range out[id] {
			indeg[to]--
			if indeg[to] == 0 {
				q = append(q, to)
				sort.Strings(q)
			}
		}
	}
	if len(order) != len(ns) {
		return nil, Diagnostic{Code: "CYCLE", Detail: ErrCycle.Error()}
	}
	return order, nil
}

func (c CompiledGraph) Verify() error {
	got, err := Compile(Graph{Nodes: c.Nodes, Edges: c.Edges})
	if err != nil {
		return err
	}
	if got.Digest != c.Digest {
		return Diagnostic{Code: "DIGEST_MISMATCH", Detail: "compiled graph was modified"}
	}
	return nil
}
