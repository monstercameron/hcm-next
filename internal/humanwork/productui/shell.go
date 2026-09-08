package productui

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/hcm-next/internal/humanwork/uicomponents"
)

func appShell(view View, page ui.Node) ui.Node {
	return appShellWithHeading(view, page, true)
}

func appShellWithHeading(view View, page ui.Node, showHeading bool) ui.Node {
	class := "app-shell"
	if view.NavCollapsed {
		class += " nav-collapsed"
	}
	if view.Loading {
		class += " is-loading"
	} else if view.ContentLoading {
		class += " is-content-loading"
	} else if view.Refreshing {
		class += " is-refreshing"
	}
	content := page
	if signedOutState(view) {
		content = signedOut(view)
		showHeading = false
	} else if strings.TrimSpace(view.Tenant) == "" {
		content = FederationEntryList(federationEntryProps(view))
		showHeading = false
	} else if view.LoadError != "" {
		content = html.Div(html.Props{Class: "page-stack"},
			unavailablePanel(view.Locale.Text("shell.live_unavailable"), view.LoadError),
			page,
		)
	}
	announcement := view.Locale.Text("shell.page_loaded", map[string]string{"title": view.Title})
	if view.Loading || view.ContentLoading || view.Refreshing {
		announcement = view.Locale.Text("shell.loading_authorized")
	}
	return html.Div(html.Props{Class: class},
		html.A(html.Props{Class: "skip-link", Href: "#main-content"}, ui.Text(view.Locale.Text("shell.skip_main"))),
		html.Div(html.Props{Class: "sr-only route-announcer", Raw: map[string]any{"role": "status", "aria-live": "polite", "aria-atomic": "true"}}, ui.Text(announcement)),
		appHeader(view),
		sessionWarning(view),
		stepUpChallenge(view),
		actingAuthorityBanner(view),
		breakGlassActivation(view),
		policySimulation(view),
		html.Div(html.Props{Class: "shell-grid"}, primarySidebar(view), pageFrame(view, content, showHeading)),
	)
}

func appHeader(view View) ui.Node {
	appearance := NormalizeCustomerTheme(view.Appearance)
	toggle := navigationToggleProps(view)
	brandProps := html.Props{Class: "wordmark", Title: appearance.BrandName, Data: map[string]string{"hcm-brand-link": ""}}
	brandContent := ui.CreateElement(BrandLogo, BrandLogoProps{Name: appearance.BrandName, Mark: appearance.BrandMark, LogoURL: appearance.BrandLogoURL})
	var brand ui.Node = html.Div(brandProps, brandContent)
	if navigationDestinationAuthorized(view, PageHome) {
		brand = appLink(view, brandProps, navigationHref(view, PageHome), brandContent)
	}
	return html.Header(html.Props{Class: "topbar"},
		html.Div(html.Props{Class: "brand-cluster"},
			brand,
			softwareLink(toggle.Navigate, html.Props{
				Class: "header-nav-toggle",
				Aria:  map[string]string{"label": toggle.Label, "expanded": fmt.Sprint(!view.NavCollapsed), "controls": "workspace-navigation"},
				Raw:   map[string]any{"title": toggle.Label},
			}, toggle.Href, navIcon(toggle.Icon)),
		),
		html.Div(html.Props{Class: "header-navigation-tools"},
			contextSwitcherSlot(view),
			delegationSelectorSlot(view),
			ui.CreateElement(HistoryNavigation, historyNavigationProps(view)),
			globalSearch(view),
			actionLauncher(view),
			utilityDrawer(view),
		),
		localeMenu(view),
		notificationSlot(view),
		viewerProfileLink(view),
	)
}

func contextSwitcherSlot(view View) ui.Node {
	if signedOutState(view) {
		return html.Fragment()
	}
	props := view.ContextSwitcher
	if !contextSwitcherVisible(props) {
		return html.Fragment()
	}
	return ui.CreateElement(ContextSwitcher, props)
}

func delegationSelectorSlot(view View) ui.Node {
	if signedOutState(view) {
		return html.Fragment()
	}
	props := view.ContextSwitcher
	if len(delegationSelectorOptions(props)) == 0 {
		return html.Fragment()
	}
	return ui.CreateElement(DelegationSelector, props)
}

