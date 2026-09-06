// Package obligations registers uppercase normative planning sentences with
// stable identities derived from their source document and normalized text.
package obligations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const registryVersion = 1

var normativeWordRE = regexp.MustCompile(`\b(?:MUST NOT|MUST|SHALL|NEVER|ALWAYS)\b`)
var todoIDRE = regexp.MustCompile(`\b[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+\b`)

// Obligation is one registered normative planning sentence.
type Obligation struct {
	ID                 string   `json:"id"`
	Document           string   `json:"document"`
	Line               int      `json:"line"`
	TextDigest         string   `json:"text_digest"`
	TodoIDsCitedNearby []string `json:"todo_ids_cited_nearby"`
	Lineage            []string `json:"lineage,omitempty"`
}

// Registry is the deterministic JSON document emitted by the obligations
// generator.
type Registry struct {
	Version     int          `json:"version"`
	Obligations []Obligation `json:"obligations"`
}

// Scan finds uppercase normative sentences in planning/plan.md and all
// Markdown files below planning/specs. It never writes to the planning tree.
func Scan(root string) (Registry, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Registry{}, fmt.Errorf("resolve repository root: %w", err)
	}
	paths, err := sourcePaths(root)
	if err != nil {
		return Registry{}, err
	}
	var all []Obligation
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return Registry{}, fmt.Errorf("read %s: %w", path, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return Registry{}, fmt.Errorf("relative path for %s: %w", path, err)
		}
		all = append(all, scanDocument(filepath.ToSlash(rel), string(data))...)
	}
	sortObligations(all)
	return Registry{Version: registryVersion, Obligations: all}, nil
}

// ScanDocument scans one document content string. It is useful for focused
// fixtures and keeps the extraction logic independent from filesystem state.
func ScanDocument(document, content string) []Obligation {
	items := scanDocument(filepath.ToSlash(filepath.Clean(document)), content)
	sortObligations(items)
	return items
}

// AttachLineage links a current obligation to the prior obligation at the
// same document and line when its normalized text changed. If line wrapping
// moved, a unique nearby todo-ID match is used as a safe fallback.
func AttachLineage(current, previous Registry) Registry {
	previousByLocation := make(map[string][]Obligation)
	for _, item := range previous.Obligations {
		key := item.Document + "\x00" + fmt.Sprint(item.Line)
		previousByLocation[key] = append(previousByLocation[key], item)
	}

	for i := range current.Obligations {
		item := &current.Obligations[i]
		var prior *Obligation
		key := item.Document + "\x00" + fmt.Sprint(item.Line)
		if candidates := previousByLocation[key]; len(candidates) == 1 {
			prior = &candidates[0]
		} else {
			prior = nearbyPrior(*item, previous.Obligations)
		}
		if prior == nil || prior.ID == item.ID {
			continue
		}
		item.Lineage = appendUnique(item.Lineage, prior.ID)
		sort.Strings(item.Lineage)
	}
	return current
}

// ValidateLineage reports changed obligations that do not link their prior
// identity. An empty result is a passing lineage check.
func ValidateLineage(current, previous Registry) []string {
	currentByLocation := make(map[string]Obligation)
	for _, item := range current.Obligations {
		currentByLocation[item.Document+"\x00"+fmt.Sprint(item.Line)] = item
	}
	var findings []string
	for _, prior := range previous.Obligations {
		currentItem, ok := currentByLocation[prior.Document+"\x00"+fmt.Sprint(prior.Line)]
		if !ok || currentItem.ID == prior.ID {
			continue
		}
		if !contains(currentItem.Lineage, prior.ID) {
			findings = append(findings, fmt.Sprintf("%s:%d changed obligation %s has no lineage link to %s", currentItem.Document, currentItem.Line, currentItem.ID, prior.ID))
		}
	}
	sort.Strings(findings)
	return findings
}

// JSON returns canonical indented JSON with a trailing newline.
func (r Registry) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal obligations registry: %w", err)
	}
	return append(data, '\n'), nil
}

// LoadJSON reads a previously emitted registry for lineage comparison.
func LoadJSON(path string) (Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Registry{}, fmt.Errorf("read prior obligations registry %s: %w", path, err)
	}
	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return Registry{}, fmt.Errorf("parse prior obligations registry %s: %w", path, err)
	}
	return registry, nil
}

