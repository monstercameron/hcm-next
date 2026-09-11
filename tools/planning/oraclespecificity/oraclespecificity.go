// Package oraclespecificity rejects placeholder RED/GREEN oracles in
// behavior-binding todos (GOV-028). It is kernel-pure: Markdown block
// scanning, reviewed-pattern classification and text emission only, with
// no database, network or mutable global state.
package oraclespecificity

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// Oracle specificity codes. PLACEHOLDER_RED_ORACLE and
// PLACEHOLDER_GREEN_ORACLE mark oracles that repeat contract boilerplate
// without a named defect or exact outcome; MISSING_PROHIBITED_EFFECT_ORACLE
// marks behavior-binding GREEN claims over persistence, ledger, outbox,
// human-work or provider effects that state no prohibited outcome.
const (
	PlaceholderRedOracle          = "PLACEHOLDER_RED_ORACLE"
	PlaceholderGreenOracle        = "PLACEHOLDER_GREEN_ORACLE"
	MissingProhibitedEffectOracle = "MISSING_PROHIBITED_EFFECT_ORACLE"
)

// Finding is one source-located oracle-specificity diagnostic.
type Finding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	TodoID  string `json:"todo_id"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// String renders the stable form consumed by planning tools and CI.
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s: %s", f.File, f.Line, f.TodoID, f.Code, f.Message)
}

// findingLess orders findings deterministically.
func findingLess(a, b Finding) bool {
	if a.File != b.File {
		return a.File < b.File
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	if a.TodoID != b.TodoID {
		return a.TodoID < b.TodoID
	}
	if a.Code != b.Code {
		return a.Code < b.Code
	}
	return a.Message < b.Message
}

// MarshalFindings renders findings as canonical indented JSON.
func MarshalFindings(findings []Finding) ([]byte, error) {
	ordered := append([]Finding(nil), findings...)
	sort.Slice(ordered, func(i, j int) bool { return findingLess(ordered[i], ordered[j]) })
	rendered, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(rendered, '\n'), nil
}

type field struct {
	name  string
	value string
	line  int
}

type todoBlock struct {
	id     string
	line   int
	fields []field
}

var (
	todoTitleRe = regexp.MustCompile("^- \\[([ x])\\] `([^`]+)`")

	// placeholderRedRe matches RED boilerplate that asserts failure
	// without naming it: contract-violation language, bare must-fail,
	// invalid-fixture hand-waving, untyped diagnostics and passive
	// rejection with no defect.
	placeholderRedRe = regexp.MustCompile(`(?i)\b(violates? (this|the) contract|must fail|fails? as expected|fails? appropriately|fails? correctly|invalid fixture|faulty input|returns? diagnostics?|reports? (an |the )?(error|diagnostic|failure)|diagnostics? (are|is) returned|handles? (the |an )?(error|case|failure)|is (rejected|invalid|handled)|gets? rejected|properly (fails?|rejects?|handles?|works?))`)

	// placeholderGreenRe matches GREEN boilerplate that asserts success
	// without naming the exact outcome. Bare suite pass/fail predicates
	// are covered by barePassRe so qualified passes ("passes the
	// qualification matrix") are not swept in.
	placeholderGreenRe = regexp.MustCompile(`(?i)\b(works? correctly|behaves? (correctly|as expected)|functions? correctly|returns? successfully|is accepted|gets? accepted|correct behavior|expected behavior|happy path|succeeds successfully)`)

	// barePassRe matches a bare subject-plus-pass predicate with no
	// outcome detail.
	barePassRe = regexp.MustCompile(`(?i)\b(all |the )?(suites?|tests?|operation|request|build|command) (pass|passes|succeed|succeeds)\b`)

	// errTypeRe and literalRe redeem an oracle: a named typed rejection
	// or a quoted literal is specific by construction.
	errTypeRe = regexp.MustCompile(`Err[A-Za-z0-9_]+`)
	literalRe = regexp.MustCompile("`[^`]+`")

	// resultMarkerRe redeems a GREEN with exact-outcome language.
	resultMarkerRe = regexp.MustCompile(`(?i)\b(exact|typed|precisely|counts?|exactly|zero|digest|canonical|pinned|byte-identical|deterministic|stable|reproducib|match|only|bound)`)

	// prohibitedDomainRe matches the effect domains whose GREEN claims
	// must carry a prohibited outcome: persistence, ledger, outbox,
	// human work and provider effects. Bare emission/computation words
	// are deliberately excluded: they belong to exact-result language,
	// governed by the placeholder rules.
	prohibitedDomainRe = regexp.MustCompile(`(?i)\b(persist|ledger|outbox|human([ -]?work)?|assign|reviewer|operator|escalat|provider|database|payment|charg|publish|provision|revok|delet)`)

	// effectVerbRe matches one lowercase word that acts as an effect
	// verb: a reviewed stem with an inflectional ending, plus the
	// irregulars paid and sent. Derivational forms ("persistence",
	// "assignment", "storage") do not match, so vocabulary listings and
	// artifact nouns never read as claimed actions.
	effectVerbRe = regexp.MustCompile(`^(persist|emit|publish|assign|provision|send|writ|stor|record|creat|delet|charg|revok|remov|pay)(s|es|ed|ing|ten)?$|^notif(ies|ied|y|s|ed|ing)?$|^(paid|sent)$`)

	wordRe = regexp.MustCompile(`[a-z]+`)

	// listedRe matches enumeration members (", stores and transport",
	// "ledger, human work, or connectivity"): nouns recruited as list
	// items are vocabulary, not claimed effects.
	listedRe = regexp.MustCompile(`,\s*[a-z]+\s+(and|or)\b|\b(and|or)\s+[a-z]+\s*,`)
)

// CheckMarkdown classifies every todo oracle in a Markdown corpus,
// returning one finding per violated specificity rule.
func CheckMarkdown(markdown, file string) ([]Finding, error) {
	_, _ = todoregistry.ParseTodos(markdown)
	blocks := parseBlocks(markdown)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no todo blocks found in %s", file)
	}
	var findings []Finding
	for _, block := range blocks {
		findings = append(findings, checkBlock(block, file)...)
	}
	sort.Slice(findings, func(i, j int) bool { return findingLess(findings[i], findings[j]) })
	return findings, nil
}

