package ssrshell

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
)

// TestTodo_WEB_025_Integration exercises the real, cross-package pipeline
// this renderer sits in: the two real Promotion PageDefinitions
// (tools/uxqual/pagedef.PromotionListPageDefinition and
// PromotionDetailPageDefinition, WEB-002's own projection of the live
// Promotion journey pages) go through pagedef.Validate, then [Render],
// then the data island each produces is read back out and cross-checked
// against the same PageDefinition's own [pagedef.PageDefinition.Digest] --
// proving the digest the shell carries is not a second, independently
// computed value that could drift from the contract it describes.
func TestTodo_WEB_025_Integration(t *testing.T) {
	for _, tc := range []struct {
		name string
		pd   pagedef.PageDefinition
	}{
		{"promotion list", pagedef.PromotionListPageDefinition()},
		{"promotion detail", pagedef.PromotionDetailPageDefinition()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if v := tc.pd.Validate(); len(v) != 0 {
				t.Fatalf("fixture PageDefinition is not valid: %v", v)
			}

			rs, err := Render(tc.pd)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			island := extractIsland(t, rs.HTML)
			var got pageIsland
			if err := json.Unmarshal(island, &got); err != nil {
				t.Fatalf("data island is not valid JSON: %v", err)
			}
			if want := tc.pd.Digest(); got.Digest != want {
				t.Errorf("island digest = %q, does not match pagedef.PageDefinition.Digest() = %q: the shell's data island has drifted from its own source contract", got.Digest, want)
			}
			if got.PageID != tc.pd.PageID || got.Version != tc.pd.Version {
				t.Errorf("island (page_id=%q, version=%d) does not match source (page_id=%q, version=%d)", got.PageID, got.Version, tc.pd.PageID, tc.pd.Version)
			}

			// Every widget slot the real page defines must reach the
			// document as an empty mount point -- this package never
			// resolves widget content, it only names where it goes.
			for _, r := range tc.pd.Regions {
				for _, w := range r.Widgets {
					marker := `data-widget-ref="` + w.WidgetRef + `"`
					if !strings.Contains(rs.HTML, marker) {
						t.Errorf("region %q widget %q: expected mount point %q in the document", r.ID, w.ID, marker)
					}
				}
			}

			// The CSP this package would advertise for the document it
			// just produced must at minimum admit the exact stylesheet
			// the document actually inlines.
			csp := ContentSecurityPolicy()
			if !strings.Contains(csp, shellStylesheetHash) {
				t.Errorf("ContentSecurityPolicy() = %q does not admit this document's own stylesheet hash %q", csp, shellStylesheetHash)
			}
			if !strings.Contains(rs.HTML, ShellCSS()) {
				t.Errorf("rendered document does not inline ShellCSS() verbatim, so the pinned CSP hash would not match what the browser actually receives")
			}
		})
	}
}
