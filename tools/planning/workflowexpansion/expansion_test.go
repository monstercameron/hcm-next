package workflowexpansion

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
)

func fixtureRecipe() Recipe {
	return Recipe{
		Archetype: "A2",
		Phases: []string{
			"resolve current fact and expected version",
			"approve when policy requires",
			"atomic end and append revision with evidence",
			"observe and reconcile effects",
			"close the revision",
		},
	}
}

func fixtureProfile() Profile {
	return Profile{Code: "PPL", Reads: "person facts", Writes: "people facts", Closure: "reconciliation"}
}

func fixtureRecord() workflowdesign.DesignRecord {
	return workflowdesign.DesignRecord{
		Intent:         "ChangeManager",
		Definition:     "hcmnext.people.change_manager/v1",
		Disposition:    workflowdesign.DispositionWorkflow,
		Archetype:      "A2",
		DomainProfile:  "PPL",
		InputBoundary:  "manager-edge-proposal",
		SnapshotPolicy: "authority-snapshot",
		Engines:        workflowdesign.Dimension{Items: []string{"identity-resolution"}},
		HumanWork:      workflowdesign.Dimension{Value: "manager-self-service"},
		Writes:         workflowdesign.Dimension{Value: "manager-edge-replacement"},
		Waits:          workflowdesign.Dimension{Value: "approval-window"},
		Invalidators:   workflowdesign.Dimension{Value: "org-reparent"},
		Reconciliation: workflowdesign.Dimension{Value: "relationship-access"},
		Correction:     workflowdesign.Dimension{Value: "revoke-and-replace"},
		Completion:     "edge-observed",
	}
}

func fixtureInput() Input {
	return Input{Recipe: fixtureRecipe(), Profile: fixtureProfile(), Record: fixtureRecord()}
}

func findingCodes(findings []Finding) map[string]int {
	codes := make(map[string]int)
	for _, finding := range findings {
		codes[finding.Code]++
	}
	return codes
}

