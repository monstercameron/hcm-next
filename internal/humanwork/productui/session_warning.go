package productui

import (
	"net/url"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// reauthResumeHref carries the current address on a re-authentication
// destination as its resume target, so signing in again restores the
// user's place. The target comes from the shell's own current-address
// primitive — never a second record of where the user was. A base that is
// unparsable or outside the shell passes through untouched: the warning
// keeps its projected link instead of inventing a destination.
func reauthResumeHref(view View, base string) string {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/workspace/") {
		return base
	}
	query := parsed.Query()
	query.Set("resume", currentPageHref(view, view.NavCollapsed))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// SessionWarningProps carries the server-projected expiring-session
// warning. Detail is the server's localized countdown text and ReauthHref
// its re-authentication destination. Timing truth — when a session counts
// as expiring — stays server-side; presentation renders the projection and
// never computes, thresholds, or invents expiry.
type SessionWarningProps struct {
	I18nProps
	Detail     string
	ReauthHref string
	Navigate   func(string)
}

// SessionWarning renders the expiring-session banner with its detail text,
// re-authentication link, and dismiss control. Dismissal is page-load
// client state: the warning returns on the next load until the server
// stops projecting it.
func SessionWarning(props SessionWarningProps) ui.Node {
	dismissed := ui.UseState(false)
	if dismissed.Get() {
		return html.Fragment()
	}
	dismissLabel := props.Text("session_expiry.dismiss")
	return html.Section(html.Props{ID: "session-warning", Class: "session-warning", Aria: map[string]string{"labelledby": "session-warning-title"}},
		html.H2(html.Props{ID: "session-warning-title", Class: "session-warning-title"}, ui.Text(props.Text("session_expiry.title"))),
		html.P(html.Props{Class: "session-warning-detail"}, ui.Text(props.Detail)),
		html.Div(html.Props{Class: "session-warning-actions"},
			softwareLink(props.Navigate, html.Props{Class: "session-warning-reauth"}, props.ReauthHref, ui.Text(props.Text("session_expiry.reauth"))),
			html.Button(html.Props{
				ID: "session-warning-dismiss", Class: "session-warning-dismiss", Type: "button",
				Aria:    map[string]string{"label": dismissLabel},
				OnClick: ui.UseEvent(func(ui.MouseEvent) { dismissed.Set(true) }),
			}, ui.Text(dismissLabel)),
		),
	)
}

func sessionWarning(view View) ui.Node {
	if signedOutState(view) {
		return html.Fragment()
	}
	if view.SessionWarning == nil {
		return html.Fragment()
	}
	props := *view.SessionWarning
	props.I18nProps = I18nProps{Locale: view.Locale}
	props.Navigate = view.Navigate
	props.ReauthHref = reauthResumeHref(view, props.ReauthHref)
	return ui.CreateElement(SessionWarning, props)
}
