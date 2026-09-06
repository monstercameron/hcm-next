package taint

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/transformation"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/ir"
)

func testProgram(t *testing.T, op ir.OpCode) ir.Program {
	t.Helper()
	type source struct {
		name string
		typ  transformation.Type
	}
	var sources []source
	instruction := ir.Instruction{Op: op, Destination: transformation.Path{Schema: "out", Field: "value", Type: transformation.TypeString}}
	switch op {
	case ir.OpMap:
		instruction.Function = ir.FuncDefault
		instruction.Literal = "mapped"
	case ir.OpFilter:
		sources = []source{{"name", transformation.TypeString}}
		instruction.Function = ir.FuncNotNull
	case ir.OpProject:
		sources = []source{{"name", transformation.TypeString}}
	case ir.OpJoinByKey:
		sources = []source{{"left", transformation.TypeString}, {"right", transformation.TypeString}}
		instruction.JoinKey = "person_id"
	case ir.OpAggregate:
		sources = []source{{"left", transformation.TypeString}, {"right", transformation.TypeString}}
		instruction.Function = ir.FuncConcat
	case ir.OpCoerce:
		sources = []source{{"name", transformation.TypeInt}}
		instruction.Destination.Type = transformation.TypeString
		instruction.TargetType = transformation.TypeString
	}
	for _, source := range sources {
		instruction.Sources = append(instruction.Sources, transformation.Path{Schema: "in", Field: source.name, Type: source.typ})
	}
	program := ir.Program{IRVersion: ir.IRVersion, DefinitionName: "taint-test", Instructions: []ir.Instruction{instruction},
		Limits: ir.Limits{MaxSteps: 4, MaxFanOut: 4}}
	if err := program.Validate(); err != nil {
		t.Fatalf("program %s: %v", op, err)
	}
	return program
}

func metadata(class Classification, provenance ...string) Metadata {
	return Metadata{Presence: PresencePresent, Classification: class, Provenance: provenance}
}

func field(data any, class Classification, provenance string, taint ...string) Field {
	return Field{Type: transformation.TypeString, Data: data, Presence: PresencePresent,
		Classification: class, Provenance: []string{provenance}, Taint: taint}
}

// TestTodo_XFORM_004 is the primary contract: all declared rules retain the
// source envelope, and the paired executor returns metadata beside values.
func TestTodo_XFORM_004(t *testing.T) {
	for _, op := range []ir.OpCode{ir.OpMap, ir.OpFilter, ir.OpProject, ir.OpJoinByKey, ir.OpAggregate, ir.OpCoerce} {
		if _, err := RuleFor(op); err != nil {
			t.Fatalf("RuleFor(%s): %v", op, err)
		}
	}
	p := testProgram(t, ir.OpProject)
	got, err := Execute(p, []Record{{"in.name": field("Ada", ClassificationConfidential, "source:people.name", "untrusted")}}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	output := got[0]["out.value"]
	if output.Data != "Ada" || output.Classification != ClassificationConfidential || output.Presence != PresencePresent {
		t.Fatalf("value or envelope changed: %+v", output)
	}
	if !contains(output.Provenance, "source:people.name") || len(output.Taint) != 1 || output.Taint[0] != "untrusted" {
		t.Fatalf("lineage changed: %+v", output)
	}
}

// TestTodo_XFORM_004_Property proves the taint set is monotonic for every
// operation and for a multi-source union.
func TestTodo_XFORM_004_Property(t *testing.T) {
	sources := []Metadata{
		{Presence: PresencePresent, Classification: ClassificationInternal, Provenance: []string{"b"}, Taint: []string{"low"}},
		{Presence: PresenceUnknown, Classification: ClassificationSecret, Provenance: []string{"a"}, Taint: []string{"high"}},
	}
	for _, op := range []ir.OpCode{ir.OpMap, ir.OpFilter, ir.OpProject, ir.OpJoinByKey, ir.OpAggregate, ir.OpCoerce} {
		got, err := Propagate(op, sources)
		if err != nil {
			t.Fatalf("%s: %v", op, err)
		}
		if got.Classification != ClassificationSecret || got.Presence != PresenceUnknown {
			t.Fatalf("%s downgraded envelope: %+v", op, got)
		}
		for _, source := range sources {
			for _, item := range source.Taint {
				if !contains(got.Taint, item) {
					t.Fatalf("%s lost taint %q: %+v", op, item, got)
				}
			}
		}
	}
}

func TestTodo_XFORM_004_Golden(t *testing.T) {
	got, err := Propagate(ir.OpAggregate, []Metadata{
		metadata(ClassificationInternal, "source:z"),
		{Presence: PresencePresent, Classification: ClassificationRestricted, Provenance: []string{"source:a"}, Taint: []string{"pii"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Metadata{Presence: PresencePresent, Classification: ClassificationRestricted,
		Provenance: []string{"source:a", "source:z"}, Taint: []string{"pii"}}
	if got.Classification != want.Classification || !equalStrings(got.Provenance, want.Provenance) || !equalStrings(got.Taint, want.Taint) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func FuzzTodo_XFORM_004(f *testing.F) {
	f.Add("source:a", "taint:a")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, provenance, taint string) {
		if provenance == "" {
			provenance = "source:fuzz"
		}
		m := metadata(ClassificationInternal, provenance)
		if taint != "" {
			m.Taint = []string{taint}
		}
		got, err := Propagate(ir.OpCoerce, []Metadata{m})
		if err != nil {
			t.Fatalf("unexpected propagation error: %v", err)
		}
		if len(got.Taint) != len(m.Taint) || len(got.Provenance) != 1 {
			t.Fatalf("metadata changed: %+v", got)
		}
	})
}

func TestTodo_XFORM_004_Security(t *testing.T) {
	output := metadata(ClassificationPublic, "source:public")
	err := CheckOutput(ir.OpProject, []Metadata{metadata(ClassificationSecret, "source:secret")}, output)
	var refusal Refusal
	if !errors.As(err, &refusal) || !errors.Is(err, ErrRefused) {
		t.Fatalf("classification downgrade was not refused: %v", err)
	}
	if refusal.Code != "XFORM_004_REFUSED" || !stringsContains(refusal.Reason, "classification") {
		t.Fatalf("unexpected refusal: %+v", refusal)
	}
	err = CheckOutput(ir.OpProject, []Metadata{metadata(ClassificationInternal, "source:private")}, metadata(ClassificationInternal, "source:other"))
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("provenance loss was not refused: %v", err)
	}
}

func TestTodo_XFORM_004_Mutation(t *testing.T) {
	if _, err := Propagate(ir.OpProject, []Metadata{{Presence: PresencePresent, Classification: ClassificationPublic}}); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("missing provenance accepted: %v", err)
	}
	if _, err := Propagate(ir.OpCode("script"), []Metadata{metadata(ClassificationPublic, "source:x")}); !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("unknown operation accepted: %v", err)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func stringsContains(s, want string) bool {
	return len(s) >= len(want) && stringsIndex(s, want) >= 0
}

func stringsIndex(s, want string) int {
	for i := 0; i+len(want) <= len(s); i++ {
		if s[i:i+len(want)] == want {
			return i
		}
	}
	return -1
}
