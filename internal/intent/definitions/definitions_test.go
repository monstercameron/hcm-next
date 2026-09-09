package definitions

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

func TestAllCount(t *testing.T) {
	all := All()
	if len(all) != 14 {
		t.Fatalf("got %d want 14", len(all))
	}
	for _, d := range all {
		if err := d.Validate(); err != nil {
			t.Fatalf("%s validate: %v", d.Ref, err)
		}
	}
}

func TestCatalog(t *testing.T) {
	c := Catalog()
	if len(c.Schemas) == 0 {
		t.Fatal("no schemas")
	}
	if len(c.Capabilities) == 0 {
		t.Fatal("no capabilities")
	}
	seen := map[string]bool{}
	for _, s := range c.Schemas {
		if seen[s.String()] {
			t.Fatalf("duplicate schema %s", s.String())
		}
		seen[s.String()] = true
	}
}

func TestPolicies(t *testing.T) {
	policies := Policies()
	if len(policies) != 3 {
		t.Fatalf("got %d want 3", len(policies))
	}
	ids := map[string]bool{}
	for _, p := range policies {
		ids[p.ID] = true
		if len(p.Rules) != 7 {
			t.Fatalf("policy %s rules %d", p.ID, len(p.Rules))
		}
	}
	for _, want := range []string{"hcmnext.negative_state.analytical_read", "hcmnext.negative_state.pure_calculation", "hcmnext.negative_state.change_transaction"} {
		if !ids[want] {
			t.Fatalf("missing policy %s", want)
		}
	}
}

func TestNewRegistry(t *testing.T) {
	reg, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if reg == nil {
		t.Fatal("nil registry")
	}
	def, err := reg.Resolve(intent.Ref{TypeID: "hcmnext.people.explain_worker_state", Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if def.DisplayName != "ExplainWorkerState" {
		t.Fatalf("display %q", def.DisplayName)
	}
}

func TestBindings(t *testing.T) {
	b := Bindings()
	if len(b) != 14 {
		t.Fatalf("got %d want 14", len(b))
	}
	for _, binding := range b {
		if binding.Definition.TypeID == "" {
			t.Fatal("empty binding ref")
		}
	}
}

func TestCoverage(t *testing.T) {
	reg, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	report := Coverage(reg)
	_ = report
}
