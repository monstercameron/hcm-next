// Package researchquestions checks that the fourteen blocking research
// contradictions listed in planning/specs/legal-rule-packs-and-state-configuration.md
// section 11 (LEGAL-018) are tracked to a resolution in the research
// review log, and that a state whose file still carries an unresolved
// contradiction is not marked releasable in the per-state matrix.
//
// It reuses tools/planning/legalmatrix's markdown-table conventions (the
// Section 5 legend, the two-letter state codes, the pipe-table shape) but
// cannot reuse legalmatrix.Matrix itself for cell-level lookups: Parse
// returns a *Matrix whose tableA/tableB/totalsA/totalsB fields (and the
// parsedTable/rowData types behind them) are all unexported, so a caller
// outside that package can confirm the matrix parses cleanly but cannot
// ask "what is state X's PERSONNEL_FILE cell". Section 5's cellTable is
// therefore a second, deliberately minimal, read of the same two tables -
// columns, rows and leading legend tokens only, no totals, no row-count or
// duplicate-row validation, all of which legalmatrix.Check already
// performs. A future revision of legalmatrix that exports a
// Matrix.Cell(state, column) accessor would let this package delete
// cellTable and call it directly.
//
// This package does no file I/O of its own: callers (tools/planning's
// plancheck subcommand) read the spec, the research review log
// (planning/research/state-employment-law/README.md) and the state
// research files and pass their content in.
package researchquestions

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Violation is one concrete defect: a blocking question with no tracked
// outcome, a disputed/open item whose affected state is releasable above
// UNREVIEWED, or a state file whose own Status header disagrees with the
// research README's queue table. Kind is a stable, machine-readable
// category; Detail names the offending item/state/file in prose.
type Violation struct {
	Kind   string
	Detail string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s", v.Kind, v.Detail)
}

// BlockingQuestion is one numbered item from section 11's "Blocking for a
// release" list.
type BlockingQuestion struct {
	Number int
	Text   string
}

// ReviewLogEntry is one numbered item from the review log's LEGAL-018
// contradiction-closure section, with the set of recognized outcome
// keywords (FIXED, DISPUTED, OPEN) found anywhere in its text - an entry
// commonly carries more than one, e.g. item 12's "FIXED for nine states;
// DISPUTED for one."
type ReviewLogEntry struct {
	Number   int
	Text     string
	Outcomes map[string]bool
}

var (
	// numberedItemRe finds the start of a numbered list item at the
	// beginning of a line: "1. **Title**...".
	numberedItemRe = regexp.MustCompile(`(?m)^(\d+)\.\s+\*\*`)

	// outcomeRe finds a bolded outcome keyword.
	outcomeRe = regexp.MustCompile(`\*\*(FIXED|DISPUTED|OPEN)\b`)

	// reviewLogHeadingRe finds the review log's LEGAL-018
	// contradiction-closure section heading, tolerant of the date
	// prefix and heading level actually used
	// ("### 2026-09-03 — LEGAL-018 contradiction closure").
	reviewLogHeadingRe = regexp.MustCompile(`(?m)^#{2,4}.*LEGAL-018.*contradiction closure.*$`)

	// nextHeadingRe finds the next same-or-higher-level heading after a
	// matched section start, to bound the section's extent.
	nextHeadingRe = regexp.MustCompile(`(?m)^#{1,4}\s`)

	// blockingSectionRe finds section 11's "Blocking for a release" list,
	// bounded before the "Non-blocking" paragraph that follows it in the
	// same section.
	openQuestionsHeadingRe = regexp.MustCompile(`(?m)^##\s*11\.\s*Open Questions\s*$`)
	nonBlockingRe          = regexp.MustCompile(`(?m)^Non-blocking,`)

	// fileStatusRe finds a state research file's own H1 status field:
	// "**State:** X | **Researched:** date | **Status:** REVIEWED".
	fileStatusRe = regexp.MustCompile(`\*\*Status:\*\*\s*([A-Z_]+)`)

	// disputedMarkerRe finds the literal "DISPUTED:" marker this
	// package's RED condition names explicitly (distinct from a bare
	// "**DISPUTED**" used elsewhere for a different purpose, e.g. a
	// litigation-status caveat unrelated to any of the fourteen items).
	disputedMarkerRe = regexp.MustCompile(`DISPUTED:`)
)