func historyNavigationProps(view View) HistoryNavigationProps {
	props := view.HistoryNavigation
	props.I18nProps = I18nProps{Locale: view.Locale}
	return props
}

func viewerProfileLink(view View) ui.Node {
	if view.Loading {
		return html.Div(html.Props{
			Class: "viewer-profile-link viewer-profile-loading network-slot network-slot-pending",
			Raw:   map[string]any{"aria-hidden": "true"},
		}, html.Span(html.Props{Class: "loading-block loading-viewer-profile"}))
	}
	profile := view.Viewer
	if strings.TrimSpace(profile.Name) == "" {
		profile.Name = view.Principal
	}
	if strings.TrimSpace(profile.Initials) == "" {
		profile.Initials = uicomponents.Initials(profile.Name)
	}
	label := view.Locale.Text("shell.myself", map[string]string{"name": profile.Name})
	props := html.Props{
		Class: "viewer-profile-link network-slot network-slot-ready", Title: label,
		Aria: map[string]string{"label": label},
	}
	avatar := personAvatar(profile.Name, profile.Initials, profile.PhotoURL, "viewer")
	if !navigationDestinationAuthorized(view, PageMyself) {
		return html.Div(props, avatar)
	}
	return appLink(view, props, statefulHref(view, PageMyself), avatar)
}

func notificationSlot(view View) ui.Node {
	if view.Loading {
		return html.Div(html.Props{Class: "notifications notification-loading network-slot network-slot-pending", Raw: map[string]any{"aria-hidden": "true"}},
			html.Span(html.Props{Class: "loading-block loading-notification"}),
		)
	}
	return notificationMenu(view)
}

func globalSearch(view View) ui.Node {
	props := globalSearchProps(view)
	if view.NavigationProjection != nil {
		props.Items = authorizedGlobalSearchItems(view, props.Items)
		props.FallbackHref = authorizedGlobalSearchFallback(view)
		if favorites := authorizedFavoritePages(view.Navigation, view.FavoritePages); len(favorites) == 0 {
			delete(props.HiddenInputs, "favorites")
		} else {
			values := make([]string, 0, len(favorites))
			for _, page := range favorites {
				values = append(values, string(page))
			}
			props.HiddenInputs["favorites"] = strings.Join(values, ",")
		}
	}
	return ui.CreateElement(GlobalSearch, props)
}

func actionLauncher(view View) ui.Node {
	props := actionLauncherProps(view)
	props.Items = authorizedActionLauncherItems(view, props.Items)
	return ui.CreateElement(ActionLauncher, props)
}

// authorizedActionLauncherItems intersects the allowed starts with the pages
// the authorized navigation (projection or legacy) admits, so the launcher
// never advertises a route the shell itself omits.
func authorizedActionLauncherItems(view View, items []ActionLauncherItem) []ActionLauncherItem {
	allowed := authorizedNavigationPages(view)
	result := make([]ActionLauncherItem, 0, len(items))
	for _, item := range items {
		if allowed[item.Page] {
			result = append(result, item)
		}
	}
	return result
}

func authorizedGlobalSearchItems(view View, items []GlobalSearchItem) []GlobalSearchItem {
	allowed := authorizedNavigationPages(view)
	if allowed[PagePeople] {
		// Person results come only from the already-filtered People projection.
		// This enables a destination; it does not authorize its independent RPC.
		allowed[PagePerson] = true
	}
	result := make([]GlobalSearchItem, 0, len(items))
	for _, item := range items {
		parsed, err := url.Parse(item.Href)
		if err != nil || parsed.IsAbs() || parsed.Host != "" {
			continue
		}
		definition, ok := LookupRoute(parsed.Path)
		if ok && allowed[definition.ID] {
			result = append(result, item)
		}
	}
	return result
}

func authorizedGlobalSearchFallback(view View) string {
	for _, page := range []PageID{PagePeople, PageHome, view.Page} {
		if navigationDestinationAuthorized(view, page) {
			return statefulHref(view, page)
		}
	}
	return "#"
}

// navigationDestinationAuthorized answers only whether the resolver chose to
// advertise a destination. It is deliberately not action or route authority;
// every destination service still authenticates and authorizes its own read.
func navigationDestinationAuthorized(view View, page PageID) bool {
	if view.NavigationProjection == nil {
		return view.Allows(page, "view")
	}
	if err := validateAuthorizedNavigationProjection(*view.NavigationProjection); err != nil {
		return false
	}
	return authorizedNavigationPages(view)[page]
}

