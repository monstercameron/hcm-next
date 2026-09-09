package modelbinding

import (
	"fmt"
	"sort"

	model "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/model"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// Gap is one binding element [Bind] could not resolve against the generated
// model registry, or a write it refused outright.
type Gap struct {
	Definition intent.Ref
	// Element is "aggregate_root", "read_property", "write_property" or
	// "immutable_write".
	Element string
	Detail  string
}

func (g Gap) String() string {
	return fmt.Sprintf("%s: %s (%s)", g.Definition, g.Element, g.Detail)
}

// ModelBinding is one definition bound to the exact generated model
// behavior its [intent.Binding] names: the resolved entity descriptors its
// AggregateRoots name, and the resolved property descriptors it reads and
// (non-immutable) writes.
type ModelBinding struct {
	Definition      intent.Ref
	Entities        []model.EntityMeta
	ReadProperties  []model.PropertyMeta
	WriteProperties []model.PropertyMeta
}

// Table is the full answer [Bind] and [BindCatalog] return: one ModelBinding
// per definition whose binding fully resolved, plus every gap found across
// every definition. A definition with even one gap contributes no
// ModelBinding — partial binding is not binding.
type Table struct {
	Bindings []ModelBinding
	Gaps     []Gap
}

// FullyBound reports whether every one of total definitions bound cleanly.
func (t Table) FullyBound(total int) bool {
	return len(t.Gaps) == 0 && len(t.Bindings) == total
}

// Bind resolves each binding's AggregateRoots, ReadProperties and
// WriteProperties against reg.
//
// It refuses, as a [Gap] rather than a panic or a silent skip, in exactly
// two ways: a binding naming an entity or property [reg] does not publish
// (MSRC-009 RED: "an intent lacks ... reads/writes ... it cannot resolve"),
// and a WriteProperties entry naming a property reg marks Immutable — a
// property the model says no drafted definition may write (MSRC-009's own
// "refuses a write to a property the model marks immutable").
//
// Bind is a pure function of bindings and reg: it never mutates either, and
// the same inputs always produce the same Table, entries sorted by
// definition then, within a definition, by resolution order (aggregate
// roots, then reads, then writes) — see golden_test.go's TestTodo_MSRC_009_Golden.
func Bind(bindings []intent.Binding, reg *model.Registry) Table {
	var table Table
	for _, b := range bindings {
		mb, gaps := bindOne(b, reg)
		if len(gaps) == 0 {
			table.Bindings = append(table.Bindings, mb)
			continue
		}
		table.Gaps = append(table.Gaps, gaps...)
	}
	sort.Slice(table.Bindings, func(i, j int) bool {
		return table.Bindings[i].Definition.String() < table.Bindings[j].Definition.String()
	})
	sort.Slice(table.Gaps, func(i, j int) bool {
		if table.Gaps[i].Definition.String() != table.Gaps[j].Definition.String() {
			return table.Gaps[i].Definition.String() < table.Gaps[j].Definition.String()
		}
		if table.Gaps[i].Element != table.Gaps[j].Element {
			return table.Gaps[i].Element < table.Gaps[j].Element
		}
		return table.Gaps[i].Detail < table.Gaps[j].Detail
	})
	return table
}

func bindOne(b intent.Binding, reg *model.Registry) (ModelBinding, []Gap) {
	mb := ModelBinding{Definition: b.Definition}
	var gaps []Gap

	for _, name := range b.AggregateRoots {
		e, ok := reg.EntityByName(name)
		if !ok {
			gaps = append(gaps, Gap{
				Definition: b.Definition, Element: "aggregate_root",
				Detail: fmt.Sprintf("%q is not published by the generated model registry", name),
			})
			continue
		}
		mb.Entities = append(mb.Entities, e)
	}

	for _, ref := range b.ReadProperties {
		p, ok := reg.Property(ref)
		if !ok {
			gaps = append(gaps, Gap{
				Definition: b.Definition, Element: "read_property",
				Detail: fmt.Sprintf("%q is not published by the generated model registry", ref),
			})
			continue
		}
		mb.ReadProperties = append(mb.ReadProperties, p)
	}

	for _, ref := range b.WriteProperties {
		p, ok := reg.Property(ref)
		if !ok {
			gaps = append(gaps, Gap{
				Definition: b.Definition, Element: "write_property",
				Detail: fmt.Sprintf("%q is not published by the generated model registry", ref),
			})
			continue
		}
		if p.Immutable {
			gaps = append(gaps, Gap{
				Definition: b.Definition, Element: "immutable_write",
				Detail: fmt.Sprintf(
					"%q is IMMUTABLE/IMMUTABLE_NO_CORRECTION on a non-EVIDENCE entity; no definition may write it", ref),
			})
			continue
		}
		mb.WriteProperties = append(mb.WriteProperties, p)
	}

	return mb, gaps
}
