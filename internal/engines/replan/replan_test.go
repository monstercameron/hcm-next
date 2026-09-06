package replan

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/fielddiff"
)

func drift(input string) FieldDiff {
	return FieldDiff{Input: input, Outcome: fielddiff.Outcome{Relation: fielddiff.RelationExternalAhead, Reason: fielddiff.ReasonExternalNewer, Ordered: true}}
}

func match(input string) FieldDiff {
	return FieldDiff{Input: input, Outcome: fielddiff.Outcome{Relation: fielddiff.RelationMatch, Reason: fielddiff.ReasonValuesEqual}}
}

func TestTodo_REPLAN_003(t *testing.T) {
	declaration := Declaration{
		ProposalRevisionID: "proposal:1",
		Components: []Component{
			{ID: "leave-eligibility", Dependencies: []Dependency{{Input: "balance", Classification: ClassificationMaterial}}},
			{ID: "evidence-review", Dependencies: []Dependency{{Input: "evidence", Classification: ClassificationMaterial}}},
			{ID: "notice", Dependencies: []Dependency{{Input: "balance", Classification: ClassificationInformative}}},
		},
	}
	result, err := Compute(declaration, []FieldDiff{drift("balance"), match("evidence"), drift("unknown")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Invalidated, []string{"leave-eligibility"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("invalidated=%v, want %v", got, want)
	}
	if len(result.UnboundDrift) != 1 || result.UnboundDrift[0] != "unknown" {
		t.Fatalf("unbound drift=%v", result.UnboundDrift)
	}
	if len(result.Explain()) != 1 || result.Explain()[0].DriftedInputs[0] != "balance" {
		t.Fatalf("explain=%v", result.Explain())
	}
	if result.Digest == "" {
		t.Fatal("missing stable digest")
	}
}

func TestTodo_REPLAN_003_Property(t *testing.T) {
	declaration := Declaration{Components: []Component{
		{ID: "a", Dependencies: []Dependency{{Input: "changed", Classification: ClassificationMaterial}}},
		{ID: "b", Dependencies: []Dependency{{Input: "steady", Classification: ClassificationMaterial}}},
	}}
	result, err := Compute(declaration, []FieldDiff{drift("changed")})
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range result.Findings {
		if finding.ComponentID == "b" && finding.Invalidated {
			t.Fatalf("component without drift was invalidated: %+v", finding)
		}
	}
	permuted := Declaration{Components: []Component{declaration.Components[1], declaration.Components[0]}}
	other, err := Compute(permuted, []FieldDiff{drift("changed")})
	if err != nil || result.Digest != other.Digest {
		t.Fatalf("declaration order changed digest: %q != %q (err=%v)", result.Digest, other.Digest, err)
	}
}

func TestTodo_REPLAN_003_Race(t *testing.T) {
	declaration := Declaration{Components: []Component{{ID: "component", Dependencies: []Dependency{{Input: "x", Classification: ClassificationMaterial}}}}}
	for i := 0; i < 20; i++ {
		result, err := Compute(declaration, []FieldDiff{drift("x")})
		if err != nil {
			t.Fatal(err)
		}
		if result.Digest == "" || len(result.Invalidated) != 1 {
			t.Fatalf("run %d result=%+v", i, result)
		}
	}
}

func TestTodo_REPLAN_003_Mutation(t *testing.T) {
	declaration := Declaration{Components: []Component{{ID: "component", Dependencies: []Dependency{{Input: "x", Classification: ClassificationMaterial}}}}}
	if _, err := Compute(declaration, []FieldDiff{{Input: "x", Outcome: fielddiff.Outcome{}}}); !errors.Is(err, ErrInvalidDiff) {
		t.Fatalf("invalid outcome error=%v", err)
	}
	if _, err := Compute(Declaration{Components: []Component{{ID: "component", Dependencies: []Dependency{{Input: "x", Classification: Classification("future")}}}}}, nil); !errors.Is(err, ErrInvalidDeclaration) {
		t.Fatalf("unknown classification error=%v", err)
	}
}

func TestReplanRejectsDuplicateInputWithDifferentOutcome(t *testing.T) {
	declaration := Declaration{Components: []Component{{ID: "component", Dependencies: []Dependency{{Input: "x", Classification: ClassificationMaterial}}}}}
	first := drift("x")
	second := first
	second.Outcome.Relation = fielddiff.RelationCanonicalAhead
	second.Outcome.Reason = fielddiff.ReasonCanonicalNewer
	if _, err := Compute(declaration, []FieldDiff{first, second}); !errors.Is(err, ErrInvalidDiff) {
		t.Fatalf("duplicate diff error=%v", err)
	}
}
