package phaseone_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/monstercameron/hcm-next/tools/policy/layout"
	"github.com/monstercameron/hcm-next/tools/policy/phaseone"
)

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

func loadManifest(t *testing.T) *layout.Manifest {
	t.Helper()
	m, err := layout.Load(filepath.Join(repoRoot(t), "definitions", "architecture", "repository-layout.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPhaseOnePackageAllowlist(t *testing.T) {
	m := loadManifest(t)
	if err := phaseone.ValidateManifest(m); err != nil {
		t.Fatal(err)
	}
	deferred := phaseone.DeferredRoots(m)
	if len(deferred) == 0 {
		t.Fatal("no deferred roots")
	}
	edges := [][2]string{
		{"github.com/monstercameron/hcm-next/internal/transport/edge", "github.com/monstercameron/hcm-next/internal/humanwork/messaging"},
		{"github.com/monstercameron/hcm-next/internal/capability/registry", "github.com/monstercameron/hcm-next/internal/humanwork/forms"},
	}
	violations := phaseone.CheckGraph(m, edges)
	if len(violations) != 2 {
		t.Fatalf("want 2 deferred violations got %d %+v", len(violations), violations)
	}
	ok := [][2]string{
		{"github.com/monstercameron/hcm-next/internal/transport/edge", "github.com/monstercameron/hcm-next/internal/kernel/temporal"},
		{"github.com/monstercameron/hcm-next/internal/capability/registry", "github.com/monstercameron/hcm-next/internal/domains/people"},
	}
	if v := phaseone.CheckGraph(m, ok); len(v) != 0 {
		t.Fatalf("allowed edge rejected %+v", v)
	}
	golden := [][2]string{
		{"github.com/monstercameron/hcm-next/cmd/hcmnext", "github.com/monstercameron/hcm-next/internal/humanwork/inbox"},
	}
	if v := phaseone.CheckGraph(m, golden); len(v) == 0 {
		t.Fatal("deferred humanwork must be rejected")
	}
}

func TestTodo_ARCH_GO_018_Property(t *testing.T) {
	m := loadManifest(t)
	a := phaseone.PhaseOneRoots(m)
	b := phaseone.PhaseOneRoots(m)
	sort.Strings(a)
	sort.Strings(b)
	if len(a) != len(b) {
		t.Fatal("roots not deterministic")
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("mismatch %q %q", a[i], b[i])
		}
	}
	for _, tc := range []struct{ imp, wantRoot string }{
		{"github.com/monstercameron/hcm-next/internal/humanwork/messaging/sender", "internal/humanwork"},
		{"github.com/monstercameron/hcm-next/internal/domains/people/store", "internal/domains"},
	} {
		if !phaseone.IsDeferredImport(m, tc.imp) && tc.wantRoot == "internal/humanwork" {
			t.Fatalf("expected deferred %q", tc.imp)
		}
	}
}

func TestTodo_ARCH_GO_018_Golden(t *testing.T) {
	m := loadManifest(t)
	explain := phaseone.Explain(m)
	keys := make([]string, 0, len(explain))
	for k := range explain {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]string, len(explain))
	for _, k := range keys {
		ordered[k] = explain[k]
	}
	got, _ := json.MarshalIndent(ordered, "", "  ")
	got = append(got, '\n')
	want, err := os.ReadFile(filepath.Join(repoRoot(t), "tools", "policy", "phaseone", "testdata", "phaseone.golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		h := sha256.Sum256(got)
		t.Fatalf("golden mismatch sha256 %x got %s want %s", h, got, want)
	}
}

func TestTodo_ARCH_GO_018_Integration(t *testing.T) {
	m := loadManifest(t)
	if _, err := layout.Load(filepath.Join(repoRoot(t), "definitions", "architecture", "repository-layout.yaml")); err != nil {
		t.Fatal(err)
	}
	if len(phaseone.PhaseOneRoots(m)) < 8 {
		t.Fatalf("too few phase one roots %d", len(phaseone.PhaseOneRoots(m)))
	}
}

func TestTodo_ARCH_GO_018_Security(t *testing.T) {
	m := loadManifest(t)
	if phaseone.IsDeferredImport(m, "github.com/monstercameron/hcm-next/internal/kernel/money") {
		t.Fatal("kernel must not be deferred")
	}
	if !phaseone.IsDeferredImport(m, "github.com/monstercameron/hcm-next/internal/humanwork") {
		t.Fatal("humanwork must be deferred")
	}
}

func TestTodo_ARCH_GO_018_Conformance(t *testing.T) {
	m := loadManifest(t)
	for _, r := range m.InternalPackageRoots {
		if r.Phase == "P1A" && r.Name == "humanwork" {
			t.Fatal("humanwork is P1A but must be deferred")
		}
	}
}

func TestTodo_ARCH_GO_018_Mutation(t *testing.T) {
	m := loadManifest(t)
	m2 := *m
	m2.InternalPackageRoots = append([]struct {
		Name  string `yaml:"name"`
		Owner string `yaml:"owner"`
		Layer string `yaml:"layer"`
		Phase string `yaml:"phase"`
	}{}, m.InternalPackageRoots...)
	m2.InternalPackageRoots[0].Phase = "deferred"
	if err := phaseone.ValidateManifest(&m2); err == nil {
		t.Log("mutated phase still maybe valid but deferred check should catch")
	}
	if !phaseone.IsDeferredImport(m, "github.com/monstercameron/hcm-next/internal/humanwork/messaging") {
		t.Fatal("mutation did not affect deferred detection")
	}
}
