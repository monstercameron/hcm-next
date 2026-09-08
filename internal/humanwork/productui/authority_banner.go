package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ActingAuthorityBanner renders the persistent acting-authority strip: the
// exact current authority from the server-resolved context projection —
// tenant, acting context, delegator, expiry, elevation — through the same
// detail builder as the switcher summary, never a second record. It shows
// for every valid projection (own authority included): persistence covers
// the authority the user acts under, not just delegations. An invalid or
// missing projection renders nothing.
func ActingAuthorityBanner(props ContextSwitcherProps) ui.Node {
	if !validAuthorityContext(props.Current) {
		return nil
	}
	return html.Section(html.Props{ID: "acting-authority", Class: "acting-authority-banner", Aria: map[string]string{"labelledby": "acting-authority-title"}},
		html.H2(html.Props{ID: "acting-authority-title", Class: "acting-authority-title"}, ui.Text(props.Text("acting_authority.title"))),
		html.P(html.Props{Class: "acting-authority-detail"}, ui.Text(currentContextDetail(props.Locale, props))),
	)
}

func actingAuthorityBanner(view View) ui.Node {
	if signedOutState(view) {
		return html.Fragment()
	}
	props := view.ContextSwitcher
	props.I18nProps = I18nProps{Locale: view.Locale}
	if !validAuthorityContext(props.Current) {
		return html.Fragment()
	}
	return ui.CreateElement(ActingAuthorityBanner, props)
}