// TestWorkflowDesignExpansionProducesCompleteOrderedResponsibilities is the
// WF-DISC-007 primary oracle: the recipe expands completely and in order
// with typed nodes, sequenced edges, ordered effects and a stable digest,
// while the RED shapes fail closed without a partial graph.
func TestWorkflowDesignExpansionProducesCompleteOrderedResponsibilities(t *testing.T) {
	graph, findings := Expand(fixtureInput())
	if len(findings) > 0 {
		t.Fatalf("clean expansion raised findings: %+v", findings)
	}
	if graph == nil {
		t.Fatal("clean expansion produced no graph")
	}
	if len(graph.Nodes) != 5 {
		t.Fatalf("nodes = %d, want every recipe phase expanded", len(graph.Nodes))
	}
	wantKinds := []NodeKind{KindObservation, KindGovernance, KindEffect, KindObservation, KindCompletion}
	for i, node := range graph.Nodes {
		if node.Seq != i {
			t.Errorf("node %d has seq %d", i, node.Seq)
		}
		if node.Kind != wantKinds[i] {
			t.Errorf("node %d kind = %s, want %s (%q)", i, node.Kind, wantKinds[i], node.Responsibility)
		}
		if node.Origin != OriginInherited {
			t.Errorf("node %d origin = %s, want INHERITED", i, node.Origin)
		}
	}
	for i, phase := range fixtureRecipe().Phases {
		if graph.Nodes[i].Responsibility != phase {
			t.Errorf("node %d lost its recipe phase: %q", i, graph.Nodes[i].Responsibility)
		}
	}
	sequence := 0
	repair := 0
	for _, edge := range graph.Edges {
		switch edge.Kind {
		case EdgeSequence:
			sequence++
			if edge.To != edge.From+1 {
				t.Errorf("sequence edge %+v skips a node", edge)
			}
		case EdgeRepair:
			repair++
			if graph.Nodes[edge.To].Kind != KindCompletion {
				t.Errorf("repair edge %+v misses the completion node", edge)
			}
		default:
			t.Errorf("edge %+v has an unknown kind", edge)
		}
	}
	if sequence != 4 {
		t.Errorf("sequence edges = %d, want the complete chain", sequence)
	}
	if repair != 2 {
		t.Errorf("repair edges = %d, want effect plus reconciliation coverage", repair)
	}
	if len(graph.Effects) != 1 {
		t.Fatalf("effects = %d, want the record write ordered", len(graph.Effects))
	}
	effect := graph.Effects[0]
	if effect.Node != 2 || effect.Order != 0 || effect.Description != "manager-edge-replacement" {
		t.Errorf("effect misplaced: %+v", effect)
	}
	if graph.Completion.Policy != "edge-observed" || graph.Completion.TerminalSeq != 4 {
		t.Errorf("completion policy misbound: %+v", graph.Completion)
	}
	if graph.Digest == "" {
		t.Error("expansion carries no digest")
	}
	if validate := graph.Validate(); len(validate) > 0 {
		t.Errorf("clean graph fails validation: %+v", validate)
	}

	omitMandatory := fixtureInput()
	omitMandatory.Delta.Omit = []OmittedPhase{{Seq: 1, Reason: "faster"}}
	if graph, findings := Expand(omitMandatory); graph != nil || findingCodes(findings)[MandatoryOmission] == 0 {
		t.Errorf("mandatory omission produced a graph or no finding: %+v", findings)
	}
	replaceMandatory := fixtureInput()
	replaceMandatory.Delta.Replace = []ReplacedPhase{{Seq: 3, With: "glance at effects", Reason: "simpler"}}
	if graph, findings := Expand(replaceMandatory); graph != nil || findingCodes(findings)[MandatoryReplacement] == 0 {
		t.Errorf("mandatory replacement produced a graph or no finding: %+v", findings)
	}
	directWrites := fixtureInput()
	directWrites.Recipe = Recipe{Archetype: "D1", Phases: []string{
		"authenticate purpose and scope",
		"authorize fields and population",
		"execute deterministic read",
		"return typed result with trace",
	}}
	directWrites.Record.Disposition = workflowdesign.DispositionDirect
	directWrites.Record.Archetype = "D1"
	if graph, findings := Expand(directWrites); graph != nil || findingCodes(findings)[UnorderableEffect] == 0 {
		t.Errorf("DIRECT writes produced a graph or no finding: %+v", findings)
	}
	broken := *graph
	for i, edge := range broken.Edges {
		if edge.Kind == EdgeSequence && edge.From == 2 {
			broken.Edges = append(append([]Edge(nil), broken.Edges[:i]...), broken.Edges[i+1:]...)
			break
		}
	}
	if validate := broken.Validate(); findingCodes(validate)[UnreachableNode] == 0 {
		t.Errorf("severed graph validates: %+v", validate)
	}
}

