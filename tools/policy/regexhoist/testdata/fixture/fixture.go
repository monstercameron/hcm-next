package fixture

import "regexp"

var packagePattern = regexp.MustCompile(`package`)

func FunctionPattern(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

func init() {
	_ = regexp.MustCompile(`init`)
}

// aliasedCompile hides a per-call compile behind a package-level name.
var aliasedCompile = regexp.MustCompile

// DynamicPattern is exempt because its line carries the marker.
func DynamicPattern(pattern string) (*regexp.Regexp, error) {
	return regexp.Compile(pattern) // regexhoist:dynamic
}

// AliasedPattern uses the alias; the alias declaration is the finding.
func AliasedPattern(pattern string) *regexp.Regexp {
	return aliasedCompile(pattern)
}

// Package keeps the package-level pattern referenced.
func Package() *regexp.Regexp { return packagePattern }