// ParseBlockingQuestions extracts the numbered "Blocking for a release"
// items from section 11 of the spec. It returns a "missing_section"
// violation and no items if section 11 cannot be found.
func ParseBlockingQuestions(spec string) ([]BlockingQuestion, []Violation) {
	loc := openQuestionsHeadingRe.FindStringIndex(spec)
	if loc == nil {
		return nil, []Violation{{"missing_section", "spec has no \"## 11. Open Questions\" heading"}}
	}
	rest := spec[loc[1]:]
	if end := nextHeadingRe.FindStringIndex(rest); end != nil {
		rest = rest[:end[0]]
	}

	blockingEnd := len(rest)
	if m := nonBlockingRe.FindStringIndex(rest); m != nil {
		blockingEnd = m[0]
	}
	blocking := rest[:blockingEnd]

	items := splitNumberedItems(blocking)
	if len(items) == 0 {
		return nil, []Violation{{"missing_section", "section 11 has no numbered \"Blocking for a release\" items"}}
	}
	return items, nil
}

// ParseReviewLog extracts the numbered items and their outcome keywords
// from the research README's LEGAL-018 contradiction-closure section. It
// returns a "missing_section" violation and no entries if that section
// cannot be found.
func ParseReviewLog(readme string) ([]ReviewLogEntry, []Violation) {
	loc := reviewLogHeadingRe.FindStringIndex(readme)
	if loc == nil {
		return nil, []Violation{{"missing_section", "research README has no LEGAL-018 contradiction-closure review-log heading"}}
	}
	rest := readme[loc[1]:]
	if end := nextHeadingRe.FindStringIndex(rest); end != nil {
		rest = rest[:end[0]]
	}

	items := splitNumberedItems(rest)
	if len(items) == 0 {
		return nil, []Violation{{"missing_section", "LEGAL-018 contradiction-closure section has no numbered items"}}
	}

	entries := make([]ReviewLogEntry, 0, len(items))
	for _, it := range items {
		outcomes := make(map[string]bool)
		for _, m := range outcomeRe.FindAllStringSubmatch(it.Text, -1) {
			outcomes[m[1]] = true
		}
		entries = append(entries, ReviewLogEntry{Number: it.Number, Text: it.Text, Outcomes: outcomes})
	}
	return entries, nil
}

// splitNumberedItems splits text into "N. **..." numbered items, each
// item's Text running up to (not including) the next numbered item or the
// end of text.
func splitNumberedItems(text string) []BlockingQuestion {
	locs := numberedItemRe.FindAllStringSubmatchIndex(text, -1)
	items := make([]BlockingQuestion, 0, len(locs))
	for i, loc := range locs {
		start := loc[0]
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		num, _ := strconv.Atoi(text[loc[2]:loc[3]])
		items = append(items, BlockingQuestion{Number: num, Text: strings.TrimSpace(text[start:end])})
	}
	return items
}

// CheckBlockingQuestionsTracked verifies every blocking item in section
// 11 has a review-log entry with the same number, and that entry carries
// at least one recognized outcome keyword (FIXED, DISPUTED or OPEN).
func CheckBlockingQuestionsTracked(spec, readme string) []Violation {
	blocking, v1 := ParseBlockingQuestions(spec)
	entries, v2 := ParseReviewLog(readme)
	if len(v1) > 0 || len(v2) > 0 {
		return append(v1, v2...)
	}

	byNumber := make(map[int]ReviewLogEntry, len(entries))
	for _, e := range entries {
		byNumber[e.Number] = e
	}

	var out []Violation
	for _, q := range blocking {
		e, ok := byNumber[q.Number]
		if !ok {
			out = append(out, Violation{"missing_review_log_entry", fmt.Sprintf("blocking item %d has no review-log entry", q.Number)})
			continue
		}
		if len(e.Outcomes) == 0 {
			out = append(out, Violation{"no_recognized_outcome", fmt.Sprintf("blocking item %d's review-log entry states no FIXED/DISPUTED/OPEN outcome", q.Number)})
		}
	}
	return out
}

