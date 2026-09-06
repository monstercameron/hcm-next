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
	content := page
	if view.LoadError != "" {
		content = html.Div(html.Props{Class: "page-stack"},
			unavailablePanel(view.Locale.Text("shell.live_unavailable"), view.LoadError),
			page,
		)
	}
	return html.Div(html.Props{Class: class},
		html.A(html.Props{Class: "skip-link", Href: "#main-content"}, ui.Text(view.Locale.Text("shell.skip_main"))),
		appHeader(view),
		html.Div(html.Props{Class: "shell-grid"}, primarySidebar(view), pageFrame(view, content, showHeading)),
	)
}

func appHeader(view View) ui.Node {
	appearance := NormalizeCustomerTheme(view.Appearance)
	return html.Header(html.Props{Class: "topbar"},
		appLink(view, html.Props{Class: "wordmark", Title: appearance.BrandName}, navigationHref(view, PageHome),
			html.Span(html.Props{Class: "wordmark-mark", Aria: map[string]string{"hidden": "true"}, Data: map[string]string{"hcm-brand-mark": ""}}, ui.Text(appearance.BrandMark)),
			html.Span(html.Props{Class: "wordmark-label", Data: map[string]string{"hcm-brand-name": ""}}, ui.Text(appearance.BrandName)),
		),
		globalSearch(view),
		localeMenu(view),
		notificationMenu(view),
		avatar(uicomponents.Initials(view.Principal), ""),
	)
}

func globalSearch(view View) ui.Node {
	children := []ui.Node{
		html.Tag("input", html.Props{Name: "q", Value: view.Query, Raw: map[string]any{"type": "search", "placeholder": view.Locale.Text("shell.search_employees"), "aria-label": view.Locale.Text("shell.search_employees")}}),
	}
	if locale := view.Locale.normalized(); locale.Resolved != DefaultProductLocale {
		children = append(children, html.Tag("input", html.Props{Name: "locale", Value: locale.Resolved, Raw: map[string]any{"type": "hidden"}}))
	}
	if view.NavCollapsed {
		children = append(children, html.Tag("input", html.Props{Name: "nav", Value: "collapsed", Raw: map[string]any{"type": "hidden"}}))
	}
	return html.Form(html.Props{Class: "global-search", Action: pageHref(PagePeople), Method: "get", Raw: map[string]any{"role": "search"}}, children...)
}

func notificationMenu(view View) ui.Node {
	label := view.Locale.Text("shell.work_overview") + ", " + view.Locale.Plural("shell.work_count", int64(len(view.Work)))
	return html.Details(html.Props{Class: "notifications"},
		html.Summary(html.Props{Aria: map[string]string{"label": label}}, ui.Text(view.Locale.Text("shell.work_overview"))),
		html.Div(html.Props{Class: "popover"},
			html.H2(html.Props{}, ui.Text(view.Locale.Text("shell.work_overview"))),
			html.P(html.Props{}, ui.Text(view.Locale.Plural("shell.work_count", int64(len(view.Work))))),
			appLink(view, html.Props{}, statefulHref(view, PageWork), ui.Text(view.Locale.Text("shell.open_work"))),
		),
	)
}

func localeMenu(view View) ui.Node {
	locale := view.Locale.normalized()
	items := make([]ui.Node, 0, len(SupportedProductLocales()))
	for _, candidate := range SupportedProductLocales() {
		candidateView := view
		candidateView.Locale = ResolveProductLocale(candidate)
		labelKey := map[string]string{"en-US": "shell.locale_en", "de-DE": "shell.locale_de", "ar": "shell.locale_ar"}[candidate]
		props := html.Props{Class: "locale-option"}
		if candidate == locale.Resolved {
			props.Aria = map[string]string{"current": "true"}
		}
		items = append(items, appLink(view, props, currentPageHref(candidateView, view.NavCollapsed), ui.Text(candidateView.Locale.Text(labelKey))))
	}
	return html.Details(html.Props{Class: "locale-menu"},
		html.Summary(html.Props{Aria: map[string]string{"label": locale.Text("shell.locale")}, Raw: map[string]any{"title": locale.Text("shell.locale")}}, ui.Text(strings.ToUpper(strings.Split(locale.Resolved, "-")[0]))),
		html.Div(html.Props{Class: "popover locale-popover"}, items...),
	)
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
		if view.PeoplePage > 1 {
			values.Set("page", fmt.Sprint(view.PeoplePage))
		}
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
	if showHeading {
		children = append(children, pageHeader(view))
	}
	children = append(children, page,
		html.Footer(html.Props{Class: "footer"},
			html.Span(html.Props{Data: map[string]string{"hcm-brand-name": ""}}, ui.Text(NormalizeCustomerTheme(view.Appearance).BrandName+" · GoWebComponents")),
			html.Span(html.Props{}, ui.Text(view.Locale.Text("shell.live_source", map[string]string{"source": source}))),
		),
	)
	return html.Main(html.Props{ID: "main-content", Class: "main-scroll"},
		html.Div(html.Props{Class: "main"}, children...),
	)
}

func pageHeader(view View) ui.Node {
	scope := view.Scope
	if scope == "" {
		scope = view.Locale.Text("shell.authenticated_scope")
	}
	return html.Div(html.Props{Class: "page-head"},
		html.Div(html.Props{}, html.H1(html.Props{}, ui.Text(view.Title)), html.P(html.Props{Class: "subtitle"}, ui.Text(view.Subtitle))),
		html.Div(html.Props{Class: "scope-wrap"},
			appLink(view, html.Props{Class: "scope"}, statefulHref(view, PageSettings), ui.Text(scope+" ⌄")),
			html.Span(html.Props{}, ui.Text(view.Locale.Text("shell.acting_self"))),
		),
	)
}
