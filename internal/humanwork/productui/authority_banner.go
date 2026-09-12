package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// ActingAuthorityBanner renders the persistent acting-authority strip: the
// exact current authority from the server-resolved context projection —
// tenant, acting context, delegator, expiry, elevation — through the same
// detail builder as the switcher summary, never a second record.
//
// UXAUDIT-007: it shows only when [authorityNoticeworthy] finds the
// projection delegated, view-as, elevated, break-glass, or otherwise
// unrecognized authority; ordinary self authority, and a missing or invalid
// projection, render nothing. This is deliberately the shell's one
// acting-context component: no page forks it, and no page re-derives
// "elevated" from its own logic -- every page that wants this notice passes
// the same server-resolved [ContextSwitcherProps.Current] this function
// already renders from.
func ActingAuthorityBanner(props ContextSwitcherProps) ui.Node {
	if !validAuthorityContext(props.Current) || !authorityNoticeworthy(props.Current) {
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
	if !validAuthorityContext(props.Current) || !authorityNoticeworthy(props.Current) {
		return html.Fragment()
	}
	return ui.CreateElement(ActingAuthorityBanner, props)
}

// authorityNoticeworthy decides, from the server-resolved authority
// projection alone, whether the persistent acting-authority banner belongs
// on screen (UXAUDIT-007's GREEN: shown only for delegated, view-as,
// elevated, or break-glass authority; quiet for ordinary self).
//
// Delegated and Elevated are themselves already a complete, known answer
// about authority -- true or false, never "unset" -- so either one being
// true discloses regardless of State. Only when both are false does State
// get a say: an empty State defers to those two known-false booleans (self,
// quiet) rather than being treated as unknown on its own, but any non-empty
// State is judged strictly by [AuthorityState.noticeworthy], which discloses
// for every named non-self state and, with no permissive default, for
// anything it does not recognize at all.
func authorityNoticeworthy(current AuthorityContext) bool {
	if current.Delegated || current.Elevated {
		return true
	}
	if current.State == "" {
		return false
	}
	return current.State.noticeworthy()
}
