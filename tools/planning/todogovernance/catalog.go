package todogovernance

import (
	"fmt"
	"os"
	"regexp"
)

// LoadCatalogDefinitions reads planning/specs/business-intent-catalog.md and
// returns every `hcmnext.<domain>.<verb_noun>/v<N>` definition_ref listed in
// its "Initial draft-contract slice" table. This is the authoritative list
// of BusinessIntent names a todo's INTENT CONTEXT DIRECT field may claim
// (besides "none").
func LoadCatalogDefinitions(path string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read catalog %s: %w", path, err)
	}
	return ParseCatalogDefinitions(string(content)), nil
}

var definitionRefRe = regexp.MustCompile("`(hcmnext\\.[a-z0-9_]+\\.[a-z0-9_]+/v[0-9]+)`")

// ParseCatalogDefinitions extracts every distinct definition_ref
// ("hcmnext.<domain>.<verb_noun>/v<N>") backtick-quoted anywhere in content.
func ParseCatalogDefinitions(content string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range definitionRefRe.FindAllStringSubmatch(content, -1) {
		ref := m[1]
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}
