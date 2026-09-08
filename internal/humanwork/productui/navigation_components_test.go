package productui

import (
	"strings"
	"testing"
)

func TestNavigationRegistryBuildsReusableSubmenus(t *testing.T) {
	view := testView(PageHistory)
	_, items := projectNavigation(view)
	work, ok := projectedNavigationItem(items, PageWork)
	if !ok {
		t.Fatal("My Work navigation group is missing")
	}
	if !work.Active || len(work.Children) != 2 || work.Children[0].Label != "Work queue" || work.Children[1].Label != "Work History" {
		t.Fatalf("My Work submenu = %+v", work)
	}
	admin, ok := projectedNavigationItem(items, PageAdmin)
	if !ok || len(admin.Children) != 11 || admin.Children[1].Page != PageWorkerIDs || admin.Children[2].Page != PageRoles || admin.Children[3].Page != PageOrganizationVisibility || admin.Children[4].Page != PageAppearance || admin.Children[5].Page != PageStudio || admin.Children[6].Page != PagePolicyStudio || admin.Children[7].Page != PagePolicySimulation || admin.Children[8].Page != PageConfigurationCenter || admin.Children[9].Page != PageIntegrationOperations || admin.Children[10].Page != PageReconciliationWorkbench {
		t.Fatalf("Admin submenu = %+v, present=%t", admin, ok)
	}
}

func TestMenuFilterKeepsOnlyMatchingHierarchy(t *testing.T) {
	view := ApplyRequest(testView(PageHome), PageRequest{MenuQuery: "history"})
	favorites, items := projectNavigation(view)
	if len(favorites) != 0 || len(items) != 1 {
		t.Fatalf("filtered navigation favorites=%+v items=%+v", favorites, items)
	}
	work := items[0]
	if work.Page != PageWork || !work.Expanded || len(work.Children) != 1 || work.Children[0].Page != PageHistory {
		t.Fatalf("history filter did not retain its open parent: %+v", work)
	}

	view.MenuQuery = "does not exist"
	favorites, items = projectNavigation(view)
	if len(favorites) != 0 || len(items) != 0 {
		t.Fatalf("empty menu filter invented results: favorites=%+v items=%+v", favorites, items)
	}
}

func TestMenuFilterFuzzyRanksMetadataAndHidesUnrelatedSupport(t *testing.T) {
	view := ApplyRequest(testView(PageSettings), PageRequest{MenuQuery: "pe"})
	favorites, items := projectNavigation(view)
	if len(favorites) != 0 || len(items) != 1 || items[0].Page != PagePeople {
		t.Fatalf("short prefix should resolve only People: favorites=%+v items=%+v", favorites, items)
	}
	props := navigationSidebarProps(view)
	if len(props.Support) != 0 {
		t.Fatalf("unrelated support links remained during search: %+v", props.Support)
	}
	if items[0].MatchDetail == "" || items[0].MatchScore == 0 {
		t.Fatalf("matching metadata was not projected: %+v", items[0])
	}
}

func TestMenuFilterFindsAliasesTyposAndMultipleTerms(t *testing.T) {
	for _, test := range []struct {
		query string
		page  PageID
	}{
		{query: "employee", page: PagePeople},
		{query: "poeple", page: PagePeople},
		{query: "ppl", page: PagePeople},
		{query: "dark mode", page: PageAdmin},
		{query: "past workflow", page: PageWork},
		{query: "org chart", page: PageOrganization},
	} {
		t.Run(test.query, func(t *testing.T) {
			view := ApplyRequest(testView(PageHome), PageRequest{MenuQuery: test.query})
			_, items := projectNavigation(view)
			if len(items) == 0 || items[0].Page != test.page {
				t.Fatalf("fuzzy query %q ranked %+v, want %s first", test.query, items, test.page)
			}
		})
	}
}

func TestSupportMenusParticipateInFuzzySearch(t *testing.T) {
	view := ApplyRequest(testView(PageHome), PageRequest{MenuQuery: "language"})
	props := navigationSidebarProps(view)
	if len(props.Items) != 0 || len(props.Support) != 1 || props.Support[0].Page != PageSettings {
		t.Fatalf("language query = items %+v support %+v, want Settings only", props.Items, props.Support)
	}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, ">Help</span>") || !strings.Contains(doc, "available preferences") {
		t.Fatalf("filtered support result did not render metadata: %s", doc)
	}
}

