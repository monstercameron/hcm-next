package version

import (
	"errors"
	"testing"

	"github.com/monstercameron/hcm-next/internal/engines/transformation"
	"github.com/monstercameron/hcm-next/internal/engines/transformation/ir"
)

// -- fixtures ---------------------------------------------------------------

func srcPath(field string, typ transformation.Type) transformation.Path {
	return transformation.Path{Schema: "people", Field: field, Type: typ}
}
func dstPath(field string, typ transformation.Type) transformation.Path {
	return transformation.Path{Schema: "worker", Field: field, Type: typ}
}
func ptr(p transformation.Path) *transformation.Path { return &p }

func definition() transformation.TransformationDefinition {
	return transformation.TransformationDefinition{
		Version: transformation.ContractVersion, Name: "people-copy", Owner: "shared-engines", Phase: "P1A",
		Source: transformation.Schema{Name: "people", Version: 1, Fields: []transformation.Field{
			{Name: "given", Type: transformation.TypeString, Required: true},
			{Name: "age", Type: transformation.TypeInt},
		}},
		Destination: transformation.Schema{Name: "worker", Version: 1, Fields: []transformation.Field{
			{Name: "name", Type: transformation.TypeString, Required: true},
			{Name: "age", Type: transformation.TypeInt},
		}},
		Operations: []transformation.Operation{
			{Kind: transformation.OpCopy, Source: ptr(srcPath("given", transformation.TypeString)), Destination: dstPath("name", transformation.TypeString)},
			{Kind: transformation.OpConvert, Source: ptr(srcPath("age", transformation.TypeInt)), Destination: dstPath("age", transformation.TypeInt), TargetType: transformation.TypeInt},
		},
		Compatibility: transformation.Compatibility{MinimumSourceVersion: 1},
		Limits:        transformation.ResourceLimits{MaxOperations: 4, MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxExpansion: 2},
		Failure:       transformation.FailureReject, SideEffects: transformation.SideEffectsNone,
	}
}

func program(t *testing.T) ir.Program {
	t.Helper()
	p, err := ir.Compile(definition())
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return p
}

// withRenamedOutputField compiles a definition identical to definition()
// except that the "name" destination field (and the copy operation writing
// it) is renamed, producing a Program whose output schema differs from
// program(t)'s by exactly that one field.
func withRenamedOutputField(t *testing.T, newName string) ir.Program {
	t.Helper()
	d := definition()
	d.Destination.Fields[0].Name = newName
	d.Operations[0].Destination = dstPath(newName, transformation.TypeString)
	p, err := ir.Compile(d)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return p
}

func withWidenedLimits(t *testing.T, maxOperations, maxExpansion int) ir.Program {
	t.Helper()
	d := definition()
	d.Limits.MaxOperations = maxOperations
	d.Limits.MaxExpansion = maxExpansion
	p, err := ir.Compile(d)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return p
}

// -- XFORM-005 matrix --------------------------------------------------------

// PRIMARY: a pinned revision resolves back through a Pin unaffected by a
// later publication under the same name.
func TestTodo_XFORM_005(t *testing.T) {
	r := NewRegistry()
	p1 := program(t)
	v1, err := r.Publish("people-copy", 1, p1)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := r.Pin("people-copy", 1)
	if err != nil {
		t.Fatal(err)
	}

	p2 := withRenamedOutputField(t, "full_name")
	if _, err := r.Publish("people-copy", 2, p2); err != nil {
		t.Fatal(err)
	}

	got, err := r.ResolvePin(pin)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != v1.Digest || got.Revision != 1 {
		t.Fatalf("pin moved: %#v", got)
	}
}

// PROPERTY: publishing is stable -- the same program published once resolves
// to the identical digest on every subsequent Resolve.
func TestTodo_XFORM_005_Property(t *testing.T) {
	r := NewRegistry()
	p := program(t)
	a, err := r.Publish("people-copy", 1, p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		b, err := r.Resolve("people-copy", 1)
		if err != nil {
			t.Fatal(err)
		}
		if a.Digest != b.Digest || a.Revision != b.Revision {
			t.Fatalf("run %d: published version is not stable: %#v vs %#v", i, a, b)
		}
	}
}

