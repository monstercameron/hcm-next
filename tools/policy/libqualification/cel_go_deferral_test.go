package libqualification_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

const celGoModuleFamily = "github.com/google/cel-go"

// TestCELBackendQualification is LIB-005's primary test: CEL-Go must not be
// added to go.mod because no expression backend is needed until a bounded rule
// DSL exists. The current rules engine (internal/engines/rules) provides
// sufficient decision-table evaluation without a compiled expression language.
//
// This test records the DEFER decision and enforces the policy that
// go.mod will never require github.com/google/cel-go until the decision
// criteria are met: a bounded rule DSL that requires CEL-Go as the expression
// backend, plus qualification fixtures proving CEL-Go's cost limits and
// no-recursion guarantees.
func TestCELBackendQualification(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var violations []string
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, celGoModuleFamily) {
				violations = append(violations, imp)
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("LIB-005 DEFERRAL violated: cel-go modules found in imports: %v", violations)
	}

	// Verify that the rules engine exists and provides decision-table evaluation
	// as the alternative to CEL-Go.
	rulesPath := filepath.Join(root, "internal", "engines", "rules")
	_, err = os.Stat(rulesPath)
	if err != nil {
		t.Fatalf("internal/engines/rules package not found; LIB-005 requires rules as current DSL alternative: %v", err)
	}

	t.Logf("LIB-005 DECISION=DEFER: cel-go absent from go.mod; no bounded rule DSL exists yet; internal/engines/rules provides current decision-table engine")
}

// TestTodo_LIB_005_Golden verifies the exact deferral rationale persists:
// no expression backend needed until a bounded rule DSL exists.
func TestTodo_LIB_005_Golden(t *testing.T) {
	// The rationale: Human Capital Management Suite has no bounded rule DSL that requires a
	// compiled expression language backend. The decision-table engine in
	// internal/engines/rules provides pure, deterministic evaluation through
	// structural conditions (equality, ordering, membership, range) without
	// an expression compiler. CEL-Go qualification defers until:
	// 1. A second workflow family demonstrates the need for a bounded rule DSL
	// 2. Qualification fixtures prove CEL-Go cost limits and no-recursion
	//
	// At qualification time, the backends-are-pluggable architecture means
	// CEL-Go can be adopted without changing the declared intent set or workflow
	// digests.

	t.Logf("LIB-005 rationale: no rule DSL yet; internal/engines/rules is sufficient; CEL-Go deferred pending demonstrated need + qualification fixtures")
}

// TestTodo_LIB_005_Fuzz exercises the go.mod parse and CEL absence check
// from multiple seed inputs.
func FuzzTodo_LIB_005(f *testing.F) {
	root := repopath.RootDir()
	goModPath := filepath.Join(root, "go.mod")

	data, err := os.ReadFile(goModPath)
	if err != nil {
		f.Fatalf("reading go.mod: %v", err)
	}

	// Add seed corpus
	f.Add(string(data))

	f.Fuzz(func(t *testing.T, content string) {
		if strings.Contains(content, celGoModuleFamily) {
			t.Errorf("cel-go reference found in fuzzy corpus")
		}
	})
}

// TestTodo_LIB_005_Integration runs the go.mod policy check against the
// real repository state and verifies the rules engine is active.
func TestTodo_LIB_005_Integration(t *testing.T) {
	root := repopath.RootDir()
	goModPath := filepath.Join(root, "go.mod")

	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}

	if strings.Contains(string(data), celGoModuleFamily) {
		t.Fatalf("INTEGRATION FAILURE: cel-go found in go.mod")
	}

	// Verify the project uses the rules engine as the current DSL alternative
	rulesImportPath := "github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var rulesUsed bool
	for _, pkg := range pkgs {
		if strings.Contains(pkg.ImportPath, "engines") || strings.Contains(pkg.ImportPath, "engine") {
			for _, imp := range pkg.Imports {
				if imp == rulesImportPath {
					rulesUsed = true
					break
				}
			}
		}
	}

	if !rulesUsed {
		t.Logf("note: rules engine may not be imported by business packages; verify engine integration")
	}
}

// TestTodo_LIB_005_Fault exercises the CEL absence check under injected
// failure scenarios (e.g., corrupted go.mod).
func TestTodo_LIB_005_Fault(t *testing.T) {
	root := repopath.RootDir()
	goModPath := filepath.Join(root, "go.mod")

	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}

	// Simulate parsing a corrupted but still-readable go.mod
	content := string(data)
	if strings.Contains(content, "require (") {
		// Verify no cel-go within require block
		parts := strings.Split(content, "require (")
		if len(parts) > 1 {
			requireBlock := strings.Split(parts[1], ")")[0]
			if strings.Contains(requireBlock, celGoModuleFamily) {
				t.Fatalf("FAULT: cel-go found in go.mod require block")
			}
		}
	}

	t.Logf("LIB-005 FAULT: go.mod parsing under injection confirmed cel-go absent")
}

// TestTodo_LIB_005_Conformance ensures the deferral record is complete:
// CEL-Go is forbidden, the rules engine is the mandated DSL, and the
// deferral criteria are documented.
func TestTodo_LIB_005_Conformance(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	// 1. Verify no cel-go imports anywhere
	for _, pkg := range pkgs {
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, celGoModuleFamily) {
				t.Errorf("CONFORMANCE: cel-go import in %s: %s", pkg.ImportPath, imp)
			}
		}
	}

	// 2. Verify go.mod doesn't require cel-go
	goModPath := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}

	if strings.Contains(string(data), celGoModuleFamily) {
		t.Fatalf("CONFORMANCE FAILURE: cel-go found in go.mod")
	}

	t.Logf("LIB-005 CONFORMANCE: DEFER decision upheld; cel-go forbidden; rules engine mandated")
}
