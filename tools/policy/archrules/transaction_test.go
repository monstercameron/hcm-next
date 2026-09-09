package archrules_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// TestTransactionConflictDependencyDirection is the ARCH-GO-011 primary
// test: internal/transaction (the prepare/commit coordinator) may depend on
// internal/transaction/conflict (it calls conflict analysis before
// committing), but internal/transaction/conflict must never import back up
// into the coordinator's own internals -- that reverse edge is exactly the
// cycle this rule exists to forbid. Note that ConflictRoot is itself a
// subpackage of TransactionRoot, so the check (CheckConflictImportsCoordinator)
// specifically excludes conflict's own intra-package imports rather than
// naively flagging "importer under conflict, imported under transaction".
func TestTransactionConflictDependencyDirection(t *testing.T) {
	cfg := loadArchConfig(t)
	tc := cfg.TransactionConflict

	cases := []struct {
		name     string
		importer string
		imported string
		wantV    bool
	}{
		{"coordinator importing conflict analysis is allowed", tc.TransactionRoot, tc.ConflictRoot, false},
		{"coordinator subpackage importing conflict analysis is allowed", tc.TransactionRoot + "/prepare", tc.ConflictRoot, false},
		{"conflict importing the coordinator is forbidden", tc.ConflictRoot, tc.TransactionRoot, true},
		{"conflict importing a coordinator subpackage is forbidden", tc.ConflictRoot, tc.TransactionRoot + "/commit", true},
		{"conflict importing itself is allowed", tc.ConflictRoot, tc.ConflictRoot + "/footprint", false},
		{"conflict subpackage importing conflict root is allowed", tc.ConflictRoot + "/footprint", tc.ConflictRoot, false},
		{"an unrelated package is not this check's concern", "internal/domains/people", tc.TransactionRoot, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := archrules.CheckConflictImportsCoordinator(tc, c.importer, c.imported)
			if (v != nil) != c.wantV {
				t.Errorf("CheckConflictImportsCoordinator(%q -> %q) violation=%v, want %v", c.importer, c.imported, v != nil, c.wantV)
			}
		})
	}
}

// TestTodo_ARCH_GO_011_Integration runs the direction check against the
// real tree (a no-op today since neither internal/transaction nor
// internal/transaction/conflict exists yet; it activates automatically the
// day either package lands).
func TestTodo_ARCH_GO_011_Integration(t *testing.T) {
	cfg := loadArchConfig(t)
	tc := cfg.TransactionConflict
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var total int
	for _, pkg := range pkgs {
		rel, ok := archrules.TrimModule(cfg.Module, pkg.ImportPath)
		if !ok {
			continue
		}
		for _, imp := range pkg.Imports {
			impRel, ok := archrules.TrimModule(cfg.Module, imp)
			if !ok {
				continue
			}
			if v := archrules.CheckConflictImportsCoordinator(tc, rel, impRel); v != nil {
				total++
				t.Errorf("ARCH-GO-011 violation: %s imports %s (conflict must never import the coordinator back)", rel, impRel)
			}
		}
	}
	t.Logf("ARCH-GO-011: %d transaction/conflict direction violations", total)
}

// TestTodo_ARCH_GO_011_Race runs the direction check concurrently.
func TestTodo_ARCH_GO_011_Race(t *testing.T) {
	cfg := loadArchConfig(t)
	tc := cfg.TransactionConflict
	done := make(chan struct{}, 16)
	for i := 0; i < 16; i++ {
		go func() {
			_ = archrules.CheckConflictImportsCoordinator(tc, tc.ConflictRoot, tc.TransactionRoot)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 16; i++ {
		<-done
	}
}

// TestTodo_ARCH_GO_011_Security checks the declared forbidden_edges entry
// is exactly the conflict-to-transaction direction, never the reverse
// (which would wrongly forbid the intended coordinator-calls-conflict
// dependency).
func TestTodo_ARCH_GO_011_Security(t *testing.T) {
	cfg := loadArchConfig(t)
	tc := cfg.TransactionConflict
	for _, e := range tc.ForbiddenEdges {
		if e.From == tc.TransactionRoot && e.To == tc.ConflictRoot {
			t.Errorf("forbidden_edges wrongly forbids the intended coordinator -> conflict dependency (from=%s to=%s)", e.From, e.To)
		}
	}
	if archrules.CheckConflictImportsCoordinator(tc, tc.TransactionRoot, tc.ConflictRoot) != nil {
		t.Errorf("CheckConflictImportsCoordinator wrongly forbids the intended coordinator -> conflict dependency")
	}
}
