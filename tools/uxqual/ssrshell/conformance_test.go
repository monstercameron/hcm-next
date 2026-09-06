package ssrshell

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
)

// TestTodo_WEB_025_Conformance proves the two structural guarantees the
// WEB-025 todo names beyond "it renders something": every region kind in
// pagedef's closed vocabulary renders to a documented landmark (not a
// silent fallback, not an omission), and heading order is preserved
// through the renderer rather than only validated upstream by pagedef.
func TestTodo_WEB_025_Conformance(t *testing.T) {
	t.Run("every RegionKind has a documented landmark mapping in the closed tag set", func(t *testing.T) {
		allowed := map[string]bool{}
		for _, tag := range LandmarkTags {
			allowed[tag] = true
		}
		for _, kind := range pagedef.RegionKinds() {
			tag, label, ok := LandmarkForRegionKind(kind)
			if !ok {
				t.Errorf("RegionKind %q has no documented landmark mapping", kind)
				continue
			}
			if !allowed[tag] {
				t.Errorf("RegionKind %q maps to tag %q, which is not in the documented LandmarkTags set %v", kind, tag, LandmarkTags)
			}
			if label == "" {
				t.Errorf("RegionKind %q maps to an empty label", kind)
			}
		}
	})

	t.Run("every region kind actually renders to its documented landmark element", func(t *testing.T) {
		pd := validMinimalPage() // covers all eight kinds, see validMinimalPage's doc comment
		rs, err := Render(pd)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		for _, r := range pd.Regions {
			tag, _, ok := LandmarkForRegionKind(r.Kind)
			if !ok {
				t.Fatalf("test fixture uses undocumented RegionKind %q", r.Kind)
			}
			openTag := fmt.Sprintf("<%s id=\"", tag)
			closeTag := fmt.Sprintf("</%s>", tag)
			regionMarker := fmt.Sprintf("region-%s", r.ID)
			if r.Kind == pagedef.RegionPrimary {
				regionMarker = "main-content" // the one <main> uses a fixed id, see render.go
			}
			if !strings.Contains(rs.HTML, openTag) {
				t.Errorf("region %q (kind %q): expected an open %q tag, found none", r.ID, r.Kind, tag)
			}
			if !strings.Contains(rs.HTML, regionMarker) {
				t.Errorf("region %q (kind %q): expected its id marker %q in the document", r.ID, r.Kind, regionMarker)
			}
			if !strings.Contains(rs.HTML, closeTag) {
				t.Errorf("region %q (kind %q): expected a closing %q tag, found none", r.ID, r.Kind, tag)
			}
		}
	})

	t.Run("heading order is preserved through the renderer, not just validated by pagedef", func(t *testing.T) {
		pd := validMinimalPage()
		var wantLevels []int
		for _, r := range pd.Regions {
			if r.Heading != nil {
				wantLevels = append(wantLevels, r.Heading.Level)
			}
		}
		rs, err := Render(pd)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		gotLevels := headingLevelsInOrder(rs.HTML)
		if len(gotLevels) != len(wantLevels) {
			t.Fatalf("rendered heading levels = %v, want %v (same length as pd's own headings in order)", gotLevels, wantLevels)
		}
		for i := range wantLevels {
			if gotLevels[i] != wantLevels[i] {
				t.Errorf("rendered heading[%d] level = %d, want %d (pd's own declared order): full sequence got=%v want=%v", i, gotLevels[i], wantLevels[i], gotLevels, wantLevels)
			}
		}
	})

	t.Run("reordering regions in the PageDefinition reorders the rendered landmarks identically", func(t *testing.T) {
		pd := validMinimalPage()
		// Swap the supporting and utility regions (both render as <aside>,
		// so this specifically proves document order -- not just kind --
		// drives render order).
		pd.Regions[5], pd.Regions[6] = pd.Regions[6], pd.Regions[5]
		rs, err := Render(pd)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		utilityIdx := strings.Index(rs.HTML, `id="region-utility"`)
		supportingIdx := strings.Index(rs.HTML, `id="region-supporting"`)
		if utilityIdx < 0 || supportingIdx < 0 {
			t.Fatalf("expected both region-utility and region-supporting in the document")
		}
		if utilityIdx > supportingIdx {
			t.Errorf("expected region-utility (now first in pd.Regions) to render before region-supporting; got utility at %d, supporting at %d", utilityIdx, supportingIdx)
		}
	})
}
