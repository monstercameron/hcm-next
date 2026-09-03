// Package vocab loads the canonical kernel-primitive vocabulary directly
// from planning/specs/workflow-runtime.md's "Kernel Vocabulary" tables,
// rather than duplicating the ten core primitives, three structural
// primitives, and four retired names by hand. Re-deriving the vocabulary
// from the authoritative spec on every run means a future edit to that
// table changes what CONF-001 checks against without a second, driftable
// copy to keep in sync.
package vocab

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Primitive is one row of either the core or the structural primitive
// table: a name, its runtime meaning, and the phase column value ("First
// release" for core primitives, "Gate" for structural primitives).
type Primitive struct {
	Name    string
	Meaning string
	Phase   string
}

// RetiredMapping is one row of the "Retired name" table: a name earlier
// drafts used as a primitive/step type, and what it is expressed as under
// the normative ten-plus-three vocabulary.
type RetiredMapping struct {
	Name        string
	ExpressedAs string
}

// Vocabulary is the parsed Kernel Vocabulary section.
type Vocabulary struct {
	SourcePath string
	Core       []Primitive
	Structural []Primitive
	Retired    []RetiredMapping
}

// Load reads and parses the Kernel Vocabulary tables from the
// workflow-runtime.md spec at specPath.
func Load(specPath string) (*Vocabulary, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("vocab: read %s: %w", specPath, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")

	coreRows, err := extractTable(lines, "Primitive", "First release")
	if err != nil {
		return nil, fmt.Errorf("vocab: core primitive table: %w", err)
	}
	structuralRows, err := extractTable(lines, "Structural", "Gate")
	if err != nil {
		return nil, fmt.Errorf("vocab: structural primitive table: %w", err)
	}
	retiredRows, err := extractTable(lines, "Retired name", "Expressed as")
	if err != nil {
		return nil, fmt.Errorf("vocab: retired name table: %w", err)
	}

	v := &Vocabulary{SourcePath: specPath}
	for _, row := range coreRows {
		if len(row) < 3 {
			return nil, fmt.Errorf("vocab: core primitive row has %d cells, want 3: %v", len(row), row)
		}
		v.Core = append(v.Core, Primitive{Name: unbacktick(row[0]), Meaning: row[1], Phase: row[2]})
	}
	for _, row := range structuralRows {
		if len(row) < 3 {
			return nil, fmt.Errorf("vocab: structural primitive row has %d cells, want 3: %v", len(row), row)
		}
		v.Structural = append(v.Structural, Primitive{Name: unbacktick(row[0]), Meaning: row[1], Phase: row[2]})
	}
	for _, row := range retiredRows {
		if len(row) < 2 {
			return nil, fmt.Errorf("vocab: retired name row has %d cells, want 2: %v", len(row), row)
		}
		v.Retired = append(v.Retired, RetiredMapping{Name: unbacktick(row[0]), ExpressedAs: row[1]})
	}

	sort.Slice(v.Core, func(i, j int) bool { return v.Core[i].Name < v.Core[j].Name })
	sort.Slice(v.Structural, func(i, j int) bool { return v.Structural[i].Name < v.Structural[j].Name })
	sort.Slice(v.Retired, func(i, j int) bool { return v.Retired[i].Name < v.Retired[j].Name })

	return v, nil
}

// CoreNames returns the sorted names of the core (P1A/P1B) primitives.
func (v *Vocabulary) CoreNames() []string {
	return primitiveNames(v.Core)
}

// StructuralNames returns the sorted names of the structural primitives.
func (v *Vocabulary) StructuralNames() []string {
	return primitiveNames(v.Structural)
}

// AllPrimitiveNames returns the sorted union of core and structural
// primitive names (the full thirteen-name normative vocabulary).
func (v *Vocabulary) AllPrimitiveNames() []string {
	names := append(v.CoreNames(), v.StructuralNames()...)
	sort.Strings(names)
	return names
}

// RetiredNames returns the sorted names retired from the vocabulary
// (CHECKPOINT, RULE, AGENT, DOCUMENT as of the current spec).
func (v *Vocabulary) RetiredNames() []string {
	names := make([]string, 0, len(v.Retired))
	for _, r := range v.Retired {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	return names
}

// IsCore reports whether name is one of the core primitives.
func (v *Vocabulary) IsCore(name string) bool {
	for _, p := range v.Core {
		if p.Name == name {
			return true
		}
	}
	return false
}

// IsStructural reports whether name is one of the structural primitives.
func (v *Vocabulary) IsStructural(name string) bool {
	for _, p := range v.Structural {
		if p.Name == name {
			return true
		}
	}
	return false
}

// ExpressedAs returns the retired-name mapping's "Expressed as" text for
// name, or "" if name is not a retired name.
func (v *Vocabulary) ExpressedAs(name string) string {
	for _, r := range v.Retired {
		if r.Name == name {
			return r.ExpressedAs
		}
	}
	return ""
}

func primitiveNames(ps []Primitive) []string {
	names := make([]string, 0, len(ps))
	for _, p := range ps {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return names
}

func unbacktick(s string) string {
	return strings.Trim(strings.TrimSpace(s), "`")
}

// splitRow splits one "| a | b | c |" markdown table row into trimmed
// cells, dropping the leading/trailing empty cells the boundary pipes
// produce.
func splitRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	parts := strings.Split(line, "|")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

// isSeparatorRow reports whether every cell of a split table row is a
// markdown header/body separator (only '-' and ':' characters).
func isSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if c == "" {
			return false
		}
		for _, r := range c {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

// extractTable finds the markdown table whose header row's first cell is
// firstHeader and last cell is lastHeader, and returns its data rows (the
// header and separator rows excluded).
func extractTable(lines []string, firstHeader, lastHeader string) ([][]string, error) {
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := splitRow(trimmed)
		if len(cells) == 0 || cells[0] != firstHeader || cells[len(cells)-1] != lastHeader {
			continue
		}
		if i+1 >= len(lines) || !isSeparatorRow(splitRow(lines[i+1])) {
			return nil, fmt.Errorf("table %q: missing separator row after header at line %d", firstHeader, i+1)
		}
		var rows [][]string
		for j := i + 2; j < len(lines); j++ {
			rowTrimmed := strings.TrimSpace(lines[j])
			if !strings.HasPrefix(rowTrimmed, "|") {
				break
			}
			rows = append(rows, splitRow(rowTrimmed))
		}
		if len(rows) == 0 {
			return nil, fmt.Errorf("table %q: no data rows found after line %d", firstHeader, i+1)
		}
		return rows, nil
	}
	return nil, fmt.Errorf("table %q: header row not found", firstHeader)
}
