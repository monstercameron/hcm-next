package legal

// californiaSourceFile is the research file every California citation in this
// package points back to. It is drafted, unreviewed research, not legal
// advice; see [ReviewStatusUnreviewed].
const californiaSourceFile = "planning/research/state-employment-law/california.md"

// californiaSeedDefinition is the checked-in definition file the California
// seed pack is built from, relative to definitions/legal/packs.
var californiaSeedDefinition = []string{"seed", "us-ca.json"}

// CaliforniaPromotionPack builds the seed California rule pack for a
// promotion-and-base-pay-change transaction, version 1.0, open-ended from
// 2026-01-01. Every rule cites californiaSourceFile and carries
// [ReviewStatusUnreviewed]: this is a fixture proving the [RulePack]
// skeleton, not a reviewed legal interpretation.
//
// The content is no longer a Go literal. LEGAL-010's rule is that a
// [RulePack] comes from a checked-in [PackDefinition] and nothing else, so
// this function loads definitions/legal/packs/seed/us-ca.json through the
// same loader every state draft goes through. The pack it returns is a
// [PackCandidate]'s content — validated and unsigned — because a fixture is
// exactly the thing that has not been through the signing pipeline.
func CaliforniaPromotionPack() (RulePack, error) {
	return loadDefinedPack(californiaSeedDefinition...)
}
