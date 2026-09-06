package pagedef

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

// canonicalSchema and canonicalSchemaVersion open every encoding this file
// produces, so a PageDefinition digest can never collide with a digest
// computed over an unrelated schema even if the byte shapes happened to
// coincide.
const (
	canonicalSchema        = "hcmnext.uxqual.pagedef.PageDefinition"
	canonicalSchemaVersion = 1
)

// canonicalWriter accumulates a deterministic, self-delimiting byte stream:
// every field is written netstring-style ("<byte length>:<tag>=<byte
// length>:<value>;") so that no concatenation of two fields' text can ever
// be mistaken for a different pair of fields -- the collision a naive
// "join with a separator" encoding risks whenever a field's own value can
// contain that separator. It has no map traversal and no clock read, so the
// only thing that can change its output is the caller changing what it
// writes.
type canonicalWriter struct {
	buf bytes.Buffer
}

func (w *canonicalWriter) field(tag, value string) {
	fmt.Fprintf(&w.buf, "%d:%s=%d:%s;", len(tag), tag, len(value), value)
}

func (w *canonicalWriter) int(tag string, v int) {
	w.field(tag, strconv.Itoa(v))
}

// Canonical returns pd's deterministic byte encoding: the same
// PageDefinition value always encodes to the same bytes, and any change to
// a semantically meaningful field -- including Version -- changes them.
// Field and slice order follow pd's own order throughout, because that
// order (region order, heading order, binding/action order) is itself
// semantically meaningful and must be able to change the digest.
func (pd PageDefinition) Canonical() []byte {
	w := &canonicalWriter{}
	w.field("$schema", canonicalSchema)
	w.int("$schema_version", canonicalSchemaVersion)
	w.field("page_id", pd.PageID)
	w.int("version", pd.Version)
	w.field("floorplan_ref", pd.FloorplanRef)

	w.int("regions#", len(pd.Regions))
	for i, r := range pd.Regions {
		p := fmt.Sprintf("regions[%d].", i)
		w.field(p+"id", r.ID)
		w.field(p+"kind", string(r.Kind))
		if r.Heading != nil {
			w.field(p+"heading?", "1")
			w.int(p+"heading.level", r.Heading.Level)
			w.field(p+"heading.text", r.Heading.Text)
		} else {
			w.field(p+"heading?", "0")
		}

		w.int(p+"widgets#", len(r.Widgets))
		for j, wg := range r.Widgets {
			wp := fmt.Sprintf("%swidgets[%d].", p, j)
			w.field(wp+"id", wg.ID)
			w.field(wp+"widget_ref", wg.WidgetRef)
		}

		w.int(p+"bindings#", len(r.Bindings))
		for j, b := range r.Bindings {
			bp := fmt.Sprintf("%sbindings[%d].", p, j)
			w.field(bp+"id", b.ID)
			w.field(bp+"rpc", b.RPC)
		}

		w.int(p+"actions#", len(r.Actions))
		for j, a := range r.Actions {
			ap := fmt.Sprintf("%sactions[%d].", p, j)
			w.field(ap+"id", a.ID)
			w.field(ap+"rpc", a.RPC)
			w.field(ap+"required_role", a.RequiredRole)
		}
	}

	w.int("accessibility.landmarks#", len(pd.Accessibility.Landmarks))
	for i, lm := range pd.Accessibility.Landmarks {
		w.field(fmt.Sprintf("accessibility.landmarks[%d]", i), lm)
	}
	w.field("accessibility.live_region", string(pd.Accessibility.LiveRegion))

	w.int("brand_tokens#", len(pd.BrandTokens))
	for i, t := range pd.BrandTokens {
		w.field(fmt.Sprintf("brand_tokens[%d]", i), t)
	}

	return w.buf.Bytes()
}

// Digest returns "sha256:<lowercase hex>" over pd.Canonical(). It never
// fails: Canonical is a pure function of pd's own fields with no I/O and no
// value that can refuse to encode, unlike a digest computed through a
// protobuf reflection profile.
func (pd PageDefinition) Digest() string {
	sum := sha256.Sum256(pd.Canonical())
	return "sha256:" + hex.EncodeToString(sum[:])
}
