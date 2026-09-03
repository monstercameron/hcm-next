package legal

// newYorkSourceFile is the research file every New York citation in this
// package points back to. It is drafted, unreviewed research, not legal
// advice; see [ReviewStatusUnreviewed].
const newYorkSourceFile = "planning/research/state-employment-law/new-york.md"

// newYorkSeedDefinition is the checked-in definition file the New York seed
// pack is built from, relative to definitions/legal/packs.
var newYorkSeedDefinition = []string{"seed", "us-ny.json"}

// NewYorkPromotionPack builds the seed New York rule pack for a
// promotion-and-base-pay-change transaction, version 1.0, open-ended from
// 2026-01-01. Every rule cites newYorkSourceFile and carries
// [ReviewStatusUnreviewed]: this is a fixture proving the [RulePack]
// skeleton, not a reviewed legal interpretation.
//
// Like [CaliforniaPromotionPack] it loads a checked-in [PackDefinition]
// rather than building a Go literal; see that function for why.
func NewYorkPromotionPack() (RulePack, error) {
	return loadDefinedPack(newYorkSeedDefinition...)
}
