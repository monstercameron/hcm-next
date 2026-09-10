// Package docintegrity checks the planning Markdown corpus for resolvable
// documents, links, anchors and todo references (DOC-001).
package docintegrity

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	CodeInvalidUTF8          = "INVALID_UTF8"
	CodeUnbalancedFence      = "UNBALANCED_FENCE"
	CodeInvalidASCIIBlock    = "INVALID_ASCII_BLOCK"
	CodeMissingDocument      = "MISSING_DOCUMENT"
	CodeBrokenLink           = "BROKEN_LINK"
	CodeMissingAnchor        = "MISSING_ANCHOR"
	CodeUnknownTodo          = "UNKNOWN_TODO"
	CodeDuplicateNormativeID = "DUPLICATE_NORMATIVE_ID"
	CodeOrphanDocument       = "ORPHAN_DOCUMENT"
)

// Finding is a stable document-integrity diagnostic.
type Finding struct {
	Code        string
	Path        string
	Line        int
	Target      string
	Detail      string
	Owner       string
	Allowlisted bool
}

func (f Finding) Key() string {
	return f.Code + "|" + f.Path + "|" + f.Target + "|" + f.Detail
}

func (f Finding) String() string {
	location := f.Path
	if f.Line > 0 {
		location = fmt.Sprintf("%s:%d", location, f.Line)
	}
	if f.Target != "" {
		return fmt.Sprintf("%s: %s: %s (%s)", location, f.Code, f.Detail, f.Target)
	}
	return fmt.Sprintf("%s: %s: %s", location, f.Code, f.Detail)
}

// Report is the complete deterministic result of a planning-corpus scan.
type Report struct {
	Findings           []Finding
	NormativeDocuments []string
	TodoReferences     []string
	OrphanCount        int
	AllowlistedOrphans int
}

func (r Report) Violations() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if !f.Allowlisted {
			out = append(out, f)
		}
	}
	return out
}

// KnownOrphanOwners is the reviewed DOC-001 baseline for planning documents
// that are intentionally source material but are not linked by the current
// normative plan/spec index. Paths use slash separators relative to the repo.
// A new orphan is never silently accepted: it is returned as an unallowlisted
// finding until an owner is deliberately added here.
var KnownOrphanOwners = func() map[string]string {
	owners := map[string]string{
		"planning/reviewer-ledger-004.md":  "planning-review",
		"planning/test_coverage_nested.md": "test-governance",
		"planning/test_coverage_root.md":   "test-governance",
	}
	for _, state := range []string{
		"alabama", "alaska", "arizona", "arkansas", "california", "colorado", "connecticut", "delaware",
		"florida", "georgia", "hawaii", "idaho", "illinois", "indiana", "iowa", "kansas", "kentucky",
		"louisiana", "maine", "maryland", "massachusetts", "michigan", "minnesota", "mississippi",
		"missouri", "montana", "nebraska", "nevada", "new-hampshire", "new-jersey", "new-mexico",
		"new-york", "north-carolina", "north-dakota", "ohio", "oklahoma", "oregon", "pennsylvania",
		"rhode-island", "south-carolina", "south-dakota", "tennessee", "texas", "us-federal", "utah",
		"vermont", "virginia", "washington", "west-virginia", "wisconsin", "wyoming",
	} {
		owners["planning/research/state-employment-law/"+state+".md"] = "legal-research"
	}
	return owners
}()

// EvaluateRepository loads todo IDs from registryPath and scans root/planning.
func EvaluateRepository(root, registryPath string) (Report, error) {
	ids, err := LoadTodoIDs(registryPath)
	if err != nil {
		return Report{}, err
	}
	return Evaluate(root, ids, KnownOrphanOwners)
}

// LoadTodoIDs reads the generated planning registry without depending on its
// generator package, keeping this policy usable as a standalone command.
func LoadTodoIDs(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read todo registry %s: %w", path, err)
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parse todo registry %s: %w", path, err)
	}
	ids := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row.ID != "" {
			ids[row.ID] = true
		}
	}
	return ids, nil
}

