// Package tddcontract enforces the explicit red-first TDD contract for every
// planning todo (GOV-017). It supplements todoregistry's structural parser
// with source-located diagnostics for executable test names, observable
// oracles, red-before-green evidence, and the narrow EVIDENCE_ONLY exception.
package tddcontract

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

const (
	MissingTest                  = "MISSING_TEST"
	DuplicateTest                = "DUPLICATE_TEST"
	MissingTestMatrix            = "MISSING_TEST_MATRIX"
	DuplicateTestMatrix          = "DUPLICATE_TEST_MATRIX"
	MissingRed                   = "MISSING_RED"
	DuplicateRed                 = "DUPLICATE_RED"
	MissingGreen                 = "MISSING_GREEN"
	DuplicateGreen               = "DUPLICATE_GREEN"
	MissingRefactor              = "MISSING_REFACTOR"
	DuplicateRefactor            = "DUPLICATE_REFACTOR"
	CombinedRedGreen             = "COMBINED_RED_GREEN"
	InvalidGoTestName            = "INVALID_GO_TEST_NAME"
	NonUniqueGoTestName          = "NON_UNIQUE_GO_TEST_NAME"
	GreenEvidenceWithoutRed      = "GREEN_EVIDENCE_WITHOUT_RED"
	VagueOracle                  = "VAGUE_ORACLE"
	InvalidEvidenceOnlyStatus    = "INVALID_EVIDENCE_ONLY_STATUS"
	NonUniqueEvidenceOnly        = "NON_UNIQUE_EVIDENCE_ONLY"
	EvidenceOnlyMissingOwner     = "EVIDENCE_ONLY_MISSING_OWNER"
	EvidenceOnlyMissingRationale = "EVIDENCE_ONLY_MISSING_RATIONALE"
	EvidenceOnlyMissingOracle    = "EVIDENCE_ONLY_MISSING_ORACLE"
	EvidenceOnlyMissingExpiry    = "EVIDENCE_ONLY_MISSING_EXPIRY"
)

// Diagnostic is a stable source-located TDD contract finding.
type Diagnostic struct {
	File    string
	Line    int
	TodoID  string
	Code    string
	Message string
}

// String renders the stable form consumed by planning tools and CI.
func (d Diagnostic) String() string {
	return fmt.Sprintf("%s:%d: %s: %s: %s", d.File, d.Line, d.TodoID, d.Code, d.Message)
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
	todoTitleRe  = regexp.MustCompile("^- \\[([ x])\\] `([^`]+)`")
	goTestNameRe = regexp.MustCompile(`^(Test|Fuzz|Benchmark)[A-Za-z0-9_]+$`)
	observableRe = regexp.MustCompile(`(?i)\b(return|returned|returns|persist|persisted|stored|error|diagnostic|evidence|state|status|count|digest|report|decision|event|row|effect|result|reject|rejected|accept|accepted|fail|failed|exact|field|test|command|oracle)\b`)
)

// CheckMarkdown validates a Markdown todo corpus. The parser from
// todoregistry is invoked for canonical-field compatibility; raw block
// scanning is retained here so malformed fixtures still receive exact field
// and source-line diagnostics instead of being discarded by that parser.
func CheckMarkdown(markdown, file string) ([]Diagnostic, error) {
	_, _ = todoregistry.ParseTodos(markdown)
	blocks := parseBlocks(markdown)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("no todo blocks found in %s", file)
	}
	var findings []Diagnostic
	seenPrimary := make(map[string]Diagnostic)
	for _, block := range blocks {
		findings = append(findings, validateBlock(block, file, seenPrimary)...)
	}
	return sortDiagnostics(findings), nil
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

