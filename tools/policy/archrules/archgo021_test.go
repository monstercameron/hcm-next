package archrules_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/archrules"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

const archGo021Module = "github.com/monstercameron/human-capital-management-suite"

func archGo021Boundary() archrules.IntegrationBoundary {
	return archrules.DefaultIntegrationBoundary(archGo021Module)
}

// TestIntegrationPackagesRejectDuplicateTransformAndProviderLeakage is the
// ARCH-GO-021 primary test. It pins the sub-boundary decisions that prevent
// connectivity from growing a second transformation runtime or reaching
// transformation implementation details, and keeps adapters as the only
// reverse dependency.
func TestIntegrationPackagesRejectDuplicateTransformAndProviderLeakage(t *testing.T) {
	b := archGo021Boundary()
	cases := []struct {
		name     string
		importer string
		imported string
		rule     string
		wantV    bool
	}{
		{"connectivity may consume ir", "internal/connectivity/mapping", "internal/engines/transformation/ir", "", false},
		{"connectivity may consume lineage", "internal/connectivity/observe", "internal/engines/transformation/lineage", "", false},
		{"connectivity may consume adapters", "internal/connectivity/adapter", "internal/engines/transformation/adapters", "", false},
		{"connectivity root is not a surface", "internal/connectivity/mapping", "internal/engines/transformation", archrules.IntegrationRuleConnectivityTransformationSurface, true},
		{"connectivity cannot import version", "internal/connectivity/mapping", "internal/engines/transformation/version", archrules.IntegrationRuleConnectivityTransformationVersion, true},
		{"only mapping execute may import exec", "internal/connectivity/mapping", "internal/engines/transformation/exec", archrules.IntegrationRuleMappingExecuteOnlyIRExecutor, true},
		{"mapping execute may import exec", "internal/connectivity/mapping/execute", "internal/engines/transformation/exec", "", false},
		{"transformation core cannot import connectivity", "internal/engines/transformation/exec", "internal/connectivity", archrules.IntegrationRuleTransformationConnectivitySurface, true},
		{"transformation adapters may import connectivity", "internal/engines/transformation/adapters", "internal/connectivity/mapping", "", false},
		{"schemasnapshot cannot import mapping", "internal/connectivity/schemasnapshot", "internal/connectivity/mapping", archrules.IntegrationRuleSchemasnapshotMapping, true},
		{"mft cannot import mapping", "internal/connectivity/mft", "internal/connectivity/mapping", archrules.IntegrationRuleMFTMapping, true},
		{"mft cannot import transformation", "internal/connectivity/mft", "internal/engines/transformation/ir", archrules.IntegrationRuleMFTTransformation, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := archrules.CheckIntegrationImport(b, "fixture.go", tc.importer, tc.imported)
			if (len(findings) != 0) != tc.wantV {
				t.Fatalf("CheckIntegrationImport(%q -> %q) returned %+v, violation=%v, want %v", tc.importer, tc.imported, findings, len(findings) != 0, tc.wantV)
			}
			if !tc.wantV {
				return
			}
			foundRule := false
			for _, finding := range findings {
				if finding.Rule == tc.rule {
					foundRule = true
				}
				if finding.File == "" || finding.Rule == "" {
					t.Errorf("finding must name file and rule: %+v", finding)
				}
			}
			if !foundRule {
				t.Errorf("findings did not include rule %q: %+v", tc.rule, findings)
			}
		})
	}
}

// TestTodo_ARCH_GO_021_Golden pins the complete allowed cross-boundary edge
// table and the exact live exceptions. A change to either is therefore a
// visible reviewed policy diff.
func TestTodo_ARCH_GO_021_Golden(t *testing.T) {
	wantEdges := []string{
		"internal/connectivity/** -> internal/engines/transformation/adapters/** [connectivity may consume adapter contracts]",
		"internal/connectivity/** -> internal/engines/transformation/ir/** [connectivity may consume IR contracts]",
		"internal/connectivity/** -> internal/engines/transformation/lineage/** [connectivity may consume lineage contracts]",
		"internal/connectivity/mapping/execute -> internal/engines/transformation/exec/** [only mapping/execute may execute IR]",
		"internal/engines/transformation/adapters/** -> internal/connectivity/** [only transformation adapters may depend on connectivity]",
	}
	gotEdges := make([]string, 0, len(archrules.IntegrationAllowedEdges))
	for _, edge := range archrules.IntegrationAllowedEdges {
		gotEdges = append(gotEdges, edge.From+" -> "+edge.To+" ["+edge.Detail+"]")
	}
	sort.Strings(gotEdges)
	if strings.Join(gotEdges, "\n") != strings.Join(wantEdges, "\n") {
		t.Fatalf("ARCH-GO-021 allowed edge table drifted\n got:\n%s\nwant:\n%s", strings.Join(gotEdges, "\n"), strings.Join(wantEdges, "\n"))
	}

	wantExceptions := []string{
		"internal/connectivity/mapping/execute->internal/engines/transformation|ARCH-GO-021|shared transformation scalar types remain a compatibility edge until the executor surface is narrowed",
		"internal/connectivity/mapping/execute->internal/engines/transformation/taint|ARCH-GO-021|the executor carries the shared taint envelope until that concern is exposed through the approved executor surface",
	}
	gotExceptions := make([]string, 0, len(archrules.IntegrationExceptions()))
	for _, exception := range archrules.IntegrationExceptions() {
		gotExceptions = append(gotExceptions, exception.Importer+"->"+exception.Imported+"|"+exception.OwnerTodo+"|"+exception.Reason)
	}
	sort.Strings(wantExceptions)
	sort.Strings(gotExceptions)
	if strings.Join(gotExceptions, "\n") != strings.Join(wantExceptions, "\n") {
		t.Fatalf("ARCH-GO-021 exception allowlist drifted\n got:\n%s\nwant:\n%s", strings.Join(gotExceptions, "\n"), strings.Join(wantExceptions, "\n"))
	}
}