// Evaluate scans Markdown beneath root/planning. todoIDs is the authoritative
// generated registry identity set; orphanOwners maps slash-normalized paths
// to accountable owners.
func Evaluate(root string, todoIDs map[string]bool, orphanOwners map[string]string) (Report, error) {
	planning := filepath.Join(root, "planning")
	files, err := markdownFiles(planning)
	if err != nil {
		return Report{}, err
	}
	relFiles := make(map[string]string, len(files))
	for _, path := range files {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return Report{}, fmt.Errorf("relative path for %s: %w", path, err)
		}
		relFiles[slashPath(rel)] = path
	}

	report := Report{}
	anchors := map[string]map[string]bool{}
	for rel, path := range relFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			return Report{}, fmt.Errorf("read %s: %w", path, err)
		}
		anchors[rel] = headingAnchors(string(data))
		report.Findings = append(report.Findings, documentSyntaxFindings(rel, data)...)
	}

	rootDocs := map[string]bool{"planning/plan.md": true}
	for rel := range relFiles {
		if strings.HasPrefix(rel, "planning/specs/") {
			rootDocs[rel] = true
			report.NormativeDocuments = append(report.NormativeDocuments, rel)
		}
	}
	sort.Strings(report.NormativeDocuments)

	referenced := map[string]bool{}
	for rel, path := range relFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			return Report{}, fmt.Errorf("read %s: %w", path, err)
		}
		for _, link := range markdownLinks(string(data)) {
			line := lineNumber(string(data), link.Offset)
			target, fragment := splitTarget(link.Target)
			if isExternalTarget(target) {
				continue
			}
			// DOC-001 governs Markdown documents and their anchors. Image,
			// binary and other asset references are owned by their respective
			// design/tooling checks and are not Markdown links.
			if target != "" && !strings.HasSuffix(strings.ToLower(target), ".md") {
				continue
			}
			resolved, resolvedRel, exists := resolveTarget(root, rel, target, relFiles)
			if target == "" {
				resolved = relFiles[rel]
				resolvedRel = rel
				exists = true
			}
			if !exists {
				code := CodeBrokenLink
				if isNormativeSource(rel) {
					code = CodeMissingDocument
				}
				report.Findings = append(report.Findings, Finding{Code: code, Path: rel, Line: line, Target: link.Target, Detail: "intra-repository target does not exist"})
				continue
			}
			if strings.HasSuffix(strings.ToLower(resolvedRel), ".md") {
				referenced[resolvedRel] = true
			}
			if fragment != "" && !anchors[resolvedRel][fragment] {
				report.Findings = append(report.Findings, Finding{Code: CodeMissingAnchor, Path: rel, Line: line, Target: link.Target, Detail: "anchor does not exist in target document"})
			}
			_ = resolved
		}
		if isNormativeSource(rel) {
			for _, ref := range todoReferences(string(data)) {
				report.TodoReferences = append(report.TodoReferences, ref)
				if !todoIDs[ref] {
					report.Findings = append(report.Findings, Finding{Code: CodeUnknownTodo, Path: rel, Line: lineNumber(string(data), strings.Index(string(data), ref)), Target: ref, Detail: "todo ID is absent from the generated registry"})
				}
			}
		}
	}
	sort.Strings(report.TodoReferences)
	report.TodoReferences = unique(report.TodoReferences)

	for rel := range relFiles {
		if rootDocs[rel] || referenced[rel] {
			continue
		}
		report.OrphanCount++
		owner := orphanOwners[rel]
		allowlisted := owner != ""
		if allowlisted {
			report.AllowlistedOrphans++
		}
		report.Findings = append(report.Findings, Finding{Code: CodeOrphanDocument, Path: rel, Detail: "Markdown file is not reachable from the normative plan/spec index", Owner: owner, Allowlisted: allowlisted})
	}
	normativeIDs := map[string][]string{}
	for rel, path := range relFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			return Report{}, fmt.Errorf("read %s: %w", path, err)
		}
		for _, id := range normativeIDRe.FindAllString(string(data), -1) {
			normativeIDs[id] = append(normativeIDs[id], rel)
		}
	}
	for id, paths := range normativeIDs {
		if len(paths) < 2 {
			continue
		}
		sort.Strings(paths)
		for _, rel := range paths {
			report.Findings = append(report.Findings, Finding{Code: CodeDuplicateNormativeID, Path: rel, Target: id, Detail: "normative identifier is declared in multiple documents"})
		}
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Path != report.Findings[j].Path {
			return report.Findings[i].Path < report.Findings[j].Path
		}
		if report.Findings[i].Line != report.Findings[j].Line {
			return report.Findings[i].Line < report.Findings[j].Line
		}
		return report.Findings[i].Code < report.Findings[j].Code
	})
	return report, nil
}

type markdownLink struct {
	Target string
	Offset int
}

