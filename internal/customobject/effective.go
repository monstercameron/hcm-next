package customobject

import (
	"sort"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// EffectiveAt returns facts whose half-open effective interval contains at.
// The returned slice is deterministic and is independent of input ordering.
func EffectiveAt(facts []RelationshipFact, at values.Instant) []RelationshipFact {
	out := make([]RelationshipFact, 0, len(facts))
	for _, fact := range facts {
		if ok, err := fact.Effective.ContainsInstant(at); err == nil && ok {
			out = append(out, fact)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Target < out[j].Target
	})
	return out
}

// KnownAt applies bitemporal replay: only facts known by the supplied instant
// are returned, after effective-time filtering.
func KnownAt(facts []RelationshipFact, effectiveAt, knownAt values.Instant) []RelationshipFact {
	out := EffectiveAt(facts, effectiveAt)
	filtered := out[:0]
	for _, fact := range out {
		if fact.Known.Instant().IsSet() && !fact.Known.Instant().After(knownAt) {
			filtered = append(filtered, fact)
		}
	}
	return filtered
}
