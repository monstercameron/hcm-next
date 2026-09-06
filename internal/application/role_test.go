package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/internal/platform/bootstrap"
)

// TestRolesAreAClosedSetNotARegistry is the role half of ARCH-GO-020's RED
// clause about global mutable registries. The assertion is not that there is
// one role today; it is that the answer to "which roles does this binary
// have" is computed by a function in this package rather than accumulated by
// whichever packages happened to be linked in.
func TestRolesAreAClosedSetNotARegistry(t *testing.T) {
	first := Roles()
	if len(first) == 0 {
		t.Fatal("Roles() is empty; a composition root with no roles composes nothing")
	}
	first[0] = Role("mutated-by-a-caller")
	second := Roles()
	if second[0] == Role("mutated-by-a-caller") {
		t.Fatal("Roles() handed out shared state: a caller mutated the role set")
	}
	if second[0] != RoleServe {
		t.Errorf("Roles()[0] = %q, want %q", second[0], RoleServe)
	}
	if !strings.Contains(RoleNames(), string(RoleServe)) {
		t.Errorf("RoleNames() = %q, want it to name %q", RoleNames(), RoleServe)
	}
}

// TestRoleValidateRejectsAnUnknownRole proves role selection is a decision
// this package makes, with a refusal a command can report.
func TestRoleValidateRejectsAnUnknownRole(t *testing.T) {
	if err := RoleServe.Validate(); err != nil {
		t.Fatalf("RoleServe.Validate: %v", err)
	}
	err := Role("projector").Validate()
	if err == nil {
		t.Fatal("Validate accepted an unknown role")
	}
	if !strings.Contains(err.Error(), "projector") || !strings.Contains(err.Error(), string(RoleServe)) {
		t.Errorf("error = %q, want it to name both the rejected role and the known set", err)
	}
	if RoleServe.String() != "serve" {
		t.Errorf("RoleServe.String() = %q, want serve", RoleServe.String())
	}
}

// TestSpecForSelectsARoleAndDeclaresItsWholeConfiguration is what a command
// actually calls: one selection that yields a bootstrap Spec already carrying
// the role's fields, validation, database dependency and shutdown deadline,
// so the command adds none of them.
func TestSpecForSelectsARoleAndDeclaresItsWholeConfiguration(t *testing.T) {
	spec, err := SpecFor(RoleServe, []string{"-tenant=acme"}, WithMigrator(nil))
	if err != nil {
		t.Fatalf("SpecFor(serve): %v", err)
	}
	if spec.Role != bootstrap.RoleHCMNext {
		t.Errorf("spec.Role = %q, want %q", spec.Role, bootstrap.RoleHCMNext)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "-tenant=acme" {
		t.Errorf("spec.Args = %v, want the command's arguments passed through", spec.Args)
	}
	if len(spec.ConfigFields) != len(ServeConfigFields()) {
		t.Errorf("spec declares %d fields, want the role's %d", len(spec.ConfigFields), len(ServeConfigFields()))
	}
	if spec.Validate == nil || spec.Build == nil || spec.DBPoolFactory == nil {
		t.Error("spec left validation, build or the pool factory to the command")
	}
	if spec.DatabaseURLField != FieldDatabaseURL {
		t.Errorf("spec.DatabaseURLField = %q, want %q", spec.DatabaseURLField, FieldDatabaseURL)
	}
	if spec.ShutdownDeadline != ShutdownGrace {
		t.Errorf("spec.ShutdownDeadline = %s, want %s", spec.ShutdownDeadline, ShutdownGrace)
	}
	if spec.Logger == nil {
		t.Error("spec has no logger; the command would have to build one")
	}

	if _, err := SpecFor(Role("scheduler"), nil); err == nil {
		t.Fatal("SpecFor accepted a role this root cannot compose")
	}
}

// TestSpecForHonoursTheSuppliedLogger proves the composition seams reach the
// Spec too: a caller that supplies a logger gets that logger, with no
// production-only branch deciding which one is in play.
func TestSpecForHonoursTheSuppliedLogger(t *testing.T) {
	recorder := &recordingLogger{}
	spec, err := SpecFor(RoleServe, nil, WithLogger(recorder))
	if err != nil {
		t.Fatalf("SpecFor: %v", err)
	}
	if spec.Logger != bootstrap.Logger(recorder) {
		t.Errorf("spec.Logger = %T, want the supplied recorder", spec.Logger)
	}
}
