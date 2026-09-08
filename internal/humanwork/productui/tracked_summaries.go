package productui

// TrackedSummary is the derived-only tracking projection for
// one admitted work item: exactly the fields the UF-005
// status summary needs. Openness derives from terminality so
// counts reconcile with the attention (WEB-098) and
// recent-work (WEB-099) lists. No dates are interpreted and
// no new truth is added.
type TrackedSummary struct {
	ID     string
	Status string
	Due    string
	Open   bool
}

// SummarizeTracked projects one admitted work stream to its
// tracked-request summaries in admission order. Fields pass
// through untouched.
func SummarizeTracked(items []WorkItem) []TrackedSummary {
	summaries := make([]TrackedSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, TrackedSummary{
			ID:     item.ID,
			Status: item.Status,
			Due:    item.Due,
			Open:   !item.Terminal,
		})
	}
	return summaries
}
