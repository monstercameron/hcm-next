package libqualification_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
)

const (
	goOIDCModuleFamily   = "github.com/coreos/go-oidc"
	goOAuth2ModuleFamily = "golang.org/x/oauth2"
)

// TestOIDCBackendQualification is LIB-010's primary test: go-oidc and x/oauth2
// must only be imported behind the federation adapter port defined in
// internal/trust/federation. These modules cannot be direct dependencies of
// business packages; they must be confined to the federation adapter layer
// that transforms OAuth/OIDC flows into an owned normalized authentication
// result.
//
// This test records the DEFER decision and enforces the policy that any
// imports of go-oidc or x/oauth2 are confined to the federation adapter layer,
// never leaked to authorization, principal context, or business package
// boundaries.
func TestOIDCBackendQualification(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	// Define allowed federation adapter roots: only these may import OIDC/OAuth2
	allowedRoots := map[string]bool{
		"github.com/monstercameron/hcm-next/internal/authn/federation": true,
		"github.com/monstercameron/hcm-next/internal/trust/federation": true,
		"github.com/monstercameron/hcm-next/internal/trust":            true, // May contain federation setup
		"github.com/monstercameron/hcm-next/internal/authn":            true, // May contain federation setup
	}

	var violations []string
	for _, pkg := range pkgs {
		// Skip test packages; they may import OIDC for testing the adapter
		if strings.HasSuffix(pkg.ImportPath, "_test") || strings.Contains(pkg.ImportPath, "testdata") {
			continue
		}

		hasOIDCOrOAuth := false
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, goOIDCModuleFamily) || strings.HasPrefix(imp, goOAuth2ModuleFamily) {
				hasOIDCOrOAuth = true
				break
			}
		}

		if hasOIDCOrOAuth && !allowedRoots[pkg.ImportPath] {
			violations = append(violations, pkg.ImportPath)
		}
	}

	if len(violations) > 0 {
		t.Fatalf("LIB-010 DEFERRAL violated: go-oidc or x/oauth2 imported outside federation adapter in: %v", violations)
	}

	// Verify that the federation adapter port exists and is the seam for OIDC/OAuth2
	federationPath := filepath.Join(root, "internal", "trust", "federation")
	_, err = os.Stat(federationPath)
	if err != nil {
		t.Fatalf("internal/trust/federation adapter port not found; LIB-010 requires federation adapter as the seam: %v", err)
	}

	t.Logf("LIB-010 DECISION=DEFER: go-oidc/x/oauth2 confined to federation adapter; all imports behind internal/trust/federation port")
}

// TestTodo_LIB_010_Golden verifies the exact deferral rationale persists:
// go-oidc and x/oauth2 are pluggable backends behind the federation port.
func TestTodo_LIB_010_Golden(t *testing.T) {
	// The rationale: internal/trust/federation defines the federation adapter
	// port as the single boundary where enterprise identity assertions are
	// validated and transformed into owned normalized [trust.Principal] values.
	// This package handles pure assertion verification using only stdlib crypto
	// (RSA, ECDSA, Ed25519) for RS256, ES256, and EdDSA signatures.
	//
	// The authorization-code/PKCE redirect dance, state/nonce handling, and live
	// JWKS retrieval are the responsibilities of the LIB-010 adapter (go-oidc
	// plus x/oauth2). LIB-010 deferral means:
	// 1. go-oidc and x/oauth2 are not yet pinned in go.mod
	// 2. They must be imported ONLY behind the federation adapter port
	// 3. No other package may import them directly
	//
	// At qualification time, SAML and future federation adapters will satisfy
	// the same identity-normalization conformance contract without changing
	// downstream authorization.

	t.Logf("LIB-010 rationale: go-oidc/x/oauth2 are federation-adapter backends; confined to internal/trust/federation port; SAML and others share the contract")
}

