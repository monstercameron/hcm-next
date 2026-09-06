package productui

import (
	"fmt"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// LoadingProxyProps describes the shape of an unresolved product surface.
// The proxy contains no guessed business values and is deliberately
// non-interactive; the surrounding shell owns the accessible busy message.
type LoadingProxyProps struct {
	Page PageID
}

// BuildLoading returns the real product shell with a component-shaped proxy
// in place of database and network-backed content. The router swaps this tree
// atomically for Build(view) when every required answer has resolved.
func BuildLoading(view View) ui.Node {
	view.Loading = true
	view.Refreshing = false
	return appShell(view, ui.CreateElement(LoadingProxy, LoadingProxyProps{Page: view.Page}))
}

// BuildRefreshing keeps the last authorized page tree visible while a newer
// projection is in flight. The shell marks the content busy and supplies a
// progress cue; it never fabricates pending values or changes action authority.
func BuildRefreshing(view View) ui.Node {
	view.Loading = false
	if view.RefreshingRegion != "" {
		view.Refreshing = false
		return Build(view)
	}
	view.Refreshing = true
	return Build(view)
}

// LoadingProxy preserves the broad geometry of each page family, avoiding
// layout jumps without drawing fake names, amounts, statuses, or permissions.
func LoadingProxy(props LoadingProxyProps) ui.Node {
	class := "loading-proxy loading-proxy-" + safeLoadingPageClass(props.Page)
	return html.Section(html.Props{
		Class: class,
		Raw:   map[string]any{"aria-hidden": "true"},
	},
		html.Div(html.Props{Class: "loading-progress"}),
		loadingProxyBody(props.Page),
	)
}

func loadingProxyBody(page PageID) ui.Node {
	switch page {
	case PagePeople, PageHistory:
		return html.Div(html.Props{Class: "loading-table-layout"},
			loadingToolbar(),
			loadingPanel("loading-table-panel", loadingTable(7)),
		)
	case PagePerson, PageMyself:
		return html.Div(html.Props{Class: "loading-profile-layout"},
			loadingPanel("loading-profile-hero", html.Div(html.Props{Class: "loading-profile-head"},
				loadingBlock("loading-avatar"),
				html.Div(html.Props{Class: "loading-copy"}, loadingBlock("loading-line loading-line-title"), loadingBlock("loading-line loading-line-short")),
			)),
			html.Div(html.Props{Class: "loading-two-column"},
				loadingPanel("", loadingFacts(6)),
				loadingPanel("", loadingRows(4, false)),
			),
		)
	case PageOrganization, PageInsights:
		return html.Div(html.Props{Class: "loading-analysis-layout"},
			loadingMetrics(3),
			html.Div(html.Props{Class: "loading-two-column"},
				loadingPanel("loading-chart-panel", loadingBars(5)),
				loadingPanel("", loadingRows(5, true)),
			),
		)
	case PageHome, PageWork, PageJourneys:
		return html.Div(html.Props{Class: "loading-work-layout"},
			loadingToolbar(),
			html.Div(html.Props{Class: "loading-two-column"},
				loadingPanel("", loadingRows(6, true)),
				loadingPanel("loading-detail-proxy", loadingFacts(6)),
			),
		)
	default:
		return html.Div(html.Props{Class: "loading-settings-layout"},
			html.Div(html.Props{Class: "loading-two-column"},
				loadingPanel("", loadingRows(6, false)),
				loadingPanel("", loadingFacts(5)),
			),
		)
	}
}

func loadingToolbar() ui.Node {
	return html.Div(html.Props{Class: "loading-toolbar"},
		loadingBlock("loading-line loading-line-medium"),
		loadingBlock("loading-control"),
	)
}

func loadingMetrics(count int) ui.Node {
	items := make([]ui.Node, 0, count)
	for index := 0; index < count; index++ {
		items = append(items, loadingPanel("loading-metric", html.Fragment(
			loadingBlock("loading-line loading-line-short"),
			loadingBlock("loading-value"),
		)))
	}
	return html.Div(html.Props{Class: "loading-metrics"}, items...)
}

func loadingRows(count int, avatars bool) ui.Node {
	rows := make([]ui.Node, 0, count)
	for index := 0; index < count; index++ {
		children := make([]ui.Node, 0, 3)
		if avatars {
			children = append(children, loadingBlock("loading-avatar loading-avatar-small"))
		}
		children = append(children,
			html.Div(html.Props{Class: "loading-copy"},
				loadingBlock("loading-line loading-line-medium"),
				loadingBlock("loading-line loading-line-short"),
			),
			loadingBlock("loading-chip"),
		)
		rows = append(rows, html.Div(html.Props{Class: "loading-row"}, children...))
	}
	return html.Div(html.Props{Class: "loading-rows"}, rows...)
}

func loadingTable(count int) ui.Node {
	rows := make([]ui.Node, 0, count+1)
	rows = append(rows, html.Div(html.Props{Class: "loading-table-row loading-table-head"},
		loadingBlock("loading-line"), loadingBlock("loading-line"), loadingBlock("loading-line"), loadingBlock("loading-line"),
	))
	for index := 0; index < count; index++ {
		rows = append(rows, html.Div(html.Props{Class: "loading-table-row"},
			loadingBlock("loading-line loading-line-medium"), loadingBlock("loading-line"), loadingBlock("loading-line loading-line-short"), loadingBlock("loading-chip"),
		))
	}
	return html.Div(html.Props{Class: "loading-table"}, rows...)
}

func loadingFacts(count int) ui.Node {
	rows := make([]ui.Node, 0, count)
	for index := 0; index < count; index++ {
		rows = append(rows, html.Div(html.Props{Class: "loading-fact"},
			loadingBlock("loading-line loading-line-short"), loadingBlock("loading-line loading-line-medium"),
		))
	}
	return html.Div(html.Props{Class: "loading-facts"}, rows...)
}

func loadingBars(count int) ui.Node {
	rows := make([]ui.Node, 0, count)
	for index := 0; index < count; index++ {
		rows = append(rows, html.Div(html.Props{Class: "loading-bar-row"},
			loadingBlock("loading-line loading-line-short"),
			loadingBlock("loading-bar loading-bar-"+fmt.Sprint(5+index)),
		))
	}
	return html.Div(html.Props{Class: "loading-bars"}, rows...)
}

func loadingPanel(class string, content ui.Node) ui.Node {
	return html.Div(html.Props{Class: strings.TrimSpace("loading-panel " + class)}, content)
}

func loadingBlock(class string) ui.Node {
	return html.Span(html.Props{Class: strings.TrimSpace("loading-block " + class)})
}

func safeLoadingPageClass(page PageID) string {
	if _, ok := LookupPage(page); ok {
		return string(page)
	}
	return string(PageHome)
}
