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
	if view.LoadError != "" {
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
		html.Div(html.Props{Class: "shell-grid"}, primarySidebar(view), pageFrame(view, content, showHeading)),
	)
}

func appHeader(view View) ui.Node {
	appearance := NormalizeCustomerTheme(view.Appearance)
	toggle := navigationToggleProps(view)
	return html.Header(html.Props{Class: "topbar"},
		html.Div(html.Props{Class: "brand-cluster"},
			appLink(view, html.Props{Class: "wordmark", Title: appearance.BrandName, Data: map[string]string{"hcm-brand-link": ""}}, navigationHref(view, PageHome),
				ui.CreateElement(BrandLogo, BrandLogoProps{Name: appearance.BrandName, Mark: appearance.BrandMark, LogoURL: appearance.BrandLogoURL}),
			),
			softwareLink(toggle.Navigate, html.Props{
				Class: "header-nav-toggle",
				Aria:  map[string]string{"label": toggle.Label, "expanded": fmt.Sprint(!view.NavCollapsed), "controls": "workspace-navigation"},
				Raw:   map[string]any{"title": toggle.Label},
			}, toggle.Href, navIcon(toggle.Icon)),
		),
		html.Div(html.Props{Class: "header-navigation-tools"},
			contextSwitcherSlot(view),
			ui.CreateElement(HistoryNavigation, historyNavigationProps(view)),
			globalSearch(view),
		),
		localeMenu(view),
		notificationSlot(view),
		viewerProfileLink(view),
	)
}

func contextSwitcherSlot(view View) ui.Node {
	props := view.ContextSwitcher
	if !contextSwitcherVisible(props) {
		return html.Fragment()
	}
	return ui.CreateElement(ContextSwitcher, props)
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
	return appLink(view, html.Props{
		Class: "viewer-profile-link network-slot network-slot-ready", Title: label,
		Aria: map[string]string{"label": label},
	}, statefulHref(view, PageMyself), personAvatar(profile.Name, profile.Initials, profile.PhotoURL, "viewer"))
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
	return ui.CreateElement(GlobalSearch, globalSearchProps(view))
}

func notificationMenu(view View) ui.Node {
	open := len(OpenWorkItems(view.Work))
	label := view.Locale.Text("shell.work_overview") + ", " + view.Locale.Plural("shell.work_count", int64(open))
	children := []ui.Node{
		html.H2(html.Props{}, ui.Text(view.Locale.Text("shell.work_overview"))),
		html.P(html.Props{}, ui.Text(view.Locale.Plural("shell.work_count", int64(open)))),
	}
	if view.Allows(PageWork, "view") {
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
		children = append(children, pageHeader(view))
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

func pageHeader(view View) ui.Node {
	scope := view.Scope
	if scope == "" {
		scope = view.Locale.Text("shell.authenticated_scope")
	}
	return html.Div(html.Props{Class: "page-head"},
		html.Div(html.Props{}, html.H1(html.Props{ID: "page-title", Raw: map[string]any{"tabindex": "-1"}}, ui.Text(view.Title)), html.P(html.Props{Class: "subtitle"}, ui.Text(view.Subtitle))),
		html.Div(html.Props{Class: "scope-wrap"},
			appLink(view, html.Props{Class: "scope"}, statefulHref(view, PageSettings), ui.Text(scope+" ⌄")),
			html.Span(html.Props{}, ui.Text(view.Locale.Text("shell.acting_self"))),
		),
	)
}
