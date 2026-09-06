package workspace

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// AuthorityElementID is the id of the shell's authority-context region: the
// frontend plan's page-anatomy region 3 (acting role, purpose, and the
// other facts [Session] carries).
const AuthorityElementID = "workspace-authority"

// SessionStrip renders s as the shell's authority-context region. A zero
// Session (see [Session.IsZero]) renders nothing at all, matching
// [StatusRegion]'s LiveRegionOff behavior: a session the reader carries no
// display fact for is not the same as a broken renderer, so it is an
// explicit empty fragment rather than a strip full of blank labels.
func SessionStrip(s Session) ui.Node {
	if s.IsZero() {
		return html.Fragment()
	}
	var items []ui.Node
	if s.Subject != "" {
		items = append(items, labelledListItem("Acting as", s.Subject))
	}
	if s.Tenant != "" {
		items = append(items, labelledListItem("Tenant", s.Tenant))
	}
	if s.Purpose != "" {
		items = append(items, labelledListItem("Purpose", s.Purpose))
	}
	if len(s.Roles) > 0 {
		items = append(items, labelledListItem("Roles", strings.Join(s.Roles, ", ")))
	}
	return html.Section(html.Props{ID: AuthorityElementID, Aria: map[string]string{"label": "Authority context"}},
		html.Ul(html.Props{}, items...),
	)
}

func labelledListItem(label, value string) ui.Node {
	return html.Li(html.Props{},
		html.Strong(html.Props{}, ui.Text(label+": ")),
		ui.Text(value),
	)
}
