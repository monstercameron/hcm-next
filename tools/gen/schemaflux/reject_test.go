package schemaflux_test

import (
	"errors"
	"path/filepath"
	"testing"

	sfx "github.com/monstercameron/hcm-next/tools/gen/schemaflux"
)

// mutateOne returns a copy of defs with fn applied to the single definition
// whose IntentTypeID matches id, leaving every other definition untouched.
func mutateOne(t *testing.T, defs []sfx.Definition, id string, fn func(*sfx.Definition)) []sfx.Definition {
	t.Helper()
	out := append([]sfx.Definition(nil), defs...)
	found := false
	for i := range out {
		if out[i].IntentTypeID == id {
			fn(&out[i])
			found = true
		}
	}
	if !found {
		t.Fatalf("mutateOne: no definition with intent_type_id %q", id)
	}
	return out
}

// TestTodo_TOOL_005 is the TOOL-005 primary test: table tests inject an
// unknown schema, capability, owner or family reference into one of the
// fourteen definitions and expect a typed compile error at the source path.
// The valid fixture itself must compile with zero unresolved references
// (TOOL-005 GREEN), so the table's first case is the unmutated control.
func TestTodo_TOOL_005(t *testing.T) {
	defs, err := sfx.LoadDefinitions(definitionsPath(t))
	if err != nil {
		t.Fatalf("LoadDefinitions: %v", err)
	}
	manifest, err := sfx.LoadCapabilityManifest(filepath.Join("testdata", "capability_manifest.yaml"))
	if err != nil {
		t.Fatalf("LoadCapabilityManifest: %v", err)
	}

	const targetID = "hcmnext.people.change_manager"

	tests := []struct {
		name       string
		mutate     func(*sfx.Definition)
		wantKind   sfx.ReferenceKind
		wantField  string
		wantErrors int // exact count of UnresolvedReferenceError expected
	}{
		{
			name:       "valid_fixture_compiles_clean",
			mutate:     func(*sfx.Definition) {},
			wantErrors: 0,
		},
		{
			name: "unknown_schema_reference",
			mutate: func(d *sfx.Definition) {
				d.InputSchemaRef = "made_up_schema_with_no_version"
			},
			wantKind:   sfx.ReferenceKindSchema,
			wantField:  "input_schema_ref",
			wantErrors: 1,
		},
		{
			name: "unknown_capability_reference",
			mutate: func(d *sfx.Definition) {
				d.RequiredCapabilities = append(append([]string(nil), d.RequiredCapabilities...), "not a capability reference")
			},
			wantKind:   sfx.ReferenceKindCapability,
			wantField:  "required_capabilities",
			wantErrors: 1,
		},
		{
			name: "unknown_owner_domain",
			mutate: func(d *sfx.Definition) {
				d.OwnerDomain = "MADE_UP_DOMAIN"
			},
			wantKind:   sfx.ReferenceKindOwner,
			wantField:  "owner_domain",
			wantErrors: 1,
		},
		{
			name: "unknown_owner_plane",
			mutate: func(d *sfx.Definition) {
				d.OwnerPlane = "MADE_UP_PLANE"
			},
			wantKind:   sfx.ReferenceKindOwner,
			wantField:  "owner_plane",
			wantErrors: 1,
		},
		{
			name: "retired_kernel_family",
			mutate: func(d *sfx.Definition) {
				// PROCESS_REQUEST is a real but retired family
				// (schema/proto/hcmnext/intents/v1/business_intent.proto
				// reserves it); it is not merely malformed text, which is
				// exactly why it must still be rejected as unresolved.
				d.KernelFamily = "PROCESS_REQUEST"
			},
			wantKind:   sfx.ReferenceKindFamily,
			wantField:  "kernel_family",
			wantErrors: 1,
		},
		{
			name: "unknown_kernel_family",
			mutate: func(d *sfx.Definition) {
				d.KernelFamily = "TOTALLY_UNKNOWN_FAMILY"
			},
			wantKind:   sfx.ReferenceKindFamily,
			wantField:  "kernel_family",
			wantErrors: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mutated := mutateOne(t, defs, targetID, tc.mutate)
			_, errs := sfx.Compile(mutated, manifest)

			if len(errs) != tc.wantErrors {
				t.Fatalf("Compile returned %d error(s), want %d: %v", len(errs), tc.wantErrors, errs)
			}
			if tc.wantErrors == 0 {
				return
			}

			var ref *sfx.UnresolvedReferenceError
			if !errors.As(errs[0], &ref) {
				t.Fatalf("error is not *UnresolvedReferenceError: %v (%T)", errs[0], errs[0])
			}
			if ref.Kind != tc.wantKind {
				t.Errorf("Kind = %s, want %s", ref.Kind, tc.wantKind)
			}
			if ref.Field != tc.wantField {
				t.Errorf("Field = %s, want %s", ref.Field, tc.wantField)
			}
			if ref.IntentTypeID != targetID {
				t.Errorf("IntentTypeID = %s, want %s", ref.IntentTypeID, targetID)
			}
			if ref.SourceFile == "" {
				t.Error("SourceFile is empty; a compile error must cite a source path")
			}
			if ref.SourceLine <= 0 {
				t.Error("SourceLine is not positive; a compile error must cite a source line")
			}
			if ref.Error() == "" {
				t.Error("Error() returned an empty message")
			}
		})
	}
}

