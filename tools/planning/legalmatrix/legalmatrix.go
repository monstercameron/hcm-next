// Package legalmatrix parses and validates the fifty-state configuration
// matrix in planning/specs/legal-rule-packs-and-state-configuration.md
// (LEGAL-008 / GOV-004 family): every row must key to one of the fifty
// state research files under planning/research/state-employment-law, every
// cell must hold a Section 5 legend value, and both column-total blocks
// (Table A and Table B) must recompute exactly from the cells. It also
// confirms the spec is indexed in specs/README.md and carries an
// accountable role and review trigger in
// specs/specification-ownership-registry.md.
//
// This package does no file I/O of its own: callers (tools/planning's
// plancheck subcommand) read the spec, the research directory listing and
// the two registry files and pass their content/filenames in, which keeps
// the parser trivially unit-testable and independent of the caller's
// directory-walking strategy.
package legalmatrix

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Violation is one concrete defect found in the matrix or its
// registration. Kind is a stable, machine-readable category; Detail names
// the offending row/column/cell in prose.
type Violation struct {
	Kind   string
	Detail string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s", v.Kind, v.Detail)
}

// legendValues are the only values Section 5 permits in a matrix cell. A
// cell's leading whitespace-separated token is compared against this set;
// Table B appends free-form annotations after the legend token (e.g. "Y
// 45d", "L pub", "Y all") and those annotations are not validated.
var legendValues = map[string]bool{
	"Y": true,
	"L": true,
	"P": true,
	"F": true,
	"?": true,
}

// stateFile maps the matrix's two-letter state code to the base filename
// (without extension) of its research file under
// planning/research/state-employment-law. Matrix rows are keyed to these
// fifty files; there is no fifty-first (District of Columbia and
// territories are out of scope for this matrix).
var stateFile = map[string]string{
	"AL": "alabama", "AK": "alaska", "AZ": "arizona", "AR": "arkansas",
	"CA": "california", "CO": "colorado", "CT": "connecticut", "DE": "delaware",
	"FL": "florida", "GA": "georgia", "HI": "hawaii", "ID": "idaho",
	"IL": "illinois", "IN": "indiana", "IA": "iowa", "KS": "kansas",
	"KY": "kentucky", "LA": "louisiana", "ME": "maine", "MD": "maryland",
	"MA": "massachusetts", "MI": "michigan", "MN": "minnesota", "MS": "mississippi",
	"MO": "missouri", "MT": "montana", "NE": "nebraska", "NV": "nevada",
	"NH": "new-hampshire", "NJ": "new-jersey", "NM": "new-mexico", "NY": "new-york",
	"NC": "north-carolina", "ND": "north-dakota", "OH": "ohio", "OK": "oklahoma",
	"OR": "oregon", "PA": "pennsylvania", "RI": "rhode-island", "SC": "south-carolina",
	"SD": "south-dakota", "TN": "tennessee", "TX": "texas", "UT": "utah",
	"VT": "vermont", "VA": "virginia", "WA": "washington", "WV": "west-virginia",
	"WI": "wisconsin", "WY": "wyoming",
}

// tableAAliases maps each Table A (Section 5.1) column header to the label
// its column-total block states the count under. Most columns keep their
// header spelling; a few headers are abbreviated relative to the totals
// prose.
var tableAAliases = map[string]string{
	"NOTICE":         "NOTICE",
	"PAY_TRANSP":     "PAY_TRANSPARENCY",
	"FIELD_RESTR":    "FIELD_RESTRICTION",
	"WAGE_FLOOR":     "WAGE_FLOOR",
	"PAY_FREQ":       "PAY_FREQUENCY",
	"PAY_STMT":       "PAY_STATEMENT",
	"LEAVE":          "LEAVE",
	"NON_COMPETE":    "NON_COMPETE",
	"CLASSIFN":       "CLASSIFICATION",
	"PAY_EQUITY":     "PAY_EQUITY",
	"RETENTION":      "RETENTION",
	"PERSONNEL_FILE": "PERSONNEL_FILE",
}