func parseBlocks(markdown string) []todoBlock {
	var blocks []todoBlock
	var current *todoBlock
	for lineNumber, line := range strings.Split(markdown, "\n") {
		lineNumber++
		if match := todoTitleRe.FindStringSubmatch(line); match != nil {
			if current != nil {
				blocks = append(blocks, *current)
			}
			current = &todoBlock{id: match[2], line: lineNumber}
			continue
		}
		if current == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- **") {
			continue
		}
		body := strings.TrimPrefix(trimmed, "- **")
		separator := strings.Index(body, ":**")
		if separator < 0 {
			continue
		}
		name := strings.TrimSpace(body[:separator])
		value := strings.TrimSpace(body[separator+3:])
		current.fields = append(current.fields, field{name: name, value: value, line: lineNumber})
	}
	if current != nil {
		blocks = append(blocks, *current)
	}
	return blocks
}

func checkBlock(block todoBlock, file string) []Finding {
	var findings []Finding
	add := func(line int, code, message string) {
		findings = append(findings, Finding{File: file, Line: line, TodoID: block.id, Code: code, Message: message})
	}
	var red, green string
	redLine, greenLine := block.line, block.line
	for _, f := range block.fields {
		switch canonicalField(f.name) {
		case "RED":
			red = f.value
			redLine = f.line
		case "GREEN":
			green = f.value
			greenLine = f.line
		}
	}
	if red == "" || green == "" {
		return findings
	}
	if placeholder := placeholderRedRe.FindString(red); placeholder != "" &&
		!errTypeRe.MatchString(red) && !literalRe.MatchString(red) && !resultMarkerRe.MatchString(red) {
		add(redLine, PlaceholderRedOracle, fmt.Sprintf("RED is placeholder contract language (%q): name one semantic defect with its exact typed rejection and state", strings.TrimSpace(placeholder)))
	}
	greenSpecific := errTypeRe.MatchString(green) || literalRe.MatchString(green) || resultMarkerRe.MatchString(green)
	if !greenSpecific {
		if placeholder := placeholderGreenRe.FindString(green); placeholder != "" {
			add(greenLine, PlaceholderGreenOracle, fmt.Sprintf("GREEN is placeholder contract language (%q): name the exact result, persistence and effect counts", strings.TrimSpace(placeholder)))
		} else if bare := barePassRe.FindString(green); bare != "" {
			add(greenLine, PlaceholderGreenOracle, fmt.Sprintf("GREEN is a bare pass predicate (%q): name the exact result, persistence and effect counts", strings.TrimSpace(bare)))
		}
	}
	if claimsEffect(green) && !hasProhibitedBound(green) {
		add(greenLine, MissingProhibitedEffectOracle, "GREEN claims persistence, ledger, outbox, human-work or provider effects without a prohibited outcome: state the exact counts and the zero side effects")
	}
	return findings
}

// claimsEffect reports whether green pairs an effect-domain noun with
// an action verb inside a five-word window, in either active or passive
// order ("persists the settlement to the ledger", "the settlement is
// persisted"). Morphology, not spans, separates verbs from nouns.
func claimsEffect(green string) bool {
	delisted := listedRe.ReplaceAllString(strings.ToLower(green), " ")
	words := wordRe.FindAllString(delisted, -1)
	const window = 5
	for i, word := range words {
		if !effectVerbRe.MatchString(word) {
			continue
		}
		from := max(i-window, 0)
		to := min(i+window+1, len(words))
		for _, near := range words[from:to] {
			if prohibitedDomainRe.MatchString(near) {
				return true
			}
		}
	}
	return false
}

// hasProhibitedBound reports whether an effect claim states its prohibited
// outcome: an explicit zero/no/without/never/refusal bound or an exact
// effect count.
func hasProhibitedBound(green string) bool {
	return prohibitedBoundRe.MatchString(strings.ToLower(green))
}

var prohibitedBoundRe = regexp.MustCompile(`\b(zero|without|never|refus|reject|forbid|prohibit|exactly|exact counts?)|no \w+`)

func canonicalField(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	if strings.HasPrefix(name, "EVIDENCE (") {
		return "EVIDENCE"
	}
	return name
}
