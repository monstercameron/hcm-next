package productui

// DraftCenterItems composes the Draft Center view for UF-002
// save-and-resume: viewer-owned, open, and not awaiting
// approval, in admission order. Ownership comes from the My
// Work collection (WEB-103); unfinished and unsubmitted come
// from the typed collection views (WEB-104). The composition
// adds no truth — items pass through untouched.
func DraftCenterItems(items []WorkItem, viewer ViewerProfile) []WorkItem {
	mine := MyWorkItems(items, viewer)
	drafts := make([]WorkItem, 0, len(mine))
	for _, item := range mine {
		if !item.Terminal && item.Status != "Awaiting approval" {
			drafts = append(drafts, item)
		}
	}
	return drafts
}
