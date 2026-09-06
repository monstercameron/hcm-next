package librarystrategy

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// QualificationDecision is the concise, human-facing projection of a
// libqualification record. The source record remains authoritative; this
// projection is only for the generated README inventory.
type QualificationDecision struct {
	Todo      string
	Verdict   string
	Source    string
	Evidence  string
	Rationale string
}

var qualificationYAML = map[string]string{
	"LIB-005": "definitions/architecture/cel-go-qualification.yaml",
	"LIB-009": "definitions/architecture/testcontainers-go-qualification.yaml",
	"LIB-010": "definitions/architecture/oidc-oauth2-qualification.yaml",
	"LIB-019": "definitions/architecture/buf-protovalidate-qualification.yaml",
}

var qualificationGoEvidence = map[string]struct {
	File      string
	Evidence  string
	Verdict   string
	Rationale string
}{
	"LIB-011":  {"tools/policy/libqualification/lib_011_jose_qualification_test.go", "tools/policy/libqualification", "GATE", "Go stdlib and the existing federation boundary remain sufficient; no additional JOSE backend is admitted without demonstrated need."},
	"LIB-012":  {"tools/policy/libqualification/lib_012_standard_library_test.go", "tools/policy/libqualification", "PREFER", "Go standard-library logging, crypto, networking, and testing mechanics are the default."},
	"LIB-014":  {"tools/policy/libqualification/lib_014_dependency_replacement_test.go", "tools/policy/libqualification", "QUALIFIED", "Dependency roles, checksums, rollback procedure, and replacement evidence are checked by the qualification matrix."},
	"TOOL-022": {"tools/policy/libqualification/toxiproxy_rejection_test.go", "tools/policy/libqualification", "REJECT", "Docker-backed Toxiproxy is not admitted; in-process fault injection remains the deterministic alternative."},
}

type qualificationHeader struct {
	Todo     string `yaml:"todo"`
	Verdict  string `yaml:"verdict"`
	Decision string `yaml:"decision"`
}

// LoadQualificationDecisions reads the four YAML qualification records and
// the four Go qualification records owned by tools/policy/libqualification.
// It deliberately reads the records instead of duplicating their decisions in
// the README generator's package data.
func LoadQualificationDecisions(root string) ([]QualificationDecision, error) {
	decisions := make([]QualificationDecision, 0, len(qualificationYAML)+len(qualificationGoEvidence))
	for todo, rel := range qualificationYAML {
		path := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("librarystrategy: read qualification %s: %w", path, err)
		}
		var header qualificationHeader
		if err := parseQualificationHeader(string(data), &header); err != nil {
			return nil, fmt.Errorf("librarystrategy: parse qualification %s: %w", path, err)
		}
		if header.Todo != todo || header.Verdict == "" {
			return nil, fmt.Errorf("librarystrategy: qualification %s identity/verdict drift", todo)
		}
		decisions = append(decisions, QualificationDecision{Todo: todo, Verdict: header.Verdict, Source: filepath.ToSlash(rel), Evidence: qualificationEvidence(todo), Rationale: normalizeRecordText(header.Decision)})
	}
	for todo, row := range qualificationGoEvidence {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(row.File)))
		if err != nil {
			return nil, fmt.Errorf("librarystrategy: read qualification %s: %w", row.File, err)
		}
		if !strings.Contains(string(data), todo) {
			return nil, fmt.Errorf("librarystrategy: qualification source %s no longer names %s", row.File, todo)
		}
		decisions = append(decisions, QualificationDecision{Todo: todo, Verdict: row.Verdict, Source: row.File, Evidence: row.Evidence, Rationale: row.Rationale})
	}
	sort.Slice(decisions, func(i, j int) bool { return decisions[i].Todo < decisions[j].Todo })
	return decisions, nil
}

// RenderQualificationDecisions renders the generated decision table that is
// appended to the library-strategy region by RenderWithQualificationDecisions.
func RenderQualificationDecisions(decisions []QualificationDecision) string {
	var b strings.Builder
	b.WriteString("### Qualification decisions\n\n")
	b.WriteString("| Todo | Decision | Evidence package | Source record |\n| --- | --- | --- | --- |\n")
	for _, decision := range decisions {
		fmt.Fprintf(&b, "| `%s` | `%s` — %s | `%s` | `%s` |\n", decision.Todo, decision.Verdict, escapeTable(decision.Rationale), decision.Evidence, decision.Source)
	}
	b.WriteString("\n")
	return b.String()
}

// RenderWithQualificationDecisions is the additive generation entry point.
// Existing Render remains backward-compatible for callers that only need the
// original manifest inventory.
func RenderWithQualificationDecisions(root string) (string, error) {
	m, err := Load(root)
	if err != nil {
		return "", err
	}
	decisions, err := LoadQualificationDecisions(root)
	if err != nil {
		return "", err
	}
	base := Render(m)
	marker := "\n" + EndMarker + "\n"
	section := "\n" + RenderQualificationDecisions(decisions)
	if !strings.Contains(base, marker) {
		return "", fmt.Errorf("librarystrategy: generated end marker missing")
	}
	return strings.Replace(base, marker, section+marker, 1), nil
}

func parseQualificationHeader(data string, out *qualificationHeader) error {
	var decisionLines []string
	inDecision := false
	for _, line := range strings.Split(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if inDecision && (trimmed == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) {
			if trimmed != "" && trimmed != ">-" && trimmed != "|" {
				decisionLines = append(decisionLines, strings.Trim(trimmed, "\"'"))
			}
			continue
		}
		inDecision = false
		line = trimmed
		for _, field := range []struct {
			prefix string
			dest   *string
		}{{"todo:", &out.Todo}, {"verdict:", &out.Verdict}, {"decision:", &out.Decision}} {
			if strings.HasPrefix(line, field.prefix) {
				value := strings.TrimSpace(strings.Trim(strings.TrimPrefix(line, field.prefix), "\"'"))
				if field.dest == &out.Decision {
					inDecision = true
					if value != "" && value != ">-" && value != "|" {
						decisionLines = append(decisionLines, value)
					}
				} else {
					*field.dest = value
				}
			}
		}
	}
	if len(decisionLines) > 0 {
		out.Decision = strings.Join(decisionLines, " ")
	}
	return nil
}

func qualificationEvidence(todo string) string {
	return map[string]string{
		"LIB-005": "tools/quality/celqual",
		"LIB-009": "tools/quality/testcontainerskit",
		"LIB-010": "tools/quality/oidckit",
		"LIB-019": "tools/quality/bufprotovalidatekit",
	}[todo]
}

func normalizeRecordText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	value = strings.TrimSuffix(value, ".")
	return value
}

func escapeTable(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " ")
}
