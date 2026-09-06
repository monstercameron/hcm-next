package productui

import "strings"

// DisplayLabel turns a stable machine identifier into neutral shell copy.
// It is intentionally conservative: business record names always come from
// the authorized service projection and never pass through this helper.
func DisplayLabel(value string) string {
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(strings.TrimSpace(value)))
	for index, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) > 0 {
			runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		}
		words[index] = string(runes)
	}
	return strings.Join(words, " ")
}
