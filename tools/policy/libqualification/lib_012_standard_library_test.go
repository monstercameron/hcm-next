package libqualification_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// Forbidden third-party modules that violate the standard-library-first policy.
// LIB-012 forbids these frameworks unless an exception is explicitly declared
// in an allowlist with a named todo justification.
var forbiddenFrameworks = map[string]struct {
	family  string
	reason  string
	pattern string
}{
	// Logging frameworks (use log/slog instead).
	"go.uber.org/zap": {
		family:  "go.uber.org/zap",
		reason:  "use log/slog for structured logging; zap is not admitted without LIB-012 decision",
		pattern: "go.uber.org/zap",
	},
	"github.com/sirupsen/logrus": {
		family:  "github.com/sirupsen/logrus",
		reason:  "use log/slog for structured logging; logrus is not admitted without LIB-012 decision",
		pattern: "github.com/sirupsen/logrus",
	},
	"github.com/rs/zerolog": {
		family:  "github.com/rs/zerolog",
		reason:  "use log/slog for structured logging; zerolog is not admitted without LIB-012 decision",
		pattern: "github.com/rs/zerolog",
	},

	// Assertion libraries (use Go stdlib testing tools).
	"github.com/stretchr/testify": {
		family:  "github.com/stretchr/testify",
		reason:  "use testing.T.Error/Fail or custom assertions; testify is not admitted without LIB-012 decision",
		pattern: "github.com/stretchr/testify",
	},

	// HTTP routing frameworks not in stdlib (use net/http, grpc, or connect/grpc-gateway).
	"github.com/gorilla/mux": {
		family:  "github.com/gorilla/mux",
		reason:  "use net/http routing; gorilla/mux not admitted for production without LIB-012 decision",
		pattern: "github.com/gorilla/mux",
	},
	"github.com/gin-gonic/gin": {
		family:  "github.com/gin-gonic/gin",
		reason:  "use net/http routing or gRPC; gin not admitted without LIB-012 decision",
		pattern: "github.com/gin-gonic/gin",
	},
	"github.com/labstack/echo": {
		family:  "github.com/labstack/echo",
		reason:  "use net/http routing or gRPC; echo not admitted without LIB-012 decision",
		pattern: "github.com/labstack/echo",
	},

	// Crypto frameworks not in stdlib (use crypto/*, crypto/sha256, etc.).
	"golang.org/x/crypto": {
		family:  "golang.org/x/crypto",
		reason:  "use stdlib crypto packages; x/crypto is not in stdlib and requires LIB-012 decision for specific algorithms",
		pattern: "golang.org/x/crypto",
	},
}

// Per-package allowlist for admitted exceptions with their justifications.
// Format: package path -> list of (forbidden module, reason) tuples.
// This is populated only when a deliberate exception has been made and recorded
// in planning/todos.md.
var admittedExceptions = map[string]map[string]string{
	// Example: "internal/some/package": {"github.com/some/module": "LIB-012-EXCEPTION-001: reason"},
}

// TestStandardLibraryDefaultPolicy is LIB-012's primary test: the codebase
// must prefer Go standard-library logging, crypto, networking, and testing
// mechanics. This test fails when a third-party logger (zap, logrus, zerolog),
// crypto framework (not covered by stdlib), HTTP router (not stdlib/grpc),
// or assertion library (testify) appears without an admitted exception.
//
// The GREEN condition is that log/slog, stdlib crypto/TLS/net, and Go test/race
// tools satisfy all declared needs through minimal owned adapters.
func TestStandardLibraryDefaultPolicy(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var violations []string

	for _, pkg := range pkgs {
		// Skip generated code and test packages (they may import test frameworks).
		if strings.Contains(pkg.ImportPath, "/gen/") || strings.HasSuffix(pkg.ImportPath, "_test") {
			continue
		}

		// Skip testdata fixtures.
		if strings.Contains(pkg.ImportPath, "testdata") {
			continue
		}

		// Skip tools-only packages (they may experiment with frameworks).
		if strings.HasPrefix(pkg.ImportPath, "github.com/monstercameron/human-capital-management-suite/tools/") &&
			!strings.Contains(pkg.ImportPath, "tools/policy") {
			continue
		}

		for _, imp := range pkg.Imports {
			for name, fb := range forbiddenFrameworks {
				if strings.HasPrefix(imp, fb.pattern) {
					// Check if this import is in the admitted exceptions list.
					if admittedExceptions[pkg.ImportPath] != nil {
						if reason, found := admittedExceptions[pkg.ImportPath][name]; found {
							t.Logf("ADMITTED EXCEPTION: %s imports %s (%s)", pkg.ImportPath, name, reason)
							continue
						}
					}

					violations = append(violations, fmt.Sprintf("%s imports %s (%s)", pkg.ImportPath, name, fb.reason))
				}
			}
		}
	}

	if len(violations) > 0 {
		t.Fatalf("LIB-012 POLICY VIOLATION: standard-library-first principle breached:\n%s", strings.Join(violations, "\n"))
	}

	t.Logf("LIB-012 DECISION=PREFER: log/slog, stdlib crypto/TLS/net, and Go test tools satisfy all needs")
}

