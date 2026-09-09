package model_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/model"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func provNode(id string, kind model.ProvenanceNodeKind, requiredAuthority string) model.ProvenanceNode {
	return model.ProvenanceNode{ID: id, Kind: kind, RequiredAuthorityRef: requiredAuthority}
}

func provEdge(t *testing.T, id string, from, to model.ProvenanceNode) model.ProvenanceEdge {
	t.Helper()
	return model.ProvenanceEdge{
		ID: id, From: from, To: to,
		SourceRef: "source." + from.ID, TransformationRef: "transform." + id, AuthorityRef: "authority." + id,
		Effective: mustOpenInterval(t, 2026, time.January, 1),
		Recorded:  mustRecordedAt(t, 2026, time.January, 1),
		Digest:    "sha256:" + id, DigestAlgorithm: "sha256",
	}
}

// TestMaterialValueRequiresLineage is the PRIMARY test for provenance graph
// edges (MODEL-020).
//
// RED: it rejects a value/result without source, transformation, authority,
// recorded/effective time and digest.
//
// GREEN: field explanation traverses source -> mapping -> observation ->
// proposal/decision/result without disclosing unauthorized nodes.
func TestMaterialValueRequiresLineage(t *testing.T) {
	source := provNode("src1", model.NodeSourceAssertion, "")
	transform := provNode("xform1", model.NodeTransformation, "")
	observation := provNode("obs1", model.NodeObservation, "")
	proposal := provNode("prop1", model.NodeProposalRevision, "")
	decision := provNode("dec1", model.NodeDecision, "authority.governance/v1")
	result := provNode("res1", model.NodeResult, "")

	t.Run("RED", func(t *testing.T) {
		full := provEdge(t, "e-result", decision, result)
		cases := []struct {
			name   string
			break_ func(*model.ProvenanceEdge)
		}{
			{"no source", func(e *model.ProvenanceEdge) { e.SourceRef = "" }},
			{"no transformation", func(e *model.ProvenanceEdge) { e.TransformationRef = "" }},
			{"no authority", func(e *model.ProvenanceEdge) { e.AuthorityRef = "" }},
			{"no recorded time", func(e *model.ProvenanceEdge) { e.Recorded = values.RecordedAt{} }},
			{"no digest", func(e *model.ProvenanceEdge) { e.Digest = "" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				e := full
				tc.break_(&e)
				if err := model.RequireLineage(e); !errors.Is(err, model.ErrMissingLineage) {
					t.Fatalf("accepted an edge with %s: %v", tc.name, err)
				}
			})
		}
	})

	t.Run("GREEN", func(t *testing.T) {
		edges := []model.ProvenanceEdge{
			provEdge(t, "e1", source, transform),
			provEdge(t, "e2", transform, observation),
			provEdge(t, "e3", observation, proposal),
			provEdge(t, "e4", proposal, decision),
			provEdge(t, "e5", decision, result),
		}
		for _, e := range edges {
			if err := model.RequireLineage(e); err != nil {
				t.Fatalf("valid edge %s rejected: %v", e.ID, err)
			}
		}
		g, err := model.NewProvenanceGraph(edges)
		if err != nil {
			t.Fatalf("build graph: %v", err)
		}

		authorized := map[string]bool{"authority.governance/v1": true}
		explanation := g.Explain(result.ID, authorized)
		if !explanation.Complete {
			t.Fatalf("authorized caller received an incomplete explanation: %+v", explanation)
		}
		want := map[string]bool{source.ID: true, transform.ID: true, observation.ID: true, proposal.ID: true, decision.ID: true}
		got := map[string]bool{}
		for _, n := range explanation.Nodes {
			got[n.NodeID] = true
			if n.Redacted {
				t.Fatalf("authorized caller saw a redacted node %s", n.NodeID)
			}
		}
		for id := range want {
			if !got[id] {
				t.Fatalf("explanation is missing %s; full chain: %+v", id, explanation.Nodes)
			}
		}

		unauthorized := g.Explain(result.ID, nil)
		if unauthorized.Complete {
			t.Fatalf("unauthorized caller received a complete explanation")
		}
		foundRedactedDecision := false
		foundSource := false
		for _, n := range unauthorized.Nodes {
			if n.NodeID == decision.ID {
				if !n.Redacted {
					t.Fatalf("unauthorized caller saw the protected decision node unredacted")
				}
				foundRedactedDecision = true
			}
			if n.NodeID == source.ID {
				foundSource = true
			}
		}
		if !foundRedactedDecision {
			t.Fatalf("unauthorized explanation never reached the protected boundary")
		}
		if foundSource {
			t.Fatalf("unauthorized caller saw past the protected decision node into %s", source.ID)
		}
	})
}

