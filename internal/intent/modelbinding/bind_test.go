package modelbinding

import (
	"testing"

	model "github.com/monstercameron/hcm-next/gen/go/hcmnext/model"
	"github.com/monstercameron/hcm-next/internal/intent"
)

func realRegistry(t *testing.T) *model.Registry {
	t.Helper()
	return model.New()
}

// TestBindUnknownAggregateRootIsAGap is the MSRC-009 RED case for an entity
// absent from the generated registry.
func TestBindUnknownAggregateRootIsAGap(t *testing.T) {
	def := intentRefFor("hcmnext.test.unknown_entity", 1)
	table := Bind([]intent.Binding{{
		Definition:     def,
		AggregateRoots: []string{"NoSuchEntity"},
		ReadProperties: []string{"person.identity"},
	}}, realRegistry(t))

	if len(table.Bindings) != 0 {
		t.Fatalf("Bindings = %v, want none (the binding names an unknown entity)", table.Bindings)
	}
	found := false
	for _, g := range table.Gaps {
		if g.Definition == def && g.Element == "aggregate_root" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Gaps = %v, want an aggregate_root gap for %s", table.Gaps, def)
	}
}

// TestBindUnknownPropertyIsAGap is the MSRC-009 RED case for a property
// absent from the generated registry, on both the read and write side.
func TestBindUnknownPropertyIsAGap(t *testing.T) {
	def := intentRefFor("hcmnext.test.unknown_property", 1)
	table := Bind([]intent.Binding{{
		Definition:      def,
		AggregateRoots:  []string{"Person"},
		ReadProperties:  []string{"person.no_such_property"},
		WriteProperties: []string{"person.also_missing"},
	}}, realRegistry(t))

	if len(table.Bindings) != 0 {
		t.Fatalf("Bindings = %v, want none", table.Bindings)
	}
	elements := map[string]bool{}
	for _, g := range table.Gaps {
		if g.Definition == def {
			elements[g.Element] = true
		}
	}
	if !elements["read_property"] {
		t.Errorf("Gaps = %v, want a read_property gap", table.Gaps)
	}
	if !elements["write_property"] {
		t.Errorf("Gaps = %v, want a write_property gap", table.Gaps)
	}
}

// TestBindImmutableWriteIsRefused is the MSRC-009 RED case
// "refuses a write to a property the model marks immutable":
// proposal_revision.material_digest is IMMUTABLE/IMMUTABLE_NO_CORRECTION on
// ProposalRevision, an AGGREGATE_ROOT (not EVIDENCE) entity, so the
// generated registry marks it Immutable, and no binding may write it.
func TestBindImmutableWriteIsRefused(t *testing.T) {
	reg := realRegistry(t)
	prop, ok := reg.Property("proposal_revision.material_digest")
	if !ok {
		t.Fatal("proposal_revision.material_digest is not published by the generated registry")
	}
	if !prop.Immutable {
		t.Fatal("proposal_revision.material_digest is not marked Immutable; test assumption is stale")
	}

	def := intentRefFor("hcmnext.test.writes_immutable", 1)
	table := Bind([]intent.Binding{{
		Definition:      def,
		AggregateRoots:  []string{"ProposalRevision"},
		ReadProperties:  []string{"proposal_revision.material_digest"},
		WriteProperties: []string{"proposal_revision.material_digest"},
	}}, reg)

	if len(table.Bindings) != 0 {
		t.Fatalf("Bindings = %v, want none (the binding writes an immutable property)", table.Bindings)
	}
	found := false
	for _, g := range table.Gaps {
		if g.Definition == def && g.Element == "immutable_write" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Gaps = %v, want an immutable_write gap for %s", table.Gaps, def)
	}
}

// TestBindValidBindingResolves is the positive-path smoke test: a binding
// naming only real, non-immutable model behavior resolves with no gaps and
// carries the resolved descriptors.
func TestBindValidBindingResolves(t *testing.T) {
	def := intentRefFor("hcmnext.test.valid", 1)
	table := Bind([]intent.Binding{{
		Definition:      def,
		AggregateRoots:  []string{"Assignment"},
		ReadProperties:  []string{"assignment.position_ref"},
		WriteProperties: []string{"assignment.effective_interval"},
	}}, realRegistry(t))

	if len(table.Gaps) != 0 {
		t.Fatalf("Gaps = %v, want none", table.Gaps)
	}
	if len(table.Bindings) != 1 {
		t.Fatalf("Bindings len = %d, want 1", len(table.Bindings))
	}
	mb := table.Bindings[0]
	if len(mb.Entities) != 1 || mb.Entities[0].Name != "Assignment" {
		t.Errorf("Entities = %v, want [Assignment]", mb.Entities)
	}
	if len(mb.ReadProperties) != 1 || mb.ReadProperties[0].Ref != "assignment.position_ref" {
		t.Errorf("ReadProperties = %v", mb.ReadProperties)
	}
	if len(mb.WriteProperties) != 1 || mb.WriteProperties[0].Ref != "assignment.effective_interval" {
		t.Errorf("WriteProperties = %v", mb.WriteProperties)
	}
}

func TestTableFullyBound(t *testing.T) {
	empty := Table{}
	if empty.FullyBound(0) != true {
		t.Error("an empty table over zero definitions should be fully bound")
	}
	if empty.FullyBound(1) {
		t.Error("an empty table over one definition should not be fully bound")
	}
	withGap := Table{Gaps: []Gap{{Definition: intentRefFor("x", 1), Element: "read_property", Detail: "d"}}}
	if withGap.FullyBound(0) {
		t.Error("a table with a gap should never be fully bound")
	}
}

func TestGapString(t *testing.T) {
	g := Gap{Definition: intentRefFor("hcmnext.test.thing", 1), Element: "read_property", Detail: "missing"}
	want := "hcmnext.test.thing/v1: read_property (missing)"
	if got := g.String(); got != want {
		t.Errorf("Gap.String() = %q, want %q", got, want)
	}
}
