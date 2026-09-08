package productui

import "fmt"

// classificationRanks is the publication-gate sensitivity ladder,
// least to most sensitive. The labels come from the registered
// widget limits (public, internal, confidential) plus the platform
// catalog's restricted tier ("Restricted data is not merely hidden
// by UI serialization" — separate storage and cryptographic
// boundaries mark the top of the ladder). Presentation pins this
// ordering so page ceilings can compare; the Classification service
// taxonomy stays authoritative, and the server re-checks at refresh
// time. Unknown labels never rank: unranked content fails closed.
var classificationRanks = map[string]int{
	"public":       1,
	"internal":     2,
	"confidential": 3,
	"restricted":   4,
}

// ClassificationRank reports one label's ladder rank. The second
// result is false for unknown labels, which clear no ceiling.
func ClassificationRank(label string) (int, bool) {
	rank, ok := classificationRanks[label]
	return rank, ok
}

// CeilingVerdict is the ceiling answer: compatible plus the stable
// reasons, in validation order, when not. Reasons stay nil on
// success.
type CeilingVerdict struct {
	Compatible bool
	Reasons    []string
}

// ValidateDraftCeiling checks one draft against its declared page
// classification ceiling: every composed widget binding's data
// classification must sit at or below the ceiling. Rules, in order:
// the ceiling must be declared and ranked; unclassified or unranked
// bindings clear no ceiling; ranked bindings above the ceiling are
// refused. Widget capability limits (the registry's per-type maxima)
// are not compared here — the ceiling gates composed data, and the
// capability gateway (server-side) gates what actions may carry.
// Violations accumulate in composition order.
func ValidateDraftCeiling(draft PageDraft) CeilingVerdict {
	ceiling := draft.Composition.ClassificationCeiling
	ceilingRank, ceilingOK := ClassificationRank(ceiling)
	var reasons []string
	switch {
	case ceiling == "":
		reasons = append(reasons, "missing classification ceiling")
	case !ceilingOK:
		reasons = append(reasons, fmt.Sprintf("unknown classification ceiling %q", ceiling))
	}
	for _, binding := range draft.Composition.Widgets {
		rank, ok := ClassificationRank(binding.Classification)
		switch {
		case binding.Classification == "":
			reasons = append(reasons, fmt.Sprintf("unclassified binding for widget %q clears no ceiling", binding.WidgetType))
		case !ok:
			reasons = append(reasons, fmt.Sprintf("unranked classification %q for widget %q clears no ceiling", binding.Classification, binding.WidgetType))
		case ceilingOK && rank > ceilingRank:
			reasons = append(reasons, fmt.Sprintf("widget %q classification %q exceeds page ceiling %q", binding.WidgetType, binding.Classification, ceiling))
		}
	}
	if len(reasons) > 0 {
		return CeilingVerdict{Compatible: false, Reasons: reasons}
	}
	return CeilingVerdict{Compatible: true}
}