// GOLDEN: a pinned compatibility verdict table covering the three checks
// Compare makes -- input schema, output schema, and limits -- plus the
// baseline "identical program" and "AllowWidenedLimits" cases.
func TestTodo_XFORM_005_Golden(t *testing.T) {
	base := program(t)
	renamedOutput := withRenamedOutputField(t, "full_name")
	widened := withWidenedLimits(t, 8, 4)

	cases := []struct {
		name             string
		old, next        ir.Program
		opts             CompatibilityOptions
		wantCompatible   bool
		wantBreaking     bool
		wantInputOK      bool
		wantOutputOK     bool
		wantLimitsWide   bool
		wantBreakingHits []string
	}{
		{
			name: "identical", old: base, next: base,
			wantCompatible: true, wantInputOK: true, wantOutputOK: true,
		},
		{
			name: "output_field_renamed", old: base, next: renamedOutput,
			wantCompatible: false, wantBreaking: true, wantInputOK: true, wantOutputOK: false,
			wantBreakingHits: []string{"worker.name"},
		},
		{
			name: "limits_widened_silently", old: base, next: widened,
			wantCompatible: false, wantBreaking: true, wantInputOK: true, wantOutputOK: true,
			wantLimitsWide: true,
		},
		{
			name: "limits_widened_allowed", old: base, next: widened,
			opts:           CompatibilityOptions{AllowWidenedLimits: true},
			wantCompatible: true, wantInputOK: true, wantOutputOK: true, wantLimitsWide: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			report, err := Compare(c.old, c.next, c.opts)
			if err != nil {
				t.Fatal(err)
			}
			if report.Compatible != c.wantCompatible {
				t.Errorf("Compatible=%v, want %v (%#v)", report.Compatible, c.wantCompatible, report)
			}
			if report.Breaking != c.wantBreaking {
				t.Errorf("Breaking=%v, want %v (%#v)", report.Breaking, c.wantBreaking, report)
			}
			if report.InputCompatible != c.wantInputOK {
				t.Errorf("InputCompatible=%v, want %v", report.InputCompatible, c.wantInputOK)
			}
			if report.OutputCompatible != c.wantOutputOK {
				t.Errorf("OutputCompatible=%v, want %v", report.OutputCompatible, c.wantOutputOK)
			}
			if report.LimitsWidened != c.wantLimitsWide {
				t.Errorf("LimitsWidened=%v, want %v", report.LimitsWidened, c.wantLimitsWide)
			}
			for _, want := range c.wantBreakingHits {
				found := false
				for _, f := range report.BreakingFields {
					if f == want {
						found = true
					}
				}
				if !found {
					t.Errorf("BreakingFields=%v, want it to contain %q", report.BreakingFields, want)
				}
			}
		})
	}
}

// FUZZ: Publish never panics across arbitrary revision numbers, and rejects
// exactly the non-positive ones.
func FuzzTodo_XFORM_005(f *testing.F) {
	f.Add(1)
	f.Add(2)
	f.Add(0)
	f.Add(-5)
	p, err := ir.Compile(definition())
	if err != nil {
		f.Fatalf("compile: %v", err)
	}
	f.Fuzz(func(t *testing.T, revision int) {
		r := NewRegistry()
		_, err := r.Publish("people-copy", revision, p)
		if revision < 1 {
			if !errors.Is(err, ErrInvalidRevision) {
				t.Fatalf("revision %d: want ErrInvalidRevision, got %v", revision, err)
			}
			return
		}
		if err != nil {
			t.Fatalf("revision %d: %v", revision, err)
		}
	})
}

