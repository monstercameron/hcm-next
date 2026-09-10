package todogovernance

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

// Record is one todo augmented with the raw markdown detail that
// todoregistry.Todo does not preserve: the unfiltered Depends text (needed
// to detect prose dependencies, since todoregistry.ParseTodos keeps only the
// backtick-wrapped ID tokens), the raw INTENT CONTEXT key/value fields, and
// every Evidence-labeled line in the todo's block (todoregistry only
// recognizes two hard-coded Evidence field spellings).
type Record struct {
	Todo todoregistry.Todo

	// RawDepends is the trimmed field value that followed "- **Depends:**"
	// before any backtick-token filtering, or "" if the todo had no Depends
	// line (which ParseTodos would already have rejected as malformed).
	RawDepends string

	// IntentContextRaw is the backtick-quoted content of the INTENT CONTEXT
	// field, or "" if absent.
	IntentContextRaw string

	// IntentContext is IntentContextRaw parsed as "KEY=VALUE" segments split
	// on ";". Values are trimmed; a trailing "." on the whole field and the
	// closing "`" are already stripped before this split.
	IntentContext map[string]string

	// TestMatrixRaw is the unparsed TEST MATRIX value. It preserves the
	// UNIT_ONLY(reason=...) spelling, which the registry's map representation
	// cannot retain because it splits every token at the first equals sign.
	TestMatrixRaw string

	// EvidenceLines holds the full text of every line in the block whose
	// trimmed form starts with "- **Evidence" (any date/qualifier suffix),
	// in document order.
	EvidenceLines []string

	// HasCombinedRedGreen is true when the block declares a single
	// "RED/GREEN" field instead of separate RED and GREEN fields. This is
	// prohibited: it cannot prove the test failed before implementation.
	HasCombinedRedGreen bool
}

// LoadRegistry reads and unmarshals the compiled todo registry JSON produced
// by `go run ./tools/planning/cmd/todoregistry` (definitions/planning/todo-registry.json).
func LoadRegistry(path string) ([]todoregistry.Todo, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read registry %s: %w", path, err)
	}
	var todos []todoregistry.Todo
	if err := json.Unmarshal(content, &todos); err != nil {
		return nil, fmt.Errorf("parse registry %s: %w", path, err)
	}
	return todos, nil
}

// ParseRecords parses raw markdown content directly (no file I/O), returning
// one Record per successfully parsed todo plus any todoregistry parse
// errors. This is the entry point fixture-based tests use; LoadMarkdown
// wraps it for the real on-disk corpus.
func ParseRecords(content string) (records []Record, parseErrs []error) {
	todos, errs := todoregistry.ParseTodos(content)
	return BuildRecords(content, todos), errs
}

// TodosFromRecords projects the todoregistry.Todo out of each record, in
// the same order.
func TodosFromRecords(records []Record) []todoregistry.Todo {
	todos := make([]todoregistry.Todo, len(records))
	for i, r := range records {
		todos[i] = r.Todo
	}
	return todos
}

// RawDependsByID maps each record's todo ID to its raw (unfiltered) Depends
// field text, for use with ValidateDependencies.
func RawDependsByID(records []Record) map[string]string {
	m := make(map[string]string, len(records))
	for _, r := range records {
		m[r.Todo.ID] = r.RawDepends
	}
	return m
}

// LoadMarkdown reads planning/todos.md, parses it with todoregistry, and
// builds one Record per successfully parsed todo plus the raw markdown
// detail. It returns the raw content (for callers that want to re-derive
// something not modeled here), the records, and any todoregistry parse
// errors (structural violations - missing/duplicate required fields -
// surface as GOV-017 findings rather than aborting the load).
func LoadMarkdown(path string) (content string, records []Record, parseErrs []error, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, nil, fmt.Errorf("read markdown %s: %w", path, err)
	}
	content = string(raw)

	records, errs := ParseRecords(content)
	return content, records, errs, nil
}

// BuildRecords augments already-parsed todos with their raw block detail.
// todos must have accurate Line fields (as returned by todoregistry.ParseTodos
// against the same content).
func BuildRecords(content string, todos []todoregistry.Todo) []Record {
	lines := strings.Split(content, "\n")

	sorted := make([]todoregistry.Todo, len(todos))
	copy(sorted, todos)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Line < sorted[j].Line })

	records := make([]Record, len(sorted))
	for i, t := range sorted {
		start := t.Line // Line is 1-based; lines[start] is the line AFTER the title (0-based index == 1-based title line number).
		end := len(lines)
		if i+1 < len(sorted) {
			end = sorted[i+1].Line - 1
		}
		block := lines[clampInt(start, 0, len(lines)):clampInt(end, 0, len(lines))]

		rec := Record{Todo: t}
		rec.RawDepends = extractField(block, "Depends")
		rec.TestMatrixRaw = extractField(block, "TEST MATRIX")
		if ic := extractBacktickField(block, "INTENT CONTEXT"); ic != "" {
			rec.IntentContextRaw = ic
			rec.IntentContext = parseKV(ic)
		}
		rec.EvidenceLines = extractEvidenceLines(block)
		rec.HasCombinedRedGreen = hasField(block, "RED/GREEN")
		records[i] = rec
	}
	return records
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// extractField returns the trimmed value text following
// "- **<name>:**" in block, with a single trailing period stripped, or ""
// if the field is absent. Unlike todoregistry's cleanFieldValue this keeps
// the full value including any backtick-wrapped tokens and connecting
// prose, since callers need the unfiltered text.
func extractField(block []string, name string) string {
	prefix := "- **" + name + ":**"
	for _, l := range block {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		value = strings.TrimSuffix(value, ".")
		return strings.TrimSpace(value)
	}
	return ""
}

// extractBacktickField returns the content between the first pair of
// backticks following "- **<name>:**" on one line, or "" if absent.
func extractBacktickField(block []string, name string) string {
	prefix := "- **" + name + ":**"
	for _, l := range block {
		trimmed := strings.TrimSpace(l)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
		start := strings.IndexByte(rest, '`')
		if start == -1 {
			return ""
		}
		end := strings.IndexByte(rest[start+1:], '`')
		if end == -1 {
			return ""
		}
		return rest[start+1 : start+1+end]
	}
	return ""
}

// hasField reports whether block contains a "- **<name>:**" field line.
func hasField(block []string, name string) bool {
	prefix := "- **" + name + ":**"
	for _, l := range block {
		if strings.HasPrefix(strings.TrimSpace(l), prefix) {
			return true
		}
	}
	return false
}

// extractEvidenceLines returns every line in block whose trimmed text
// starts with "- **Evidence" (matching any date/qualifier suffix such as
// "Evidence (2026-09-05)" or "Evidence (partial, 2026-09-03)").
func extractEvidenceLines(block []string) []string {
	var out []string
	for _, l := range block {
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "- **Evidence") {
			out = append(out, trimmed)
		}
	}
	return out
}

// parseKV splits a "KEY=VALUE; KEY=VALUE; ..." string into a map. Values
// are trimmed; malformed segments without "=" are skipped.
func parseKV(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return out
}