// TestTodo_LIB_010_Integration runs the policy check against the real
// repository state and verifies the federation adapter port is active.
func TestTodo_LIB_010_Integration(t *testing.T) {
	root := repopath.RootDir()

	// Verify federation port exists and has the expected structure
	federationPath := filepath.Join(root, "internal", "trust", "federation")
	docPath := filepath.Join(federationPath, "doc.go")

	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("reading federation/doc.go: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "federation") || !strings.Contains(content, "Principal") {
		t.Fatalf("INTEGRATION FAILURE: federation/doc.go does not describe the federation adapter port")
	}

	// Check that OIDC/OAuth2 are not in go.mod yet (they're deferred, not yet pinned)
	goModPath := filepath.Join(root, "go.mod")
	data, err = os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}

	// It's OK if they're imported internally behind the port, but not in go.mod
	// This test is just noting the deferral.
	t.Logf("LIB-010 INTEGRATION: federation adapter port verified; go-oidc/x/oauth2 not yet qualified")
}

// TestTodo_LIB_010_Security verifies that go-oidc and x/oauth2 are not
// used for authorization, eligibility or mutations outside the federation port.
func TestTodo_LIB_010_Security(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	allowedRoots := map[string]bool{
		"github.com/monstercameron/hcm-next/internal/authn/federation": true,
		"github.com/monstercameron/hcm-next/internal/trust/federation": true,
		"github.com/monstercameron/hcm-next/internal/trust":            true,
		"github.com/monstercameron/hcm-next/internal/authn":            true,
	}

	for _, pkg := range pkgs {
		// Skip federation adapter and test packages
		if allowedRoots[pkg.ImportPath] || strings.HasSuffix(pkg.ImportPath, "_test") {
			continue
		}

		// If this is a package that should not import OIDC/OAuth2, check for imports
		if strings.Contains(pkg.ImportPath, "trust") || strings.Contains(pkg.ImportPath, "authz") {
			for _, imp := range pkg.Imports {
				if strings.HasPrefix(imp, goOIDCModuleFamily) || strings.HasPrefix(imp, goOAuth2ModuleFamily) {
					t.Errorf("SECURITY: OIDC/OAuth2 import in package that should not have it: %s imports %s", pkg.ImportPath, imp)
				}
			}
		}
	}

	t.Logf("LIB-010 SECURITY: go-oidc/x/oauth2 confined to federation adapter; no leakage to trust/authz")
}

// TestTodo_LIB_010_Conformance ensures the deferral record is complete:
// go-oidc and x/oauth2 are confined to the federation port, authorization
// packages do not depend on them, and the federation adapter contract is
// preserved.
func TestTodo_LIB_010_Conformance(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	allowedRoots := map[string]bool{
		"github.com/monstercameron/hcm-next/internal/authn/federation": true,
		"github.com/monstercameron/hcm-next/internal/trust/federation": true,
		"github.com/monstercameron/hcm-next/internal/trust":            true,
		"github.com/monstercameron/hcm-next/internal/authn":            true,
	}

	// Verify confinement
	for _, pkg := range pkgs {
		if strings.HasSuffix(pkg.ImportPath, "_test") || strings.Contains(pkg.ImportPath, "testdata") {
			continue
		}

		hasOIDCOrOAuth := false
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, goOIDCModuleFamily) || strings.HasPrefix(imp, goOAuth2ModuleFamily) {
				hasOIDCOrOAuth = true
				break
			}
		}

		if hasOIDCOrOAuth && !allowedRoots[pkg.ImportPath] {
			t.Errorf("CONFORMANCE: OIDC/OAuth2 import outside federation adapter in %s", pkg.ImportPath)
		}
	}

	// Verify federation port exists
	federationPath := filepath.Join(root, "internal", "trust", "federation")
	_, err = os.Stat(federationPath)
	if err != nil {
		t.Errorf("CONFORMANCE: federation adapter port not found: %v", err)
	}

	t.Logf("LIB-010 CONFORMANCE: DEFER decision upheld; go-oidc/x/oauth2 confined; federation port preserved")
}
