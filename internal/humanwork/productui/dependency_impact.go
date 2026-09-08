package productui

import (
	"fmt"
	"sort"
)

// Dependency change kinds over the presentation-owned dependency
// surface: registered widgets and catalog floorplans. Capabilities
// and classifications change in their owning domains; impact kinds
// for them would be a second authority, so unknown kinds refuse.
const (
	DependencyChangeWidget    = "widget"
	DependencyChangeFloorplan = "floorplan"
)

// DependencyChange is one contract change: which registered
// contract moved and to which version. Zero ToVersion retires the
// contract: nothing may target it afterward.
type DependencyChange struct {
	Kind      string
	ID        string
	ToVersion int64
}

// PageImpact is one page's dependency status: the page, whether
// the change hits it, and the stable reasons when it does. Every
// inventoried page is listed — clear pages carry nil reasons — so
// the studio sees status, not just hits.
type PageImpact struct {
	Page     PageID
	Impacted bool
	Reasons  []string
}

// ReportDependencyImpact inventories one contract change across
// page compositions. A page is hit when it binds the changed
// contract behind the new version — or at any version when the
// contract retires. Identical reasons deduplicate; pages sort by
// ID so the inventory is deterministic. Malformed changes fail
// closed: unknown kinds, blank contracts, and negative versions
// error. The report reads declared composition values; resolving
// layered configuration first stays with the caller.
func ReportDependencyImpact(pages map[PageID]PageComposition, change DependencyChange) ([]PageImpact, error) {
	switch change.Kind {
	case DependencyChangeWidget, DependencyChangeFloorplan:
	default:
		return nil, fmt.Errorf("unknown dependency change kind %q", change.Kind)
	}
	if change.ID == "" {
		return nil, fmt.Errorf("dependency change names no contract")
	}
	if change.ToVersion < 0 {
		return nil, fmt.Errorf("dependency change version must not be negative, got %d", change.ToVersion)
	}
	ordered := make([]PageID, 0, len(pages))
	for page := range pages {
		ordered = append(ordered, page)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	report := make([]PageImpact, 0, len(ordered))
	for _, page := range ordered {
		impact := PageImpact{Page: page}
		seen := map[string]bool{}
		hit := func(reason string) {
			if !seen[reason] {
				seen[reason] = true
				impact.Reasons = append(impact.Reasons, reason)
			}
			impact.Impacted = true
		}
		composition := pages[page]
		if change.Kind == DependencyChangeWidget {
			for _, binding := range composition.Widgets {
				if binding.WidgetType != change.ID {
					continue
				}
				if change.ToVersion == 0 {
					hit(fmt.Sprintf("widget %q was removed from the registry", binding.WidgetType))
				} else if binding.WidgetVersion < change.ToVersion {
					hit(fmt.Sprintf("widget %q version %d is behind registry version %d", binding.WidgetType, binding.WidgetVersion, change.ToVersion))
				}
			}
		} else if composition.Floorplan == change.ID {
			if change.ToVersion == 0 {
				hit(fmt.Sprintf("floorplan %q was removed from the catalog", composition.Floorplan))
			} else if composition.FloorplanVersion < change.ToVersion {
				hit(fmt.Sprintf("floorplan %q version %d is behind catalog version %d", composition.Floorplan, composition.FloorplanVersion, change.ToVersion))
			}
		}
		report = append(report, impact)
	}
	return report, nil
}
