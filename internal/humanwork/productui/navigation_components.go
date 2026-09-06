package productui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// NavigationSidebarProps is the complete presentation contract for the
// application navigation. Hierarchy, filtering, ordering, and preference
// links are resolved before rendering so this component owns no route rules.
type NavigationSidebarProps struct {
	I18nProps
	Collapsed bool
	Tenant    string
	Filter    MenuFilterProps
	Favorites []NavigationItemProps
	Items     []NavigationItemProps
	Support   []NavigationItemProps
	// Search reprojects the navigation catalog without loading page data.
	// Menu filtering is client-local; only its shareable URL state is debounced.
	Search func(string) NavigationSidebarProps
}

type NavigationToggleProps struct {
	Label    string
	Icon     string
	Href     string
	Navigate func(string)
}

// NavigationItemProps recursively describes a menu group or leaf.
type NavigationItemProps struct {
	I18nProps
	Page         PageID
	Label        string
	Icon         string
	Count        int
	Href         string
	Active       bool
	Expanded     bool
	ForceOpen    bool
	Favorite     bool
	FavoriteHref string
	MatchScore   int
	MatchDetail  string
	Children     []NavigationItemProps
	Navigate     func(string)
}

type MenuFilterProps struct {
	I18nProps
	Query     string
	Action    string
	ClearHref string
	Hidden    []MenuHiddenInput
	Navigate  func(string)
	OnInput   func(string)
	OnFilter  func(string)
}

type MenuHiddenInput struct {
	Name  string
	Value string
}

func navigationSidebarProps(view View) NavigationSidebarProps {
	props := navigationSidebarPropsForQuery(view)
	props.Search = func(query string) NavigationSidebarProps {
		next := view
		next.MenuQuery = strings.TrimSpace(query)
		return navigationSidebarPropsForQuery(next)
	}
	return props
}

func navigationSidebarPropsForQuery(view View) NavigationSidebarProps {
	favorites, items := projectNavigation(view)
	filterView := view
	filterView.MenuQuery = ""
	filter := MenuFilterProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Query:     view.MenuQuery, Action: pageHref(view.Page), ClearHref: currentPageHref(filterView, view.NavCollapsed),
		Hidden: menuFilterHiddenState(view), Navigate: view.Navigate,
	}
	if view.Navigate != nil {
		filterHref := func(query string) string {
			next := view
			next.MenuQuery = strings.TrimSpace(query)
			return currentPageHref(next, next.NavCollapsed)
		}
		filter.OnFilter = func(query string) {
			if view.CancelDebouncedNavigation != nil {
				view.CancelDebouncedNavigation()
			}
			view.Navigate(filterHref(query))
		}
		if view.NavigateDebounced != nil {
			filter.OnInput = func(query string) {
				if strings.TrimSpace(query) == view.MenuQuery {
					if view.CancelDebouncedNavigation != nil {
						view.CancelDebouncedNavigation()
					}
					return
				}
				view.NavigateDebounced(filterHref(query))
			}
		}
	}
	support := make([]NavigationItemProps, 0, 2)
	for _, page := range []PageID{PageHelp, PageSettings} {
		item, ok := navigationItemForPage(page, view.Locale)
		if !ok || navigationSearchScore(item, view.MenuQuery) == 0 {
			continue
		}
		props := navigationLeafProps(view, item, false)
		props.FavoriteHref = ""
		support = append(support, props)
	}
	sort.SliceStable(support, func(left, right int) bool { return support[left].MatchScore > support[right].MatchScore })
	return NavigationSidebarProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Collapsed: view.NavCollapsed, Tenant: view.Tenant, Filter: filter,
		Favorites: favorites, Items: items, Support: support,
	}
}

func navigationToggleProps(view View) NavigationToggleProps {
	toggle := NavigationToggleProps{
		Label: view.Locale.Text("nav.collapse"), Icon: "collapse", Href: currentPageHref(view, true), Navigate: view.Navigate,
	}
	if view.NavCollapsed {
		toggle.Label, toggle.Icon = view.Locale.Text("nav.expand"), "expand"
		toggle.Href = withExplicitQueryValue(currentPageHref(view, false), []string{"nav"}, "expanded")
	}
	return toggle
}

