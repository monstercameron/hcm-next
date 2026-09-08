package productui

import "strings"

// PurposeVerdict is the setup answer: compatible plus the stable
// reasons, in validation order, when not. Reasons stay nil on
// success.
type PurposeVerdict struct {
	Compatible bool
	Reasons    []string
}

// ValidatePagePurpose checks one composition's opening authoring
// step: the page must declare why it exists and who it serves.
// Both stay free-text declarations — purpose has no stated length
// rule, and audiences stay declared strings rather than a second
// authorization vocabulary. The verdict is single-concern: every
// other composition field validates in its own step.
func ValidatePagePurpose(composition PageComposition) PurposeVerdict {
	var reasons []string
	if strings.TrimSpace(composition.Purpose) == "" {
		reasons = append(reasons, "missing page purpose")
	}
	if strings.TrimSpace(composition.Audience) == "" {
		reasons = append(reasons, "missing page audience")
	}
	if len(reasons) > 0 {
		return PurposeVerdict{Compatible: false, Reasons: reasons}
	}
	return PurposeVerdict{Compatible: true}
}
