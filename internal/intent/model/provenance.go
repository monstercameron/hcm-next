package model

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ProvenanceNodeKind is a scoped subset of the node types
// planning/specs/provenance-graph-and-lineage.md declares, limited to the
// Gate A source -> mapping -> normalized fact -> simulation -> proposal
// lineage plus the decision/result nodes the fourteen definitions need.
type ProvenanceNodeKind string

// Provenance node kinds.
const (
	NodeSourceAssertion  ProvenanceNodeKind = "SOURCE_ASSERTION"
	NodeTransformation   ProvenanceNodeKind = "TRANSFORMATION"
	NodeObservation      ProvenanceNodeKind = "OBSERVATION"
	NodeProposalRevision ProvenanceNodeKind = "PROPOSAL_REVISION"
	NodeDecision         ProvenanceNodeKind = "DECISION"
	NodeResult           ProvenanceNodeKind = "RESULT"
)

// Valid reports whether k is one of the six declared node kinds.
func (k ProvenanceNodeKind) Valid() bool {
	switch k {
	case NodeSourceAssertion, NodeTransformation, NodeObservation, NodeProposalRevision, NodeDecision, NodeResult:
		return true
	default:
		return false
	}
}

// ProvenanceNode is one stable-ID node in the lineage graph.
type ProvenanceNode struct {
	ID   string
	Kind ProvenanceNodeKind

	// RequiredAuthorityRef gates disclosure: a caller lacking this authority
	// sees the node as an opaque, redacted boundary rather than nothing at
	// all (planning/specs/provenance-graph-and-lineage.md, "Security,
	// Consistency, and Evidence"). Empty means no additional authority is
	// required beyond tenant membership.
	RequiredAuthorityRef string
}

// Validate rejects a malformed node.
func (n ProvenanceNode) Validate() error {
	if n.ID == "" {
		return newError("ProvenanceNode.Validate", "id", ErrMissingLineage, "node carries no id")
	}
	if !n.Kind.Valid() {
		return newError("ProvenanceNode.Validate", "kind", ErrMissingLineage,
			"%s has kind %q, outside the declared node kinds", n.ID, n.Kind)
	}
	return nil
}

// ProvenanceEdge asserts that a material value or result (To) was produced
// USED/DERIVED_FROM/GENERATED (From) through a named transformation, under a
// named authority, with a recorded/effective time and a content digest
// (MODEL-020).
type ProvenanceEdge struct {
	ID   string
	From ProvenanceNode
	To   ProvenanceNode

	SourceRef         string
	TransformationRef string
	AuthorityRef      string

	Effective values.EffectiveInterval
	Recorded  values.RecordedAt

	Digest          string
	DigestAlgorithm string
}

// Validate is [RequireLineage]: it rejects a material value or result edge
// missing its source, transformation, authority, recorded/effective time or
// digest.
func (e ProvenanceEdge) Validate() error {
	if e.ID == "" {
		return newError("ProvenanceEdge.Validate", "id", ErrMissingLineage, "edge carries no id")
	}
	if err := e.From.Validate(); err != nil {
		return newError("ProvenanceEdge.Validate", "from", ErrMissingLineage, "%s: %v", e.ID, err)
	}
	if err := e.To.Validate(); err != nil {
		return newError("ProvenanceEdge.Validate", "to", ErrMissingLineage, "%s: %v", e.ID, err)
	}
	if e.SourceRef == "" {
		return newError("ProvenanceEdge.Validate", "source_ref", ErrMissingLineage,
			"%s names no source", e.ID)
	}
	if e.TransformationRef == "" {
		return newError("ProvenanceEdge.Validate", "transformation_ref", ErrMissingLineage,
			"%s names no transformation", e.ID)
	}
	if e.AuthorityRef == "" {
		return newError("ProvenanceEdge.Validate", "authority_ref", ErrMissingLineage,
			"%s names no authority", e.ID)
	}
	if err := e.Effective.Validate(); err != nil {
		return newError("ProvenanceEdge.Validate", "effective", ErrMissingLineage,
			"%s has no valid effective time: %v", e.ID, err)
	}
	if !e.Recorded.Instant().IsSet() {
		return newError("ProvenanceEdge.Validate", "recorded", ErrMissingLineage,
			"%s has no recorded time", e.ID)
	}
	if e.Digest == "" || e.DigestAlgorithm == "" {
		return newError("ProvenanceEdge.Validate", "digest", ErrMissingLineage,
			"%s carries no digest", e.ID)
	}
	return nil
}

// RequireLineage rejects a material value or result edge missing complete
// lineage. It is the direct enforcement point behind
// TestMaterialValueRequiresLineage.
func RequireLineage(e ProvenanceEdge) error { return e.Validate() }

// ProvenanceGraph is an immutable, indexed set of provenance edges.
type ProvenanceGraph struct {
	edges []ProvenanceEdge
	byTo  map[string][]ProvenanceEdge
}

// NewProvenanceGraph validates and indexes edges.
func NewProvenanceGraph(edges []ProvenanceEdge) (*ProvenanceGraph, error) {
	g := &ProvenanceGraph{byTo: map[string][]ProvenanceEdge{}}
	for _, e := range edges {
		if err := e.Validate(); err != nil {
			return nil, err
		}
		g.edges = append(g.edges, e)
		g.byTo[e.To.ID] = append(g.byTo[e.To.ID], e)
	}
	for k := range g.byTo {
		sort.Slice(g.byTo[k], func(i, j int) bool { return g.byTo[k][i].ID < g.byTo[k][j].ID })
	}
	return g, nil
}

// ExplanationNode is one node in a rendered explanation path.
type ExplanationNode struct {
	NodeID   string
	Kind     ProvenanceNodeKind
	Redacted bool
}

// Explanation is the answer to "how was this value produced?": the ordered
// set of nodes reachable backward from the result, redacted at any boundary
// the caller is not authorized to cross.
type Explanation struct {
	ResultNodeID string
	Nodes        []ExplanationNode
	Complete     bool
}

// Explain traverses backward from resultNodeID through USED/DERIVED_FROM
// predecessor edges, returning every node the caller is authorized to see.
// A node whose RequiredAuthorityRef is not in authorized becomes an opaque,
// redacted boundary marker; traversal does not continue past it, so a
// redacted node never discloses what lies behind it (MODEL-020 GREEN).
func (g *ProvenanceGraph) Explain(resultNodeID string, authorized map[string]bool) Explanation {
	visited := map[string]bool{}
	var nodes []ExplanationNode
	complete := true

	var walk func(id string)
	walk = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		preds := g.byTo[id]
		for _, e := range preds {
			redacted := e.From.RequiredAuthorityRef != "" && !authorized[e.From.RequiredAuthorityRef]
			nodes = append(nodes, ExplanationNode{NodeID: e.From.ID, Kind: e.From.Kind, Redacted: redacted})
			if redacted {
				complete = false
				continue
			}
			walk(e.From.ID)
		}
	}
	walk(resultNodeID)

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })
	if len(g.byTo[resultNodeID]) == 0 {
		complete = false
	}
	return Explanation{ResultNodeID: resultNodeID, Nodes: nodes, Complete: complete}
}