func validateBlock(block todoBlock, file string, seenPrimary map[string]Diagnostic) []Diagnostic {
	var findings []Diagnostic
	fields := make(map[string][]field)
	for _, f := range block.fields {
		fields[canonicalField(f.name)] = append(fields[canonicalField(f.name)], f)
	}
	add := func(line int, code, message string) {
		findings = append(findings, Diagnostic{File: file, Line: line, TodoID: block.id, Code: code, Message: message})
	}
	checkRequired := func(name, missingCode, duplicateCode string) []field {
		got := fields[name]
		if len(got) == 0 {
			add(block.line, missingCode, fmt.Sprintf("todo must declare exactly one %s field", name))
			return nil
		}
		if len(got) > 1 {
			for _, duplicate := range got[1:] {
				add(duplicate.line, duplicateCode, fmt.Sprintf("%s field is duplicated; first declaration is on line %d", name, got[0].line))
			}
		}
		return got
	}

	primary := checkRequired("TEST", MissingTest, DuplicateTest)
	matrix := checkRequired("TEST MATRIX", MissingTestMatrix, DuplicateTestMatrix)
	red := checkRequired("RED", MissingRed, DuplicateRed)
	green := checkRequired("GREEN", MissingGreen, DuplicateGreen)
	checkRequired("REFACTOR", MissingRefactor, DuplicateRefactor)

	if combined := fields["RED/GREEN"]; len(combined) > 0 {
		add(combined[0].line, CombinedRedGreen, "RED and GREEN must be independently authored so a prior failure is observable")
		if len(red) == 0 {
			red = combined
		}
		if len(green) == 0 {
			green = combined
		}
	}

	if len(primary) > 0 {
		name := cleanTestName(primary[0].value)
		if !goTestNameRe.MatchString(name) {
			add(primary[0].line, InvalidGoTestName, fmt.Sprintf("TEST name %q is not a valid Go test, fuzz, or benchmark name", name))
		} else if prior, exists := seenPrimary[name]; exists {
			add(primary[0].line, NonUniqueGoTestName, fmt.Sprintf("Go test name %q is already declared at %s:%d", name, prior.File, prior.Line))
		} else {
			seenPrimary[name] = Diagnostic{File: file, Line: primary[0].line, TodoID: block.id}
		}
	}
	if len(matrix) > 0 {
		for _, name := range matrixNames(matrix[0].value) {
			if !goTestNameRe.MatchString(name.value) {
				add(matrix[0].line, InvalidGoTestName, fmt.Sprintf("TEST MATRIX name %q is not a valid Go test, fuzz, or benchmark name", name.value))
			}
		}
	}

	for _, f := range red {
		if !observableRe.MatchString(f.value) {
			add(f.line, VagueOracle, "RED must name an observable returned, persisted, error, state, or evidence outcome")
		}
	}
	for _, f := range green {
		if !observableRe.MatchString(f.value) {
			add(f.line, VagueOracle, "GREEN must name an observable returned, persisted, error, state, or evidence outcome")
		}
	}

	evidence := evidenceFields(block.fields)
	if len(evidence) > 0 {
		priorRed := false
		for _, f := range evidence {
			body := strings.ToLower(f.value)
			if strings.Contains(body, "red") && (strings.Contains(body, "fail") || strings.Contains(body, "result")) {
				priorRed = true
			}
		}
		if !priorRed {
			add(evidence[0].line, GreenEvidenceWithoutRed, "green evidence must identify a prior failing RED run")
		}
	}

	validateEvidenceOnly(fields, block, add)
	return findings
}

func canonicalField(name string) string {
	name = strings.ToUpper(strings.TrimSpace(name))
	if strings.HasPrefix(name, "EVIDENCE (") {
		return "EVIDENCE"
	}
	return name
}

func cleanTestName(value string) string {
	value = strings.TrimSpace(strings.TrimSuffix(value, "."))
	return strings.Trim(value, "`")
}

type matrixName struct{ class, value string }

func matrixNames(value string) []matrixName {
	var result []matrixName
	for _, token := range strings.Split(strings.Trim(value, "`"), ";") {
		class, name, ok := strings.Cut(strings.TrimSpace(token), "=")
		if ok {
			result = append(result, matrixName{class: strings.TrimSpace(class), value: cleanTestName(name)})
		}
	}
	return result
}

func evidenceFields(fields []field) []field {
	var evidence []field
	for _, f := range fields {
		if canonicalField(f.name) == "EVIDENCE" || strings.HasPrefix(strings.ToUpper(strings.TrimSpace(f.name)), "EVIDENCE") {
			evidence = append(evidence, f)
		}
	}
	return evidence
}

func validateEvidenceOnly(fields map[string][]field, block todoBlock, add func(int, string, string)) {
	var statuses []field
	for _, f := range fields["DISPOSITION"] {
		if strings.Contains(strings.ToUpper(f.value), "EVIDENCE_ONLY") {
			statuses = append(statuses, f)
		}
	}
	statuses = append(statuses, fields["EVIDENCE_ONLY"]...)
	if len(statuses) == 0 {
		return
	}
	if len(statuses) > 1 {
		for _, f := range statuses[1:] {
			add(f.line, NonUniqueEvidenceOnly, "EVIDENCE_ONLY status may be declared only once")
		}
	}
	status := strings.ToUpper(strings.TrimSpace(statuses[0].value))
	if status != "EVIDENCE_ONLY" {
		add(statuses[0].line, InvalidEvidenceOnlyStatus, fmt.Sprintf("EVIDENCE_ONLY status %q is not exactly EVIDENCE_ONLY", statuses[0].value))
	}
	joined := ""
	for _, f := range block.fields {
		joined += " " + strings.ToLower(f.name+"="+f.value)
	}
	checks := []struct {
		keys []string
		code string
		text string
	}{
		{[]string{"owner", "evidence_only_owner"}, EvidenceOnlyMissingOwner, "EVIDENCE_ONLY requires an owner"},
		{[]string{"rationale", "evidence_only_rationale"}, EvidenceOnlyMissingRationale, "EVIDENCE_ONLY requires a rationale"},
		{[]string{"oracle", "validation oracle", "evidence_only_oracle"}, EvidenceOnlyMissingOracle, "EVIDENCE_ONLY requires a validation oracle"},
		{[]string{"expiry", "evidence_only_expiry"}, EvidenceOnlyMissingExpiry, "EVIDENCE_ONLY requires an expiry"},
	}
	for _, check := range checks {
		found := false
		for _, key := range check.keys {
			if strings.Contains(joined, key+"=") || strings.Contains(joined, key+":") {
				found = true
				break
			}
		}
		if !found {
			add(statuses[0].line, check.code, check.text)
		}
	}
}

func sortDiagnostics(findings []Diagnostic) []Diagnostic {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		if findings[i].TodoID != findings[j].TodoID {
			return findings[i].TodoID < findings[j].TodoID
		}
		return findings[i].Code < findings[j].Code
	})
	return findings
}