func navigationItemForPage(page PageID, locale LocaleContext) (NavItem, bool) {
	definition, ok := LookupPage(page)
	if !ok {
		return NavItem{}, false
	}
	return navigationItemFromDefinition(definition, locale), true
}

func menuFilterHiddenState(view View) []MenuHiddenInput {
	values := currentPageAddressState(view, view.NavCollapsed)
	values.Del("menu_q")
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]MenuHiddenInput, 0, len(names))
	for _, name := range names {
		result = append(result, MenuHiddenInput{Name: name, Value: values.Get(name)})
	}
	return result
}

func projectNavigation(view View) ([]NavigationItemProps, []NavigationItemProps) {
	favoriteSet := make(map[PageID]bool, len(view.FavoritePages))
	for _, page := range view.FavoritePages {
		favoriteSet[page] = true
	}
	leaves := make(map[PageID]NavItem)
	for _, item := range view.Navigation {
		collectNavigationLeaves(item, leaves)
	}
	favorites := make([]NavigationItemProps, 0, len(view.FavoritePages))
	for _, page := range view.FavoritePages {
		if leaf, ok := leaves[page]; ok && navigationSearchScore(leaf, view.MenuQuery) > 0 {
			favorites = append(favorites, navigationLeafProps(view, leaf, true))
		}
	}
	sort.SliceStable(favorites, func(left, right int) bool { return favorites[left].MatchScore > favorites[right].MatchScore })
	items := make([]NavigationItemProps, 0, len(view.Navigation))
	for _, item := range view.Navigation {
		if projected, ok := projectNavigationItem(view, item, favoriteSet); ok {
			items = append(items, projected)
		}
	}
	sort.SliceStable(items, func(left, right int) bool { return items[left].MatchScore > items[right].MatchScore })
	return favorites, items
}

func collectNavigationLeaves(item NavItem, into map[PageID]NavItem) {
	if len(item.Children) == 0 {
		into[item.Page] = item
		return
	}
	for _, child := range item.Children {
		collectNavigationLeaves(child, into)
	}
}

func projectNavigationItem(view View, item NavItem, favorites map[PageID]bool) (NavigationItemProps, bool) {
	if len(item.Children) == 0 {
		if favorites[item.Page] || navigationSearchScore(item, view.MenuQuery) == 0 {
			return NavigationItemProps{}, false
		}
		return navigationLeafProps(view, item, false), true
	}
	groupScore := navigationSearchScore(item, view.MenuQuery)
	children := make([]NavigationItemProps, 0, len(item.Children))
	for _, child := range item.Children {
		if favorites[child.Page] || navigationSearchScore(child, view.MenuQuery) == 0 {
			continue
		}
		children = append(children, navigationLeafProps(view, child, false))
	}
	sort.SliceStable(children, func(left, right int) bool { return children[left].MatchScore > children[right].MatchScore })
	if len(children) == 0 {
		return NavigationItemProps{}, false
	}
	active := navigationChildrenActive(children)
	expanded := active
	if saved, ok := view.NavigationGroupOpen[item.Page]; ok {
		expanded = saved
	}
	if view.MenuQuery != "" {
		expanded = true
	}
	return NavigationItemProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Page:      item.Page, Label: item.Label, Icon: item.Icon, Count: item.Count,
		Href: navigationHref(view, item.Page), Active: active, Expanded: expanded, ForceOpen: view.MenuQuery != "",
		MatchScore: maxNavigationScore(groupScore, children), Children: children, Navigate: view.Navigate,
	}, true
}

func navigationLeafProps(view View, item NavItem, favorite bool) NavigationItemProps {
	detail := ""
	if view.MenuQuery != "" {
		detail = item.Description
	}
	return NavigationItemProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Page:      item.Page, Label: item.Label, Icon: item.Icon, Count: item.Count,
		Href: navigationHref(view, item.Page), Active: navigationPageActive(item.Page, view.Page),
		Favorite: favorite, FavoriteHref: favoriteToggleHref(view, item.Page), MatchScore: navigationSearchScore(item, view.MenuQuery), MatchDetail: detail, Navigate: view.Navigate,
	}
}

func maxNavigationScore(groupScore int, children []NavigationItemProps) int {
	result := groupScore
	for _, child := range children {
		if child.MatchScore > result {
			result = child.MatchScore
		}
	}
	return result
}

