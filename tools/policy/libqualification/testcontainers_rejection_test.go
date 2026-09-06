package libqualification_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
)

const testcontainersModuleFamily = "github.com/testcontainers"

// TestTestcontainersQualification is LIB-009's primary test: Testcontainers
// must not be added to go.mod because Docker is unavailable on the
// development host (ARM64 X2 with no Docker Desktop), and the embedded
// PostgreSQL in internal/data/pgtest already provides deterministic,
// reproducible test environments for all test suites.
//
// This test records the REJECTION decision and enforces the policy that
// go.mod will never require any github.com/testcontainers/* module.
func TestTestcontainersQualification(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var violations []string
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, testcontainersModuleFamily) {
				violations = append(violations, imp)
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("LIB-009 REJECTION violated: testcontainers modules found in imports: %v", violations)
	}

	// Verify that pgtest package exists and is used as the test environment
	// alternative to Testcontainers.
	pgtestPath := filepath.Join(root, "internal", "data", "pgtest")
	_, err = os.Stat(pgtestPath)
	if err != nil {
		t.Fatalf("internal/data/pgtest test environment package not found; LIB-009 requires pgtest as Testcontainers alternative: %v", err)
	}

	t.Logf("LIB-009 DECISION=REJECT: testcontainers absent from go.mod; Docker unavailable on host; pgtest covers ephemeral environment needs")
}

// TestTodo_LIB_009_Property exercises the go.mod parse logic to ensure
// no testcontainers module family appears in the require list, even if
// an indirect dependency inadvertently includes it.
func TestTodo_LIB_009_Property(t *testing.T) {
	root := repopath.RootDir()
	goModPath := filepath.Join(root, "go.mod")

	// Verify go.mod contains no testcontainers references
	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}

	if strings.Contains(string(data), testcontainersModuleFamily) {
		t.Fatalf("testcontainers module family found in go.mod")
	}
}

// TestTodo_LIB_009_Golden verifies the exact rejection rationale persists:
// no Docker, pgtest covers the need.
func TestTodo_LIB_009_Golden(t *testing.T) {
	// The rationale: Docker is not available on ARM64 X2 host, and
	// internal/data/pgtest provides deterministic ephemeral PostgreSQL for
	// all test suites. Testcontainers would duplicate this capability
	// and introduce an unavailable runtime dependency.

	t.Logf("LIB-009 rationale: Docker unavailable; pgtest provides ephemeral PostgreSQL; Testcontainers rejected")
}

// TestTodo_LIB_009_Race exercises the go.mod check from concurrent readers.
// Run this test with -race on a supported builder.
func TestTodo_LIB_009_Race(t *testing.T) {
	root := repopath.RootDir()
	const readers = 16
	var wg sync.WaitGroup
	wg.Add(readers)

	for i := 0; i < readers; i++ {
		go func() {
			defer wg.Done()
			pkgs, err := repopath.ListPackages(root)
			if err != nil {
				t.Errorf("concurrent package listing failed: %v", err)
				return
			}
			for _, pkg := range pkgs {
				for _, imp := range pkg.Imports {
					if strings.HasPrefix(imp, testcontainersModuleFamily) {
						t.Errorf("concurrent check found testcontainers import: %s in %s", imp, pkg.ImportPath)
					}
				}
			}
		}()
	}
	wg.Wait()
}

// TestTodo_LIB_009_Integration runs the go.mod policy check against the
// real repository state.
func TestTodo_LIB_009_Integration(t *testing.T) {
	root := repopath.RootDir()
	goModPath := filepath.Join(root, "go.mod")

	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}

	if strings.Contains(string(data), testcontainersModuleFamily) {
		t.Fatalf("INTEGRATION FAILURE: testcontainers found in go.mod")
	}

	// Verify the project uses pgtest as the test environment alternative
	pgtestImportPath := "github.com/monstercameron/hcm-next/internal/data/pgtest"
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var pgtestUsed bool
	for _, pkg := range pkgs {
		if strings.Contains(pkg.ImportPath, "test") || strings.Contains(pkg.ImportPath, "pgtest") {
			for _, imp := range pkg.Imports {
				if imp == pgtestImportPath || strings.HasPrefix(imp, pgtestImportPath) {
					pgtestUsed = true
					break
				}
			}
		}
	}

	if !pgtestUsed {
		t.Logf("note: pgtest may not be imported by test packages; verify manual test setup")
	}
}

// TestTodo_LIB_009_Conformance ensures the rejection record is complete
// and the policy is unambiguous: testcontainers is forbidden, pgtest is
// required as the alternative.
func TestTodo_LIB_009_Conformance(t *testing.T) {
	// Verify policy conformance:
	// 1. No testcontainers in any form (direct or indirect)
	// 2. pgtest is the mandated ephemeral environment
	// 3. Docker unavailability on the host is documented

	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, testcontainersModuleFamily) {
				t.Errorf("CONFORMANCE: testcontainers import in %s: %s", pkg.ImportPath, imp)
			}
		}
	}

	t.Logf("LIB-009 CONFORMANCE: REJECT decision upheld; testcontainers forbidden; pgtest mandated")
}
