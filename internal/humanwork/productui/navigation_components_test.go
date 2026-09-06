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
	if !ok || len(admin.Children) != 3 || admin.Children[1].Page != PageAppearance || admin.Children[2].Page != PageStudio {
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

func TestSidebarRendersAccessibleFilterFavoriteAndDisclosureControls(t *testing.T) {
	view := ApplyRequest(testView(PageHistory), PageRequest{FavoritePages: []PageID{PagePeople}})
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="menu-filter"`, `aria-label="Filter navigation menu"`, `>Favorites</li>`,
		`aria-label="Remove People from favorites"`, `class="nav-group current"`, `open`,
		`>Work queue</span>`, `>Work History</span>`,
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

func projectedNavigationItem(items []NavigationItemProps, page PageID) (NavigationItemProps, bool) {
	for _, item := range items {
		if item.Page == page {
			return item, true
		}
	}
	return NavigationItemProps{}, false
}