func navigationPageActive(item, current PageID) bool {
	return item == current || item == PagePeople && current == PagePerson
}

func navigationChildrenActive(children []NavigationItemProps) bool {
	for _, child := range children {
		if child.Active {
			return true
		}
	}
	return false
}

func favoriteToggleHref(view View, page PageID) string {
	next := view
	next.FavoritePages = make([]PageID, 0, len(view.FavoritePages)+1)
	found := false
	for _, favorite := range view.FavoritePages {
		if favorite == page {
			found = true
			continue
		}
		next.FavoritePages = append(next.FavoritePages, favorite)
	}
	if !found {
		next.FavoritePages = append([]PageID{page}, next.FavoritePages...)
	}
	href := currentPageHref(next, next.NavCollapsed)
	if len(next.FavoritePages) == 0 {
		return withExplicitEmptyQuery(href, "favorites")
	}
	return href
}

// NavigationSidebar renders independently scrolling, searchable navigation.
func NavigationSidebar(props NavigationSidebarProps) ui.Node {
	query := ui.UseState(props.Filter.Query)
	propQuery := props.Filter.Query
	ui.UseEffectOf(func() func() {
		if query.Get() != propQuery {
			query.Set(propQuery)
		}
		return nil
	}, propQuery)
	if props.Search != nil {
		search := props.Search
		props = search(query.Get())
		props.Search = search
		filterInput := props.Filter.OnInput
		props.Filter.OnInput = func(next string) {
			query.Set(next)
			if filterInput != nil {
				filterInput(next)
			}
		}
	}
	class := "sidebar"
	if props.Collapsed {
		class += " collapsed"
	}
	children := []ui.Node{html.Div(html.Props{Class: "tenant"}, ui.Text(props.Tenant))}
	if !props.Collapsed {
		children = append(children, ui.CreateElement(MenuFilter, props.Filter))
	}
	menu := make([]ui.Node, 0, len(props.Favorites)+len(props.Items)+2)
	if len(props.Favorites) > 0 {
		menu = append(menu, html.Li(html.Props{Class: "nav-section-label"}, ui.Text(props.Text("nav.favorites"))))
		for _, item := range props.Favorites {
			menu = append(menu, ui.CreateElement(NavigationItem, item))
		}
		if len(props.Items) > 0 {
			menu = append(menu, html.Li(html.Props{Class: "nav-section-label"}, ui.Text(props.Text("nav.all"))))
		}
	}
	for _, item := range props.Items {
		if props.Collapsed && len(item.Children) > 0 {
			item.Children = nil
		}
		menu = append(menu, ui.CreateElement(NavigationItem, item))
	}
	if len(menu) == 0 && len(props.Support) == 0 {
		menu = append(menu, html.Li(html.Props{Class: "nav-empty", Raw: map[string]any{"role": "status"}}, ui.Text(props.Text("nav.none"))))
	}
	children = append(children, html.Nav(html.Props{Class: "primary-nav", Aria: map[string]string{"label": props.Text("nav.main")}}, html.Ul(html.Props{}, menu...)))
	if len(props.Support) > 0 {
		support := make([]ui.Node, 0, len(props.Support))
		for _, item := range props.Support {
			support = append(support, ui.CreateElement(NavigationItem, item))
		}
		children = append(children, html.Nav(html.Props{Class: "nav-bottom", Aria: map[string]string{"label": props.Text("nav.support")}}, support...))
	}
	return html.Aside(html.Props{ID: "workspace-navigation", Class: class, Aria: map[string]string{"label": props.Text("nav.workspace")}}, children...)
}

