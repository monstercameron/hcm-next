// Package workflowarchetypes owns the WF-DISC-002 catalog contract. It parses
// the six HR workflow catalogs, proves every row names a known archetype with
// explicit dependencies, data and step deltas, rejects duplicate intent
// placement across catalogs, and renders a deterministic reconciled report. It
// is kernel-pure: file parsing and sorting only, no database, network or
// mutable global.
package workflowarchetypes

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// CatalogRow is one reconciled catalog intent mapping.
type CatalogRow struct {
	Catalog      string   `json:"catalog"`
	Intent       string   `json:"intent"`
	Archetypes   []string `json:"archetypes"`
	Dependencies string   `json:"dependencies"`
	Data         string   `json:"data"`
	Delta        string   `json:"delta"`
}

// CatalogCount reconciles one catalog file.
type CatalogCount struct {
	Catalog string `json:"catalog"`
	Rows    int    `json:"rows"`
}

// Finding is one exact catalog diagnostic, located by catalog file and intent.
type Finding struct {
	Catalog string `json:"catalog"`
	Intent  string `json:"intent,omitempty"`
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Detail  string `json:"detail"`
}

// Report is the complete reconciliation result.
type Report struct {
	Catalogs []CatalogCount `json:"catalogs"`
	Rows     []CatalogRow   `json:"rows"`
	Findings []Finding      `json:"findings,omitempty"`
	Digest   string         `json:"digest"`
}

// OK reports whether the report holds no findings.
func (r Report) OK() bool { return len(r.Findings) == 0 }

var archetypeHeader = regexp.MustCompile(`^##\s+(A[0-9]+|D[0-9]+)\b`)

// KnownArchetypes is the closed archetype vocabulary: A1-A20 plus D1.
func KnownArchetypes() map[string]bool {
	known := map[string]bool{"D1": true}
	for index := 1; index <= 20; index++ {
		known[fmt.Sprintf("A%d", index)] = true
	}
	return known
}

// LoadArchetypes parses the archetype definition document and returns the
// declared archetype identifiers.
func LoadArchetypes(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workflowarchetypes: read archetype definitions: %w", err)
	}
	defined := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if match := archetypeHeader.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			defined[match[1]] = true
		}
	}
	if len(defined) == 0 {
		return nil, errors.New("workflowarchetypes: no archetype definitions found")
	}
	return defined, nil
}

// ScanCatalogs parses the catalog files below root and reconciles every row.
// Catalog paths are slash-relative to root. A missing catalog file is an
// error; a malformed row is a finding, never a silent skip.
func ScanCatalogs(root string, catalogs []string) (Report, error) {
	if strings.TrimSpace(root) == "" {
		return Report{}, errors.New("workflowarchetypes: scan root is empty")
	}
	if len(catalogs) == 0 {
		return Report{}, errors.New("workflowarchetypes: no catalogs listed")
	}
	report := Report{}
	known := KnownArchetypes()
	for _, catalog := range catalogs {
		slash := filepath.ToSlash(catalog)
		rows, malformed, err := parseCatalog(filepath.Join(root, filepath.FromSlash(catalog)), slash)
		if err != nil {
			return Report{}, err
		}
		report.Rows = append(report.Rows, rows...)
		report.Findings = append(report.Findings, malformed...)
		report.Catalogs = append(report.Catalogs, CatalogCount{Catalog: slash, Rows: len(rows)})
	}
	sort.Slice(report.Rows, func(i, j int) bool {
		if report.Rows[i].Catalog != report.Rows[j].Catalog {
			return report.Rows[i].Catalog < report.Rows[j].Catalog
		}
		return report.Rows[i].Intent < report.Rows[j].Intent
	})
	checkRows(&report, known)
	report.sortFindings()
	report.Digest = digestReport(report.Rows)
	return report, nil
}

func parseCatalog(path, catalog string) ([]CatalogRow, []Finding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("workflowarchetypes: read %s: %w", catalog, err)
	}
	var rows []CatalogRow
	var malformed []Finding
	headerWidth := 0
	headerSeen := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := splitCells(trimmed)
		if !headerSeen {
			if !hasHeader(cells) {
				return nil, nil, fmt.Errorf("workflowarchetypes: %s has no intent catalog header", catalog)
			}
			headerWidth = len(cells)
			headerSeen = true
			continue
		}
		if isSeparator(cells) {
			continue
		}
		if hasHeader(cells) {
			continue
		}
		if len(cells) != headerWidth {
			malformed = append(malformed, Finding{Catalog: catalog, Intent: cellAt(cells, 0), Code: "MALFORMED_ROW", Detail: "row does not match the catalog column count"})
			continue
		}
		current := CatalogRow{Catalog: catalog, Intent: cellAt(cells, 0)}
		current.Archetypes = parseArchetypes(cellAt(cells, 1))
		if headerWidth == 5 {
			current.Dependencies = cellAt(cells, 2)
			current.Data = cellAt(cells, 3)
			current.Delta = cellAt(cells, 4)
		} else {
			current.Dependencies = cellAt(cells, 2)
			current.Data = cellAt(cells, 2)
			current.Delta = cellAt(cells, 3)
		}
		rows = append(rows, current)
	}
	if !headerSeen {
		return nil, nil, fmt.Errorf("workflowarchetypes: %s has no intent catalog header", catalog)
	}
	return rows, malformed, nil
}

