package operations_test

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/tools/planning/operations"
	"github.com/monstercameron/hcm-next/tools/policy/processroles"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test source")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find repository root")
		}
		dir = parent
	}
}

func loadRegistries(t *testing.T) (*operations.Registry, *operations.Registry) {
	t.Helper()
	root := repoRoot(t)
	ownership, err := operations.Load(filepath.Join(root, filepath.FromSlash(operations.DefaultOwnershipPath)))
	if err != nil {
		t.Fatalf("load ownership registry: %v", err)
	}
	dependencies, err := operations.Load(filepath.Join(root, filepath.FromSlash(operations.DefaultDependencyPath)))
	if err != nil {
		t.Fatalf("load dependency registry: %v", err)
	}
	return ownership, dependencies
}

var ops007Now = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// TestTodo_OPS_007 is the OPS-007 primary readiness oracle. A registry is
// ready only while every production execution role has independent ownership
// continuity and every logical edge has a complete, expiring operating
// contract.
func TestTodo_OPS_007(t *testing.T) {
	ownership, dependencies := loadRegistries(t)
	result := operations.Validate(ownership, dependencies, ops007Now)
	if !result.Ready() {
		t.Fatalf("OPS-007 readiness = %s, want READY: %v", result.Status, result.Diagnostics)
	}

	wantKinds := map[string]bool{
		"SERVICE": true, "WORKFLOW": true, "CONNECTOR": true, "STORE": true, "PROVIDER": true,
	}
	gotKinds := map[string]bool{}
	for _, entry := range ownership.Ownership {
		gotKinds[entry.Kind] = true
	}
	for kind := range wantKinds {
		if !gotKinds[kind] {
			t.Errorf("ownership registry has no %s row", kind)
		}
	}
	if len(ownership.Ownership) < len(wantKinds) {
		t.Fatalf("ownership registry has %d rows, want at least %d", len(ownership.Ownership), len(wantKinds))
	}
	if len(dependencies.Dependencies) == 0 {
		t.Fatal("dependency registry is empty")
	}
}

// TestTodo_OPS_007_Integration cross-checks the ownership registry against
// the actual initial process-role manifest. Later commands are intentionally
// not production execution roles until their process rows become initial.
func TestTodo_OPS_007_Integration(t *testing.T) {
	ownership, dependencies := loadRegistries(t)
	root := repoRoot(t)
	roles, err := processroles.Load(filepath.Join(root, "definitions", "architecture", "process-roles.yaml"))
	if err != nil {
		t.Fatalf("load process-role manifest: %v", err)
	}
	registered := map[string]bool{}
	for _, entry := range ownership.Ownership {
		registered[entry.ID] = true
	}
	for _, command := range roles.InitialCommands() {
		if !registered[command] {
			t.Errorf("initial process %q has no ownership row", command)
		}
	}
	for _, edge := range dependencies.Dependencies {
		if !registered[edge.Consumer] {
			t.Errorf("dependency consumer %q has no ownership row", edge.Consumer)
		}
		if !registered[edge.Dependency] {
			t.Errorf("dependency target %q has no ownership row", edge.Dependency)
		}
	}
	if got := operations.OwnershipIDs(ownership); !slices.IsSorted(got) {
		t.Fatalf("ownership IDs are not deterministic/sorted: %v", got)
	}
	if result := operations.Validate(ownership, dependencies, ops007Now); !result.Ready() {
		t.Fatalf("integrated registries are not ready: %v", result.Diagnostics)
	}
}

// TestTodo_OPS_007_Recovery seeds expired continuity evidence and proves the
// release result is fail-closed. Validate is pure, so this rejection has no
// authoritative row, event, outbox, human-work or provider side effect to
// clean up or reconcile.
func TestTodo_OPS_007_Recovery(t *testing.T) {
	ownership, dependencies := loadRegistries(t)
	ownership.Ownership[0].ExpiresAt = "2026-09-02T23:59:59Z"

	result := operations.Validate(ownership, dependencies, ops007Now)
	if result.Status != operations.StatusRejected {
		t.Fatalf("expired ownership evidence returned %s, want %s", result.Status, operations.StatusRejected)
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Registry == "ownership" && diagnostic.Entry == ownership.Ownership[0].ID &&
			diagnostic.Field == "expires_at" && diagnostic.State == "EXPIRED" && diagnostic.Version == "v1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expired ownership did not produce the required field/state/version diagnostic: %v", result.Diagnostics)
	}

	ownership, dependencies = loadRegistries(t)
	ownership.Ownership[0].PrimaryOwner = ""
	result = operations.Validate(ownership, dependencies, ops007Now)
	if result.Status != operations.StatusRejected {
		t.Fatalf("missing primary owner returned %s, want %s", result.Status, operations.StatusRejected)
	}
}
