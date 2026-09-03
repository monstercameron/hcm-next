// Package celqual is the backend-neutral CEL qualification contract.
// It deliberately does not import CEL-Go: a runtime dependency must first be
// admitted and then plugged into this small, owned contract.
package celqual

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

type Contract struct {
	MaxNodes         int
	MaxCost          int
	AllowedVariables map[string]struct{}
}
type Program struct {
	Canonical string
	Nodes     int
	Cost      int
	Digest    string
}

var forbidden = regexp.MustCompile(`(?i)\b(now|timestamp|random|rand|uuid|http|https|fetch|read|write|reflect|type|proto|container)\s*\(`)
var token = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (c Contract) Validate(source string) (Program, error) {
	if c.MaxNodes <= 0 || c.MaxCost <= 0 {
		return Program{}, fmt.Errorf("invalid positive limits")
	}
	s := strings.Join(strings.Fields(source), " ")
	if s == "" {
		return Program{}, fmt.Errorf("empty expression")
	}
	if len(s) > c.MaxCost*4 {
		return Program{}, fmt.Errorf("expression exceeds bounded size")
	}
	if forbidden.MatchString(s) {
		return Program{}, fmt.Errorf("ambient capability or reflection is forbidden")
	}
	if strings.ContainsAny(s, "{};[]") {
		return Program{}, fmt.Errorf("comprehensions, blocks, and indexing are not in bounded subset")
	}
	if strings.Contains(s, "?") || strings.Contains(s, "|") {
		return Program{}, fmt.Errorf("ambiguous or non-canonical operator")
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	for _, p := range parts {
		if !token.MatchString(p) || p == "true" || p == "false" || p == "null" {
			continue
		}
		if _, ok := c.AllowedVariables[p]; !ok {
			return Program{}, fmt.Errorf("undeclared variable %q", p)
		}
	}
	nodes := len(parts)
	if nodes > c.MaxNodes {
		return Program{}, fmt.Errorf("node limit exceeded: %d > %d", nodes, c.MaxNodes)
	}
	cost := nodes + strings.Count(s, "&&") + strings.Count(s, "||")
	if cost > c.MaxCost {
		return Program{}, fmt.Errorf("cost limit exceeded: %d > %d", cost, c.MaxCost)
	}
	h := sha256.Sum256([]byte(s))
	return Program{Canonical: s, Nodes: nodes, Cost: cost, Digest: hex.EncodeToString(h[:])}, nil
}
func (p Program) Deterministic() bool { return p.Canonical != "" && p.Digest != "" }
