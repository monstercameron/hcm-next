package crosscut_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/crosscut"
)

func module() string { return "github.com/monstercameron/hcm-next" }

func TestCrossCuttingPackagesRejectOmniscientImports(t *testing.T) {
	edges := [][2]string{
		{module() + "/internal/trust/authz", module() + "/internal/domains/people/store"},
		{module() + "/internal/operations/reconciler", module() + "/internal/data/postgres"},
		{module() + "/internal/platform/telemetry", module() + "/internal/domains/compensation"},
	}
	for _, e := range edges {
		if v := crosscut.CheckEdge(module(), e[0], e[1]); v == nil {
			t.Fatalf("expected violation %v", e)
		}
	}
	ok := [][2]string{
		{module() + "/internal/trust/authz", module() + "/internal/capability/registry"},
		{module() + "/internal/operations/inspector", module() + "/internal/ledger"},
		{module() + "/internal/domains/people", module() + "/internal/domains/people/store"},
	}
	for _, e := range ok {
		if v := crosscut.CheckEdge(module(), e[0], e[1]); v != nil {
			t.Fatalf("unexpected violation %+v", v)
		}
	}
	if v := crosscut.CheckEdge(module(), module()+"/internal/trust/authz", module()+"/internal/intelligence/ranker"); v == nil {
		t.Fatal("intelligence import must be rejected in Phase 1")
	}
	if v := crosscut.CheckEdge(module(), module()+"/internal/intelligence/explain", module()+"/internal/domains/people"); v == nil {
		t.Fatal("intelligence must not import a concrete domain")
	}
}

func TestCrossCuttingOverlayRootsRejectConcreteDomains(t *testing.T) {
	// These overlays are deliberately listed independently: omitting one from
	// the checker would otherwise leave a policy hole until its first package
	// happened to be implemented.
	for _, root := range []string{
		"internal/trust/authn",
		"internal/trust/authz",
		"internal/privacy",
		"internal/governance/privacy",
		"internal/dlp",
		"internal/secrets",
		"internal/agent",
		"internal/intelligence",
		"internal/operations",
		"internal/admission",
		"internal/rollout",
		"internal/recovery",
	} {
		if v := crosscut.CheckEdge(module(), module()+"/"+root, module()+"/internal/domains/people/store"); v == nil {
			t.Errorf("%s must not import concrete domain stores", root)
		}
	}
}

func TestCrossCuttingOverlaysRejectDeferredPackages(t *testing.T) {
	for _, root := range []string{
		"internal/trust/authz",
		"internal/privacy",
		"internal/dlp",
		"internal/secrets",
		"internal/operations",
		"internal/admission",
		"internal/rollout",
		"internal/recovery",
	} {
		for _, deferred := range []string{"internal/agent/runtime", "internal/intelligence/ranker"} {
			if v := crosscut.CheckEdge(module(), module()+"/"+root, module()+"/"+deferred); v == nil {
				t.Errorf("%s must not require deferred package %s", root, deferred)
			}
		}
	}
}

func TestTodo_ARCH_GO_026_Property(t *testing.T) {
	for i := 0; i < 100; i++ {
		e := [2]string{module() + "/internal/trust/authz", module() + "/internal/domains/people/store"}
		a := crosscut.CheckEdge(module(), e[0], e[1])
		b := crosscut.CheckEdge(module(), e[0], e[1])
		if (a == nil) != (b == nil) {
			t.Fatal("non deterministic")
		}
	}
}

func TestTodo_ARCH_GO_026_Golden(t *testing.T) {
	violations := crosscut.CheckGraph(module(), [][2]string{
		{module() + "/internal/trust/authz", module() + "/internal/domains/people/store"},
		{module() + "/internal/operations/reconciler", module() + "/internal/data/postgres"},
	})
	got, _ := json.MarshalIndent(violations, "", "  ")
	got = append(got, '\n')
	want, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "policy", "crosscut", "testdata", "crosscut.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		h := sha256.Sum256(got)
		t.Fatalf("golden mismatch %x got %s want %s", h, got, want)
	}
}

func TestTodo_ARCH_GO_026_Fault(t *testing.T) {
	if v := crosscut.CheckEdge(module(), module()+"/internal/trust/authz", "github.com/external/pkg"); v != nil {
		t.Fatal("external import should not be flagged")
	}
	if v := crosscut.CheckEdge(module(), "other/module/internal/trust", module()+"/internal/domains/people"); v != nil {
		t.Fatal("outside module should be ignored")
	}
}

func TestTodo_ARCH_GO_026_Security(t *testing.T) {
	if v := crosscut.CheckEdge(module(), module()+"/internal/platform/telemetry", module()+"/internal/domains/people/store"); v == nil {
		t.Fatal("telemetry must not import domain store")
	}
	if v := crosscut.CheckEdge(module(), module()+"/internal/operations/reconciler", module()+"/internal/agent/executor"); v == nil {
		t.Fatal("operations must not require agent")
	}
}

func TestTodo_ARCH_GO_026_Conformance(t *testing.T) {
	ports := crosscut.AllowedPorts()
	if len(ports) == 0 {
		t.Fatal("no allowed ports")
	}
	for _, p := range ports {
		if p == "internal/domains/people/store" {
			t.Fatal("store must not be allowed port")
		}
	}
}

func TestTodo_ARCH_GO_026_Mutation(t *testing.T) {
	if v := crosscut.CheckEdge(module(), module()+"/internal/trust/authz", module()+"/internal/domains/people/store"); v == nil {
		t.Fatal("mutation should still be caught")
	}
}

func TestScanDirChecksProductionImportsAndSkipsTests(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("internal/trust/authz/policy.go", `package authz
import _ "github.com/monstercameron/hcm-next/internal/domains/people/store"
`)
	write("internal/operations/reconciler.go", `package operations
import _ "github.com/monstercameron/hcm-next/internal/agent/runtime"
`)
	// Test-only adapter usage must not create a production boundary finding.
	write("internal/trust/authz/policy_test.go", `package authz
import _ "github.com/monstercameron/hcm-next/internal/data/postgres"
`)
	got, err := crosscut.ScanDir(root, module())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ScanDir returned %d violations, want 2: %+v", len(got), got)
	}
	if got[0].File > got[1].File {
		t.Fatalf("violations are not deterministic/sorted: %+v", got)
	}
	for _, v := range got {
		if strings.HasSuffix(v.File, "_test.go") {
			t.Errorf("test file leaked into scan: %+v", v)
		}
		if v.File == "" {
			t.Errorf("source file missing from violation: %+v", v)
		}
	}
}

func TestScanDirRejectsMalformedProductionSource(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "trust"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal", "trust", "bad.go"), []byte("package trust\nimport ("), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := crosscut.ScanDir(root, module()); err == nil {
		t.Fatal("expected malformed source error")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}