func sourcePaths(root string) ([]string, error) {
	plan := filepath.Join(root, "planning", "plan.md")
	if _, err := os.Stat(plan); err != nil {
		return nil, fmt.Errorf("stat %s: %w", plan, err)
	}
	paths := []string{plan}
	specs := filepath.Join(root, "planning", "specs")
	err := filepath.WalkDir(specs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk planning specs: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}

type sourceLine struct {
	line int
	text string
}

func scanDocument(document, content string) []Obligation {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	var paragraphs [][]sourceLine
	var paragraph []sourceLine
	fenced := false
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if isFence(trimmed) {
			if len(paragraph) != 0 {
				paragraphs = append(paragraphs, paragraph)
				paragraph = nil
			}
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if trimmed == "" {
			if len(paragraph) != 0 {
				paragraphs = append(paragraphs, paragraph)
				paragraph = nil
			}
			continue
		}
		paragraph = append(paragraph, sourceLine{line: i + 1, text: normalizeLine(raw)})
	}
	if len(paragraph) != 0 {
		paragraphs = append(paragraphs, paragraph)
	}

	var out []Obligation
	for _, paragraph := range paragraphs {
		text, starts, sourceLines := joinParagraph(paragraph)
		for _, sentence := range sentenceParts(text) {
			if !normativeWordRE.MatchString(sentence.text) {
				continue
			}
			canonical := NormalizeSentence(sentence.text)
			if canonical == "" {
				continue
			}
			line := lineAtOffset(starts, sourceLines, sentence.start)
			digest := digestText(canonical)
			out = append(out, Obligation{
				ID:                 requirementID(document, canonical),
				Document:           document,
				Line:               line,
				TextDigest:         digest,
				TodoIDsCitedNearby: nearbyTodoIDs(lines, line),
			})
		}
	}
	return out
}

// NormalizeSentence is the canonical text normalization used for identity
// and digest computation.
func NormalizeSentence(sentence string) string {
	sentence = strings.TrimSpace(sentence)
	for strings.HasPrefix(sentence, "-") || strings.HasPrefix(sentence, "*") || strings.HasPrefix(sentence, ">") {
		sentence = strings.TrimSpace(sentence[1:])
	}
	return strings.Join(strings.Fields(sentence), " ")
}

type sentencePart struct {
	start int
	text  string
}

func sentenceParts(text string) []sentencePart {
	var parts []sentencePart
	start := 0
	for i, r := range text {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		next := i + len(string(r))
		if next < len(text) && !unicode.IsSpace(rune(text[next])) {
			continue
		}
		part := strings.TrimSpace(text[start:next])
		if part != "" {
			parts = append(parts, sentencePart{start: start, text: part})
		}
		start = next
	}
	if tail := strings.TrimSpace(text[start:]); tail != "" {
		parts = append(parts, sentencePart{start: start, text: tail})
	}
	return parts
}

func joinParagraph(lines []sourceLine) (string, []int, []int) {
	var b strings.Builder
	starts := make([]int, 0, len(lines))
	sourceLines := make([]int, 0, len(lines))
	for _, line := range lines {
		if line.text == "" {
			continue
		}
		if b.Len() != 0 {
			b.WriteByte(' ')
		}
		starts = append(starts, b.Len())
		sourceLines = append(sourceLines, line.line)
		b.WriteString(line.text)
	}
	return b.String(), starts, sourceLines
}

func lineAtOffset(starts []int, sourceLines []int, offset int) int {
	if len(sourceLines) == 0 {
		return 0
	}
	for i := len(starts) - 1; i >= 0; i-- {
		if offset >= starts[i] {
			return sourceLines[i]
		}
	}
	return sourceLines[0]
}

func nearbyTodoIDs(lines []string, line int) []string {
	seen := map[string]bool{}
	lo, hi := line-3, line+1
	if lo < 0 {
		lo = 0
	}
	if hi > len(lines) {
		hi = len(lines)
	}
	for _, raw := range lines[lo:hi] {
		for _, id := range todoIDRE.FindAllString(raw, -1) {
			seen[id] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func isFence(line string) bool {
	return strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~")
}

func normalizeLine(line string) string {
	line = strings.TrimSpace(line)
	for {
		old := line
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if strings.HasPrefix(line, ">") {
			line = strings.TrimSpace(line[1:])
		}
		if len(line) >= 2 && (strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "+ ")) {
			line = strings.TrimSpace(line[2:])
		}
		if line == old {
			return line
		}
	}
}

func requirementID(document, canonical string) string {
	h := sha256.Sum256([]byte(document + "\n" + canonical))
	return "REQ-" + strings.ToUpper(hex.EncodeToString(h[:]))
}

func digestText(canonical string) string {
	h := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(h[:])
}

func nearbyPrior(current Obligation, previous []Obligation) *Obligation {
	var matches []Obligation
	for _, candidate := range previous {
		if candidate.Document != current.Document || !overlap(candidate.TodoIDsCitedNearby, current.TodoIDsCitedNearby) {
			continue
		}
		matches = append(matches, candidate)
	}
	if len(matches) != 1 {
		return nil
	}
	return &matches[0]
}

func overlap(a, b []string) bool {
	for _, left := range a {
		for _, right := range b {
			if left == right {
				return true
			}
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	if contains(values, value) {
		return values
	}
	return append(values, value)
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func sortObligations(items []Obligation) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Document != items[j].Document {
			return items[i].Document < items[j].Document
		}
		if items[i].Line != items[j].Line {
			return items[i].Line < items[j].Line
		}
		return items[i].ID < items[j].ID
	})
}
