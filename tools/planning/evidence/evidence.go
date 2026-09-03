// Package evidence enforces the execution-plan.md Legacy Baseline Rule
// (GOV-008): "historical markdown statements that tests passed do not
// establish present status." Every Evidence field must record a commit or
// branch identity, a toolchain version, an environment, the exact command
// run, a timestamp and a result - a bare prose claim that "tests passed"
// is rejected as historical, not fresh.
//
// This package scans planning/todos.md directly rather than through
// todoregistry.Todo, because todoregistry currently folds the Evidence
// field's parenthetical date/disposition label into its field-name match
// and does not retain it (see todoregistry.parseTodoField) - freshness
// checking needs exactly that label.
//
// REFACTOR note: identical artifact metadata (the same command/environment/
// toolchain repeated across many todos in one branch) should be
// content-addressed and referenced, not duplicated as prose in every
// Evidence field.
package evidence

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Record is the structured freshness data one Evidence claim must supply.
type Record struct {
	ID               string
	Timestamp        string // e.g. "2026-09-03"
	ToolchainVersion string // e.g. "Go 1.26.3"
	Environment      string // e.g. "windows/arm64"
	Command          string // e.g. "go test -count=1 ./tools/planning/evidence/..."
	Result           string // e.g. "PASS"
	CommitOrBranch   string // e.g. "branch plan-revision-2026-09-02"
}

// Violation names one missing or malformed freshness field.
type Violation struct {
	ID    string
	Issue string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s", v.ID, v.Issue)
}

// Validate returns every missing required freshness field on r.
func Validate(r Record) []Violation {
	var violations []Violation
	add := func(issue string) { violations = append(violations, Violation{ID: r.ID, Issue: issue}) }

	if r.Timestamp == "" {
		add("missing timestamp")
	}
	if r.ToolchainVersion == "" {
		add("missing toolchain version")
	}
	if r.Environment == "" {
		add("missing environment")
	}
	if r.Command == "" {
		add("missing command")
	}
	if r.Result == "" {
		add("missing result")
	}
	if r.CommitOrBranch == "" {
		add("missing commit/branch identity")
	}

	return violations
}

var (
	timestampRe = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})`)
	commandRe   = regexp.MustCompile("`(go (?:test|vet|run|build)[^`]*)`")
	envRe       = regexp.MustCompile(`\bon\s+([\w./-]+)\s*\((Go [0-9.]+)\)`)
	resultRe    = regexp.MustCompile(`\b(PASS|FAIL|PASSED|FAILED)\b`)
	branchRe    = regexp.MustCompile(`\bbranch\s+([\w./-]+)`)

	// historicalClaimRe recognizes a bare, unstructured claim that tests
	// previously passed - the exact anti-pattern the Legacy Baseline Rule
	// prohibits as a substitute for fresh evidence.
	historicalClaimRe = regexp.MustCompile(`(?i)\b(tests?|it|this)\s+(passed|previously passed|used to pass|worked|has been verified)\b`)

	// evidenceLineRe matches "- **Evidence (LABEL):** BODY" and captures
	// LABEL (the parenthetical, e.g. "2026-09-03" or "partial, 2026-09-03")
	// and BODY separately.
	evidenceLineRe = regexp.MustCompile(`^\s*-\s+\*\*Evidence\s*\(([^)]*)\)\s*:\*\*\s*(.*)$`)
	idLineRe       = regexp.MustCompile("^- \\[( |x)\\] `([A-Za-z0-9_-]+)`")
)

// ParseEvidenceField extracts a Record from one Evidence field's label
// (the "(...)" parenthetical, without the parens) and body. ok is false
// when label carries no dated timestamp at all; every other subfield
// defaults to "" and is reported by Validate.
func ParseEvidenceField(id, label, body string) (Record, bool) {
	r := Record{ID: id}

	if m := timestampRe.FindStringSubmatch(label); m != nil {
		r.Timestamp = m[1]
	}
	if r.Timestamp == "" {
		return r, false
	}

	if m := commandRe.FindStringSubmatch(body); m != nil {
		r.Command = m[1]
	}
	if m := envRe.FindStringSubmatch(body); m != nil {
		r.Environment = m[1]
		r.ToolchainVersion = m[2]
	}
	if m := resultRe.FindStringSubmatch(body); m != nil {
		r.Result = strings.ToUpper(m[1])
	}
	if m := branchRe.FindStringSubmatch(body); m != nil {
		r.CommitOrBranch = "branch " + m[1]
	}

	return r, true
}

// CheckFreshness returns every freshness violation for one Evidence
// field's label and body. When label carries no dated timestamp, the body
// is checked for a bare historical claim instead of the full field set.
func CheckFreshness(id, label, body string) []Violation {
	record, ok := ParseEvidenceField(id, label, body)
	if !ok {
		full := label + " " + body
		if historicalClaimRe.MatchString(full) {
			return []Violation{{ID: id, Issue: "bare historical claim that tests passed, with no dated commit/toolchain/environment/command/result evidence"}}
		}
		if strings.TrimSpace(body) == "" && strings.TrimSpace(label) == "" {
			return []Violation{{ID: id, Issue: "missing Evidence"}}
		}
		return []Violation{{ID: id, Issue: "Evidence has no dated timestamp"}}
	}

	return Validate(record)
}

// Occurrence is one Evidence field found while scanning a markdown
// document, attributed to the nearest preceding todo ID.
type Occurrence struct {
	ID    string
	Line  int
	Label string
	Body  string
}

// ScanEvidenceFields walks markdown line by line and returns every
// "- **Evidence (...):**" field found, attributed to the most recently
// seen "- [ ] `ID`"/"- [x] `ID`" todo line above it.
func ScanEvidenceFields(markdown string) []Occurrence {
	var occurrences []Occurrence
	currentID := ""

	for i, line := range strings.Split(markdown, "\n") {
		if m := idLineRe.FindStringSubmatch(line); m != nil {
			currentID = m[2]
			continue
		}
		if m := evidenceLineRe.FindStringSubmatch(line); m != nil {
			occurrences = append(occurrences, Occurrence{
				ID:    currentID,
				Line:  i + 1,
				Label: m[1],
				Body:  m[2],
			})
		}
	}

	return occurrences
}

// KnownGap is an allow-listed evidence-freshness violation, recorded with
// its owner and expiry in definitions/planning/known-defects.yaml under the
// known_evidence_freshness_gaps key. It exists for pre-existing Evidence
// entries that predate this rule; it must never be used to excuse new
// evidence going forward, which is why every entry carries an expiry.
type KnownGap struct {
	ID     string `yaml:"id"`
	Issue  string `yaml:"issue"`
	Owner  string `yaml:"owner"`
	Expiry string `yaml:"expiry"`
	Reason string `yaml:"reason"`
}

type knownGapsFile struct {
	KnownGaps []KnownGap `yaml:"known_evidence_freshness_gaps"`
}

// LoadKnownGaps reads the known-evidence-freshness-gaps allow-list. A
// missing file is treated as an empty list.
func LoadKnownGaps(path string) ([]KnownGap, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var parsed knownGapsFile
	if err := yaml.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return parsed.KnownGaps, nil
}

// CheckMarkdownFreshness scans markdown for every Evidence field and
// returns every freshness violation found, across both completed and
// partial claims - a partial claim still asserts dated, structured
// evidence for the portion it covers.
func CheckMarkdownFreshness(markdown string) []Violation {
	var violations []Violation
	for _, occ := range ScanEvidenceFields(markdown) {
		violations = append(violations, CheckFreshness(occ.ID, occ.Label, occ.Body)...)
	}
	return violations
}