// tableBAliases is the Table B (Section 5.2) equivalent of tableAAliases.
// The LOCAL column is deliberately absent: it recomputes into two totals
// ("LOCAL overlay" from Y+L, "LOCAL preempted" from P) and is handled by
// checkLocalTotals instead of the single-alias path.
var tableBAliases = map[string]string{
	"FINAL_PAY":  "FINAL_PAY_DEADLINE",
	"MINI_WARN":  "MINI_WARN",
	"SEP_FILING": "SEPARATION_FILING",
	"E_VERIFY":   "E_VERIFY",
	"DRUG_TEST":  "DRUG_TEST",
	"ANTI_RETAL": "ANTI_RETALIATION",
	"JOB_SEC":    "JOB_SECURITY",
	"BREACH":     "BREACH",
	"AUTO_DEC":   "AUTOMATED_DECISION",
}

// localColumn is the Table B column with two-part totals rather than one.
const localColumn = "LOCAL"

// specFilename is the basename this package validates the registration of
// in specs/README.md and specs/specification-ownership-registry.md.
const specFilename = "legal-rule-packs-and-state-configuration.md"

// rowData is one parsed matrix row: a state code plus its cell text keyed
// by column header.
type rowData struct {
	state string
	cells map[string]string
	line  int // 1-based line number within the section passed to parseTable
}

// parsedTable is one parsed Section 5 table (Table A or Table B).
type parsedTable struct {
	columns []string
	rows    []rowData
}

// Matrix is the parsed form of Section 5: both tables and both
// column-total blocks. Fields are nil when the corresponding section or
// block could not be found or parsed; Check reports that as a violation
// rather than dereferencing a nil table.
type Matrix struct {
	tableA, tableB   *parsedTable
	totalsA, totalsB map[string]int
}

// Parse extracts Table A, Table B and both column-total blocks from the
// spec's Markdown text. It never panics: on any malformed or missing
// section it records a Violation and leaves the corresponding Matrix field
// nil (or the totals map nil) rather than failing outright, so a caller
// gets every independent problem in one pass instead of stopping at the
// first one.
func Parse(spec string) (*Matrix, []Violation) {
	var violations []Violation
	m := &Matrix{}

	secA, okA := section(spec, "### 5.1 Table A", "### 5.2 Table B")
	if !okA {
		violations = append(violations, Violation{"missing_section", "spec has no ### 5.1 Table A section"})
	} else {
		t, err := parseTable(secA)
		if err != nil {
			violations = append(violations, Violation{"unparseable_table", "table A: " + err.Error()})
		} else {
			m.tableA = t
		}
		m.totalsA = parseTotals(secA)
		if m.totalsA == nil {
			violations = append(violations, Violation{"missing_totals", "table A has no Column totals block"})
		}
	}

	secB, okB := section(spec, "### 5.2 Table B", "## 6")
	if !okB {
		secB, okB = section(spec, "### 5.2 Table B", "")
	}
	if !okB {
		violations = append(violations, Violation{"missing_section", "spec has no ### 5.2 Table B section"})
	} else {
		t, err := parseTable(secB)
		if err != nil {
			violations = append(violations, Violation{"unparseable_table", "table B: " + err.Error()})
		} else {
			m.tableB = t
		}
		m.totalsB = parseTotals(secB)
		if m.totalsB == nil {
			violations = append(violations, Violation{"missing_totals", "table B has no Column totals block"})
		}
	}

	return m, violations
}

