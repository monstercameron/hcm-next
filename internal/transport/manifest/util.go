package manifest

import "sort"

// sortStrings sorts s ascending in place and returns it, for chaining inside
// a literal or return statement.
func sortStrings(s []string) []string {
	sort.Strings(s)
	return s
}

// sortUniqueStrings returns a sorted copy of s with exact duplicates
// removed. It never mutates s.
func sortUniqueStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(s))
	out := make([]string, 0, len(s))
	for _, v := range s {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
