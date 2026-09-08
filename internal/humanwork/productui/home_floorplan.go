package productui

// Home floorplan section IDs in floorplan order: action-first
// attention, orientation summaries, doing quick-actions,
// continuity recent-work, broadcast announcements last. Later
// phase todos attach content per ID; the IDs stay stable so
// sections never drift between resolution and render.
const (
	HomeSectionAttention     = "attention"
	HomeSectionSummaries     = "summaries"
	HomeSectionQuickActions  = "quick-actions"
	HomeSectionRecentWork    = "recent-work"
	HomeSectionAnnouncements = "announcements"
)

// HomeSection is one Home floorplan slot: its stable ID and its
// placement gate. A gate naming a page places the slot only for
// viewers the projection authorizes; an empty page places the
// slot unconditionally — its contents still resolve per viewer
// in the section's own step (summaries, quick actions, recent
// work) or arrive server-scoped (announcements). Placement
// splits from content on purpose: the floorplan owns order and
// slots, never who sees which record.
type HomeSection struct {
	ID     string
	Page   PageID
	Action string
}

// homeFloorplanSections is the registered Home floorplan. The
// attention slot gates on the work surface, mirroring the work
// collection: needs-action items without work visibility have
// no governed slot.
var homeFloorplanSections = []HomeSection{
	{ID: HomeSectionAttention, Page: PageWork, Action: "view"},
	{ID: HomeSectionSummaries},
	{ID: HomeSectionQuickActions},
	{ID: HomeSectionRecentWork},
	{ID: HomeSectionAnnouncements},
}

// HomeFloorplanSections lists the registered Home floorplan in
// order. The returned slice is a copy; callers cannot move the
// registry's slots.
func HomeFloorplanSections() []HomeSection {
	return append([]HomeSection(nil), homeFloorplanSections...)
}

// ResolveHomeFloorplan resolves the visible Home floorplan for
// one authorized projection: gated slots pass only when the
// view allows, unconditional slots always place, floorplan
// order kept. Legacy projections without permissions keep
// every slot through Allows.
func ResolveHomeFloorplan(view View) []HomeSection {
	visible := make([]HomeSection, 0, len(homeFloorplanSections))
	for _, section := range homeFloorplanSections {
		if section.Page != "" && !view.Allows(section.Page, section.Action) {
			continue
		}
		visible = append(visible, section)
	}
	return visible
}