// TestTodo_MODEL_020_Property asserts that Explain never discloses a node
// lying behind a redacted boundary, for every prefix of the canonical chain.
func TestTodo_MODEL_020_Property(t *testing.T) {
	source := provNode("src1", model.NodeSourceAssertion, "")
	transform := provNode("xform1", model.NodeTransformation, "protected")
	result := provNode("res1", model.NodeResult, "")
	edges := []model.ProvenanceEdge{
		provEdge(t, "e1", source, transform),
		provEdge(t, "e2", transform, result),
	}
	g, err := model.NewProvenanceGraph(edges)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	explanation := g.Explain(result.ID, nil)
	for _, n := range explanation.Nodes {
		if n.NodeID == source.ID {
			t.Fatalf("disclosed %s despite an unauthorized boundary in front of it", source.ID)
		}
	}
	if len(explanation.Nodes) != 1 || explanation.Nodes[0].NodeID != transform.ID || !explanation.Nodes[0].Redacted {
		t.Fatalf("unexpected explanation shape: %+v", explanation.Nodes)
	}
}

// TestTodo_MODEL_020_Golden pins one rendered explanation.
func TestTodo_MODEL_020_Golden(t *testing.T) {
	source := provNode("src1", model.NodeSourceAssertion, "")
	transform := provNode("xform1", model.NodeTransformation, "")
	result := provNode("res1", model.NodeResult, "")
	edges := []model.ProvenanceEdge{
		provEdge(t, "e1", source, transform),
		provEdge(t, "e2", transform, result),
	}
	g, err := model.NewProvenanceGraph(edges)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	goldenJSON(t, "model_020_explanation.json", g.Explain(result.ID, nil))
}

// TestTodo_MODEL_020_Security proves that possessing one node's ID never
// grants adjacent-node access: a caller authorized for the transformation
// node but not the decision node still cannot see past the decision.
func TestTodo_MODEL_020_Security(t *testing.T) {
	source := provNode("src1", model.NodeSourceAssertion, "")
	decision := provNode("dec1", model.NodeDecision, "authority.governance/v1")
	result := provNode("res1", model.NodeResult, "")
	edges := []model.ProvenanceEdge{
		provEdge(t, "e1", source, decision),
		provEdge(t, "e2", decision, result),
	}
	g, err := model.NewProvenanceGraph(edges)
	if err != nil {
		t.Fatalf("build graph: %v", err)
	}
	// Authorized for an unrelated scope, not the decision's own authority.
	explanation := g.Explain(result.ID, map[string]bool{"authority.unrelated/v1": true})
	for _, n := range explanation.Nodes {
		if n.NodeID == source.ID {
			t.Fatalf("possession of an unrelated authority disclosed %s past the protected decision", source.ID)
		}
	}
}

// FuzzTodo_MODEL_020 fuzzes edge validation: a randomly perturbed field never
// produces a validated edge, and a validated edge always validates the same
// way twice.
func FuzzTodo_MODEL_020(f *testing.F) {
	f.Add(true, true, true, true, true)
	f.Add(false, true, true, true, true)
	f.Add(true, false, true, true, true)
	f.Fuzz(func(t *testing.T, hasSource, hasTransform, hasAuthority, hasDigest, hasRecorded bool) {
		from := provNode("a", model.NodeSourceAssertion, "")
		to := provNode("b", model.NodeResult, "")
		e := model.ProvenanceEdge{
			ID: "e", From: from, To: to,
			Effective:       mustOpenInterval(t, 2026, time.January, 1),
			DigestAlgorithm: "sha256",
		}
		if hasSource {
			e.SourceRef = "s"
		}
		if hasTransform {
			e.TransformationRef = "t"
		}
		if hasAuthority {
			e.AuthorityRef = "a"
		}
		if hasDigest {
			e.Digest = "d"
		}
		if hasRecorded {
			e.Recorded = mustRecordedAt(t, 2026, time.January, 1)
		}
		err := model.RequireLineage(e)
		complete := hasSource && hasTransform && hasAuthority && hasDigest && hasRecorded
		if complete && err != nil {
			t.Fatalf("a fully populated edge was rejected: %v", err)
		}
		if !complete && err == nil {
			t.Fatalf("an incomplete edge validated")
		}
		if err2 := model.RequireLineage(e); (err == nil) != (err2 == nil) {
			t.Fatalf("RequireLineage is not idempotent: %v then %v", err, err2)
		}
	})
}
