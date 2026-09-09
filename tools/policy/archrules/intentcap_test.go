package archrules_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

// TestIntentCapabilityPackagesRejectDomainRulesAndAdapters is the
// ARCH-GO-005 primary test: capability must never import intent's own
// application/orchestration subpackage (behavioral ownership stays in
// intent; capability only invokes), and capability must not itself contain
// a concrete domain/store/provider/adapter implementation subpackage.
func TestIntentCapabilityPackagesRejectDomainRulesAndAdapters(t *testing.T) {
	cfg := loadArchConfig(t)
	ic := cfg.IntentCapability

	t.Run("capability must not import intent/app", func(t *testing.T) {
		cases := []struct {
			name     string
			importer string
			imported string
			wantV    bool
		}{
			{"capability importing intent/app", ic.CapabilityRoot, "internal/intent/app", true},
			{"capability importing intent's own root port package is allowed", ic.CapabilityRoot, ic.IntentRoot, false},
			{"capability importing intent/definitions is allowed", ic.CapabilityRoot, "internal/intent/definitions", false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var forbidden bool
				for _, f := range ic.ForbiddenCapabilityImports {
					if archrules.UnderRoot(tc.importer, ic.CapabilityRoot) && archrules.UnderRoot(tc.imported, f) {
						forbidden = true
					}
				}
				if forbidden != tc.wantV {
					t.Errorf("forbidden(%q -> %q) = %v, want %v", tc.importer, tc.imported, forbidden, tc.wantV)
				}
			})
		}
	})

	t.Run("capability must not contain a concrete adapter subpackage", func(t *testing.T) {
		cases := []struct {
			path  string
			wantV bool
		}{
			{"internal/capability/discovery/store", true},
			{"internal/capability/handler/postgres", true},
			{"internal/capability/gateway/provider", true},
			{"internal/capability/discovery", false},
			{"internal/capability/handler", false},
		}
		for _, tc := range cases {
			t.Run(tc.path, func(t *testing.T) {
				got := archrules.MatchesAnyGlob(ic.CapabilityForbiddenContent, tc.path)
				if got != tc.wantV {
					t.Errorf("MatchesAnyGlob(%q) = %v, want %v", tc.path, got, tc.wantV)
				}
			})
		}
	})
}

// TestTodo_ARCH_GO_005_Integration runs both halves of the check against
// the real tree.
func TestTodo_ARCH_GO_005_Integration(t *testing.T) {
	cfg := loadArchConfig(t)
	ic := cfg.IntentCapability
	root := repopath.RootDir()

	pkgs, err := repopath.ListPackages(root)
	if err != nil {
		t.Fatalf("listing packages: %v", err)
	}

	var edgeViolations, contentViolations int
	for _, pkg := range pkgs {
		rel, ok := archrules.TrimModule(cfg.Module, pkg.ImportPath)
		if !ok {
			continue
		}
		if archrules.UnderRoot(rel, ic.CapabilityRoot) {
			if archrules.MatchesAnyGlob(ic.CapabilityForbiddenContent, rel) {
				contentViolations++
				t.Errorf("ARCH-GO-005 violation: capability package %s matches a forbidden concrete-adapter content marker", rel)
			}
			for _, imp := range pkg.Imports {
				impRel, ok := archrules.TrimModule(cfg.Module, imp)
				if !ok {
					continue
				}
				for _, f := range ic.ForbiddenCapabilityImports {
					if archrules.UnderRoot(impRel, f) {
						edgeViolations++
						t.Errorf("ARCH-GO-005 violation: capability package %s imports %s", rel, impRel)
					}
				}
			}
		}
	}
	t.Logf("ARCH-GO-005: %d forbidden-import violations, %d forbidden-content violations", edgeViolations, contentViolations)
}

// TestTodo_ARCH_GO_005_Conformance checks a canonical vector per forbidden
// marker glob and forbidden import, not just the hand-picked primary-test
// examples.
func TestTodo_ARCH_GO_005_Conformance(t *testing.T) {
	cfg := loadArchConfig(t)
	ic := cfg.IntentCapability

	// Concrete substitution: replace the single "*" segment with "x" and
	// confirm the resulting concrete path still matches its own glob.
	for _, marker := range ic.CapabilityForbiddenContent {
		concrete := substituteStar(marker, "x")
		if !archrules.MatchesAnyGlob(ic.CapabilityForbiddenContent, concrete) {
			t.Errorf("glob %q's own concrete instantiation %q was not matched", marker, concrete)
		}
	}
	for _, forbidden := range ic.ForbiddenCapabilityImports {
		if !archrules.UnderRoot(forbidden, forbidden) {
			t.Errorf("UnderRoot(%q, %q) = false, want true (a root is always under itself)", forbidden, forbidden)
		}
	}
}

func substituteStar(glob, seg string) string {
	parts := strings.Split(glob, "/")
	for i, part := range parts {
		if part == "*" {
			parts[i] = seg
		}
	}
	return strings.Join(parts, "/")
}
