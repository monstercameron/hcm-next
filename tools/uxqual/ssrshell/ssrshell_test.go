package ssrshell

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// validMinimalPage returns a PageDefinition that passes pagedef.Validate
// with one region of every closed RegionKind, monotonic heading levels
// (1, 2, 2, 2, 3, 3, 2), one widget slot, one data binding, and one action.
// Tests in this package mutate a copy of it (like pagedef's own
// security_test.go mutates validMinimalPage there) so the only variable
// under test is whatever the test case changes.
func validMinimalPage() pagedef.PageDefinition {
	return pagedef.PageDefinition{
		PageID:       "test.page",
		Version:      1,
		FloorplanRef: "floorplan.test.v1",
		Regions: []pagedef.Region{
			{ID: "shell", Kind: pagedef.RegionShell},
			{ID: "identity", Kind: pagedef.RegionPageIdentity, Heading: &pagedef.Heading{Level: 1, Text: "Test page"}},
			{ID: "authority", Kind: pagedef.RegionAuthorityContext, Heading: &pagedef.Heading{Level: 2, Text: "Authority"}},
			{ID: "nav", Kind: pagedef.RegionLocalNavigation, Heading: &pagedef.Heading{Level: 2, Text: "Navigation"}},
			{
				ID:      "primary",
				Kind:    pagedef.RegionPrimary,
				Heading: &pagedef.Heading{Level: 2, Text: "Primary job"},
				Widgets: []pagedef.WidgetSlot{{ID: "w1", WidgetRef: "widget.table.v1"}},
				Bindings: []pagedef.DataBinding{
					{ID: "b1", RPC: pagedef.RPCRef(pagedef.JourneyServiceName, "ListJourneys")},
				},
				Actions: []pagedef.ActionRef{
					{ID: "a1", RPC: pagedef.RPCRef(pagedef.JourneyServiceName, "ProposeJourney"), RequiredRole: "manager"},
				},
			},
			{ID: "supporting", Kind: pagedef.RegionSupporting, Heading: &pagedef.Heading{Level: 3, Text: "Supporting"}},
			{ID: "utility", Kind: pagedef.RegionUtility, Heading: &pagedef.Heading{Level: 3, Text: "Utility"}},
			{ID: "completion", Kind: pagedef.RegionCompletion, Heading: &pagedef.Heading{Level: 2, Text: "Completion"}},
		},
		Accessibility: pagedef.Accessibility{
			Landmarks:  []string{"banner", "navigation", "main", "complementary", "contentinfo"},
			LiveRegion: pagedef.LiveRegionPolite,
		},
		BrandTokens: []string{"brand.color.primary"},
	}
}

var headingTagPattern = regexp.MustCompile(`<h([1-6]) `)

// headingLevelsInOrder returns the heading levels found in doc, in document
// order, by scanning for <h1>..<h6> open tags.
func headingLevelsInOrder(doc string) []int {
	matches := headingTagPattern.FindAllStringSubmatch(doc, -1)
	levels := make([]int, 0, len(matches))
	for _, m := range matches {
		levels = append(levels, int(m[1][0]-'0'))
	}
	return levels
}

