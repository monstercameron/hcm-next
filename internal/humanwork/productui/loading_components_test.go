package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestLoadingProxyUsesPageShapedAccessibleShells(t *testing.T) {
	tests := map[PageID]string{
		PageHome:         "loading-work-layout",
		PageJourneys:     "loading-work-layout",
		PageWork:         "loading-work-layout",
		PagePeople:       "loading-table-layout",
		PageHistory:      "loading-table-layout",
		PagePerson:       "loading-profile-layout",
		PageOrganization: "loading-analysis-layout",
		PageInsights:     "loading-analysis-layout",
		PageSettings:     "loading-settings-layout",
		PageAppearance:   "loading-settings-layout",
	}
	for page, shape := range tests {
		t.Run(string(page), func(t *testing.T) {
			view := NewView(page, "HarborCare Demo", "manager", "self")
			out, err := ui.RenderToString(BuildLoading(view))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				`class="app-shell is-loading"`, `id="main-content"`, `aria-busy="true"`,
				`aria-live="polite"`, "Loading authorized data from the live cell", shape,
				`aria-hidden="true"`, "loading-progress",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("loading %s surface missing %q", page, want)
				}
			}
			if strings.Contains(out, "0 promotion journeys are visible") {
				t.Fatal("loading shell exposed an unresolved network count as zero")
			}
		})
	}
}

func TestLoadingProxyMotionHonorsExplicitAndOperatingSystemPreferences(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`:root:not([data-hcm-motion-preference="reduce"]):not([data-hcm-motion-preference="limited"]) .loading-block:after`,
		`@media(prefers-reduced-motion:no-preference)`,
		`animation:hcm-shimmer`,
		`@media(forced-colors:active){.loading-block,.loading-progress`,
		`.app-shell.nav-collapsed .loading-progress{left:72px}`,
		`@media(max-width:760px){.app-shell .loading-progress,.app-shell.nav-collapsed .loading-progress{top:0;left:0}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("loading motion contract missing %q", want)
		}
	}
}
