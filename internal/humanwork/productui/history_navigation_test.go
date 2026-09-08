package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestHistoryNavigationExposesAccessibleDisabledEndStates(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(HistoryNavigation, HistoryNavigationProps{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`aria-label="Recently visited resources"`,
		`aria-label="Back to the previous resource"`,
		`aria-label="Forward to the next resource"`,
		`class="history-navigation-button history-navigation-back"`,
		`class="history-navigation-button history-navigation-forward"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("history navigation markup missing %q: %s", want, markup)
		}
	}
	if got := strings.Count(markup, `disabled`); got != 2 {
		t.Fatalf("disabled history controls = %d, want 2: %s", got, markup)
	}
}

func TestHistoryNavigationEnablesBothAvailableDirections(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(HistoryNavigation, HistoryNavigationProps{
		CanGoBack: true, CanGoForward: true,
		GoBack: func() {}, GoForward: func() {},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, `disabled`) {
		t.Fatalf("available history controls rendered disabled: %s", markup)
	}
}

func TestHistoryNavigationPrecedesGlobalSearchAndHasResponsiveStyles(t *testing.T) {
	doc, err := Render(testView(PagePeople))
	if err != nil {
		t.Fatal(err)
	}
	historyIndex := strings.Index(doc, `class="history-navigation"`)
	searchIndex := strings.Index(doc, `class="global-search"`)
	if historyIndex < 0 || searchIndex < 0 || historyIndex >= searchIndex {
		t.Fatalf("history controls must render immediately before global search")
	}
	for _, want := range []string{
		`grid-template-columns:auto minmax(0,1fr);`,
		`cursor:not-allowed;opacity:0.38;`,
		`@media (max-width:760px){.topbar>.header-navigation-tools`,
		`@media (forced-colors:active){.history-navigation-button`,
	} {
		if !strings.Contains(Stylesheet(), want) {
			t.Fatalf("history navigation stylesheet missing %q", want)
		}
	}
}