// itemKind maps a blocking-item number to the single Section 5 matrix
// column its contradiction concerns, for items narrow enough to have one.
// Item 1 (the federal-baseline restatement question) is out of scope
// here: it is not a per-state matrix cell but the subject of the sibling
// federalbaseline package (LEGAL-017). Item 9 (Illinois's DRAFTED status)
// concerns the whole file, not one kind. Items 13 and 14 are corpus-wide
// completeness questions ("eighteen states", "forty-one files") naming no
// single state to gate.
var itemKind = map[int]string{
	2:  "FINAL_PAY",
	3:  "PERSONNEL_FILE",
	4:  "FIELD_RESTR",
	5:  "NON_COMPETE",
	6:  "PAY_TRANSP",
	7:  "PAY_TRANSP",
	8:  "RETENTION",
	10: "NOTICE",
	11: "LEAVE",
	12: "PERSONNEL_FILE",
}

// perStateOutcomeRe finds a "State Name: **OUTCOME**" sub-marker inside
// one review-log item's text, the format item 12 uses to break its mixed
// "FIXED for nine states; DISPUTED for one" outcome down by state (e.g.
// "New York: **DISPUTED**").
var perStateOutcomeRe = regexp.MustCompile(`([A-Z][A-Za-z .]{2,30}?):\s*\*\*(DISPUTED|OPEN)\*\*`)

// CheckDisputedItemOutcomesGated finds every per-state DISPUTED/OPEN
// sub-outcome named in the review log's blocking-item entries and
// verifies the matrix cell for that state and the item's mapped kind is
// "?" (not releasable above UNREVIEWED). An item whose overall outcome is
// a clean FIXED carries no such sub-marker and is not checked here - only
// a state explicitly still DISPUTED or OPEN must be gated.
func CheckDisputedItemOutcomesGated(spec, readme string) []Violation {
	entries, v := ParseReviewLog(readme)
	if len(v) > 0 {
		return v
	}
	tableA, tableB, cellViolations := parseCellTables(spec)
	if len(cellViolations) > 0 {
		return cellViolations
	}

	var out []Violation
	for _, e := range entries {
		kind, ok := itemKind[e.Number]
		if !ok {
			continue
		}
		for _, m := range perStateOutcomeRe.FindAllStringSubmatch(e.Text, -1) {
			code, ok := codeForStateName(m[1])
			if !ok {
				continue
			}
			cell, table := lookupCell(tableA, tableB, code, kind)
			if table == "" {
				continue // kind not a column in either table; nothing to gate
			}
			if leadToken(cell) != "?" {
				out = append(out, Violation{
					Kind:   "disputed_state_releasable",
					Detail: fmt.Sprintf("item %d: %s is %s for %s but table %s %s/%s = %q, want \"?\"", e.Number, m[1], m[2], kind, table, code, kind, cell),
				})
			}
		}
	}
	return out
}

// kindKeywords maps a lowercase keyword found near a "DISPUTED:" marker
// in a state file to the Section 5 column it identifies. Matching is
// tolerant and best-effort: a marker whose surrounding text matches none
// of these is skipped rather than guessed at.
var kindKeywords = []struct {
	keyword string
	kind    string
}{
	{"personnel file", "PERSONNEL_FILE"},
	{"final pay", "FINAL_PAY"},
	{"non-compete", "NON_COMPETE"},
	{"pay transparency", "PAY_TRANSP"},
	{"pay-range", "PAY_TRANSP"},
	{"pay statement", "PAY_STMT"},
	{"pay equity", "PAY_EQUITY"},
	{"pay frequency", "PAY_FREQ"},
	{"salary history", "FIELD_RESTR"},
	{"field restriction", "FIELD_RESTR"},
	{"wage floor", "WAGE_FLOOR"},
	{"minimum wage", "WAGE_FLOOR"},
	{"classification", "CLASSIFN"},
	{"retention", "RETENTION"},
	{"leave", "LEAVE"},
	{"mini-warn", "MINI_WARN"},
	{"separation filing", "SEP_FILING"},
	{"separation notice", "SEP_FILING"},
	{"drug test", "DRUG_TEST"},
	{"e-verify", "E_VERIFY"},
	{"breach", "BREACH"},
	{"automated decision", "AUTO_DEC"},
	{"job security", "JOB_SEC"},
	{"notice", "NOTICE"},
}

