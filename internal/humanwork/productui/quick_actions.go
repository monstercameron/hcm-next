package productui

// ResolveQuickActions resolves a viewer's chosen quick-action
// IDs against the available launcher items for the
// quick-actions slot. Chosen order is kept, repeats collapse
// to first mention, and unknown IDs drop fail-closed so stale
// or forged choices never leak through or break the slot.
// Items pass through untouched; the catalog owns the truth.
func ResolveQuickActions(available []ActionLauncherItem, chosen []string) []ActionLauncherItem {
	byID := make(map[string]ActionLauncherItem, len(available))
	for _, item := range available {
		byID[item.ID] = item
	}
	resolved := make([]ActionLauncherItem, 0, len(chosen))
	seen := make(map[string]bool, len(chosen))
	for _, id := range chosen {
		if seen[id] {
			continue
		}
		seen[id] = true
		if item, ok := byID[id]; ok {
			resolved = append(resolved, item)
		}
	}
	return resolved
}