func TestTodo_WF_DISC_007_Property(t *testing.T) {
	t.Run("adds splice after their anchor", func(t *testing.T) {
		in := fixtureInput()
		in.Delta.Add = []AddedPhase{{AfterSeq: 1, Phase: "notify the worker"}}
		graph, findings := Expand(in)
		if len(findings) > 0 {
			t.Fatalf("add raised findings: %+v", findings)
		}
		if len(graph.Nodes) != 6 || graph.Nodes[2].Responsibility != "notify the worker" || graph.Nodes[2].Origin != OriginAdded {
			t.Fatalf("add misplaced: %+v", graph.Nodes)
		}
	})
	t.Run("justified omits are reported", func(t *testing.T) {
		in := fixtureInput()
		in.Delta.Omit = []OmittedPhase{{Seq: 0, Reason: "fact pinned upstream"}}
		graph, findings := Expand(in)
		if len(findings) > 0 {
			t.Fatalf("justified omit raised findings: %+v", findings)
		}
		if len(graph.Nodes) != 4 || len(graph.Omitted) != 1 || graph.Omitted[0].Reason != "fact pinned upstream" {
			t.Fatalf("omit misreported: %+v %+v", graph.Nodes, graph.Omitted)
		}
	})
	t.Run("bare omits fail", func(t *testing.T) {
		in := fixtureInput()
		in.Delta.Omit = []OmittedPhase{{Seq: 0}}
		if graph, findings := Expand(in); graph != nil || findingCodes(findings)[BareOmission] == 0 {
			t.Errorf("bare omit produced a graph or no finding: %+v", findings)
		}
	})
	t.Run("non-mandatory replace holds", func(t *testing.T) {
		in := fixtureInput()
		in.Delta.Replace = []ReplacedPhase{{Seq: 0, With: "resolve pinned fact snapshot", Reason: "pinned upstream"}}
		graph, findings := Expand(in)
		if len(findings) > 0 {
			t.Fatalf("replace raised findings: %+v", findings)
		}
		if graph.Nodes[0].Responsibility != "resolve pinned fact snapshot" || graph.Nodes[0].Origin != OriginReplaced {
			t.Errorf("replace misapplied: %+v", graph.Nodes[0])
		}
	})
	t.Run("bare replace fails", func(t *testing.T) {
		in := fixtureInput()
		in.Delta.Replace = []ReplacedPhase{{Seq: 0, With: "resolve pinned fact snapshot"}}
		if graph, findings := Expand(in); graph != nil || findingCodes(findings)[BareReplacement] == 0 {
			t.Errorf("bare replace produced a graph or no finding: %+v", findings)
		}
	})
	t.Run("dangling references fail", func(t *testing.T) {
		in := fixtureInput()
		in.Delta.Add = []AddedPhase{{AfterSeq: 9, Phase: "nowhere"}}
		if _, findings := Expand(in); findingCodes(findings)[DanglingAdd] == 0 {
			t.Errorf("dangling add accepted: %+v", findings)
		}
		omit := fixtureInput()
		omit.Delta.Omit = []OmittedPhase{{Seq: 9, Reason: "nope"}}
		if _, findings := Expand(omit); findingCodes(findings)[DanglingOmit] == 0 {
			t.Errorf("dangling omit accepted: %+v", findings)
		}
		replace := fixtureInput()
		replace.Delta.Replace = []ReplacedPhase{{Seq: 9, With: "x", Reason: "y"}}
		if _, findings := Expand(replace); findingCodes(findings)[DanglingReplace] == 0 {
			t.Errorf("dangling replace accepted: %+v", findings)
		}
	})
	t.Run("empty phases fail", func(t *testing.T) {
		in := fixtureInput()
		in.Delta.Add = []AddedPhase{{AfterSeq: 1, Phase: "   "}}
		if _, findings := Expand(in); findingCodes(findings)[EmptyPhase] == 0 {
			t.Errorf("empty add accepted: %+v", findings)
		}
	})
	t.Run("duplicate omits fail", func(t *testing.T) {
		in := fixtureInput()
		in.Delta.Omit = []OmittedPhase{{Seq: 0, Reason: "a"}, {Seq: 0, Reason: "b"}}
		if _, findings := Expand(in); findingCodes(findings)[DuplicateOmit] == 0 {
			t.Errorf("duplicate omit accepted: %+v", findings)
		}
	})
	t.Run("mismatched recipe and profile fail", func(t *testing.T) {
		in := fixtureInput()
		in.Recipe.Archetype = "Z9"
		if _, findings := Expand(in); findingCodes(findings)[ArchetypeMismatch] == 0 {
			t.Errorf("mismatched recipe accepted: %+v", findings)
		}
		prof := fixtureInput()
		prof.Profile.Code = "ZZ"
		if _, findings := Expand(prof); findingCodes(findings)[ProfileMismatch] == 0 {
			t.Errorf("mismatched profile accepted: %+v", findings)
		}
	})
	t.Run("effects cycle across effect nodes", func(t *testing.T) {
		in := fixtureInput()
		in.Record.Writes = workflowdesign.Dimension{Items: []string{"first-write", "second-write"}}
		graph, findings := Expand(in)
		if len(findings) > 0 {
			t.Fatalf("multi-effect raised findings: %+v", findings)
		}
		if len(graph.Effects) != 2 || graph.Effects[0].Order != 0 || graph.Effects[1].Order != 1 {
			t.Fatalf("effects unordered: %+v", graph.Effects)
		}
	})
}