// FuzzTodo_ARCH_GO_021 exercises the path-prefix matcher with arbitrary
// package names and verifies it never panics or fabricates an empty rule.
func FuzzTodo_ARCH_GO_021(f *testing.F) {
	for _, seed := range [][2]string{
		{"internal/connectivity/mapping", "internal/engines/transformation/ir"},
		{"internal/engines/transformation/exec", "internal/connectivity"},
		{"internal/connectivity/mft", "internal/engines/transformation"},
	} {
		f.Add(seed[0], seed[1])
	}
	b := archGo021Boundary()
	f.Fuzz(func(t *testing.T, importer, imported string) {
		for _, finding := range archrules.CheckIntegrationImport(b, "fuzz.go", importer, imported) {
			if finding.Rule == "" || finding.File == "" {
				t.Fatalf("invalid finding: %+v", finding)
			}
		}
	})
}

// TestTodo_ARCH_GO_021_Integration runs the AST/import checker against every
// production Go file in the real connectivity and transformation trees.
func TestTodo_ARCH_GO_021_Integration(t *testing.T) {
	root := repopath.RootDir()
	findings, err := archrules.ScanIntegrationTree(root, archGo021Boundary())
	if err != nil {
		t.Fatalf("scanning real integration/transformation tree: %v", err)
	}
	var raw, allowlisted int
	for _, finding := range findings {
		raw++
		if archrules.IsIntegrationException(finding.Importer, finding.Imported) {
			allowlisted++
			continue
		}
		t.Errorf("%s", finding.Error())
	}
	t.Logf("ARCH-GO-021: %d raw findings, %d exact owner-pinned exceptions", raw, allowlisted)
}

// TestTodo_ARCH_GO_021_Fault verifies parse failures are returned as faults,
// rather than being silently treated as a clean import scan.
func TestTodo_ARCH_GO_021_Fault(t *testing.T) {
	_, err := archrules.ScanIntegrationSource(archGo021Boundary(), "internal/connectivity/mapping", "broken.go", "package mapping\nimport (")
	if err == nil {
		t.Fatal("malformed source unexpectedly scanned without an error")
	}
	if !strings.Contains(err.Error(), "broken.go") {
		t.Fatalf("parse fault omitted filename: %v", err)
	}
}

// TestTodo_ARCH_GO_021_Conformance validates the checker's policy data and
// proves each exception is exact, owned by this todo, and outside the allowed
// surface for a reason visible in the Golden.
func TestTodo_ARCH_GO_021_Conformance(t *testing.T) {
	b := archGo021Boundary()
	if b.Module != archGo021Module {
		t.Fatalf("module path drifted: got %q, want %q", b.Module, archGo021Module)
	}
	seen := map[string]bool{}
	for _, edge := range archrules.IntegrationAllowedEdges {
		key := edge.From + "->" + edge.To
		if seen[key] {
			t.Fatalf("duplicate allowed edge %q", key)
		}
		seen[key] = true
		if edge.From == "" || edge.To == "" || edge.Detail == "" {
			t.Fatalf("incomplete allowed edge: %+v", edge)
		}
	}
	for _, exception := range archrules.IntegrationExceptions() {
		if exception.OwnerTodo != "ARCH-GO-021" || exception.Reason == "" {
			t.Fatalf("exception must name ARCH-GO-021 and a reason: %+v", exception)
		}
		if !archrules.IsIntegrationException(exception.Importer, exception.Imported) {
			t.Fatalf("exception is not recognized exactly: %+v", exception)
		}
		if strings.Contains(exception.Importer, "*") || strings.Contains(exception.Imported, "*") {
			t.Fatalf("exception must not use a wildcard: %+v", exception)
		}
	}
}
