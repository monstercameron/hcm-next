package productslice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// canonicalSliceSchema names the schema tag embedded in
// [ProductSliceDefinition.Canonical]'s output. SchemaVersion changes only
// when the canonical JSON shape itself changes, never when the underlying
// slice's field values change.
const (
	canonicalSliceSchema  = "hcmnext.planning.productslice.slice"
	canonicalSliceVersion = 1
)

// sorted returns a deep copy of d with every reference list sorted. Order
// within these lists carries no presentation or precedence meaning -- unlike
// PageDefinition.Regions, whose document order is significant -- so the
// canonical form normalizes it the same way floorplan.Floorplan and
// widgetreg.WidgetDefinition normalize their own reference lists.
func (d ProductSliceDefinition) sorted() ProductSliceDefinition {
	c := d.clone()
	sort.Strings(c.BusinessIntents)
	sort.Strings(c.Features)
	sort.Strings(c.Pages)
	sort.Strings(c.Widgets)
	sort.Strings(c.Capabilities)
	sort.Strings(c.Packages)
	sort.Strings(c.Todos)
	sort.Strings(c.Jurisdictions)
	sort.Strings(c.Personas)
	sort.Strings(c.ExitCriteria)
	return c
}

// Canonical returns deterministic JSON for d: a schema tag plus the
// definition with every reference list sorted, so two definitions that
// differ only in list order produce the same bytes and the same [Digest].
func (d ProductSliceDefinition) Canonical() []byte {
	b, _ := json.Marshal(struct {
		Schema        string                 `json:"schema"`
		SchemaVersion int                    `json:"schema_version"`
		Slice         ProductSliceDefinition `json:"slice"`
	}{canonicalSliceSchema, canonicalSliceVersion, d.sorted()})
	return b
}

// Digest returns d's stable content digest, used by evidence and by the
// registry-level digest in loader.go.
func (d ProductSliceDefinition) Digest() string {
	sum := sha256.Sum256(d.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Explain renders a short, human-readable summary of d: its identity,
// version, the size of each reference list, and its digest. It never
// enumerates the reference lists themselves, so it stays bounded regardless
// of how large a slice grows.
func (d ProductSliceDefinition) Explain() string {
	return fmt.Sprintf(
		"product slice %s@%d (%d business intents, %d features, %d pages, %d widgets, %d capabilities, %d packages, %d todos; %s)",
		d.SliceID, d.Version,
		len(d.BusinessIntents), len(d.Features), len(d.Pages), len(d.Widgets),
		len(d.Capabilities), len(d.Packages), len(d.Todos), d.Digest(),
	)
}
