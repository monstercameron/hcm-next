package productui

import "sort"

// PageInventoryEntry is one page's studio status: the latest
// published revision it pins (unpublished pages carry version
// zero and no digest), the latest-version rollout scopes live at
// the build moment, whether a matching retirement is active, and
// the sorted impacted contract IDs. Live scopes report rollout
// facts without rewriting them: retirement never edits the
// scopes, and superseded-version rollouts stay excluded.
type PageInventoryEntry struct {
	Page              PageID
	Version           int64
	Digest            string
	Published         bool
	LiveScopes        []string
	Retired           bool
	ImpactedContracts []string
}

// BuildPageInventory builds the studio's page inventory: the union
// of revision-log pages, composed pages, rollout pages, and
// retirement pages, sorted by page. Latest revisions pin from the
// log; live scopes union the live scopes of the page's
// latest-version rollouts (every rollout while unpublished);
// retirement reflects any matching active retirement;
// impacted contracts collect the change IDs hitting the page's
// composition. Older-version rollouts are superseded state and
// never count as live. Malformed contract changes fail the whole
// build closed. Rollouts and retirements are read mechanically —
// feed validated, target-verified inputs.
func BuildPageInventory(log *PageRevisionLog, compositions map[PageID]PageComposition, changes []DependencyChange, rollouts []PageRollout, retirements []PageRetirement, now int64) ([]PageInventoryEntry, error) {
	known := map[PageID]bool{}
	for _, page := range log.Pages() {
		known[page] = true
	}
	for page := range compositions {
		known[page] = true
	}
	for _, rollout := range rollouts {
		known[rollout.Page] = true
	}
	for _, retirement := range retirements {
		known[retirement.Page] = true
	}
	ordered := make([]PageID, 0, len(known))
	for page := range known {
		ordered = append(ordered, page)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	inventory := make([]PageInventoryEntry, 0, len(ordered))
	for _, page := range ordered {
		entry := PageInventoryEntry{Page: page}
		if latest, ok := log.Latest(page); ok {
			entry.Published = true
			entry.Version = latest.Version
			entry.Digest = latest.Digest
		}
		live := map[string]bool{}
		for _, rollout := range rollouts {
			if rollout.Page != page || (entry.Published && rollout.Version != entry.Version) {
				continue
			}
			for _, scope := range rollout.Scopes {
				if now >= scope.EffectiveFrom {
					live[scope.Scope] = true
				}
			}
		}
		for scope := range live {
			entry.LiveScopes = append(entry.LiveScopes, scope)
		}
		sort.Strings(entry.LiveScopes)
		for _, retirement := range retirements {
			if retirement.Page == page && RetirementActiveAt(retirement, now) {
				entry.Retired = true
			}
		}
		if composition, ok := compositions[page]; ok {
			hit := map[string]bool{}
			for _, change := range changes {
				report, err := ReportDependencyImpact(map[PageID]PageComposition{page: composition}, change)
				if err != nil {
					return nil, err
				}
				if len(report) == 1 && report[0].Impacted {
					hit[change.ID] = true
				}
			}
			for id := range hit {
				entry.ImpactedContracts = append(entry.ImpactedContracts, id)
			}
			sort.Strings(entry.ImpactedContracts)
		}
		inventory = append(inventory, entry)
	}
	return inventory, nil
}