func TestTodo_WF_DISC_007_Golden(t *testing.T) {
	graph, findings := Expand(fixtureInput())
	if len(findings) > 0 {
		t.Fatalf("golden input raised findings: %+v", findings)
	}
	got, err := MarshalGraph(graph, findings)
	if err != nil {
		t.Fatal(err)
	}
	const goldenPath = "testdata/golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_WF_DISC_007_Fault(t *testing.T) {
	t.Run("empty recipe fails closed", func(t *testing.T) {
		in := fixtureInput()
		in.Recipe.Phases = nil
		if graph, findings := Expand(in); graph != nil || findingCodes(findings)[EmptyRecipe] == 0 {
			t.Errorf("empty recipe produced a graph or no finding: %+v", findings)
		}
	})
	t.Run("blank recipe phase fails closed", func(t *testing.T) {
		in := fixtureInput()
		in.Recipe.Phases[2] = "   "
		if graph, findings := Expand(in); graph != nil || findingCodes(findings)[EmptyRecipe] == 0 {
			t.Errorf("blank phase produced a graph or no finding: %+v", findings)
		}
	})
	t.Run("add after an omitted anchor fails closed", func(t *testing.T) {
		in := fixtureInput()
		in.Delta.Omit = []OmittedPhase{{Seq: 0, Reason: "pinned upstream"}}
		in.Delta.Add = []AddedPhase{{AfterSeq: 0, Phase: "late check"}}
		if graph, findings := Expand(in); graph != nil || findingCodes(findings)[DanglingAdd] == 0 {
			t.Errorf("orphaned add produced a graph or no finding: %+v", findings)
		}
	})
	t.Run("severed repair branch is unreachable", func(t *testing.T) {
		graph, findings := Expand(fixtureInput())
		if len(findings) > 0 {
			t.Fatalf("golden input raised findings: %+v", findings)
		}
		broken := *graph
		kept := broken.Edges[:0:0]
		for _, edge := range broken.Edges {
			if edge.Kind == EdgeRepair {
				continue
			}
			if edge.Kind == EdgeSequence && edge.From == 3 {
				continue
			}
			kept = append(kept, edge)
		}
		broken.Edges = kept
		if validate := broken.Validate(); findingCodes(validate)[UnreachableNode] == 0 {
			t.Errorf("repairless tail validates: %+v", validate)
		}
	})
	t.Run("malformed recipe markdown fails closed", func(t *testing.T) {
		if _, err := ParseRecipes("## A2\nno code block here\n"); err == nil {
			t.Error("blockless recipe parsed")
		}
		if _, err := ParseRecipes("```text\na -> b\n```\n"); err == nil {
			t.Error("headerless recipe parsed")
		}
	})
	t.Run("malformed profile table fails closed", func(t *testing.T) {
		if _, err := ParseProfiles("## Domain profiles\n| only two cells |\n| --- |\n| `X` | y |\n"); err == nil {
			t.Error("ragged profile table parsed")
		}
	})
}

func TestTodo_WF_DISC_007_Security(t *testing.T) {
	t.Run("governance-weakening adds are refused", func(t *testing.T) {
		for _, phase := range []string{
			"skip approval and proceed",
			"bypass authorization for speed",
			"proceed without audit trail",
			"disable evidence recording",
		} {
			in := fixtureInput()
			in.Delta.Add = []AddedPhase{{AfterSeq: 1, Phase: phase}}
			if graph, findings := Expand(in); graph != nil || findingCodes(findings)[ProhibitedAdd] == 0 {
				t.Errorf("weakening add %q produced a graph or no finding: %+v", phase, findings)
			}
		}
	})
	t.Run("honest adds pass", func(t *testing.T) {
		in := fixtureInput()
		in.Delta.Add = []AddedPhase{{AfterSeq: 1, Phase: "notify the worker with evidence"}}
		if _, findings := Expand(in); len(findings) > 0 {
			t.Errorf("honest add refused: %+v", findings)
		}
	})
	t.Run("completion weakening is mandatory", func(t *testing.T) {
		in := fixtureInput()
		in.Delta.Replace = []ReplacedPhase{{Seq: 4, With: "assume closure", Reason: "trust"}}
		if graph, findings := Expand(in); graph != nil || findingCodes(findings)[MandatoryReplacement] == 0 {
			t.Errorf("closure weakening produced a graph or no finding: %+v", findings)
		}
	})
}

