package productui

// AttentionList returns the unified attention list: the
// non-terminal items of one admitted stream in admission order.
// Terminal items belong to continuity and history, never
// attention. Order stays server-ranked — presentation assigns
// no priority — and the output never aliases the input.
func AttentionList(items []WorkItem) []WorkItem {
	list := make([]WorkItem, 0, len(items))
	for _, item := range items {
		if !item.Terminal {
			list = append(list, item)
		}
	}
	return list
}
