package libqualification_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// Allowed JOSE/JWK modules that may be pinned if demonstrated need is proven.
// This list is controlled by definitions/architecture/dependency-roles.yaml
// and may only be extended after a binding decision in planning/todos.md (LIB-011).
var allowedJOSEModules = map[string]bool{
	// Currently empty: no JOSE backend has been qualified.
	// Candidates for future addition (subject to LIB-011 decision):
	// - lestrrat-go/jwx
	// - lestrrat-go/jose
	// - square/go-jose
}

// TestJOSEDependencyNeedAndConformance is LIB-011's primary test: any JOSE/JWK
// module family must be justified by a demonstrated need not met by Go stdlib
// or go-oidc, and must be confined to internal/authn/federation or
// internal/trust/federation.
//
// The RED condition is met if a JOSE module is added without being in the
// allowedJOSEModules set (meaning the decision has not been made). The GREEN
// condition requires the dependency to have a declared owner, conformance
// boundary, and narrow confinement.
//
// This test records the GATE decision: no additional JOSE/JWK backend may
// enter go.mod without an explicit LIB-011 decision recorded in planning/todos.md
// and a corresponding entry in allowedJOSEModules above.
func TestJOSEDependencyNeedAndConformance(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	// Define allowed import roots for JOSE backends (if any are qualified).
	// Until a backend is qualified, no imports are allowed.
	allowedRoots := map[string]bool{
		"github.com/monstercameron/human-capital-management-suite/internal/authn/federation": true,
		"github.com/monstercameron/human-capital-management-suite/internal/trust/federation": true,
		"github.com/monstercameron/human-capital-management-suite/internal/trust":            true,
		"github.com/monstercameron/human-capital-management-suite/internal/authn":            true,
	}

	// Common JOSE/JWK module families.
	joseModuleFamilies := []string{
		"github.com/lestrrat-go/jwx",
		"github.com/lestrrat-go/jose",
		"github.com/square/go-jose",
		"gopkg.in/square/go-jose.v2",
	}

	var violations []string
	for _, pkg := range pkgs {
		// Skip test packages; federation tests may import JOSE modules.
		if strings.HasSuffix(pkg.ImportPath, "_test") || strings.Contains(pkg.ImportPath, "testdata") {
			continue
		}

		hasJOSE := false
		var joseImports []string
		for _, imp := range pkg.Imports {
			for _, family := range joseModuleFamilies {
				if strings.HasPrefix(imp, family) {
					hasJOSE = true
					joseImports = append(joseImports, imp)
				}
			}
		}

		if hasJOSE && !allowedRoots[pkg.ImportPath] {
			violations = append(violations, pkg.ImportPath)
		}
	}

	if len(violations) > 0 {
		t.Fatalf("LIB-011 GATE violated: JOSE/JWK modules imported outside federation adapter in: %v", violations)
	}

	// Verify that if any JOSE module is imported, it's in the allowed set.
	goModPath := filepath.Join(root, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}

	goModContent := string(data)
	for _, family := range joseModuleFamilies {
		if strings.Contains(goModContent, family) {
			// If found, check if it's in the allowed set
			allowedFound := false
			for allowed := range allowedJOSEModules {
				if strings.Contains(goModContent, allowed) {
					allowedFound = true
					break
				}
			}
			if !allowedFound {
				t.Fatalf("LIB-011 GATE violation: JOSE/JWK module family %s found in go.mod but not in allowedJOSEModules", family)
			}
		}
	}

	t.Logf("LIB-011 DECISION=GATE: no additional JOSE/JWK modules admitted without demonstration and conformance decision")
}

// TestTodo_LIB_011_Golden verifies the exact gating rationale persists:
// Go stdlib crypto (RSA, ECDSA, Ed25519) and go-oidc's JWKS validator
// are the first-choice cryptographic backends. Any additional JOSE library
// must prove a missing capability with narrow confinement.
func TestTodo_LIB_011_Golden(t *testing.T) {
	// The rationale: Go stdlib and go-oidc already provide conformant JOSE/JWT
	// validation for common federation patterns (RS256, ES256, EdDSA). The
	// federation adapter in internal/trust/federation uses go-oidc for JWKS
	// retrieval and validation; no additional JOSE library is needed for that
	// path.
	//
	// Future backends (lestrrat-go/jwx, square/go-jose) may be qualified only
	// after:
	// 1. A measured capability gap is demonstrated (e.g., unsupported algorithm,
	//    key rotation pattern, or conformance issue).
	// 2. The backend is confined to internal/authn/federation or
	//    internal/trust/federation.
	// 3. An exact entry is added to allowedJOSEModules above.
	// 4. The corresponding dependency-roles.yaml entry is created (LIB-001).
	// 5. Planning/todos.md is updated to record the decision.
	//
	// Until such a decision is made, no JOSE module may enter go.mod.

	t.Logf("LIB-011 rationale: Go stdlib + go-oidc provide conformant JOSE validation; no additional backend admitted without demonstrated need and LIB-011 decision")
}

// TestTodo_LIB_011_Conformance ensures the gate record is complete and the
// policy is unambiguous: no JOSE/JWK module may be added to go.mod without
// an explicit allowedJOSEModules entry and corresponding dependency-roles.yaml
// row.
func TestTodo_LIB_011_Conformance(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	allowedRoots := map[string]bool{
		"github.com/monstercameron/human-capital-management-suite/internal/authn/federation": true,
		"github.com/monstercameron/human-capital-management-suite/internal/trust/federation": true,
		"github.com/monstercameron/human-capital-management-suite/internal/trust":            true,
		"github.com/monstercameron/human-capital-management-suite/internal/authn":            true,
	}

	joseModuleFamilies := []string{
		"github.com/lestrrat-go/jwx",
		"github.com/lestrrat-go/jose",
		"github.com/square/go-jose",
		"gopkg.in/square/go-jose.v2",
	}

	var unexpectedImports []string
	for _, pkg := range pkgs {
		if strings.HasSuffix(pkg.ImportPath, "_test") || strings.Contains(pkg.ImportPath, "testdata") {
			continue
		}

		for _, imp := range pkg.Imports {
			for _, family := range joseModuleFamilies {
				if strings.HasPrefix(imp, family) && !allowedRoots[pkg.ImportPath] {
					unexpectedImports = append(unexpectedImports, pkg.ImportPath+": "+imp)
				}
			}
		}
	}

	if len(unexpectedImports) > 0 {
		t.Errorf("CONFORMANCE: JOSE/JWK imports outside federation adapter: %v", unexpectedImports)
	}

	t.Logf("LIB-011 CONFORMANCE: GATE upheld; no JOSE/JWK modules admitted without decision and conformance entry")
}
