package legal

var washingtonStateDefinition = []string{"states", "us-wa.json"}

// WashingtonPromotionPack loads the existing Washington state rule-pack
// fixture through the same definition loader used by the seed packs.
func WashingtonPromotionPack() (RulePack, error) {
	return loadDefinedPack(washingtonStateDefinition...)
}