func splitCells(line string) []string {
	parts := strings.Split(line, "|")
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	cells := make([]string, 0, len(parts))
	for _, part := range parts {
		cells = append(cells, strings.TrimSpace(part))
	}
	return cells
}

func hasHeader(cells []string) bool {
	joined := strings.ToLower(strings.Join(cells, " "))
	return strings.Contains(joined, "intent") && strings.Contains(joined, "arch")
}

func isSeparator(cells []string) bool {
	if len(cells) == 0 {
		return true
	}
	matched := regexp.MustCompile(`^:?-{3,}:?$`)
	for _, cell := range cells {
		if !matched.MatchString(cell) {
			return false
		}
	}
	return true
}

func cellAt(cells []string, index int) string {
	if index < 0 || index >= len(cells) {
		return ""
	}
	return cells[index]
}

func parseArchetypes(cell string) []string {
	var out []string
	for _, token := range strings.FieldsFunc(cell, func(r rune) bool { return r == '/' || r == ',' }) {
		token = strings.TrimSpace(token)
		if token != "" {
			out = append(out, token)
		}
	}
	return out
}

func checkRows(report *Report, known map[string]bool) {
	placements := map[string][]string{}
	for _, row := range report.Rows {
		if row.Intent != "" {
			placements[row.Intent] = append(placements[row.Intent], row.Catalog)
		}
	}
	for _, row := range report.Rows {
		if row.Intent == "" {
			report.add(Finding{Catalog: row.Catalog, Code: "EMPTY_REQUIRED_CELL", Field: "intent", Detail: "catalog row names no intent"})
			continue
		}
		if len(row.Archetypes) == 0 {
			report.add(Finding{Catalog: row.Catalog, Intent: row.Intent, Code: "EMPTY_REQUIRED_CELL", Field: "arch", Detail: "catalog row names no archetype"})
		}
		for _, arch := range row.Archetypes {
			if !known[arch] {
				report.add(Finding{Catalog: row.Catalog, Intent: row.Intent, Code: "UNKNOWN_ARCHETYPE", Field: "arch", Detail: "archetype " + arch + " is not defined"})
			}
		}
		if row.Dependencies == "" {
			report.add(Finding{Catalog: row.Catalog, Intent: row.Intent, Code: "EMPTY_REQUIRED_CELL", Field: "dependencies", Detail: "catalog row states no dependencies"})
		}
		if row.Data == "" {
			report.add(Finding{Catalog: row.Catalog, Intent: row.Intent, Code: "EMPTY_REQUIRED_CELL", Field: "data", Detail: "catalog row states no data"})
		}
		if row.Delta == "" {
			report.add(Finding{Catalog: row.Catalog, Intent: row.Intent, Code: "EMPTY_REQUIRED_CELL", Field: "delta", Detail: "catalog row states no step delta"})
		}
		if catalogs := placements[row.Intent]; len(catalogs) > 1 {
			report.add(Finding{Catalog: row.Catalog, Intent: row.Intent, Code: "DUPLICATE_INTENT_PLACEMENT", Detail: "intent is placed in more than one catalog"})
		}
	}
}

func (r *Report) add(finding Finding) {
	r.Findings = append(r.Findings, finding)
}

func (r *Report) sortFindings() {
	sort.Slice(r.Findings, func(i, j int) bool {
		if r.Findings[i].Catalog != r.Findings[j].Catalog {
			return r.Findings[i].Catalog < r.Findings[j].Catalog
		}
		if r.Findings[i].Intent != r.Findings[j].Intent {
			return r.Findings[i].Intent < r.Findings[j].Intent
		}
		return r.Findings[i].Code < r.Findings[j].Code
	})
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
}

func digestReport(rows []CatalogRow) string {
	var lines []string
	for _, row := range rows {
		lines = append(lines, row.Catalog+"\x00"+row.Intent+"\x00"+strings.Join(row.Archetypes, ",")+"\x00"+row.Delta)
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
