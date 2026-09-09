package lineage

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/taint"
)

func lineageProgram(t *testing.T) ir.Program {
	t.Helper()
	program := ir.Program{
		IRVersion: ir.IRVersion, DefinitionName: "lineage-fixture",
		Instructions: []ir.Instruction{
			{Op: ir.OpProject, Sources: []transformation.Path{{Schema: "people", Field: "given", Type: transformation.TypeString}}, Destination: transformation.Path{Schema: "worker", Field: "given", Type: transformation.TypeString}},
			{Op: ir.OpMap, Function: ir.FuncDefault, Literal: "unknown", Destination: transformation.Path{Schema: "worker", Field: "status", Type: transformation.TypeString}},
		},
		Limits: ir.Limits{MaxSteps: 4, MaxFanOut: 2},
	}
	if err := program.Validate(); err != nil {
		t.Fatalf("program.Validate: %v", err)
	}
	return program
}

func lineageInput() taint.Record {
	return taint.Record{
		"people.given": {Type: transformation.TypeString, Data: "Ada", Presence: taint.PresencePresent, Classification: taint.ClassificationRestricted, Provenance: []string{"hr.people.given"}, Taint: []string{"pii"}},
	}
}

// TestTodo_XFORM_007 proves one executed transformation has a complete,
// value-free graph and that querying an output walks through its operation to
// the source field.
func TestTodo_XFORM_007(t *testing.T) {
	execution, err := Execute(lineageProgram(t), lineageInput())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if execution.Digest == "" || execution.Graph.Digest() != execution.Digest {
		t.Fatalf("digest mismatch: execution=%q graph=%q", execution.Digest, execution.Graph.Digest())
	}
	path, err := execution.Graph.SourcePath("worker.given")
	if err != nil {
		t.Fatalf("SourcePath: %v", err)
	}
	if len(path) != 3 || path[0] != "output:worker.given" || path[1] != "operation:project:worker.given" || path[2] != "source:people.given" {
		t.Fatalf("path = %v", path)
	}
	if strings.Contains(execution.Graph.Explain(), "Ada") {
		t.Fatal("explanation repeated a restricted source value")
	}
	if err := execution.Graph.Validate(); err != nil {
		t.Fatalf("graph.Validate: %v", err)
	}
}

// TestTodo_XFORM_007_Property pins replay and goroutine determinism.
func TestTodo_XFORM_007_Property(t *testing.T) {
	program := lineageProgram(t)
	want, err := Execute(program, lineageInput())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		got, err := Execute(program, lineageInput())
		if err != nil || got.Digest != want.Digest {
			t.Fatalf("run %d: result=%+v err=%v want=%s", i, got, err, want.Digest)
		}
	}
}

// TestTodo_XFORM_007_Golden pins the canonical graph digest for the fixture.
func TestTodo_XFORM_007_Golden(t *testing.T) {
	execution, err := Execute(lineageProgram(t), lineageInput())
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:c3de75803b83e74767b2f330333ada20c90fb38adb9bb1aa945e3b3477b09525"
	if execution.Digest != want {
		t.Fatalf("lineage digest = %s, want %s", execution.Digest, want)
	}
}

// FuzzTodo_XFORM_007 checks that arbitrary source text cannot enter the
// value-free explanation or make graph construction nondeterministic.
func FuzzTodo_XFORM_007(f *testing.F) {
	for _, value := range []string{"Ada", "", "restricted-value", "\x00"} {
		f.Add(value)
	}
	program := lineageProgramForFuzz()
	f.Fuzz(func(t *testing.T, value string) {
		input := lineageInput()
		field := input["people.given"]
		field.Data = value
		input["people.given"] = field
		first, err1 := Execute(program, input)
		second, err2 := Execute(program, input)
		if (err1 == nil) != (err2 == nil) || first.Digest != second.Digest {
			t.Fatalf("replay changed for %q: %v/%v %s/%s", value, err1, err2, first.Digest, second.Digest)
		}
	})
}

func lineageProgramForFuzz() ir.Program {
	return ir.Program{IRVersion: ir.IRVersion, DefinitionName: "fuzz", Instructions: []ir.Instruction{{Op: ir.OpProject, Sources: []transformation.Path{{Schema: "people", Field: "given", Type: transformation.TypeString}}, Destination: transformation.Path{Schema: "worker", Field: "given", Type: transformation.TypeString}}}, Limits: ir.Limits{MaxSteps: 1, MaxFanOut: 1}}
}

// TestTodo_XFORM_007_Fault ensures malformed metadata is refused before an
// incomplete graph can be published.
func TestTodo_XFORM_007_Fault(t *testing.T) {
	input := lineageInput()
	field := input["people.given"]
	field.Provenance = nil
	input["people.given"] = field
	if _, err := Execute(lineageProgram(t), input); err == nil || !errors.Is(err, taint.ErrRefused) {
		t.Fatalf("Execute error = %v, want invalid metadata", err)
	}
}

// TestTodo_XFORM_007_Security verifies explanations contain identifiers and
// decisions, but never transformed values.
func TestTodo_XFORM_007_Security(t *testing.T) {
	execution, err := Execute(lineageProgram(t), lineageInput())
	if err != nil {
		t.Fatal(err)
	}
	explanation := Explain(execution.Graph)
	for _, want := range []string{"worker.given", "operation:project", "worker.status", "literal=declared"} {
		if !strings.Contains(explanation, want) {
			t.Fatalf("explanation %q missing %q", explanation, want)
		}
	}
	if strings.Contains(explanation, "Ada") || strings.Contains(explanation, "pii") {
		t.Fatalf("explanation exposed restricted data: %q", explanation)
	}
}

// TestTodo_XFORM_007_Mutation ensures a graph with an output disconnected
// from its source cannot pass validation.
func TestTodo_XFORM_007_Mutation(t *testing.T) {
	execution, err := Execute(lineageProgram(t), lineageInput())
	if err != nil {
		t.Fatal(err)
	}
	for i, edge := range execution.Graph.Edges {
		if edge.To == "operation:project:worker.given" {
			execution.Graph.Edges = append(execution.Graph.Edges[:i], execution.Graph.Edges[i+1:]...)
			break
		}
	}
	if err := execution.Graph.Validate(); err == nil {
		t.Fatal("disconnected output graph was accepted")
	}
}
