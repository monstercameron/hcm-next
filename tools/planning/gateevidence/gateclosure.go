package gateevidence

import (
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/phaseone"
)

// GateClosureReport is the evidence result together with the independently
// validated dependency closure.  The graph is derived from the signed
// manifest and observed results; callers cannot supply assurance or restore
// flags to make a closure pass.
type GateClosureReport struct {
	*Report
	Graph   phaseone.GateDependencyGraph
	Closure phaseone.AssuranceClosure
}

// CompileGateClosure compiles evidence and then validates the exact gate
// graph represented by that evidence.  The manifest's release selects the
// gate (P1A is Gate A); no caller-provided graph or assurance claim is trusted.
func CompileGateClosure(manifest P1AManifest, results []ResultRecord, opts CompileOptions) (*GateClosureReport, error) {
	report, err := Compile(manifest, results, opts)
	if err != nil {
		return nil, err
	}
	gate, err := gateForRelease(manifest.Release)
	if err != nil {
		return nil, err
	}
	graph, err := graphFromEvidence(manifest, gate)
	if err != nil {
		return nil, err
	}
	return &GateClosureReport{Report: report, Graph: graph, Closure: phaseone.CompileAssuranceClosure(graph, gate)}, nil
}

func gateForRelease(release string) (string, error) {
	switch release {
	case "P1A", "GATE_A":
		return phaseone.GateA, nil
	case "P1B", "GATE_B":
		return phaseone.GateB, nil
	case "GATE_C":
		return phaseone.GateC, nil
	default:
		return "", fmt.Errorf("unsupported gate release %q", release)
	}
}

func graphFromEvidence(manifest P1AManifest, gate string) (phaseone.GateDependencyGraph, error) {
	graph := phaseone.GateDependencyGraph{}
	finalID := "GATE_EVIDENCE:" + manifest.Release
	graph.Nodes = append(graph.Nodes, phaseone.GateNode{ID: finalID, Gate: gate, Kind: phaseone.EvidenceNode, Package: "tools/planning/gateevidence", Selected: true, Final: true})
	todos := map[string]bool{}
	tests := map[string]bool{}
	for i, entry := range manifest.Evidence {
		if entry.TodoID == "" || entry.Test == "" || entry.Package == "" {
			return phaseone.GateDependencyGraph{}, fmt.Errorf("evidence[%d] must name todo_id, test, and package", i)
		}
		todoID := "TODO:" + entry.TodoID
		testID := "TEST:" + entry.Package + ":" + entry.Test
		evidenceID := "EVIDENCE:" + entry.TodoID + ":" + entry.Test
		if !todos[todoID] {
			graph.Nodes = append(graph.Nodes, phaseone.GateNode{ID: todoID, Gate: gate, Kind: phaseone.TodoNode, Selected: true})
			todos[todoID] = true
		}
		if tests[testID] {
			return phaseone.GateDependencyGraph{}, fmt.Errorf("evidence[%d] repeats test %s in package %s", i, entry.Test, entry.Package)
		}
		tests[testID] = true
		graph.Nodes = append(graph.Nodes,
			phaseone.GateNode{ID: testID, Gate: gate, Kind: phaseone.TestNode, Package: entry.Package, Selected: true},
			phaseone.GateNode{ID: evidenceID, Gate: gate, Kind: phaseone.EvidenceNode, Package: entry.Package, Selected: true},
		)
		graph.Edges = append(graph.Edges,
			phaseone.GateEdge{From: finalID, To: evidenceID, Kind: phaseone.EvidenceNode},
			phaseone.GateEdge{From: evidenceID, To: testID, Kind: phaseone.TestNode},
			phaseone.GateEdge{From: testID, To: todoID, Kind: phaseone.TodoNode},
		)
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	sort.Slice(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].From != graph.Edges[j].From {
			return graph.Edges[i].From < graph.Edges[j].From
		}
		return graph.Edges[i].To < graph.Edges[j].To
	})
	return graph, nil
}