// section returns the text strictly between the first occurrence of start
// and the following occurrence of end (or to the end of the string when
// end is empty or not found), and whether start was found at all.
func section(spec, start, end string) (string, bool) {
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

// parseTable extracts the first Markdown pipe-table found in text: a
// header row, a "|---|---|" separator row, then data rows until the first
// line that is not a pipe row.
func parseTable(text string) (*parsedTable, error) {
	lines := strings.Split(text, "\n")

	headerLine := -1
	sepLine := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, "|") {
			if headerLine != -1 && sepLine == -1 {
				// A non-pipe line between a candidate header and its
				// separator means that candidate wasn't a real header;
				// keep scanning for another one.
				headerLine = -1
			}
			continue
		}
		if headerLine == -1 {
			headerLine = i
			continue
		}
		if sepLine == -1 {
			if isSeparatorRow(t) {
				sepLine = i
				break
			}
			headerLine = -1
		}
	}
	if headerLine == -1 || sepLine == -1 {
		return nil, fmt.Errorf("no markdown table found")
	}

	headerCells := splitRow(lines[headerLine])
	if len(headerCells) < 2 {
		return nil, fmt.Errorf("table header has too few columns")
	}
	columns := make([]string, 0, len(headerCells)-1)
	for _, c := range headerCells[1:] {
		columns = append(columns, strings.TrimSpace(c))
	}

	pt := &parsedTable{columns: columns}
	for i := sepLine + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, "|") {
			break
		}
		cells := splitRow(lines[i])
		if len(cells) < 2 {
			continue
		}
		state := strings.TrimSpace(cells[0])
		if state == "" {
			continue
		}
		row := rowData{state: state, cells: make(map[string]string, len(columns)), line: i + 1}
		for j, col := range columns {
			if j+1 < len(cells) {
				row.cells[col] = strings.TrimSpace(cells[j+1])
			} else {
				row.cells[col] = ""
			}
		}
		pt.rows = append(pt.rows, row)
	}
	return pt, nil
}

// isSeparatorRow reports whether an already-trimmed line is a Markdown
// table separator row: made up only of "|", "-", ":" and spaces.
func isSeparatorRow(t string) bool {
	if !strings.HasPrefix(t, "|") {
		return false
	}
	hasDash := false
	for _, r := range t {
		switch r {
		case '|', ' ', ':':
		case '-':
			hasDash = true
		default:
			return false
		}
	}
	return hasDash
}

// splitRow splits a Markdown pipe-table row into its cell texts, dropping
// the leading/trailing "|".
func splitRow(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	return strings.Split(t, "|")
}

// parseTotals extracts the fenced ```text ... ``` block that follows the
// first "Column totals" occurrence in text and tokenizes it into a label ->
// count map. A label may be more than one word (e.g. "LOCAL overlay"): the
// tokenizer treats every non-numeric run of whitespace-separated tokens
// preceding a numeric token as one label, so it does not depend on how
// many label/count pairs share a line. Returns nil when no "Column totals"
// block is found.
func parseTotals(text string) map[string]int {
	idx := strings.Index(text, "Column totals")
	if idx == -1 {
		return nil
	}
	rest := text[idx:]

	f1 := strings.Index(rest, "```")
	if f1 == -1 {
		return nil
	}
	rest = rest[f1+3:]
	if nl := strings.IndexByte(rest, '\n'); nl != -1 {
		firstLine := strings.TrimSpace(rest[:nl])
		if firstLine == "" || isLanguageTag(firstLine) {
			rest = rest[nl+1:]
		}
	}

	f2 := strings.Index(rest, "```")
	if f2 == -1 {
		return nil
	}
	block := rest[:f2]

	tokens := strings.Fields(block)
	totals := make(map[string]int)
	i := 0
	for i < len(tokens) {
		var labelParts []string
		for i < len(tokens) {
			if _, err := strconv.Atoi(tokens[i]); err == nil {
				break
			}
			labelParts = append(labelParts, tokens[i])
			i++
		}
		if i >= len(tokens) || len(labelParts) == 0 {
			break
		}
		n, err := strconv.Atoi(tokens[i])
		if err != nil {
			break
		}
		totals[strings.Join(labelParts, " ")] = n
		i++
	}
	return totals
}

// isLanguageTag reports whether a fenced-code-block opening line is a bare
// language tag (e.g. "text") rather than content, so parseTotals can skip
// it without also swallowing a genuine content line on a language-less
// fence.
func isLanguageTag(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// Check parses spec and cross-checks it against the fifty research-file
// basenames in researchFiles (e.g. "alabama.md"), the specs/README.md
// content and the specs/specification-ownership-registry.md content.
// Returned violations are sorted by (Kind, Detail) for deterministic
// output.
func Check(spec string, researchFiles []string, specsReadme string, ownershipRegistry string) []Violation {
	m, violations := Parse(spec)

	if m.tableA != nil {
		violations = append(violations, checkRows("A", m.tableA, researchFiles)...)
		violations = append(violations, checkCells("A", m.tableA)...)
		if m.totalsA != nil {
			violations = append(violations, checkTotals("A", m.tableA, m.totalsA, tableAAliases)...)
		}
	}
	if m.tableB != nil {
		violations = append(violations, checkRows("B", m.tableB, researchFiles)...)
		violations = append(violations, checkCells("B", m.tableB)...)
		if m.totalsB != nil {
			violations = append(violations, checkTotals("B", m.tableB, m.totalsB, tableBAliases)...)
		}
	}

	violations = append(violations, checkResearchCoverage(m, researchFiles)...)
	violations = append(violations, checkSpecRegistration(specsReadme, ownershipRegistry)...)

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].Kind != violations[j].Kind {
			return violations[i].Kind < violations[j].Kind
		}
		return violations[i].Detail < violations[j].Detail
	})
	return violations
}

