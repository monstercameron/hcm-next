package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PageIdentity is the governed header identity for one rendered page: a
// stable registry stamp plus localized copy. Resolving it in one place
// keeps page titles, subtitles, and the scope control from drifting into
// per-page ad-hoc fields while the registry stays the single authority.
//
// It deliberately carries no acting-context field of its own (UXAUDIT-007
// removed the header's unconditional "Acting as yourself" label): a
// per-page identity resolution is not the place a page decides for itself
// whether the acting authority is worth a notice. [ActingAuthorityBanner] is
// the shell's one acting-context component, fed by the server-resolved
// projection, shown or quiet the same way on every page.
type PageIdentity struct {
	Page       PageID
	Title      string
	Subtitle   string
	ScopeLabel string
	ScopeHref  string
}

// ResolvePageIdentity derives the header identity from the canonical page
// registry, the locale, and the authorized navigation projection. Unknown
// pages fall back to Home exactly like ApplyLocale; the settings
// destination is withheld when the identity cannot open it.
func ResolvePageIdentity(view View) PageIdentity {
	definition, ok := LookupPage(view.Page)
	if !ok {
		definition, _ = LookupPage(PageHome)
	}
	scope := strings.TrimSpace(view.Scope)
	if scope == "" {
		scope = view.Locale.Text("shell.authenticated_scope")
	}
	identity := PageIdentity{
		Page:       definition.ID,
		Title:      view.Locale.Text(definition.TitleKey),
		Subtitle:   view.Locale.Text(definition.SubtitleKey),
		ScopeLabel: scope,
	}
	if navigationDestinationAuthorized(view, PageSettings) {
		identity.ScopeHref = statefulHref(view, PageSettings)
	}
	if definition.ID == PageHome && view.Principal != "" {
		name := strings.TrimSpace(view.Viewer.Name)
		if name == "" {
			name = view.Principal
		}
		identity.Title = view.Locale.Text("page.home.greeting", map[string]string{"name": name})
	}
	return identity
}

// PageIdentityHeader renders the canonical page head: the breadcrumb trail,
// the resolved title and subtitle, and the scope control. The stable page
// id is stamped on the header so tests, styles, and automation can address
// a page without parsing localized copy.
//
// UXAUDIT-007 removed this header's second, unconditional acting-context
// label ("Acting as yourself" on every page regardless of authority): the
// scope control below names the authorized data boundary, nothing about who
// the viewer is acting as. Acting-context notices now come from exactly one
// place, [ActingAuthorityBanner], shown in the shell above this header only
// when the server-resolved authority actually calls for one.
func PageIdentityHeader(view View) ui.Node {
	identity := ResolvePageIdentity(view)
	scopeText := ui.Text(identity.ScopeLabel)
	var scopeControl ui.Node = html.Span(html.Props{Class: "scope"}, scopeText)
	return html.Div(html.Props{Class: "page-head", Data: map[string]string{"hcm-page": string(identity.Page)}},
		Breadcrumbs(view, ResolveBreadcrumbs(view)),
		html.Div(html.Props{}, html.H1(html.Props{ID: "page-title", Raw: map[string]any{"tabindex": "-1"}}, ui.Text(identity.Title)), html.P(html.Props{Class: "subtitle"}, ui.Text(identity.Subtitle))),
		html.Div(html.Props{Class: "scope-wrap"}, scopeControl),
	)
}
