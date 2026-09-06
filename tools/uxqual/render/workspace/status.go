package workspace

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
)

// StatusElementID is the id of the shell's live region, matching
// tools/uxqual/ssrshell's own "live-region" id so a caller comparing the two
// renderers' output (or a later hydration step matching a live tree against
// a server-rendered one) finds the same id in both.
const StatusElementID = "workspace-status"

// StatusRegion builds the shell's one status/live region from politeness,
// the page's own declared [pagedef.Accessibility.LiveRegion]. It follows
// tools/uxqual/ssrshell's own mapping exactly (polite -> role="status",
// assertive -> role="alert"), so the two renderers' live regions are
// interchangeable landmarks with the same semantics, and LiveRegionOff
// renders nothing at all -- an explicit "this page declares no live
// region", not the absence of a declaration (see
// [pagedef.LiveRegionPoliteness]'s own doc comment).
func StatusRegion(politeness pagedef.LiveRegionPoliteness) ui.Node {
	var ariaLive, role string
	switch politeness {
	case pagedef.LiveRegionPolite:
		ariaLive, role = "polite", "status"
	case pagedef.LiveRegionAssertive:
		ariaLive, role = "assertive", "alert"
	default:
		return html.Fragment()
	}
	return html.Div(html.Props{
		ID:   StatusElementID,
		Role: role,
		Aria: map[string]string{"live": ariaLive, "atomic": "true"},
	})
}