// kindNear returns the first kindKeywords match found in text, or "" if
// none match.
func kindNear(text string) string {
	lower := strings.ToLower(text)
	for _, kk := range kindKeywords {
		if strings.Contains(lower, kk.keyword) {
			return kk.kind
		}
	}
	return ""
}

// CheckDisputedMarkersGated scans every state file for the literal
// "DISPUTED:" marker and verifies the matrix does not mark that state's
// affected kind releasable above UNREVIEWED ("?"). The kind is inferred
// from keywords on the marker's own line, falling back to the two
// preceding lines, since the corpus states this marker inside a bolded
// pseudo-heading naming the topic (e.g. "Employee access to personnel
// files ... (DISPUTED: legislative status)"). A marker whose context
// matches no known kind, or whose file does not correspond to one of the
// fifty matrix state codes, is skipped rather than guessed at.
func CheckDisputedMarkersGated(spec string, stateFiles map[string]string) []Violation {
	tableA, tableB, cellViolations := parseCellTables(spec)
	if len(cellViolations) > 0 {
		return cellViolations
	}

	names := make([]string, 0, len(stateFiles))
	for name := range stateFiles {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []Violation
	for _, name := range names {
		code, ok := codeForFilename(name)
		if !ok {
			continue
		}
		content := stateFiles[name]
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if !disputedMarkerRe.MatchString(line) {
				continue
			}
			kind := kindNear(line)
			if kind == "" {
				start := i - 2
				if start < 0 {
					start = 0
				}
				kind = kindNear(strings.Join(lines[start:i], "\n"))
			}
			if kind == "" {
				continue
			}
			cell, table := lookupCell(tableA, tableB, code, kind)
			if table == "" {
				continue
			}
			if leadToken(cell) != "?" {
				out = append(out, Violation{
					Kind:   "disputed_marker_releasable",
					Detail: fmt.Sprintf("%s:%d: DISPUTED: marker for %s but table %s %s/%s = %q, want \"?\"", name, i+1, kind, table, code, kind, cell),
				})
			}
		}
	}
	return out
}

// queueRowRe matches one row of the research README's "## Queue" table:
// "| State | `file.md` | STATUS | date | date |".
var queueRowRe = regexp.MustCompile("(?m)^\\|\\s*[A-Za-z .]+\\s*\\|\\s*`([a-z0-9-]+\\.md)`\\s*\\|\\s*([A-Z_]+)\\s*\\|")

// CheckFileStatusMatchesReadme verifies every state research file's own
// H1 "**Status:**" field agrees with the Status cell the research
// README's Queue table states for that same file. The Queue table is
// what a release decision reads; a file whose own header still says
// DRAFTED while the table it is read through says REVIEWED (or vice
// versa) is a corpus inconsistency regardless of which one is right.
func CheckFileStatusMatchesReadme(readme string, stateFiles map[string]string) []Violation {
	var out []Violation
	for _, m := range queueRowRe.FindAllStringSubmatch(readme, -1) {
		file, tableStatus := m[1], m[2]
		content, ok := stateFiles[file]
		if !ok {
			continue
		}
		fm := fileStatusRe.FindStringSubmatch(content)
		if fm == nil {
			out = append(out, Violation{"missing_file_status", fmt.Sprintf("%s has no \"**Status:**\" field to compare against the README queue table", file)})
			continue
		}
		if fm[1] != tableStatus {
			out = append(out, Violation{"file_status_disagrees_with_readme", fmt.Sprintf("%s: file header states Status=%s, README queue table states Status=%s", file, fm[1], tableStatus)})
		}
	}
	return out
}

// Check runs every researchquestions checker and returns their combined,
// sorted violations.
func Check(spec, readme string, stateFiles map[string]string) []Violation {
	var violations []Violation
	violations = append(violations, CheckBlockingQuestionsTracked(spec, readme)...)
	violations = append(violations, CheckDisputedItemOutcomesGated(spec, readme)...)
	violations = append(violations, CheckDisputedMarkersGated(spec, stateFiles)...)
	violations = append(violations, CheckFileStatusMatchesReadme(readme, stateFiles)...)

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Kind != violations[j].Kind {
			return violations[i].Kind < violations[j].Kind
		}
		return violations[i].Detail < violations[j].Detail
	})
	return violations
}
