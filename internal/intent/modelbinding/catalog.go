package modelbinding

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	model "github.com/monstercameron/hcm-next/gen/go/hcmnext/model"
	"github.com/monstercameron/hcm-next/internal/intent"
	"github.com/monstercameron/hcm-next/internal/intent/definitions"
)

// BindCatalog binds the real fourteen drafted definitions
// ([definitions.NewRegistry], [definitions.Bindings]) against the real
// generated model registry ([model.New]).
//
// Beyond what [Bind] alone checks, BindCatalog also refuses (as a "binding"
// element [Gap]) any [intent.Binding] whose Definition does not resolve in
// the compiled intent registry — a binding for a definition that was never
// published, or was renamed/reversioned out from under it.
func BindCatalog() (Table, error) {
	intentReg, err := definitions.NewRegistry()
	if err != nil {
		return Table{}, fmt.Errorf("modelbinding: compile intent registry: %w", err)
	}
	modelReg := model.New()
	bindings := definitions.Bindings()

	table := Bind(bindings, modelReg)

	for _, b := range bindings {
		if _, err := intentReg.Resolve(b.Definition); err != nil {
			table.Gaps = append(table.Gaps, Gap{
				Definition: b.Definition, Element: "binding",
				Detail: fmt.Sprintf("does not resolve in the compiled intent registry: %v", err),
			})
		}
	}
	sort.Slice(table.Gaps, func(i, j int) bool {
		if table.Gaps[i].Definition.String() != table.Gaps[j].Definition.String() {
			return table.Gaps[i].Definition.String() < table.Gaps[j].Definition.String()
		}
		if table.Gaps[i].Element != table.Gaps[j].Element {
			return table.Gaps[i].Element < table.Gaps[j].Element
		}
		return table.Gaps[i].Detail < table.Gaps[j].Detail
	})

	return table, nil
}

// Digest computes a stable sha256 digest over t's compiled content, the same
// canonical length-tagged-parts convention
// [github.com/monstercameron/hcm-next/internal/intent/model.Registry.Digest]
// and [github.com/monstercameron/hcm-next/tools/gen/storagemanifest] use.
// Two Tables built from identical bindings and an identical generated
// registry always agree — this is what TestTodo_MSRC_009_Golden pins.
func (t Table) Digest() string {
	h := sha256.New()
	w := func(parts ...string) {
		for _, p := range parts {
			h.Write([]byte(p))
			h.Write([]byte{0})
		}
	}
	for _, b := range t.Bindings {
		w("BINDING", b.Definition.String())
		for _, e := range b.Entities {
			w("ENTITY", e.Ref())
		}
		for _, p := range b.ReadProperties {
			w("READ", p.Ref)
		}
		for _, p := range b.WriteProperties {
			w("WRITE", p.Ref)
		}
	}
	for _, g := range t.Gaps {
		w("GAP", g.Definition.String(), g.Element, g.Detail)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// intentRefFor is a small test/reporting convenience: it parses "id/vN" the
// same way [intent.ParseRef] does, kept local so callers of this package
// never need to import intent just to build a Ref for a lookup.
func intentRefFor(typeID string, version uint32) intent.Ref {
	return intent.Ref{TypeID: typeID, Version: version}
}
