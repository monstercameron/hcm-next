package conformance

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
)

func checkedIn(t *testing.T) (VectorSet, []byte) {
	t.Helper()
	raw, err := os.ReadFile("testdata/vectors/vectors.json")
	if err != nil {
		generated, generateErr := GenerateVectors()
		if generateErr != nil {
			t.Fatalf("read vectors: %v; generate: %v", err, generateErr)
		}
		pretty, _ := MarshalVectors(generated)
		t.Fatalf("read vectors: %v\nGENERATED VECTOR FILE:\n%s", err, pretty)
	}
	set, err := ParseVectors(raw)
	if err != nil {
		generated, generateErr := GenerateVectors()
		pretty, _ := MarshalVectors(generated)
		t.Fatalf("parse vectors: %v; generate=%v\nGENERATED VECTOR FILE:\n%s", err, generateErr, pretty)
	}
	return set, raw
}

// TestTodo_XFORM_006 is the primary drift gate: the checked-in suite is the
// exact output of the pure generator and its complete-set digest is pinned.
func TestTodo_XFORM_006(t *testing.T) {
	set, raw := checkedIn(t)
	generated, err := GenerateVectors()
	if err != nil {
		t.Fatal(err)
	}
	if generated.Digest != PinnedDigest || set.Digest != PinnedDigest {
		t.Fatalf("digest=%s generated=%s pinned=%s", set.Digest, generated.Digest, PinnedDigest)
	}
	want, err := MarshalVectors(generated)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, want) {
		t.Fatalf("checked-in vectors drifted; regenerate vectors from GenerateVectors")
	}
}

func TestTodo_XFORM_006_Property(t *testing.T) {
	a, err := GenerateVectors()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateVectors()
	if err != nil {
		t.Fatal(err)
	}
	aa, _ := MarshalVectors(a)
	bb, _ := MarshalVectors(b)
	if !bytes.Equal(aa, bb) || len(a.Vectors) < 6 {
		t.Fatalf("generation is not deterministic or does not cover every opcode: %d", len(a.Vectors))
	}
}

func TestTodo_XFORM_006_Golden(t *testing.T) {
	set, _ := checkedIn(t)
	seen := map[ir.OpCode]bool{}
	for _, vector := range set.Vectors {
		seen[vector.Kind] = true
		if vector.WantError == "" && len(vector.Expected) == 0 {
			t.Errorf("%s has no expected output", vector.Name)
		}
	}
	for _, kind := range []ir.OpCode{ir.OpMap, ir.OpFilter, ir.OpProject, ir.OpJoinByKey, ir.OpAggregate, ir.OpCoerce} {
		if !seen[kind] {
			t.Errorf("missing opcode vector %s", kind)
		}
	}
}

func FuzzTodo_XFORM_006(f *testing.F) {
	f.Add([]byte(`{"schema":"bad"}`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if set, err := ParseVectors(raw); err == nil {
			if _, err := json.Marshal(set); err != nil {
				t.Fatalf("valid vector set did not marshal: %v", err)
			}
		}
	})
}

func TestTodo_XFORM_006_Conformance(t *testing.T) {
	set, _ := checkedIn(t)
	verdicts, err := Run(set)
	if err != nil {
		t.Fatal(err)
	}
	for _, verdict := range verdicts {
		if !verdict.Pass {
			t.Errorf("%s (%s): %s", verdict.Name, verdict.Kind, verdict.Error)
		}
	}
}