func TestEveryRegisteredPageCarriesNavigationSearchMetadata(t *testing.T) {
	for _, definition := range PageDefinitions() {
		if definition.SubtitleKey == "" || len(definition.SearchTerms) == 0 {
			t.Errorf("page %s has incomplete navigation search metadata: %+v", definition.ID, definition)
		}
	}
}

func TestFavoriteOnlySearchDoesNotRenderAnEmptyAllNavigationSection(t *testing.T) {
	view := ApplyRequest(testView(PageHome), PageRequest{MenuQuery: "staff", FavoritePages: []PageID{PagePeople}})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, ">Favorites</li>") || strings.Contains(doc, ">All navigation</li>") {
		t.Fatal("favorite-only fuzzy result rendered an empty ordinary-navigation section")
	}
}

func TestMenuFilterUsesDebouncedSoftwareNavigationAndImmediateSubmit(t *testing.T) {
	view := testView(PageSettings)
	var scheduled, immediate string
	cancelled := 0
	view.Navigate = func(href string) { immediate = href }
	view.NavigateDebounced = func(href string) { scheduled = href }
	view.CancelDebouncedNavigation = func() { cancelled++ }
	filter := navigationSidebarProps(view).Filter
	if filter.OnInput == nil || filter.OnFilter == nil {
		t.Fatal("live menu filter has no input or submit enhancement")
	}

	filter.OnInput(" work ")
	if scheduled != "/workspace/app/settings?menu_q=work" || immediate != "" || cancelled != 0 {
		t.Fatalf("debounced filter scheduled=%q immediate=%q cancelled=%d", scheduled, immediate, cancelled)
	}
	filter.OnFilter("people")
	if immediate != "/workspace/app/settings?menu_q=people" || cancelled != 1 {
		t.Fatalf("submitted filter immediate=%q cancelled=%d", immediate, cancelled)
	}

	filter.OnInput("")
	if cancelled != 2 {
		t.Fatalf("returning to the rendered query did not cancel pending navigation: %d", cancelled)
	}
}

func TestFavoritesMoveLeavesToTheTopAndToggleWithoutLosingPageState(t *testing.T) {
	view := ApplyRequest(testView(PageWork), PageRequest{
		WorkFilter: "review", MenuQuery: "", FavoritePages: []PageID{PageHistory, PagePeople, PageHistory, "unknown"},
	})
	if got := view.FavoritePages; len(got) != 2 || got[0] != PageHistory || got[1] != PagePeople {
		t.Fatalf("authorized favorites = %v", got)
	}
	favorites, items := projectNavigation(view)
	if len(favorites) != 2 || favorites[0].Page != PageHistory || favorites[1].Page != PagePeople {
		t.Fatalf("favorite order = %+v", favorites)
	}
	work, ok := projectedNavigationItem(items, PageWork)
	if !ok || len(work.Children) != 1 || work.Children[0].Page != PageWork {
		t.Fatalf("favorited history was not moved out of My Work: %+v", work)
	}
	if _, ok := projectedNavigationItem(items, PagePeople); ok {
		t.Fatal("favorited People remained duplicated in all navigation")
	}
	href := favoriteToggleHref(view, PageOrganization)
	for _, want := range []string{"favorites=organization%2Chistory%2Cpeople", "filter=review"} {
		if !strings.Contains(href, want) {
			t.Fatalf("favorite toggle lost state %q in %s", want, href)
		}
	}
}

func TestRemovingTheLastFavoriteAndExpandingNavigationAreExplicit(t *testing.T) {
	view := ApplyRequest(testView(PagePeople), PageRequest{FavoritePages: []PageID{PagePeople}, NavCollapsed: true})
	if href := favoriteToggleHref(view, PagePeople); !strings.Contains(href, "favorites=") {
		t.Fatalf("last-favorite removal was indistinguishable from a missing server preference: %s", href)
	}
	if href := navigationToggleProps(view).Href; !strings.Contains(href, "nav=expanded") {
		t.Fatalf("expanded navigation was indistinguishable from a missing server preference: %s", href)
	}
}

