package productui

import "fmt"

// RegionDefinition is one registered semantic region from the page
// anatomy: every full page resolves shell, identity, authority,
// navigation, primary, supporting, utility, and completion in that
// order. The shell is platform-owned and never composable; the other
// seven register here with their anatomy rank.
type RegionDefinition struct {
	ID   string
	Name string
	Rank int
}

// registeredRegions is the composable anatomy in resolution order.
// Shell (rank 0) is platform-owned: naming it is a violation, never
// a composition.
var registeredRegions = []RegionDefinition{
	{ID: "identity", Name: "Page identity", Rank: 1},
	{ID: "authority", Name: "Authority context", Rank: 2},
	{ID: "navigation", Name: "Local navigation", Rank: 3},
	{ID: "primary", Name: "Primary region", Rank: 4},
	{ID: "supporting", Name: "Supporting region", Rank: 5},
	{ID: "utility", Name: "Utility surface", Rank: 6},
	{ID: "completion", Name: "Completion layer", Rank: 7},
}

// platformOwnedRegions names regions composition may never claim.
var platformOwnedRegions = map[string]bool{"shell": true}

// RegisteredRegions returns the composable anatomy in resolution
// order, freshly copied on every call.
func RegisteredRegions() []RegionDefinition {
	regions := make([]RegionDefinition, len(registeredRegions))
	copy(regions, registeredRegions)
	return regions
}

// RegionVerdict is the composition answer: compatible plus the stable
// reasons, in validation order, when not. Reasons stay nil on
// success.
type RegionVerdict struct {
	Compatible bool
	Reasons    []string
}

// ValidateRegionComposition checks one composition's regions: every
// region registered (platform-owned shell refused), anatomy order
// non-decreasing, exactly one primary, and a primary present once
// composition starts. An empty composition validates — a fresh draft
// names nothing yet.
func ValidateRegionComposition(composition PageComposition) RegionVerdict {
	ranks := make(map[string]int, len(registeredRegions))
	for _, region := range registeredRegions {
		ranks[region.ID] = region.Rank
	}
	var reasons []string
	highest := 0
	primaries := 0
	for _, region := range composition.Regions {
		rank, ok := ranks[region]
		if !ok {
			if platformOwnedRegions[region] {
				reasons = append(reasons, fmt.Sprintf("platform-owned region %q", region))
			} else {
				reasons = append(reasons, fmt.Sprintf("unknown region %q", region))
			}
			continue
		}
		if rank < highest {
			reasons = append(reasons, fmt.Sprintf("region %q out of order", region))
		} else {
			highest = rank
		}
		if region == "primary" {
			primaries++
		}
	}
	if primaries > 1 {
		reasons = append(reasons, "duplicate primary region")
	}
	if len(composition.Regions) > 0 && primaries == 0 {
		reasons = append(reasons, "missing primary region")
	}
	if len(reasons) > 0 {
		return RegionVerdict{Compatible: false, Reasons: reasons}
	}
	return RegionVerdict{Compatible: true}
}

// ValidateDraftRegions validates one draft through its regions.
func ValidateDraftRegions(draft PageDraft) RegionVerdict {
	return ValidateRegionComposition(draft.Composition)
}
