package gateevidence

import (
	"path/filepath"
	"testing"
	"time"
)

func TestGateClosureCompilesRealManifestGraph(t *testing.T) {
	manifest, err := LoadP1AManifest(filepath.Join("..", "..", "..", "definitions", "planning", "gates", "p1a-manifest.yaml"))
	if err != nil {
		t.Skipf("checked-in P1A manifest unavailable in this package fixture: %v", err)
	}
	got, err := CompileGateClosure(*manifest, nil, CompileOptions{RepoRoot: filepath.Join("..", "..", ".."), Now: time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("CompileGateClosure: %v", err)
	}
	wantNodes := 1 + len(manifest.Evidence)*2 + uniqueTodoCount(manifest.Evidence)
	if len(got.Graph.Nodes) != wantNodes {
		t.Fatalf("got %d graph nodes, want %d", len(got.Graph.Nodes), wantNodes)
	}
	if got.Closure.Gate != "GATE_A" || got.Closure.Final == "" {
		t.Fatalf("unexpected closure: %#v", got.Closure)
	}
	if len(got.Closure.Order) != len(got.Graph.Nodes) || got.Closure.Order[len(got.Closure.Order)-1] != got.Closure.Final {
		t.Fatalf("closure did not expand every selected manifest node: order=%v graph=%v", got.Closure.Order, got.Graph.Nodes)
	}
}

func TestGateClosureRejectsMissingOrDuplicateEvidenceDefinitions(t *testing.T) {
	for name, mutate := range map[string]func(*P1AManifest){
		"missing todo":    func(m *P1AManifest) { m.Evidence[0].TodoID = "" },
		"missing test":    func(m *P1AManifest) { m.Evidence[0].Test = "" },
		"missing package": func(m *P1AManifest) { m.Evidence[0].Package = "" },
		"duplicate test":  func(m *P1AManifest) { m.Evidence = append(m.Evidence, m.Evidence[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			manifest := baseManifest()
			manifest.Evidence = []EvidenceEntry{{TodoID: "X-001", Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg"}}
			mutate(&manifest)
			if _, err := CompileGateClosure(manifest, nil, CompileOptions{Now: time.Now()}); err == nil {
				t.Fatal("malformed evidence definition unexpectedly compiled")
			}
		})
	}
}

func TestCompileProductionEntryPointCannotBypassGateClosure(t *testing.T) {
	manifest := baseManifest()
	manifest.Release = "P9"
	if _, err := Compile(manifest, nil, CompileOptions{Now: time.Now()}); err == nil {
		t.Fatal("production Compile entry point bypassed release/gate validation")
	}
}

func uniqueTodoCount(entries []EvidenceEntry) int {
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.TodoID] = true
	}
	return len(seen)
}

func TestGateClosureRejectsUnknownRelease(t *testing.T) {
	manifest := baseManifest()
	manifest.Release = "P9"
	if _, err := CompileGateClosure(manifest, nil, CompileOptions{Now: time.Now()}); err == nil {
		t.Fatal("unknown release unexpectedly compiled")
	}
}
