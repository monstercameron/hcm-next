package productui

// CompletedEntry is the derived-only history record for one
// terminal work item: identity plus completion evidence. It
// carries exactly what the completed-work history needs and
// covers precisely the recent-work list (WEB-099).
type CompletedEntry struct {
	ID          string
	CompletedAt string
}

// CompletedHistory projects one admitted work stream to its
// completed-work history in admission order. Evidence passes
// through untouched — including empty stamps, which stay
// empty rather than invented.
func CompletedHistory(items []WorkItem) []CompletedEntry {
	history := make([]CompletedEntry, 0, len(items))
	for _, item := range items {
		if item.Terminal {
			history = append(history, CompletedEntry{ID: item.ID, CompletedAt: item.CompletedAt})
		}
	}
	return history
}
