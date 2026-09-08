package productui

// MyWorkItems scopes one work stream to the viewer's
// collection for the My Work page: items whose PersonRef
// equals the viewer's PersonID, in admission order. Empty
// viewer identities and empty refs match nothing
// fail-closed, so unassigned work never leaks into a
// collection and logged-out viewers see none. Items pass
// through untouched.
func MyWorkItems(items []WorkItem, viewer ViewerProfile) []WorkItem {
	mine := make([]WorkItem, 0, len(items))
	if viewer.PersonID == "" {
		return mine
	}
	for _, item := range items {
		if item.PersonRef != "" && item.PersonRef == viewer.PersonID {
			mine = append(mine, item)
		}
	}
	return mine
}
