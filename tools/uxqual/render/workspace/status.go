package workspace

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/hcm-next/tools/uxqual/pagedef"
	"github.com/monstercameron/hcm-next/tools/uxqual/render/page"
)

// StatusElementID is the id of the governed page's canonical live region.
// It aliases page.LiveRegionElementID so standalone and workspace-composed
// GWC renderings retain the same identity as the semantic SSR shell.
const StatusElementID = page.LiveRegionElementID

// StatusRegion retains WEB-121's status-region constructor while delegating
// to the page renderer's canonical implementation. Build does not call this
// separately because page.Render already includes the page-owned node; doing
// so would create duplicate assistive-technology announcements.
func StatusRegion(politeness pagedef.LiveRegionPoliteness) ui.Node {
	return page.LiveRegion(politeness)
}
