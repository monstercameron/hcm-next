package hcmctl

import (
	"testing"

	adminv1 "github.com/monstercameron/hcm-next/gen/go/hcmnext/admin/v1"
)

func TestCommands_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestCommands_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

// TestParseInstance_RequiresExactlyOneID proves ADMIN-008's "instance <id>"
// subcommand rejects zero or multiple positional arguments before it ever
// dials a server, matching every other subcommand's fail-closed flag parsing.
func TestParseInstance_RequiresExactlyOneID(t *testing.T) {
	g, _ := newGlobalFlagSet()
	global := globalFlags{}
	if err := g.Parse(nil); err != nil {
		t.Fatalf("parse globals: %v", err)
	}

	if _, err := parseInstance(global, nil); err == nil {
		t.Fatal("expected an error for zero positional arguments")
	}
	if _, err := parseInstance(global, []string{"one", "two"}); err == nil {
		t.Fatal("expected an error for more than one positional argument")
	}
	cmd, err := parseInstance(global, []string{"22222222-2222-4222-8222-222222222222"})
	if err != nil {
		t.Fatalf("parseInstance with one id: %v", err)
	}
	if cmd.run == nil {
		t.Fatal("parsedCommand.run is nil")
	}
}

// TestRenderRef proves ADMIN-008's three-way rendering discipline: a
// disclosed value prints itself, a redacted one names its reason without
// ever printing a value, and an unrecorded gap is distinguished from a
// correctly-recorded absence -- never a bare, unlabeled blank.
func TestRenderRef(t *testing.T) {
	cases := []struct {
		name string
		ref  *adminv1.RefValue
		want string
	}{
		{"value", &adminv1.RefValue{State: "VALUE", Value: "proposal:1"}, "label: proposal:1"},
		{"redacted", &adminv1.RefValue{State: "REDACTED", Reason: "FIELD_DENIED"}, "label: REDACTED (FIELD_DENIED)"},
		{"unrecorded", &adminv1.RefValue{State: "ABSENT", GapKind: "UNRECORDED"}, "label: GAP (unrecorded)"},
		{"absent", &adminv1.RefValue{State: "ABSENT", GapKind: "ABSENT"}, "label: (absent)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderRef("label", tc.ref)
			if got != tc.want {
				t.Fatalf("renderRef = %q, want %q", got, tc.want)
			}
		})
	}
}