// TestTodo_WEB_025 is the PRIMARY test: Render turns a validated
// PageDefinition into a semantic HTML shell carrying every structural
// element the WEB-025 todo names -- landmark elements per region kind,
// headings at their declared levels, a live-region attribute, empty
// widget-slot mount points identified by widget ref, and a data island
// naming the page id/version/digest -- with no inline script and no free
// HTML copied verbatim from the definition.
func TestTodo_WEB_025(t *testing.T) {
	pd := validMinimalPage()
	rs, err := Render(pd)
	if err != nil {
		t.Fatalf("Render(validMinimalPage()) returned an error: %v", err)
	}
	doc := rs.HTML

	t.Run("doctype and lang", func(t *testing.T) {
		if !strings.HasPrefix(doc, "<!doctype html>\n<html lang=\"en\">") {
			t.Fatalf("document does not start with the expected doctype/html tag: %q", doc[:min(60, len(doc))])
		}
	})

	t.Run("every region kind renders its documented landmark element", func(t *testing.T) {
		wantSubstrings := []string{
			`<header id="region-shell" aria-label="Application shell: shell">`,
			`<section id="region-identity" aria-label="Page identity: identity">`,
			`<section id="region-authority" aria-label="Authority context: authority">`,
			`<nav id="region-nav" aria-label="Local navigation: nav">`,
			`<main id="main-content" aria-label="Primary: primary">`,
			`<aside id="region-supporting" aria-label="Supporting: supporting">`,
			`<aside id="region-utility" aria-label="Utility: utility">`,
			`<footer id="region-completion" aria-label="Completion: completion">`,
		}
		for _, want := range wantSubstrings {
			if !strings.Contains(doc, want) {
				t.Errorf("document does not contain expected landmark markup %q", want)
			}
		}
	})

	t.Run("headings render at their declared levels, in order", func(t *testing.T) {
		got := headingLevelsInOrder(doc)
		want := []int{1, 2, 2, 2, 3, 3, 2}
		if len(got) != len(want) {
			t.Fatalf("heading levels = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("heading[%d] level = %d, want %d (full sequence %v)", i, got[i], want[i], got)
			}
		}
	})

	t.Run("live region carries the declared politeness as an attribute", func(t *testing.T) {
		if !strings.Contains(doc, `id="live-region" role="status" aria-live="polite" aria-atomic="true"`) {
			t.Errorf("document does not carry the declared polite live region")
		}
	})

	t.Run("widget slots are empty mount points identified by widget ref", func(t *testing.T) {
		if !strings.Contains(doc, `<div class="widget-slot" data-slot-id="w1" data-widget-ref="widget.table.v1" role="presentation"></div>`) {
			t.Errorf("document does not carry an empty widget-slot mount point for widget ref widget.table.v1")
		}
	})

	t.Run("the data island names page id, version, and digest", func(t *testing.T) {
		island := extractIsland(t, doc)
		var got pageIsland
		if err := json.Unmarshal(island, &got); err != nil {
			t.Fatalf("data island is not valid JSON: %v (%s)", err, island)
		}
		if got.PageID != pd.PageID {
			t.Errorf("island page_id = %q, want %q", got.PageID, pd.PageID)
		}
		if got.Version != pd.Version {
			t.Errorf("island version = %d, want %d", got.Version, pd.Version)
		}
		if got.Digest != pd.Digest() {
			t.Errorf("island digest = %q, want pd.Digest() = %q", got.Digest, pd.Digest())
		}
	})

	t.Run("no inline script other than the JSON data island", func(t *testing.T) {
		if strings.Count(doc, "<script") != 1 {
			t.Fatalf("document has %d <script tags, want exactly 1 (the data island): %s", strings.Count(doc, "<script"), doc)
		}
		if !strings.Contains(doc, `<script type="application/json" id="page-definition">`) {
			t.Errorf("the one <script> element is not the expected application/json data island")
		}
	})

	t.Run("rendering is deterministic", func(t *testing.T) {
		again, err := Render(pd)
		if err != nil {
			t.Fatalf("second Render call returned an error: %v", err)
		}
		if again.HTML != rs.HTML {
			t.Errorf("Render(pd) is not deterministic: two calls produced different bytes")
		}
		if again.Digest != rs.Digest {
			t.Errorf("Render(pd) digest is not deterministic: %q vs %q", again.Digest, rs.Digest)
		}
	})

	t.Run("RenderedShell.Digest matches sha256 of the rendered bytes", func(t *testing.T) {
		if rs.Digest == "" || !strings.HasPrefix(rs.Digest, "sha256:") {
			t.Fatalf("RenderedShell.Digest = %q, want a sha256:<hex> digest", rs.Digest)
		}
	})
}

// TestTodo_WEB_025_RefusesInvalidPageDefinition proves the GREEN condition's
// other half: Render is a renderer for validated definitions, not a
// best-effort renderer that silently papers over a pagedef violation.
func TestTodo_WEB_025_RefusesInvalidPageDefinition(t *testing.T) {
	pd := validMinimalPage()
	pd.Regions = nil // pagedef.Validate requires at least one region
	if _, err := Render(pd); err == nil {
		t.Fatalf("Render(pd with no regions) = nil error, want a refusal")
	}
}

// extractIsland pulls the JSON payload out of the page-definition data
// island in doc.
func extractIsland(t *testing.T, doc string) []byte {
	t.Helper()
	const open = `<script type="application/json" id="page-definition">`
	i := strings.Index(doc, open)
	if i < 0 {
		t.Fatalf("document does not contain the page-definition data island")
	}
	rest := doc[i+len(open):]
	j := strings.Index(rest, "</script>")
	if j < 0 {
		t.Fatalf("page-definition data island has no closing </script>")
	}
	return []byte(rest[:j])
}