// NavigationItem renders either a leaf with its favorite control or a native,
// keyboard-operable disclosure group.
func NavigationItem(props NavigationItemProps) ui.Node {
	if len(props.Children) == 0 {
		linkProps := html.Props{Class: "nav-link", Aria: map[string]string{"label": props.Label}, Raw: map[string]any{"title": props.Label}}
		if props.Active {
			linkProps.Aria["current"] = "page"
		}
		copy := []ui.Node{html.Span(html.Props{Class: "nav-label"}, ui.Text(props.Label))}
		class := "nav-link"
		if props.MatchDetail != "" {
			class += " has-search-detail"
			copy = append(copy, html.Small(html.Props{Class: "nav-search-detail"}, ui.Text(props.MatchDetail)))
		}
		linkProps.Class = class
		content := []ui.Node{navIcon(props.Icon), html.Span(html.Props{Class: "nav-copy"}, copy...)}
		if props.Count > 0 {
			content = append(content, html.Span(html.Props{Class: "nav-count"}, ui.Text(fmt.Sprint(props.Count))))
		}
		children := []ui.Node{softwareLink(props.Navigate, linkProps, props.Href, content...)}
		if props.FavoriteHref != "" {
			label, glyph := props.Text("nav.favorite_add", map[string]string{"label": props.Label}), "☆"
			if props.Favorite {
				label, glyph = props.Text("nav.favorite_remove", map[string]string{"label": props.Label}), "★"
			}
			children = append(children, softwareLink(props.Navigate, html.Props{Class: "nav-favorite", Aria: map[string]string{"label": label}, Raw: map[string]any{"title": label}}, props.FavoriteHref, ui.Text(glyph)))
		}
		return html.Li(html.Props{Class: "nav-entry"}, children...)
	}
	class := "nav-group"
	if props.Active {
		class += " current"
	}
	raw := map[string]any{}
	if props.Expanded {
		raw["open"] = true
	}
	data := map[string]string{"hcm-nav-group": string(props.Page)}
	if props.ForceOpen {
		data["hcm-nav-force-open"] = "true"
	}
	children := make([]ui.Node, 0, len(props.Children))
	for _, child := range props.Children {
		children = append(children, ui.CreateElement(NavigationItem, child))
	}
	summary := []ui.Node{navIcon(props.Icon), html.Span(html.Props{Class: "nav-label"}, ui.Text(props.Label))}
	if props.Count > 0 {
		summary = append(summary, html.Span(html.Props{Class: "nav-count"}, ui.Text(fmt.Sprint(props.Count))))
	}
	summary = append(summary, html.Span(html.Props{Class: "nav-chevron", Aria: map[string]string{"hidden": "true"}}, ui.Text("›")))
	return html.Li(html.Props{}, html.Details(html.Props{Class: class, Raw: raw, Data: data},
		html.Summary(html.Props{Class: "nav-group-summary"}, summary...),
		html.Ul(html.Props{Class: "subnav"}, children...),
	))
}

// MenuFilter is an SSR-safe GET form upgraded to software navigation in WASM.
func MenuFilter(props MenuFilterProps) ui.Node {
	query := props.Query
	inputProps := html.Props{ID: "menu-filter", Name: "menu_q", Value: props.Query,
		Raw: map[string]any{"type": "search", "placeholder": props.Text("nav.filter_placeholder"), "aria-label": props.Text("nav.filter")}}
	formProps := html.Props{Class: "menu-filter", Action: props.Action, Method: "get", Raw: map[string]any{"role": "search"}}
	if props.OnFilter != nil || props.OnInput != nil {
		inputProps.OnInput = ui.UseEvent(func(event ui.InputEvent) {
			query = event.GetValue()
			if props.OnInput != nil {
				props.OnInput(query)
			}
		})
	}
	if props.OnFilter != nil {
		onFilter := props.OnFilter
		formProps.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			onFilter(query)
		})
	}
	children := []ui.Node{
		html.Label(html.Props{For: "menu-filter", Class: "sr-only"}, ui.Text(props.Text("nav.filter"))),
		html.Div(html.Props{Class: "menu-filter-control"}, html.Tag("input", inputProps),
			html.Button(html.Props{Class: "menu-filter-submit", Type: "submit", Aria: map[string]string{"label": props.Text("nav.filter_apply")}, Raw: map[string]any{"title": props.Text("nav.filter_apply")}}, ui.Text("⌕"))),
	}
	for _, hidden := range props.Hidden {
		children = append(children, html.Tag("input", html.Props{Name: hidden.Name, Value: hidden.Value, Raw: map[string]any{"type": "hidden"}}))
	}
	if props.Query != "" {
		children = append(children, softwareLink(props.Navigate, html.Props{Class: "menu-filter-clear"}, props.ClearHref, ui.Text(props.Text("nav.filter_clear"))))
	}
	return html.Form(formProps, children...)
}