func authorizedNavigationPages(view View) map[PageID]bool {
	result := make(map[PageID]bool)
	if view.NavigationProjection != nil {
		if err := validateAuthorizedNavigationProjection(*view.NavigationProjection); err != nil {
			return result
		}
		var collect func([]AuthorizedNavigationItem)
		collect = func(items []AuthorizedNavigationItem) {
			for _, item := range items {
				result[item.Page] = true
				collect(item.Children)
			}
		}
		collect(view.NavigationProjection.Items)
		collect(view.NavigationProjection.Support)
		return result
	}
	var collect func([]NavItem)
	collect = func(items []NavItem) {
		for _, item := range items {
			result[item.Page] = true
			collect(item.Children)
		}
	}
	collect(view.Navigation)
	collect(view.NavigationSupport)
	return result
}

func notificationMenu(view View) ui.Node {
	open := len(OpenWorkItems(view.Work))
	label := view.Locale.Text("shell.work_overview") + ", " + view.Locale.Plural("shell.work_count", int64(open))
	children := []ui.Node{
		html.H2(html.Props{}, ui.Text(view.Locale.Text("shell.work_overview"))),
		html.P(html.Props{}, ui.Text(view.Locale.Plural("shell.work_count", int64(open)))),
	}
	if navigationDestinationAuthorized(view, PageWork) {
		children = append(children, appLink(view, html.Props{}, statefulHref(view, PageWork), ui.Text(view.Locale.Text("shell.open_work"))))
	}
	return ui.CreateElement(TransientPopover, TransientPopoverProps{
		Kind: "notification", Class: "notifications network-slot network-slot-ready", Label: label,
		Trigger: []ui.Node{navIcon("notifications")}, PanelClass: "popover notification-popover",
		Children: children,
	})
}

func localeMenu(view View) ui.Node {
	locale := view.Locale.normalized()
	localePreferences := localePreferencesProps(view)
	items := make([]ui.Node, 0, len(localePreferences.Options))
	for _, option := range localePreferences.Options {
		props := html.Props{Class: "locale-option"}
		if option.Current {
			props.Aria = map[string]string{"current": "true"}
		}
		items = append(items, softwareLink(option.Navigate, props, option.Href, ui.Text(option.Label)))
	}
	label := locale.Text("shell.locale")
	return ui.CreateElement(TransientPopover, TransientPopoverProps{
		Kind: "locale", Class: "locale-menu", Label: label, Title: label,
		Trigger:    []ui.Node{ui.Text(strings.ToUpper(strings.Split(locale.Resolved, "-")[0]))},
		PanelClass: "popover locale-popover", Children: items,
	})
}

func primarySidebar(view View) ui.Node {
	return ui.CreateElement(NavigationSidebar, navigationSidebarProps(view))
}

func navigationHref(view View, page PageID) string {
	return statefulHref(view, page)
}

func navigationHrefForItem(view View, item NavItem) string {
	if strings.TrimSpace(item.Href) == "" {
		return navigationHref(view, item.Page)
	}
	return statefulHrefAtRoute(view, item.Href)
}

func statefulHrefAtRoute(view View, route string) string {
	route = strings.TrimSpace(route)
	if route == "" {
		return pageHref(PageHome)
	}
	parsed, err := url.Parse(route)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/workspace/app/") {
		return pageHref(PageHome)
	}
	values := parsed.Query()
	setMenuAddressState(values, view)
	parsed.RawQuery = values.Encode()
	return parsed.String()
}

func currentPageHref(view View, collapsed bool) string {
	values := currentPageAddressState(view, collapsed)
	href := pageHref(view.Page)
	if query := values.Encode(); query != "" {
		return href + "?" + query
	}
	return href
}

