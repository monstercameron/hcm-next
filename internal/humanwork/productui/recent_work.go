package productui

// RecentWork filters one admitted work stream to the items the
// recent-work slot governs: terminal items in admission order.
// Attention (WEB-098) covers the active side; terminal items
// belong to continuity and history. Order stays server-ranked;
// the compiler presents without assigning recency. The output
// never aliases the input.
func RecentWork(items []WorkItem) []WorkItem {
	recent := make([]WorkItem, 0, len(items))
	for _, item := range items {
		if item.Terminal {
			recent = append(recent, item)
		}
	}
	return recent
}