var markdownLinkRe = regexp.MustCompile(`!?\[[^\]\n]*\]\(([^)\n]+)\)`)
var todoIDRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+\b`)
var normativeIDRe = regexp.MustCompile(`\b(?:REQ|NORM|OBL)-[A-Z0-9-]+\b`)

func markdownLinks(content string) []markdownLink {
	var out []markdownLink
	for _, match := range markdownLinkRe.FindAllStringSubmatchIndex(content, -1) {
		out = append(out, markdownLink{Target: strings.TrimSpace(content[match[2]:match[3]]), Offset: match[0]})
	}
	return out
}

func splitTarget(raw string) (string, string) {
	raw = strings.TrimSpace(strings.Trim(raw, "<>"))
	fragment := ""
	if index := strings.IndexByte(raw, '#'); index >= 0 {
		fragment = normalizeAnchor(raw[index+1:])
		raw = raw[:index]
	}
	if index := strings.IndexAny(raw, "? "); index >= 0 {
		raw = raw[:index]
	}
	decoded, err := url.PathUnescape(raw)
	if err == nil {
		raw = decoded
	}
	return raw, fragment
}

func isExternalTarget(target string) bool {
	if target == "" || strings.HasPrefix(target, "#") {
		return false
	}
	return strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:")
}

func resolveTarget(root, source, target string, files map[string]string) (string, string, bool) {
	if target == "" {
		return files[source], source, true
	}
	base := filepath.Dir(filepath.Join(root, filepath.FromSlash(source)))
	candidate := filepath.Clean(filepath.Join(base, filepath.FromSlash(target)))
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", "", false
	}
	rel = slashPath(rel)
	if _, ok := files[rel]; ok {
		return candidate, rel, true
	}
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		for _, index := range []string{"README.md", "index.md"} {
			indexPath := filepath.Join(candidate, index)
			if _, err := os.Stat(indexPath); err == nil {
				indexRel := slashPath(filepath.Join(rel, index))
				return indexPath, indexRel, true
			}
		}
	}
	return candidate, rel, false
}

func todoReferences(content string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ref := range todoIDRe.FindAllString(content, -1) {
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	sort.Strings(out)
	return out
}

func headingAnchors(content string) map[string]bool {
	anchors := map[string]bool{}
	counts := map[string]int{}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<a ") {
			for _, attr := range []string{"id=\"", "name=\""} {
				if start := strings.Index(trimmed, attr); start >= 0 {
					value := trimmed[start+len(attr):]
					if end := strings.IndexByte(value, '"'); end >= 0 {
						anchors[normalizeAnchor(value[:end])] = true
					}
				}
			}
		}
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		title := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		title = strings.TrimRight(title, "#")
		base := normalizeAnchor(title)
		if base == "" {
			continue
		}
		counts[base]++
		anchor := base
		if counts[base] > 1 {
			anchor = fmt.Sprintf("%s-%d", base, counts[base]-1)
		}
		anchors[anchor] = true
	}
	return anchors
}

func normalizeAnchor(value string) string {
	var b strings.Builder
	separator := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' {
			if separator && b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			separator = false
			continue
		}
		if r == '—' || r == '–' {
			if b.Len() > 0 {
				b.WriteByte('-')
			}
			continue
		}
		if unicode.IsSpace(r) && b.Len() > 0 {
			separator = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func documentSyntaxFindings(rel string, data []byte) []Finding {
	var findings []Finding
	if !utf8.Valid(data) {
		findings = append(findings, Finding{Code: CodeInvalidUTF8, Path: rel, Detail: "document is not valid UTF-8"})
		return findings
	}
	lines := strings.Split(string(data), "\n")
	open := false
	openLine := 0
	marker := ""
	asciiBlock := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if open {
			if strings.HasPrefix(trimmed, marker) {
				open = false
				openLine = 0
				marker = ""
				asciiBlock = false
				continue
			}
			if asciiBlock && !isASCII(line) {
				findings = append(findings, Finding{Code: CodeInvalidASCIIBlock, Path: rel, Line: i + 1, Detail: "text-ascii fenced block contains a non-ASCII character"})
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker = trimmed[:3]
			asciiBlock = strings.EqualFold(strings.TrimSpace(trimmed[3:]), "text-ascii")
			open = true
			openLine = i + 1
		}
	}
	if open {
		findings = append(findings, Finding{Code: CodeUnbalancedFence, Path: rel, Line: openLine, Detail: "fenced block is not closed"})
	}
	return findings
}

func isASCII(line string) bool {
	for _, r := range line {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

func markdownFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk planning Markdown: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func isNormativeSource(rel string) bool {
	return rel == "planning/plan.md" || strings.HasPrefix(rel, "planning/specs/")
}

func lineNumber(content string, offset int) int {
	if offset < 0 {
		return 0
	}
	return 1 + strings.Count(content[:offset], "\n")
}

func slashPath(path string) string { return filepath.ToSlash(filepath.Clean(path)) }

func unique(items []string) []string {
	if len(items) < 2 {
		return items
	}
	out := items[:1]
	for _, item := range items[1:] {
		if item != out[len(out)-1] {
			out = append(out, item)
		}
	}
	return out
}