func currentPageAddressState(view View, collapsed bool) url.Values {
	values := url.Values{}
	if collapsed {
		values.Set("nav", "collapsed")
	}
	setMenuAddressState(values, view)
	switch view.Page {
	case PageJourneys:
		if view.JourneyID != "" {
			values.Set("journey", view.JourneyID)
		}
		if view.JourneyMode != "" {
			values.Set("mode", view.JourneyMode)
		}
		if view.JourneyWorker != "" {
			values.Set("worker", view.JourneyWorker)
		}
	case PageStudio:
		if view.Mode != "" {
			values.Set("mode", view.Mode)
		}
	case PagePeople:
		if view.Query != "" {
			values.Set("q", view.Query)
		}
		setPeopleDirectoryAddressState(values, view)
		if view.PeoplePage > 1 {
			values.Set("page", fmt.Sprint(view.PeoplePage))
		}
	case PagePerson:
		if view.SelectedPerson != "" {
			values.Set("person", view.SelectedPerson)
		}
		if view.Query != "" {
			values.Set("q", view.Query)
		}
		setPeopleDirectoryAddressState(values, view)
		if view.PeoplePage > 1 {
			values.Set("page", fmt.Sprint(view.PeoplePage))
		}
		if view.WorkflowQuery != "" {
			values.Set("workflow_q", view.WorkflowQuery)
		}
		setHistoryAddressState(values, view)
	case PageMyself:
		if view.WorkflowQuery != "" {
			values.Set("workflow_q", view.WorkflowQuery)
		}
		setHistoryAddressState(values, view)
	case PageWork:
		if view.WorkFilter != "" {
			values.Set("filter", view.WorkFilter)
		}
		if view.SelectedWork != "" {
			values.Set("selected", view.SelectedWork)
		}
	case PageHistory:
		setHistoryAddressState(values, view)
	}
	return values
}

func setPeopleDirectoryAddressState(values url.Values, view View) {
	if view.PeopleTeam != "" {
		values.Set("team", view.PeopleTeam)
	}
	if view.PeopleLocation != "" {
		values.Set("location", view.PeopleLocation)
	}
	if view.PeopleSort != "" && view.PeopleSort != peopleSortName {
		values.Set("sort", view.PeopleSort)
	}
	if view.PeopleDirection == peopleSortDescending {
		values.Set("dir", view.PeopleDirection)
	}
}

func setHistoryAddressState(values url.Values, view View) {
	if view.HistoryQuery != "" {
		values.Set("history_q", view.HistoryQuery)
	}
	if view.HistoryOutcome != "" {
		values.Set("outcome", view.HistoryOutcome)
	}
	if view.HistoryPerson != "" {
		values.Set("history_person", view.HistoryPerson)
	}
	if view.HistoryYear != "" {
		values.Set("history_year", view.HistoryYear)
	}
	if view.HistorySort != "" {
		values.Set("history_sort", view.HistorySort)
	}
	if view.HistoryDirection != "" {
		values.Set("history_dir", view.HistoryDirection)
	}
}

func pageFrame(view View, page ui.Node, showHeading bool) ui.Node {
	source := view.Source
	if source == "" {
		source = view.Locale.Text("shell.no_source")
	}
	children := make([]ui.Node, 0, 3)
	if view.Refreshing {
		children = append(children, html.Div(html.Props{
			Class: "loading-progress network-progress",
			Raw:   map[string]any{"aria-hidden": "true"},
		}))
	}
	if showHeading {
		children = append(children, PageIdentityHeader(view))
	}
	children = append(children, page,
		html.Footer(html.Props{Class: "footer"},
			html.Span(html.Props{Data: map[string]string{"hcm-brand-name": ""}}, ui.Text(NormalizeCustomerTheme(view.Appearance).BrandName+" · GoWebComponents")),
			html.Span(html.Props{}, ui.Text(view.Locale.Text("shell.live_source", map[string]string{"source": source}))),
		),
	)
	mainProps := html.Props{ID: "main-content", Class: "main-scroll", Aria: map[string]string{}}
	if showHeading {
		mainProps.Aria["labelledby"] = "page-title"
	}
	if view.Loading || view.ContentLoading || view.Refreshing {
		mainProps.Aria["busy"] = "true"
	}
	stageClass := "main network-stage network-stage-ready"
	stage := "ready"
	if view.Loading || view.ContentLoading {
		stageClass = "main network-stage network-stage-pending"
		stage = "pending"
	} else if view.Refreshing {
		stageClass = "main network-stage network-stage-refreshing"
		stage = "refreshing"
	}
	return html.Main(mainProps,
		html.Div(html.Props{Class: stageClass, Data: map[string]string{"network-state": stage}}, children...),
	)
}
