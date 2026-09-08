package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// BreakGlassActivationProps carries the server-projected break-glass
// activation for emergency access. IncidentRef identifies the emergency,
// ReasonDetail the server's reason, Capabilities the narrow named grants,
// and TTLDetail the server-localized bounded window; ActivateHref is its
// activation destination. The prompt authorizes nothing itself: opening a
// grant is a server decision under the break-glass contract (incident,
// justification, distinct approver, named capabilities, finite TTL), and
// dismissing the prompt merely hides it for the page load.
type BreakGlassActivationProps struct {
	I18nProps
	IncidentRef  string
	ReasonDetail string
	Capabilities []string
	TTLDetail    string
	ActivateHref string
	Navigate     func(string)
}

// BreakGlassActivation renders the emergency-access prompt: which incident
// needs it, why, which narrow capabilities it covers, and how long it
// lasts, with an activation link and a dismiss control. Without an
// incident reference there is no prompt; an unsafe destination yields no
// activation link instead of an invented one. Empty reason, capability,
// and window projections omit their rows rather than rendering blanks.
// breakGlassSuppressed reports whether the prompt stays hidden: no
// incident, or this incident already dismissed. Dismissal is keyed by
// incident reference, so a new emergency always resurfaces — an old
// dismissal never transfers across authority drift.
func breakGlassSuppressed(incidentRef, dismissedRef string) bool {
	return incidentRef == "" || dismissedRef == incidentRef
}

func BreakGlassActivation(props BreakGlassActivationProps) ui.Node {
	if strings.TrimSpace(props.IncidentRef) == "" {
		return nil
	}
	dismissed := ui.UseState("")
	if breakGlassSuppressed(strings.TrimSpace(props.IncidentRef), dismissed.Get()) {
		return html.Fragment()
	}
	href := props.ActivateHref
	if !validRecoveryHref(href) {
		href = ""
	}
	rows := []ui.Node{
		html.P(html.Props{Class: "break-glass-row"},
			html.Span(html.Props{Class: "break-glass-label"}, ui.Text(props.Text("break_glass.incident"))),
			html.Span(html.Props{Class: "break-glass-value"}, ui.Text(strings.TrimSpace(props.IncidentRef)))),
	}
	if strings.TrimSpace(props.ReasonDetail) != "" {
		rows = append(rows, html.P(html.Props{Class: "break-glass-detail"}, ui.Text(props.ReasonDetail)))
	}
	var capabilities []string
	for _, capability := range props.Capabilities {
		if trimmed := strings.TrimSpace(capability); trimmed != "" {
			capabilities = append(capabilities, trimmed)
		}
	}
	if len(capabilities) > 0 {
		items := make([]ui.Node, 0, len(capabilities))
		for _, capability := range capabilities {
			items = append(items, html.Li(html.Props{Class: "break-glass-capability"}, ui.Text(capability)))
		}
		rows = append(rows, html.Ul(html.Props{Class: "break-glass-capabilities", Aria: map[string]string{"label": props.Text("break_glass.capabilities")}}, items...))
	}
	if strings.TrimSpace(props.TTLDetail) != "" {
		rows = append(rows, html.P(html.Props{Class: "break-glass-row"},
			html.Span(html.Props{Class: "break-glass-label"}, ui.Text(props.Text("break_glass.expires"))),
			html.Span(html.Props{Class: "break-glass-value"}, ui.Text(strings.TrimSpace(props.TTLDetail)))))
	}
	actions := []ui.Node{}
	if href != "" {
		actions = append(actions, softwareLink(props.Navigate, html.Props{Class: "break-glass-activate"}, href, ui.Text(props.Text("break_glass.activate"))))
	}
	dismissLabel := props.Text("break_glass.dismiss")
	actions = append(actions, html.Button(html.Props{
		ID: "break-glass-dismiss", Class: "break-glass-dismiss", Type: "button",
		Aria:    map[string]string{"label": dismissLabel},
		OnClick: ui.UseEvent(func(ui.MouseEvent) { dismissed.Set(strings.TrimSpace(props.IncidentRef)) }),
	}, ui.Text(dismissLabel)))
	rows = append(rows, html.Div(html.Props{Class: "break-glass-actions"}, actions...))
	return html.Section(html.Props{ID: "break-glass-activation", Class: "break-glass-activation", Aria: map[string]string{"labelledby": "break-glass-title"}},
		append([]ui.Node{html.H2(html.Props{ID: "break-glass-title", Class: "break-glass-title"}, ui.Text(props.Text("break_glass.title")))}, rows...)...,
	)
}

func breakGlassActivation(view View) ui.Node {
	if signedOutState(view) {
		return html.Fragment()
	}
	if view.BreakGlassActivation == nil {
		return html.Fragment()
	}
	props := *view.BreakGlassActivation
	props.I18nProps = I18nProps{Locale: view.Locale}
	props.Navigate = view.Navigate
	return ui.CreateElement(BreakGlassActivation, props)
}