// checkRows validates that every row names a recognized state code with a
// research file, that no state repeats within the table, and that the
// table has exactly fifty rows.
func checkRows(label string, t *parsedTable, researchFiles []string) []Violation {
	var out []Violation

	have := make(map[string]bool, len(researchFiles))
	for _, f := range researchFiles {
		have[strings.TrimSuffix(f, ".md")] = true
	}

	seen := make(map[string]int)
	for _, row := range t.rows {
		seen[row.state]++
		slug, known := stateFile[row.state]
		if !known {
			out = append(out, Violation{"unknown_state", fmt.Sprintf("table %s row %q (line %d): not a recognized state code", label, row.state, row.line)})
			continue
		}
		if !have[slug] {
			out = append(out, Violation{"missing_research_file", fmt.Sprintf("table %s row %s (line %d): no research file %s.md", label, row.state, row.line, slug)})
		}
	}
	for state, n := range seen {
		if n > 1 {
			out = append(out, Violation{"duplicate_row", fmt.Sprintf("table %s: state %s appears %d times", label, state, n)})
		}
	}
	if len(t.rows) != 50 {
		out = append(out, Violation{"row_count", fmt.Sprintf("table %s has %d rows, want 50", label, len(t.rows))})
	}
	return out
}

// checkCells validates that every cell's leading token is a Section 5
// legend value.
func checkCells(label string, t *parsedTable) []Violation {
	var out []Violation
	for _, row := range t.rows {
		for _, col := range t.columns {
			if !legendValues[leadToken(row.cells[col])] {
				out = append(out, Violation{"bad_cell", fmt.Sprintf("table %s %s/%s (line %d): %q is not a legend value", label, row.state, col, row.line, row.cells[col])})
			}
		}
	}
	return out
}

// leadToken returns the first whitespace-separated token of a cell's raw
// text (the legend value), or the whole trimmed text if there is no
// following annotation.
func leadToken(raw string) string {
	if sp := strings.IndexByte(raw, ' '); sp != -1 {
		return raw[:sp]
	}
	return raw
}

// checkTotals recomputes each column's Y+L count and compares it against
// the stated total under that column's alias, except the LOCAL column
// (present only in aliases-less Table B), which checkLocalTotals handles.
func checkTotals(label string, t *parsedTable, totals map[string]int, aliases map[string]string) []Violation {
	var out []Violation
	for _, col := range t.columns {
		if col == localColumn {
			out = append(out, checkLocalTotals(label, t, totals)...)
			continue
		}
		alias, ok := aliases[col]
		if !ok {
			out = append(out, Violation{"unmapped_column", fmt.Sprintf("table %s column %s has no known column-total label", label, col)})
			continue
		}
		want, ok := totals[alias]
		if !ok {
			out = append(out, Violation{"missing_total", fmt.Sprintf("table %s: no stated total for %s (column %s)", label, alias, col)})
			continue
		}
		if got := countLead(t, col, "Y", "L"); got != want {
			out = append(out, Violation{"bad_total", fmt.Sprintf("table %s column %s: stated %s=%d, counted %d", label, col, alias, want, got)})
		}
	}
	return out
}

