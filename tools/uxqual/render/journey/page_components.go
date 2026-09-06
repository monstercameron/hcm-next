package journey

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// pageHeaderProps is the shared heading contract for the journey overview
// and focused workflow pages. Feature views supply content and optional
// actions; this component owns hierarchy and spacing.
type pageHeaderProps struct {
	Class   string
	Eyebrow string
	Title   string
	Lead    string
	Actions []ui.Node
}

func pageHeader(props pageHeaderProps) ui.Node {
	class := strings.TrimSpace("jn-pagehead " + props.Class)
	return html.Div(html.Props{Class: class},
		htmlIf(props.Eyebrow != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-eyebrow"}, html.Text(props.Eyebrow))
		}),
		html.H1(html.Props{}, html.Text(props.Title)),
		htmlIf(props.Lead != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-lead"}, html.Text(props.Lead))
		}),
		htmlIf(len(props.Actions) > 0, func() ui.Node {
			return html.Div(html.Props{Class: "jn-context-actions"}, html.Fragment(props.Actions...))
		}),
	)
}

// engineUnavailableCallout is shared by every page that can offer an
// execution-backed action. Feature views decide availability; the callout
// owns one consistent explanation and accessible structure.
func engineUnavailableCallout(available bool, notice string) ui.Node {
	if available || notice == "" {
		return nil
	}
	return html.Div(html.Props{Class: "jn-callout"},
		iconInfo("jn-callout-icon"),
		html.Div(html.Props{},
			html.P(html.Props{Class: "jn-callout-title"}, html.Text("Execution is not composed on this cell")),
			html.P(html.Props{Class: "jn-callout-detail"}, html.Text(notice)),
		),
	)
}

// loadingPanel is the shared stable placeholder for a page region whose
// facts are in flight. It deliberately contains no disabled copy of the
// eventual controls.
func loadingPanel(title, detail string) ui.Node {
	return html.Section(html.Props{Class: "jn-panel jn-loading", Raw: map[string]any{"aria-busy": "true"}},
		html.H2(html.Props{}, html.Text(title)),
		htmlIf(detail != "", func() ui.Node {
			return html.P(html.Props{Class: "jn-muted"}, html.Text(detail))
		}),
	)
}