// TestTodo_TOOL_005_Golden pins the exact diagnostic shape (file:line prefix,
// "unresolved <kind> reference" phrasing) so the human-facing error message
// cannot silently degrade into something less actionable.
func TestTodo_TOOL_005_Golden(t *testing.T) {
	defs, err := sfx.LoadDefinitions(definitionsPath(t))
	if err != nil {
		t.Fatalf("LoadDefinitions: %v", err)
	}
	manifest, err := sfx.LoadCapabilityManifest(filepath.Join("testdata", "capability_manifest.yaml"))
	if err != nil {
		t.Fatalf("LoadCapabilityManifest: %v", err)
	}

	mutated := mutateOne(t, defs, "hcmnext.people.change_manager", func(d *sfx.Definition) {
		d.OwnerDomain = "MADE_UP_DOMAIN"
	})
	_, errs := sfx.Compile(mutated, manifest)
	if len(errs) != 1 {
		t.Fatalf("Compile returned %d error(s), want 1: %v", len(errs), errs)
	}

	var ref *sfx.UnresolvedReferenceError
	if !errors.As(errs[0], &ref) {
		t.Fatalf("error is not *UnresolvedReferenceError: %v", errs[0])
	}

	want := "hcmnext.people.change_manager: unresolved owner reference \"MADE_UP_DOMAIN\" in field \"owner_domain\": not a declared owner domain"
	got := ref.Error()
	// The golden compares everything after "<file>:<line>: " since SourceFile
	// is an absolute, machine-specific path: only the suffix is portable.
	suffix := got
	if idx := indexAfterLocation(got); idx >= 0 {
		suffix = got[idx:]
	}
	if suffix != want {
		t.Errorf("error message suffix = %q, want %q\nfull message: %s", suffix, want, got)
	}
}

// indexAfterLocation returns the index just past the second ": " in s
// (skipping the "<file>:<line>: " location prefix an UnresolvedReferenceError
// always starts with), or -1 if s has fewer than two colon-space separators
// before the rest of the message.
func indexAfterLocation(s string) int {
	// Location is "<file>:<line>: " — file paths on Windows contain ':'
	// (drive letters) so split on ": " and drop exactly one leading segment
	// (the file:line pair), not "up to the first colon".
	for i := 0; i+1 < len(s); i++ {
		if s[i] == ':' && s[i+1] == ' ' {
			return i + 2
		}
	}
	return -1
}