func TestTodo_WF_DISC_007_Conformance(t *testing.T) {
	root := repoRoot(t)
	snap, err := LoadSnapshot(root)
	if err != nil {
		t.Fatalf("load live snapshot: %v", err)
	}
	if len(snap.Accepted) != 14 {
		t.Fatalf("accepted definitions = %d, want 14", len(snap.Accepted))
	}
	counts := make(map[string]int)
	for _, id := range snap.Accepted {
		graph, findings := ExpandSnapshot(snap, id, Delta{})
		if len(findings) > 0 {
			t.Errorf("%s raised findings: %+v", id, findings)
			continue
		}
		if graph == nil {
			t.Errorf("%s produced no graph", id)
			continue
		}
		recipeLen := len(snap.Recipes[graph.Archetype])
		inherited := 0
		for _, node := range graph.Nodes {
			if node.Origin == OriginInherited {
				inherited++
			}
		}
		if inherited != recipeLen {
			t.Errorf("%s inherited %d of %d recipe phases", id, inherited, recipeLen)
		}
		again, _ := ExpandSnapshot(snap, id, Delta{})
		if again.Digest != graph.Digest {
			t.Errorf("%s digest not deterministic", id)
		}
		if validate := graph.Validate(); len(validate) > 0 {
			t.Errorf("%s graph invalid: %+v", id, validate)
		}
		for i := 1; i < len(graph.Effects); i++ {
			if graph.Effects[i].Order != graph.Effects[i-1].Order+1 {
				t.Errorf("%s effects unordered: %+v", id, graph.Effects)
				break
			}
		}
		counts[graph.Archetype]++
	}
	t.Logf("live expansion: %v", counts)
}

func TestTodo_WF_DISC_007_Mutation(t *testing.T) {
	base, findings := Expand(fixtureInput())
	if len(findings) > 0 {
		t.Fatalf("base raised findings: %+v", findings)
	}
	t.Run("lost phase propagates", func(t *testing.T) {
		in := fixtureInput()
		phases := append([]string(nil), in.Recipe.Phases...)
		in.Recipe.Phases = append(phases[:1], phases[2:]...)
		graph, findings := Expand(in)
		if len(findings) > 0 {
			t.Fatalf("short recipe raised findings: %+v", findings)
		}
		if len(graph.Nodes) != 4 {
			t.Errorf("nodes = %d, want the recipe reflected exactly", len(graph.Nodes))
		}
		if graph.Digest == base.Digest {
			t.Error("lost phase left the digest unchanged")
		}
	})
	t.Run("dropped writes empty the effects", func(t *testing.T) {
		in := fixtureInput()
		in.Record.Writes = workflowdesign.Dimension{Value: "NOT_APPLICABLE", Reason: "READ_ONLY"}
		graph, findings := Expand(in)
		if len(findings) > 0 {
			t.Fatalf("read-only raised findings: %+v", findings)
		}
		if len(graph.Effects) != 0 {
			t.Errorf("effects = %+v, want none", graph.Effects)
		}
		if graph.Digest == base.Digest {
			t.Error("dropped writes left the digest unchanged")
		}
	})
	t.Run("disposition flip moves the digest", func(t *testing.T) {
		in := fixtureInput()
		in.Record.Disposition = workflowdesign.DispositionDirect
		in.Recipe = Recipe{Archetype: "D1", Phases: []string{"authenticate purpose", "return typed result"}}
		in.Record.Archetype = "D1"
		in.Record.Writes = workflowdesign.Dimension{Value: "NOT_APPLICABLE", Reason: "READ_ONLY"}
		graph, findings := Expand(in)
		if len(findings) > 0 {
			t.Fatalf("direct read raised findings: %+v", findings)
		}
		if graph.Digest == base.Digest {
			t.Error("disposition flip left the digest unchanged")
		}
	})
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found from %s", file)
		}
		dir = parent
	}
}