// checkLocalTotals recomputes the LOCAL column's two totals: "LOCAL
// overlay" from Y+L cells and "LOCAL preempted" from P cells.
func checkLocalTotals(label string, t *parsedTable, totals map[string]int) []Violation {
	var out []Violation

	if want, ok := totals["LOCAL overlay"]; !ok {
		out = append(out, Violation{"missing_total", fmt.Sprintf("table %s: no stated total for LOCAL overlay", label)})
	} else if got := countLead(t, localColumn, "Y", "L"); got != want {
		out = append(out, Violation{"bad_total", fmt.Sprintf("table %s column LOCAL: stated LOCAL overlay=%d, counted %d", label, want, got)})
	}

	if want, ok := totals["LOCAL preempted"]; !ok {
		out = append(out, Violation{"missing_total", fmt.Sprintf("table %s: no stated total for LOCAL preempted", label)})
	} else if got := countLead(t, localColumn, "P"); got != want {
		out = append(out, Violation{"bad_total", fmt.Sprintf("table %s column LOCAL: stated LOCAL preempted=%d, counted %d", label, want, got)})
	}

	return out
}

// countLead counts the rows whose column cell's lead token is one of
// leads.
func countLead(t *parsedTable, col string, leads ...string) int {
	want := make(map[string]bool, len(leads))
	for _, l := range leads {
		want[l] = true
	}
	n := 0
	for _, row := range t.rows {
		if want[leadToken(row.cells[col])] {
			n++
		}
	}
	return n
}

// checkResearchCoverage reports research files with no corresponding
// matrix row, keying off Table A when present (falling back to Table B),
// since both tables are keyed to the same fifty states. Files that do not
// match any of the fifty known state slugs (e.g. a federal-baseline
// reference doc living alongside the per-state files) are outside the
// matrix's per-state scope by definition and are silently skipped rather
// than flagged: the matrix only ever claims to cover the fifty states, not
// every file in the research directory.
func checkResearchCoverage(m *Matrix, researchFiles []string) []Violation {
	var out []Violation

	t := m.tableA
	if t == nil {
		t = m.tableB
	}
	if t == nil {
		return out
	}

	present := make(map[string]bool, len(t.rows))
	for _, row := range t.rows {
		if slug, ok := stateFile[row.state]; ok {
			present[slug] = true
		}
	}

	for _, f := range researchFiles {
		slug := strings.TrimSuffix(f, ".md")
		if !isKnownSlug(slug) {
			continue
		}
		if !present[slug] {
			out = append(out, Violation{"orphan_research_file", fmt.Sprintf("research file %s has no matrix row", f)})
		}
	}
	return out
}

// isKnownSlug reports whether slug is one of the fifty state-file slugs.
func isKnownSlug(slug string) bool {
	for _, s := range stateFile {
		if s == slug {
			return true
		}
	}
	return false
}

// checkSpecRegistration validates that the spec is indexed in
// specs/README.md and carries an accountable role and a review trigger in
// specs/specification-ownership-registry.md.
func checkSpecRegistration(specsReadme, ownershipRegistry string) []Violation {
	var out []Violation

	if !strings.Contains(specsReadme, specFilename) {
		out = append(out, Violation{"missing_readme_index", fmt.Sprintf("specs/README.md does not reference %s", specFilename)})
	}

	found := false
	needle := "`" + specFilename + "`"
	for _, line := range strings.Split(ownershipRegistry, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		found = true

		var fields []string
		for _, f := range strings.Split(line, "|") {
			f = strings.TrimSpace(f)
			if f != "" {
				fields = append(fields, f)
			}
		}
		// Expected columns: filename, role, status, review trigger.
		if len(fields) < 4 {
			out = append(out, Violation{"incomplete_ownership_row", fmt.Sprintf("ownership registry row for %s is missing role/status/trigger columns", specFilename)})
			break
		}
		if isPlaceholder(fields[1]) {
			out = append(out, Violation{"missing_owner", fmt.Sprintf("ownership registry row for %s names no accountable role", specFilename)})
		}
		if isPlaceholder(fields[3]) {
			out = append(out, Violation{"missing_review_trigger", fmt.Sprintf("ownership registry row for %s names no review trigger", specFilename)})
		}
		break
	}
	if !found {
		out = append(out, Violation{"missing_ownership_row", fmt.Sprintf("specification-ownership-registry.md has no row for %s", specFilename)})
	}

	return out
}

// isPlaceholder reports whether a registry field is empty or a known
// placeholder rather than a real value.
func isPlaceholder(s string) bool {
	switch strings.ToLower(strings.Trim(s, "`")) {
	case "", "tbd", "todo", "n/a", "none", "unassigned":
		return true
	}
	return false
}
