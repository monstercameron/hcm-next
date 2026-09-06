package phaseonegate_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/internal/repopath"
	"github.com/monstercameron/hcm-next/tools/policy/layout"
	"github.com/monstercameron/hcm-next/tools/policy/phaseonegate"
)

func liveInputs(t *testing.T) (string, *layout.Manifest, *phaseonegate.Graph, phaseonegate.Manifest) {
	t.Helper()
	root := repopath.RootDir()
	layoutManifest, err := layout.Load(filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
	if err != nil {
		t.Fatalf("load repository layout: %v", err)
	}
	forbidden, err := phaseonegate.LoadForbiddenImportPrefixes(filepath.Join(root, "definitions", "planning", "gates", "p1a-manifest.yaml"))
	if err != nil {
		t.Fatalf("load P1A forbidden prefixes: %v", err)
	}
	graph, err := phaseonegate.BuildProductionGraph(root, phaseonegate.EntryPoint)
	if err != nil {
		t.Fatalf("build production graph: %v", err)
	}
	physical, err := phaseonegate.DiscoverPhysicalPackages(root, graph.Module)
	if err != nil {
		t.Fatalf("discover physical packages: %v", err)
	}
	return root, layoutManifest, graph, phaseonegate.Generate(layoutManifest, graph, physical, forbidden)
}

// TestPhaseOnePackageAllowlist is the ARCH-GO-018 primary test. It checks the
// real production closure of cmd/hcmnext, rejects every outside edge and
// rejects every allowlist row that is not actually reached.
func TestPhaseOnePackageAllowlist(t *testing.T) {
	_, _, graph, manifest := liveInputs(t)
	if graph.Entry != phaseonegate.EntryPoint {
		t.Fatalf("graph entry = %q, want %q", graph.Entry, phaseonegate.EntryPoint)
	}
	if len(graph.Packages) == 0 || len(graph.Edges) == 0 {
		t.Fatalf("production graph is unexpectedly empty: packages=%d edges=%d", len(graph.Packages), len(graph.Edges))
	}
	if violations := phaseonegate.Check(manifest, graph); len(violations) != 0 {
		for _, violation := range violations {
			t.Errorf("Phase 1 package violation: %+v", violation)
		}
	}
	if len(manifest.Allowlist) != len(graph.Packages) {
		t.Fatalf("allowlist has %d rows for %d graph packages", len(manifest.Allowlist), len(graph.Packages))
	}
	for _, required := range []string{
		"/internal/kernel/", "/internal/intent", "/internal/capability",
		"/internal/engines/", "/internal/workflow", "/internal/ledger",
		"/internal/data/", "/internal/domains/people", "/internal/connectivity",
		"/internal/operations/", "/internal/platform/", "/internal/transport",
	} {
		if !hasPath(manifest.Allowlist, required) {
			t.Errorf("real Phase 1 closure lacks requested package family %q", required)
		}
	}
}

func TestTodo_ARCH_GO_018_Property(t *testing.T) {
	graph := &phaseonegate.Graph{
		Module: "github.com/monstercameron/hcm-next",
		Entry:  phaseonegate.EntryPoint,
		Packages: []phaseonegate.Package{
			{Path: "github.com/monstercameron/hcm-next/cmd/hcmnext"},
			{Path: "github.com/monstercameron/hcm-next/internal/kernel"},
		},
		Edges: []phaseonegate.Edge{{
			Importer: "github.com/monstercameron/hcm-next/cmd/hcmnext",
			Imported: "github.com/monstercameron/hcm-next/internal/kernel",
		}},
	}
	manifest := phaseonegate.Manifest{
		Version:    phaseonegate.ManifestVersion,
		Module:     graph.Module,
		EntryPoint: graph.Entry,
		Allowlist: []phaseonegate.Inclusion{
			{Path: graph.Packages[0].Path, Root: "cmd", Layer: "composition", Reason: "root"},
			{Path: graph.Packages[1].Path, Root: "internal/kernel", Layer: "kernel", Reason: "kernel"},
		},
	}
	if violations := phaseonegate.Check(manifest, graph); len(violations) != 0 {
		t.Fatalf("repeated checks should be stable and clean: %+v", violations)
	}
	first, err := phaseonegate.CanonicalJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	second, err := phaseonegate.CanonicalJSON(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("canonical manifest changed across identical evaluations")
	}
}

func TestTodo_ARCH_GO_018_Golden(t *testing.T) {
	root, layoutManifest, graph, manifest := liveInputs(t)
	got, err := phaseonegate.CanonicalJSON(manifest)
	if err != nil {
		t.Fatalf("canonical manifest: %v", err)
	}
	got = append(got, '\n')
	goldenPath := filepath.Join(root, "tools", "policy", "phaseonegate", "testdata", "phaseone-manifest.golden.json")
	if os.Getenv("HCMNEXT_UPDATE_PHASEONE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write generated golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read generated golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("generated Phase 1 manifest differs from golden; rerun with HCMNEXT_UPDATE_PHASEONE_GOLDEN=1 (allowlist=%d deferred=%d live_gaps=%d)", len(manifest.Allowlist), len(manifest.Deferred), len(manifest.LiveGaps))
	}
	if layoutManifest.Module != graph.Module {
		t.Fatalf("layout module %q differs from graph module %q", layoutManifest.Module, graph.Module)
	}
}

func TestTodo_ARCH_GO_018_Integration(t *testing.T) {
	root := repopath.RootDir()
	graph, err := phaseonegate.BuildProductionGraph(root, phaseonegate.EntryPoint)
	if err != nil {
		t.Fatalf("go list -deps production graph: %v", err)
	}
	seen := make(map[string]bool, len(graph.Packages))
	for _, pkg := range graph.Packages {
		if seen[pkg.Path] {
			t.Fatalf("go list returned duplicate package %q", pkg.Path)
		}
		seen[pkg.Path] = true
		if strings.HasSuffix(pkg.Path, "_test") || strings.Contains(pkg.Path, "/testdata/") {
			t.Fatalf("production graph contains a test-only package %q", pkg.Path)
		}
	}
	if !seen[graph.Module+"/cmd/hcmnext"] {
		t.Fatalf("production graph does not contain cmd/hcmnext")
	}
}

func TestTodo_ARCH_GO_018_Security(t *testing.T) {
	module := "github.com/monstercameron/hcm-next"
	manifest := phaseonegate.Manifest{
		Module: module,
		Allowlist: []phaseonegate.Inclusion{
			{Path: module + "/cmd/hcmnext"},
		},
		ForbiddenImportPrefixes: []string{module + "/internal/connectivity/writeadapters"},
	}
	graph := &phaseonegate.Graph{
		Module:   module,
		Entry:    phaseonegate.EntryPoint,
		Packages: []phaseonegate.Package{{Path: module + "/cmd/hcmnext"}},
		Edges: []phaseonegate.Edge{{
			Importer: module + "/cmd/hcmnext",
			Imported: module + "/internal/connectivity/writeadapters/provider",
		}},
	}
	violations := phaseonegate.Check(manifest, graph)
	if !hasViolation(violations, "forbidden-import-prefix", graph.Edges[0].Importer, graph.Edges[0].Imported) {
		t.Fatalf("forbidden provider-write edge was not rejected: %+v", violations)
	}
	if !hasViolation(violations, "outside-allowlist-edge", graph.Edges[0].Importer, graph.Edges[0].Imported) {
		t.Fatalf("unlisted provider-write edge was not rejected: %+v", violations)
	}
}

func TestTodo_ARCH_GO_018_Conformance(t *testing.T) {
	_, _, graph, manifest := liveInputs(t)
	if len(manifest.Deferred) == 0 {
		t.Fatal("physical packages outside the selected closure need explicit deferred rows")
	}
	for _, entry := range manifest.Allowlist {
		if entry.Path == "" || entry.Root == "" || entry.Layer == "" || entry.Reason == "" {
			t.Fatalf("inclusion is not explainable: %+v", entry)
		}
	}
	for _, entry := range manifest.Deferred {
		if entry.Path == "" || entry.Gate == "" || entry.OwnerTodo == "" || entry.Reason == "" {
			t.Fatalf("deferred package is not pinned to an owner gate: %+v", entry)
		}
		for _, pkg := range graph.Packages {
			if pkg.Path == entry.Path {
				t.Fatalf("deferred entry %q is actually reachable", entry.Path)
			}
		}
	}
	if !hasDeferredGate(manifest.Deferred, "DB-016") {
		t.Fatal("deferred-domain packages are not pinned to DB-016")
	}
}

func TestTodo_ARCH_GO_018_Mutation(t *testing.T) {
	module := "github.com/monstercameron/hcm-next"
	manifest := phaseonegate.Manifest{
		Module: module,
		Allowlist: []phaseonegate.Inclusion{
			{Path: module + "/cmd/hcmnext"},
			{Path: module + "/internal/not-reached"},
		},
	}
	graph := &phaseonegate.Graph{
		Module:   module,
		Entry:    phaseonegate.EntryPoint,
		Packages: []phaseonegate.Package{{Path: module + "/cmd/hcmnext"}},
		Edges: []phaseonegate.Edge{{
			Importer: module + "/cmd/hcmnext",
			Imported: module + "/internal/not-allowlisted",
		}},
	}
	violations := phaseonegate.Check(manifest, graph)
	if !hasViolation(violations, "stale-allowlist-entry", "", module+"/internal/not-reached") {
		t.Fatalf("stale allowlist mutation survived: %+v", violations)
	}
	if !hasViolation(violations, "outside-allowlist-edge", graph.Edges[0].Importer, graph.Edges[0].Imported) {
		t.Fatalf("outside edge mutation survived: %+v", violations)
	}
}

func hasPath(entries []phaseonegate.Inclusion, needle string) bool {
	for _, entry := range entries {
		if strings.Contains(entry.Path, needle) {
			return true
		}
	}
	return false
}

func hasViolation(violations []phaseonegate.Violation, kind, importer, imported string) bool {
	for _, violation := range violations {
		if violation.Kind == kind && violation.Importer == importer && violation.Imported == imported {
			return true
		}
	}
	return false
}

func hasDeferredGate(entries []phaseonegate.Deferred, gate string) bool {
	for _, entry := range entries {
		if entry.Gate == gate {
			return true
		}
	}
	return false
}

// Keep the test's imported JSON package intentional: this assertion catches
// accidental changes to the generated shape before the golden comparison.
func TestTodo_ARCH_GO_018_GoldenShape(t *testing.T) {
	_, _, _, manifest := liveInputs(t)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"allowlist", "deferred", "forbidden_import_prefixes", "live_gaps"} {
		if _, ok := decoded[field]; !ok {
			t.Fatalf("generated manifest omitted %q", field)
		}
	}
	keys := make([]string, 0, len(decoded))
	for key := range decoded {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) != 7 {
		t.Fatalf("unexpected generated manifest field count %d (%v)", len(keys), keys)
	}
}
