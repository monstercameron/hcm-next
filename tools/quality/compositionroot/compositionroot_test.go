package compositionroot

import (
	"strings"
	"testing"
)

var testRules = Rules{Module: "example.com/hcm", CompositionRoots: []string{"internal/application"}, CommandRoots: []string{"cmd"}, BusinessRoots: []string{"internal/domains", "internal/workflow"}, AdapterRoots: []string{"internal/store", "internal/connectivity"}}

func TestCompositionRootRejectsGlobalRegistrationAndHiddenDependencies(t *testing.T) {
	cases := []struct{ name, path, source, kind string }{
		{"init", "internal/domains/people", "package people\nfunc init() {}", "init-registration"},
		{"global registry", "internal/domains/people", "package people\nvar registry = map[string]string{}", "global-mutable-registry"},
		{"locator", "internal/domains/people", "package people\nfunc Load() { Resolve(1) }", "service-locator"},
		{"adapter construction", "internal/domains/people", "package people\nimport \"example.com/hcm/internal/store/postgres\"\nfunc Open() { postgres.NewStore() }", "concrete-adapter-construction"},
		{"command import", "cmd/hcmnext", "package main\nimport _ \"example.com/hcm/internal/domains/people\"", "command-business-import"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckCompositionSources([]SourcePackage{{ImportPath: tc.path, Source: tc.source}}, testRules)
			if len(got) != 1 || got[0].Kind != tc.kind {
				t.Fatalf("findings=%v, want one %s", got, tc.kind)
			}
		})
	}
}

func TestTodo_ARCH_GO_020_Conformance(t *testing.T) {
	clean := []SourcePackage{{ImportPath: "internal/application", Source: "package application\nvar registry = map[string]string{}"}, {ImportPath: "internal/domains/people", Source: "package people\nconst Version = 1"}}
	if got := CheckCompositionSources(clean, testRules); len(got) != 0 {
		t.Fatalf("clean composition root produced findings: %v", got)
	}
}

func TestTodo_ARCH_GO_020_Golden(t *testing.T) {
	got := CheckCompositionSources([]SourcePackage{{ImportPath: "internal/domains/people", Source: "package people\nfunc init() {}\nvar r = make(map[string]int)"}}, testRules)
	want := []string{"global-mutable-registry", "init-registration"}
	for _, v := range got {
		for i, k := range want {
			if v.Kind == k {
				want = append(want[:i], want[i+1:]...)
				break
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing deterministic findings %v (got %v)", want, got)
	}
}

func TestTodo_ARCH_GO_020_Integration(t *testing.T) {
	got := CheckCompositionSources([]SourcePackage{{ImportPath: "internal/application", Source: "package application\nimport _ \"example.com/hcm/internal/store/postgres\""}}, testRules)
	if len(got) != 0 {
		t.Fatalf("composition root should be allowed to import adapters: %v", got)
	}
}

func TestCompositionRootAllowsExplicitTestSwaps(t *testing.T) {
	got := CheckCompositionSources([]SourcePackage{{ImportPath: "internal/application", Source: "package application\nfunc Build() { NewStore() }"}}, testRules)
	if strings.Contains(strings.Join(kinds(got), ","), "service-locator") {
		t.Fatal("explicit constructor was mistaken for locator")
	}
}

func kinds(v []Violation) []string {
	out := make([]string, len(v))
	for i, x := range v {
		out[i] = x.Kind
	}
	return out
}
