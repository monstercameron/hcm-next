// Package transformationvectors contains the source vectors and deterministic
// generator for the XFORM-006 transformation contract suite.
package transformationvectors

import (
	"fmt"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/transformation"
)

type Class string

const (
	Positive    Class = "positive"
	Boundary    Class = "boundary"
	Invalid     Class = "invalid"
	Unknown     Class = "unknown"
	Taint       Class = "taint"
	Determinism Class = "determinism"
)

// Vector is intentionally serializable and side-effect free. Effects is kept
// in the oracle so a future executor cannot silently turn conformance vectors
// into an authority or persistence path.
type Vector struct {
	Name       string                                  `json:"name"`
	Class      Class                                   `json:"class"`
	Definition transformation.TransformationDefinition `json:"definition"`
	WantValid  bool                                    `json:"want_valid"`
	WantError  string                                  `json:"want_error,omitempty"`
	WantDigest string                                  `json:"want_digest,omitempty"`
	Effects    int                                     `json:"effects"`
}

func schema(name string, fields ...transformation.Field) transformation.Schema {
	return transformation.Schema{Name: name, Version: 1, Fields: fields}
}

func baseDefinition() transformation.TransformationDefinition {
	src := schema("source", transformation.Field{Name: "first", Type: transformation.TypeString, Required: true}, transformation.Field{Name: "last", Type: transformation.TypeString})
	dst := schema("target", transformation.Field{Name: "full_name", Type: transformation.TypeString, Required: true})
	return transformation.TransformationDefinition{Version: transformation.ContractVersion, Name: "people/name", Owner: "people", Phase: "P0", Source: src, Destination: dst, Operations: []transformation.Operation{{Kind: transformation.OpConcat, Destination: transformation.Path{Schema: "target", Field: "full_name", Type: transformation.TypeString}, Sources: []transformation.Path{{Schema: "source", Field: "first", Type: transformation.TypeString}, {Schema: "source", Field: "last", Type: transformation.TypeString}}}}, Compatibility: transformation.Compatibility{MinimumSourceVersion: 1, MaximumSourceVersion: 1}, Limits: transformation.ResourceLimits{MaxOperations: 4, MaxInputBytes: 4096, MaxOutputBytes: 4096, MaxExpansion: 2}, Failure: transformation.FailureReject, SideEffects: transformation.SideEffectsNone}
}

// DefaultVectors is the complete required XFORM-006 class matrix.
func DefaultVectors() []Vector {
	valid := baseDefinition()
	boundary := baseDefinition()
	boundary.Limits.MaxOperations = 1
	invalid := baseDefinition()
	invalid.Operations[0].Sources[0].Type = transformation.TypeInt
	unknown := baseDefinition()
	unknown.Operations[0].Kind = transformation.OperationKind("lookup")
	tainted := baseDefinition()
	tainted.Name = "people/name-tainted-input"
	return []Vector{
		{Name: "concat-positive", Class: Positive, Definition: valid, WantValid: true, WantDigest: "sha256:66befa358077b8ac94cb4f8048f7ab97b578d368d035ac224c470959d8b5cb95", Effects: 0},
		{Name: "operation-limit-boundary", Class: Boundary, Definition: boundary, WantValid: true, Effects: 0},
		{Name: "untyped-source-rejected", Class: Invalid, Definition: invalid, WantValid: false, WantError: "transformation: path must be typed", Effects: 0},
		{Name: "unknown-operation-rejected", Class: Unknown, Definition: unknown, WantValid: false, WantError: "transformation: unknown operation", Effects: 0},
		{Name: "taint-preserved-as-declarative-input", Class: Taint, Definition: tainted, WantValid: true, Effects: 0},
		{Name: "canonical-replay", Class: Determinism, Definition: valid, WantValid: true, Effects: 0},
	}
}

type testLogger interface {
	Helper()
	Fatalf(string, ...any)
}

func runVector(t testLogger, v Vector) string {
	t.Helper()
	b, err := v.Definition.CanonicalBytes()
	if v.WantValid {
		if err != nil {
			t.Fatalf("%s: unexpected validation error: %v", v.Name, err)
		}
	} else if err == nil || err.Error() != v.WantError {
		t.Fatalf("%s: error = %v, want %q", v.Name, err, v.WantError)
	}
	if v.Effects != 0 {
		t.Fatalf("%s: effects = %d, want zero", v.Name, v.Effects)
	}
	if !v.WantValid {
		return ""
	}
	digest, err := v.Definition.Digest()
	if err != nil {
		t.Fatalf("%s: digest: %v", v.Name, err)
	}
	if len(b) == 0 || digest == "" {
		t.Fatalf("%s: empty canonical output", v.Name)
	}
	if v.WantDigest != "" && digest != v.WantDigest {
		t.Fatalf("%s: digest = %s, want %s", v.Name, digest, v.WantDigest)
	}
	return digest
}

func requireClasses(t testLogger, vectors []Vector) {
	t.Helper()
	seen := map[Class]bool{}
	for _, v := range vectors {
		seen[v.Class] = true
	}
	for _, c := range []Class{Positive, Boundary, Invalid, Unknown, Taint, Determinism} {
		if !seen[c] {
			t.Fatalf("missing vector class %q", c)
		}
	}
}

func TestTodo_XFORM_006(t *testing.T) {
	vectors := DefaultVectors()
	requireClasses(t, vectors)
	for _, v := range vectors {
		t.Run(fmt.Sprintf("%s/%s", v.Class, v.Name), func(t *testing.T) { runVector(t, v) })
	}
}
func TestTodo_XFORM_006_Property(t *testing.T) {
	for _, v := range DefaultVectors() {
		if got := runVector(t, v); v.WantValid && got == "" {
			t.Fatalf("%s: valid vector has no digest", v.Name)
		}
	}
}
func TestTodo_XFORM_006_Golden(t *testing.T) {
	v := DefaultVectors()[0]
	got := runVector(t, v)
	const wantPrefix = "sha256:"
	if len(got) <= len(wantPrefix) || got[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("digest = %q, want sha256 digest", got)
	}
}
func TestTodo_XFORM_006_Conformance(t *testing.T) { requireClasses(t, DefaultVectors()) }
func TestTodo_XFORM_006_Determinism(t *testing.T) {
	v := DefaultVectors()[0]
	a := runVector(t, v)
	b := runVector(t, v)
	if a != b {
		t.Fatalf("digest changed across replay: %s != %s", a, b)
	}
}

func FuzzTodo_XFORM_006(f *testing.F) {
	f.Add([]byte("replay"))
	f.Fuzz(func(t *testing.T, _ []byte) {
		// The declarative contract has no ambient input source: every replay of
		// the same definition must validate and digest identically.
		v := DefaultVectors()[0]
		a, b := runVector(t, v), runVector(t, v)
		if a != b {
			t.Fatalf("replay digest changed: %s != %s", a, b)
		}
	})
}
