// Package atomicity checks that every todo has one primary deliverable, one
// bounded test group and a deterministic completion signal (GOV-007): its
// Title must not bundle two independently shippable verbs, and its GREEN
// field must describe an observable expected result rather than a vague
// placeholder.
//
// REFACTOR note: this permits a small declared TEST MATRIX (several test
// classes for one deliverable) without treating that as a second
// deliverable; only a second *verb* in the Title counts against atomicity.
package atomicity

import (
	"fmt"
	"regexp"
	"strings"
)

// actionVerbs are the leading action verbs observed in planning/todos.md
// titles. A todo whose Title joins two of these with " and " bundles two
// independently shippable deliverables into one todo.
var actionVerbs = map[string]bool{
	"add": true, "bind": true, "bootstrap": true, "build": true,
	"check": true, "commit": true, "compile": true, "construct": true,
	"create": true, "deduplicate": true, "define": true, "deliver": true,
	"deploy": true, "detect": true, "enforce": true, "ensure": true,
	"establish": true, "export": true, "generate": true, "implement": true,
	"ingest": true, "intake": true, "maintain": true, "materialize": true,
	"normalize": true, "persist": true, "produce": true, "propagate": true,
	"prove": true, "publish": true, "record": true, "register": true,
	"reject": true, "reconcile": true, "require": true, "resolve": true,
	"review": true, "route": true, "sign": true, "version": true,
}

// vaguePlaceholders are GREEN values that name no observable expected
// result at all.
var vaguePlaceholders = map[string]bool{
	"": true, "tbd": true, "n/a": true, "na": true, "it works": true,
	"looks good": true, "works": true, "done": true, "complete": true,
}

var andSplitRe = regexp.MustCompile(`(?i)\band\b`)

// Violation names one atomicity defect in a todo.
type Violation struct {
	ID     string
	Reason string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s", v.ID, v.Reason)
}

// CheckAtomicity returns every atomicity violation for one todo's ID,
// Title and Green field.
func CheckAtomicity(id, title, green string) []Violation {
	var violations []Violation

	if verb, ok := secondShippableVerb(title); ok {
		violations = append(violations, Violation{
			ID:     id,
			Reason: fmt.Sprintf("Title bundles two independently shippable verbs (second verb %q); split into separate todos", verb),
		})
	}

	normalizedGreen := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(green), ".")))
	if vaguePlaceholders[normalizedGreen] {
		violations = append(violations, Violation{
			ID:     id,
			Reason: "GREEN names no observable expected result (empty or a vague placeholder)",
		})
	}

	return violations
}

// secondShippableVerb reports the second action verb in title when it is
// joined to an earlier clause by the word "and", e.g. "Implement X and
// deploy Y." A plain noun-phrase "and" (e.g. "trust and assurance") does
// not match because "assurance" is not an action verb.
func secondShippableVerb(title string) (string, bool) {
	locs := andSplitRe.FindAllStringIndex(title, -1)
	for _, loc := range locs {
		rest := strings.TrimSpace(title[loc[1]:])
		word := firstWord(rest)
		if actionVerbs[strings.ToLower(word)] {
			return word, true
		}
	}
	return "", false
}

func firstWord(s string) string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9')
	})
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