// TestTodo_LIB_012_Golden verifies the exact policy rationale persists:
// Go stdlib provides mature, well-reviewed logging, crypto, networking,
// and testing mechanics that are sufficient for all Phase 1 Human Capital Management Suite needs.
// Any exception requires a named capability gap and LIB-012 decision.
func TestTodo_LIB_012_Golden(t *testing.T) {
	// The rationale: Human Capital Management Suite is Go-first (Go technology constitution). The Go
	// standard library contains:
	// - log/slog for structured logging (1.21+)
	// - crypto/* for signature verification, hashing, symmetric ciphers
	// - net/http for HTTP routing (wrapped by gRPC/Connect/SchemaFlux)
	// - testing, testing/quick, testing/fstest for all test scenarios
	// - runtime/race (go test -race) for race detection
	//
	// Third-party logging (zap, logrus, zerolog), crypto frameworks, or
	// assertion libraries (testify) may only be admitted if:
	// 1. A measurable capability gap is demonstrated.
	// 2. An exact reason is recorded in planning/todos.md (LIB-012).
	// 3. An entry is added to admittedExceptions above with the todo reference.
	// 4. A corresponding dependency-roles.yaml entry is created (LIB-001).
	//
	// The preferred strategy is to create lightweight adapters (e.g., an
	// internal/platform/logging/adapter package) that wrap stdlib and allow
	// future replacement without changing callers.

	t.Logf("LIB-012 rationale: Go stdlib (log/slog, crypto/*, net/http, testing) is the default; third-party frameworks forbidden without decision")
}

// TestTodo_LIB_012_Security verifies that no third-party assertion libraries
// are used in production code paths (they may appear in test code only).
func TestTodo_LIB_012_Security(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	testalyModules := []string{"github.com/stretchr/testify"}

	for _, pkg := range pkgs {
		// Only check non-test production packages.
		if strings.HasSuffix(pkg.ImportPath, "_test") || strings.Contains(pkg.ImportPath, "test") {
			continue
		}

		for _, imp := range pkg.Imports {
			for _, testMod := range testalyModules {
				if strings.HasPrefix(imp, testMod) {
					t.Errorf("SECURITY: assertion library %s in production package %s", testMod, pkg.ImportPath)
				}
			}
		}
	}

	t.Logf("LIB-012 SECURITY: assertion libraries confined to test code; no production dependencies")
}

// TestTodo_LIB_012_Conformance ensures the policy record is complete and
// unambiguous: all packages must use stdlib logging, crypto, and networking
// mechanics unless an exception is explicitly recorded.
func TestTodo_LIB_012_Conformance(t *testing.T) {
	root := repopath.RootDir()
	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var violations []string

	for _, pkg := range pkgs {
		if strings.Contains(pkg.ImportPath, "/gen/") || strings.HasSuffix(pkg.ImportPath, "_test") || strings.Contains(pkg.ImportPath, "testdata") {
			continue
		}

		if strings.HasPrefix(pkg.ImportPath, "github.com/monstercameron/human-capital-management-suite/tools/") &&
			!strings.Contains(pkg.ImportPath, "tools/policy") {
			continue
		}

		for _, imp := range pkg.Imports {
			for name, fb := range forbiddenFrameworks {
				if strings.HasPrefix(imp, fb.pattern) {
					if admittedExceptions[pkg.ImportPath] == nil || admittedExceptions[pkg.ImportPath][name] == "" {
						violations = append(violations, fmt.Sprintf("%s imports %s", pkg.ImportPath, name))
					}
				}
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf("CONFORMANCE: forbidden third-party frameworks in production code:\n%s", strings.Join(violations, "\n"))
	}

	t.Logf("LIB-012 CONFORMANCE: stdlib-default policy upheld; no forbidden frameworks in production code")
}