func TestSidebarRendersAccessibleFilterFavoriteAndDisclosureControls(t *testing.T) {
	view := ApplyRequest(testView(PageHistory), PageRequest{FavoritePages: []PageID{PagePeople}})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="menu-filter"`, `aria-label="Filter navigation menu"`, `>Favorites</li>`,
		`aria-label="Remove People from favorites"`, `class="nav-group current"`, `open`,
		`data-hcm-nav-group="work"`, `>Work queue</span>`, `>Work History</span>`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("sidebar missing %q", want)
		}
	}
	if strings.Index(doc, `>Favorites</li>`) > strings.Index(doc, `>All navigation</li>`) {
		t.Fatal("favorites are not rendered before the ordinary menu")
	}
}

func TestCollapsedSidebarOmitsFilterAndUsesGroupDestination(t *testing.T) {
	view := ApplyRequest(testView(PageHistory), PageRequest{NavCollapsed: true, MenuQuery: "history"})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, `id="menu-filter"`) {
		t.Fatal("collapsed navigation retained a hidden focusable menu filter")
	}
	if !strings.Contains(doc, `href="/workspace/app/work?menu_q=history&amp;nav=collapsed"`) {
		t.Fatal("collapsed My Work group has no software-navigation destination")
	}
}

func TestFilteredNavigationForcesMatchingGroupsOpenWithoutChangingPreference(t *testing.T) {
	view := ApplyRequest(testView(PageHome), PageRequest{MenuQuery: "history"})
	view.NavigationGroupOpen = map[PageID]bool{PageWork: false}
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-hcm-nav-group="work"`, `data-hcm-nav-force-open="true"`, `open`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("filtered navigation group missing %q", want)
		}
	}
}

func TestSavedNavigationDisclosureOverridesContextualDefault(t *testing.T) {
	view := testView(PageHistory)
	view.NavigationGroupOpen = map[PageID]bool{PageWork: false, PageAdmin: true}
	_, items := projectNavigation(view)
	work, _ := projectedNavigationItem(items, PageWork)
	admin, _ := projectedNavigationItem(items, PageAdmin)
	if work.Expanded {
		t.Fatal("saved closed state did not override the active-route default")
	}
	if !admin.Expanded {
		t.Fatal("saved open state did not override the inactive-route default")
	}
}

func TestNavigationToggleIsGlyphOnlyBesideBrandAndControlsSidebar(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="brand-cluster"`, `class="header-nav-toggle"`,
		`aria-label="Collapse navigation"`, `aria-expanded="true"`,
		`aria-controls="workspace-navigation"`, `id="workspace-navigation"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("header navigation control missing %q", want)
		}
	}
	start := strings.Index(doc, `class="header-nav-toggle"`)
	if strings.Contains(doc, `class="sidebar-toggle"`) || start < 0 {
		t.Fatal("navigation toggle is not a glyph-only header control")
	}
	end := strings.Index(doc[start:], `</a>`)
	if end < 0 {
		t.Fatal("navigation toggle has no closing link")
	}
	bodyStart := strings.Index(doc[start:start+end], `>`)
	if bodyStart < 0 {
		t.Fatal("navigation toggle is not a glyph-only header control")
	}
	toggleBody := doc[start+bodyStart+1 : start+end]
	if !strings.Contains(toggleBody, `<svg`) || strings.Contains(toggleBody, `<span`) {
		t.Fatalf("navigation toggle body is not glyph-only: %s", toggleBody)
	}
	if strings.Index(doc, `class="header-nav-toggle"`) > strings.Index(doc, `id="workspace-navigation"`) {
		t.Fatal("navigation toggle is not colocated with the brand before the sidebar")
	}
}

func projectedNavigationItem(items []NavigationItemProps, page PageID) (NavigationItemProps, bool) {
	for _, item := range items {
		if item.Page == page {
			return item, true
		}
	}
	return NavigationItemProps{}, false
}
