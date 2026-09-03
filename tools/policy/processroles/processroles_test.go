package processroles_test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
	"github.com/monstercameron/hcm-next/tools/policy/layout"
	"github.com/monstercameron/hcm-next/tools/policy/processroles"
)

// TestTodo_SVC_001 is the SVC-001 primary test: every cmd/* directory has
// exactly one process-roles.yaml row and vice versa, except a directory
// explicitly waived by the repository-layout manifest (e.g. cmd/gen-todos,
// tracked there as ARCH-GO-001 kind "unapproved_command_root" rather than
// duplicated here); scheduler is listed with status "later" and must not
// have a directory yet. hcmctl (SVC-011/ADMIN-001's operator CLI) was
// promoted from "later" to "initial" on 2026-09-03 alongside
// repository-layout.yaml's approved_commands.initial, once cmd/hcmctl
// itself existed.
func TestTodo_SVC_001(t *testing.T) {
	root := repopath.RootDir()

	m, err := processroles.Load(filepath.Join(root, "definitions", "architecture", "process-roles.yaml"))
	if err != nil {
		t.Fatalf("loading process-roles manifest: %v", err)
	}

	layoutManifest, err := layout.Load(filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
	if err != nil {
		t.Fatalf("loading repository-layout manifest: %v", err)
	}

	if dupes := m.DuplicateCommands(); len(dupes) > 0 {
		t.Fatalf("process-roles manifest has duplicate command rows: %v", dupes)
	}

	initial := m.InitialCommands()
	later := m.LaterCommands()
	sort.Strings(initial)
	sort.Strings(later)

	wantInitial := []string{"hcmnext", "hcmctl", "migrate", "projector", "worker"}
	sort.Strings(wantInitial)
	if !equalStrings(initial, wantInitial) {
		t.Fatalf("process-roles initial commands = %v, want %v", initial, wantInitial)
	}

	wantLater := []string{"scheduler"}
	sort.Strings(wantLater)
	if !equalStrings(later, wantLater) {
		t.Fatalf("process-roles later commands = %v, want %v", later, wantLater)
	}

	cmdDirs, err := processroles.ListCmdDirectories(root)
	if err != nil {
		t.Fatalf("listing cmd directories: %v", err)
	}

	// Every actual cmd/* directory is either an initial-status manifest row
	// or a documented repository-layout waiver (reported, not silently
	// dropped).
	initialSet := toSet(initial)
	for _, dir := range cmdDirs {
		if initialSet[dir] {
			continue
		}
		v := layoutManifest.ClassifyImportPath(m.Module + "/cmd/" + dir)
		if v.Waived {
			t.Logf("cmd/%s has no process-roles row; covered by a repository-layout waiver instead: %s", dir, v.Reason)
			continue
		}
		t.Errorf("cmd/%s exists but has no process-roles manifest row and no repository-layout waiver", dir)
	}

	// Every initial-status manifest row has a real directory.
	dirSet := toSet(cmdDirs)
	for _, name := range initial {
		if !dirSet[name] {
			t.Errorf("process-roles lists %q as an initial command but cmd/%s does not exist", name, name)
		}
	}

	// "later" commands (scheduler) must not have a directory yet.
	for _, name := range later {
		if dirSet[name] {
			t.Errorf("process-roles lists %q as status \"later\" but cmd/%s already exists", name, name)
		}
	}
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

func toSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, i := range items {
		set[i] = true
	}
	return set
}
