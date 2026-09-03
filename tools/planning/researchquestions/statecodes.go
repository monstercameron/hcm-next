package researchquestions

import (
	"regexp"
	"strings"
)

// stateCode maps a state's full name to its two-letter matrix code. This
// mirrors legalmatrix's private stateFile map (inverted: name instead of
// file slug) because that map is not exported; see the package doc
// comment for why a small duplicate is unavoidable here.
var stateCode = map[string]string{
	"Alabama": "AL", "Alaska": "AK", "Arizona": "AZ", "Arkansas": "AR",
	"California": "CA", "Colorado": "CO", "Connecticut": "CT", "Delaware": "DE",
	"Florida": "FL", "Georgia": "GA", "Hawaii": "HI", "Idaho": "ID",
	"Illinois": "IL", "Indiana": "IN", "Iowa": "IA", "Kansas": "KS",
	"Kentucky": "KY", "Louisiana": "LA", "Maine": "ME", "Maryland": "MD",
	"Massachusetts": "MA", "Michigan": "MI", "Minnesota": "MN", "Mississippi": "MS",
	"Missouri": "MO", "Montana": "MT", "Nebraska": "NE", "Nevada": "NV",
	"New Hampshire": "NH", "New Jersey": "NJ", "New Mexico": "NM", "New York": "NY",
	"North Carolina": "NC", "North Dakota": "ND", "Ohio": "OH", "Oklahoma": "OK",
	"Oregon": "OR", "Pennsylvania": "PA", "Rhode Island": "RI", "South Carolina": "SC",
	"South Dakota": "SD", "Tennessee": "TN", "Texas": "TX", "Utah": "UT",
	"Vermont": "VT", "Virginia": "VA", "Washington": "WA", "West Virginia": "WV",
	"Wisconsin": "WI", "Wyoming": "WY",
}

// filenameCode maps a state research file's basename to its two-letter
// matrix code, derived from stateCode by the same slug rule legalmatrix
// uses (lowercase, spaces to hyphens).
var filenameCode = func() map[string]string {
	m := make(map[string]string, len(stateCode))
	for name, code := range stateCode {
		slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
		m[slug+".md"] = code
	}
	return m
}()

// codeForStateName resolves a full or partial state name (as found in
// review-log prose, e.g. "New York" from "New York: **DISPUTED**") to its
// two-letter code.
func codeForStateName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if code, ok := stateCode[name]; ok {
		return code, true
	}
	// The regex capture can include a trailing word from adjacent prose
	// (e.g. a leading "and " from a list); try progressively trimming
	// leading words rather than requiring an exact match.
	words := strings.Fields(name)
	for i := range words {
		if code, ok := stateCode[strings.Join(words[i:], " ")]; ok {
			return code, true
		}
	}
	return "", false
}

// codeForFilename resolves a state research file's basename to its
// two-letter matrix code.
func codeForFilename(name string) (string, bool) {
	code, ok := filenameCode[name]
	return code, ok
}

// leadToken returns the first whitespace-separated token of a matrix
// cell's raw text (the legend value), mirroring legalmatrix's leadToken:
// Table B appends free-form annotations after the legend token (e.g. "Y
// 45d") that are not part of the value being compared.
func leadToken(raw string) string {
	raw = strings.TrimSpace(raw)
	if sp := strings.IndexByte(raw, ' '); sp != -1 {
		return raw[:sp]
	}
	return raw
}

// cellTable is a minimal read of one Section 5 table: columns and, per
// state code, the raw cell text under each column.
type cellTable struct {
	columns []string
	rows    map[string]map[string]string
}

var (
	pipeRowRe  = regexp.MustCompile(`(?m)^\|.*\|\s*$`)
	sepCellsRe = regexp.MustCompile(`^[\s|:-]+$`)
)

// parseCellTables extracts Table A and Table B's cells from the spec.
// Violations are returned only when a table cannot be located at all;
// legalmatrix.Check (run separately, unmodified, over the same spec) is
// the authority on cell-content and total-count validity - this parser
// exists solely to answer "what is this cell" for a state+column pair
// already known to be interesting.
func parseCellTables(spec string) (tableA, tableB cellTable, violations []Violation) {
	secA, okA := sectionBetween(spec, "### 5.1 Table A", "### 5.2 Table B")
	if !okA {
		violations = append(violations, Violation{"missing_section", "spec has no ### 5.1 Table A section"})
	} else {
		tableA = parseOneCellTable(secA)
	}

	secB, okB := sectionBetween(spec, "### 5.2 Table B", "## 6")
	if !okB {
		secB, okB = sectionBetween(spec, "### 5.2 Table B", "")
	}
	if !okB {
		violations = append(violations, Violation{"missing_section", "spec has no ### 5.2 Table B section"})
	} else {
		tableB = parseOneCellTable(secB)
	}

	return tableA, tableB, violations
}

// sectionBetween returns the text strictly between the first occurrence
// of start and the following occurrence of end (or to the end of the
// string when end is empty or not found).
func sectionBetween(spec, start, end string) (string, bool) {
	si := strings.Index(spec, start)
	if si == -1 {
		return "", false
	}
	rest := spec[si+len(start):]
	if end != "" {
		if ei := strings.Index(rest, end); ei != -1 {
			return rest[:ei], true
		}
	}
	return rest, true
}

// parseOneCellTable extracts the first Markdown pipe-table in text: a
// header row, a separator row, then data rows keyed by the row's first
// cell (the state code).
func parseOneCellTable(text string) cellTable {
	t := cellTable{rows: make(map[string]map[string]string)}
	lines := pipeRowRe.FindAllString(text, -1)
	if len(lines) < 2 {
		return t
	}

	header := splitCells(lines[0])
	if len(header) < 2 {
		return t
	}
	t.columns = make([]string, len(header)-1)
	for i, column := range header[1:] {
		t.columns[i] = strings.TrimSpace(column)
	}

	dataLines := lines[1:]
	if sepCellsRe.MatchString(strings.Join(splitCells(lines[1]), "")) {
		dataLines = lines[2:]
	}

	for _, line := range dataLines {
		cells := splitCells(line)
		if len(cells) < 2 {
			continue
		}
		state := strings.TrimSpace(cells[0])
		if state == "" {
			continue
		}
		row := make(map[string]string, len(t.columns))
		for i, col := range t.columns {
			if i+1 < len(cells) {
				row[col] = strings.TrimSpace(cells[i+1])
			}
		}
		t.rows[state] = row
	}
	return t
}

// splitCells splits one Markdown pipe-table row into its cell texts.
func splitCells(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	return strings.Split(s, "|")
}

// lookupCell returns the raw cell text for state+kind, searching Table A
// then Table B, and which table it was found in ("A", "B", or "" if the
// kind is not a column of either).
func lookupCell(tableA, tableB cellTable, state, kind string) (cell, table string) {
	if row, ok := tableA.rows[state]; ok {
		if _, isCol := colIndex(tableA.columns, kind); isCol {
			return row[kind], "A"
		}
	}
	if row, ok := tableB.rows[state]; ok {
		if _, isCol := colIndex(tableB.columns, kind); isCol {
			return row[kind], "B"
		}
	}
	return "", ""
}

func colIndex(columns []string, name string) (int, bool) {
	for i, c := range columns {
		if c == name {
			return i, true
		}
	}
	return 0, false
}