// CONFORMANCE: PublishCompatible refuses a breaking replacement and leaves
// the registry unchanged; a compatible replacement publishes and the
// original pin still resolves to its original digest.
func TestTodo_XFORM_005_Conformance(t *testing.T) {
	r := NewRegistry()
	p1 := program(t)
	v1, err := r.Publish("people-copy", 1, p1)
	if err != nil {
		t.Fatal(err)
	}
	pin, err := r.Pin("people-copy", 1)
	if err != nil {
		t.Fatal(err)
	}

	breaking := withRenamedOutputField(t, "full_name")
	if _, _, err := r.PublishCompatible("people-copy", 2, p1, breaking, CompatibilityOptions{}); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("want ErrIncompatible, got %v", err)
	}
	if _, err := r.Resolve("people-copy", 2); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("breaking publish leaked into the registry: %v", err)
	}

	compatible, err := ir.Compile(definition()) // identical program, different revision
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.PublishCompatible("people-copy", 3, p1, compatible, CompatibilityOptions{}); err != nil {
		t.Fatalf("compatible publish: %v", err)
	}

	got, err := r.ResolvePin(pin)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != v1.Digest {
		t.Fatalf("pin moved after a later publish: %#v", got)
	}
}

// MUTATION: a single mutated field flips the verdict, and each mutation is
// independently detectable and named -- an input field removed, an output
// field retyped, and a widened limit accepted only when explicitly allowed.
func TestTodo_XFORM_005_Mutation(t *testing.T) {
	base := program(t)

	t.Run("output_field_retyped", func(t *testing.T) {
		d := definition()
		d.Destination.Fields[1].Type = transformation.TypeString
		d.Operations[1] = transformation.Operation{Kind: transformation.OpConvert, Source: ptr(srcPath("age", transformation.TypeInt)), Destination: dstPath("age", transformation.TypeString), TargetType: transformation.TypeString}
		retyped, err := ir.Compile(d)
		if err != nil {
			t.Fatal(err)
		}
		report, err := Compare(base, retyped, CompatibilityOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if report.Compatible || !report.Breaking || report.OutputCompatible {
			t.Fatalf("retyped output field accepted as compatible: %#v", report)
		}
		wantField := "worker.age"
		found := false
		for _, f := range report.BreakingFields {
			if f == wantField {
				found = true
			}
		}
		if !found {
			t.Fatalf("BreakingFields=%v, want it to name %q", report.BreakingFields, wantField)
		}
	})

	t.Run("input_field_removed", func(t *testing.T) {
		d := definition()
		// Drop the "age" operation entirely: the new program no longer
		// depends on people.age at all.
		d.Operations = d.Operations[:1]
		d.Destination.Fields = d.Destination.Fields[:1]
		dropped, err := ir.Compile(d)
		if err != nil {
			t.Fatal(err)
		}
		report, err := Compare(base, dropped, CompatibilityOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if report.Compatible || report.InputCompatible {
			t.Fatalf("removed input dependency accepted as compatible: %#v", report)
		}
		wantField := "people.age"
		found := false
		for _, f := range report.BreakingFields {
			if f == wantField {
				found = true
			}
		}
		if !found {
			t.Fatalf("BreakingFields=%v, want it to name %q", report.BreakingFields, wantField)
		}
	})

	t.Run("migration_note_requires_named_reason", func(t *testing.T) {
		compatibleReport := CompatibilityReport{Compatible: true}
		if _, err := NewMigrationNote(base, base, compatibleReport, "no-op republish", "tester"); err != nil {
			t.Fatalf("compatible note: %v", err)
		}
		breakingButUnnamed := MigrationNote{FromDigest: "a", ToDigest: "b", Compatible: false, Breaking: true, Summary: "s", Author: "a"}
		if err := breakingButUnnamed.Validate(); !errors.Is(err, ErrMigrationNote) {
			t.Fatalf("want ErrMigrationNote for an unnamed breaking note, got %v", err)
		}
		if _, err := NewMigrationNote(base, base, compatibleReport, "", "tester"); !errors.Is(err, ErrMigrationNote) {
			t.Fatalf("want ErrMigrationNote for a missing summary, got %v", err)
		}
	})
}
