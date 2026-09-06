// Package riskbinding validates that the declared architecture and product
// risks have executable prevention, detection, and recovery evidence.
package riskbinding

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const tableVersion = 1

// Category identifies the kind of risk being governed.
type Category string

const (
	Architecture Category = "ARCHITECTURE"
	Product      Category = "PRODUCT"
)

// EvidenceKind is the lifecycle control stage covered by an evidence item.
type EvidenceKind string

const (
	Prevention EvidenceKind = "PREVENTION"
	Detection  EvidenceKind = "DETECTION"
	Recovery   EvidenceKind = "RECOVERY"
)

// Status is the gate-facing residual-risk status.
type Status string

const (
	Mitigated    Status = "MITIGATED"
	Partial      Status = "PARTIAL"
	Accepted     Status = "ACCEPTED"
	OutOfPhase   Status = "OUT_OF_PHASE"
	Uncontrolled Status = "UNCONTROLLED"
)

// Risk is a row in the declared risk table. SourceDocument and SourceLine
// make the checked-in fixture auditable back to the named specification.
type Risk struct {
	ID             string   `json:"id"`
	Category       Category `json:"category"`
	Statement      string   `json:"statement"`
	Owner          string   `json:"owner"`
	SourceDocument string   `json:"source_document"`
	SourceLine     int      `json:"source_line"`
}

// Evidence is either a todo reference or a directly named policy/planning
// test. Exactly one of TodoID and TestName must be set.
type Evidence struct {
	Kind     EvidenceKind `json:"kind"`
	TodoID   string       `json:"todo_id,omitempty"`
	TestName string       `json:"test_name,omitempty"`
	Package  string       `json:"package,omitempty"`
}

// Binding groups all evidence attached to one risk.
type Binding struct {
	RiskID   string     `json:"risk_id"`
	Evidence []Evidence `json:"evidence"`
}

// AllowlistedGap is a reviewed, owner-backed gap that is permitted for the
// current corpus snapshot. It never permits a malformed or unresolved item.
type AllowlistedGap struct {
	RiskID     string `json:"risk_id"`
	Kind       string `json:"kind"`
	Owner      string `json:"owner"`
	Reason     string `json:"reason"`
	ReviewDate string `json:"review_date"`
	Status     Status `json:"status"`
}

// Table is the complete checked-in risk register and binding declaration.
type Table struct {
	Version         int              `json:"version"`
	Risks           []Risk           `json:"risks"`
	Bindings        []Binding        `json:"bindings"`
	DefaultEvidence []Evidence       `json:"default_evidence,omitempty"`
	Allowlist       []AllowlistedGap `json:"allowlist"`
}

var riskHeadingRE = regexp.MustCompile(`^### Risk ([0-9]+): (.+)$`)

// Finding is a stable diagnostic emitted by validation.
type Finding struct {
	RiskID string
	Kind   string
	Code   string
	Detail string
	Reason string
}

func (f Finding) Key() string {
	return strings.Join([]string{f.RiskID, f.Kind, f.Code, f.Detail}, "|")
}

func (f Finding) String() string {
	id := f.RiskID
	if id == "" {
		id = "<table>"
	}
	if f.Kind != "" {
		return fmt.Sprintf("%s: %s/%s: %s", id, f.Kind, f.Code, f.Reason)
	}
	return fmt.Sprintf("%s: %s: %s", id, f.Code, f.Reason)
}

// Gap is a missing or unusable stage for a risk. Allowlisted is true only
// when the exact gap has an owner-backed current exception.
type Gap struct {
	RiskID      string       `json:"risk_id"`
	Category    Category     `json:"category"`
	Kind        EvidenceKind `json:"kind"`
	Code        string       `json:"code"`
	Detail      string       `json:"detail,omitempty"`
	Owner       string       `json:"owner"`
	Status      Status       `json:"status"`
	Allowlisted bool         `json:"allowlisted"`
}

func (g Gap) Key() string {
	return strings.Join([]string{g.RiskID, string(g.Kind), g.Code, g.Detail}, "|")
}

// CategoryReport is the aggregate unbound-risk report for one category.
type CategoryReport struct {
	Category    Category `json:"category"`
	Total       int      `json:"total"`
	Bound       int      `json:"bound"`
	Unbound     int      `json:"unbound"`
	GapCount    int      `json:"gap_count"`
	NewGapCount int      `json:"new_gap_count"`
}

// BindingRow is the deterministic, golden-testable projection of a binding.
type BindingRow struct {
	RiskID     string   `json:"risk_id"`
	Category   Category `json:"category"`
	Prevention []string `json:"prevention"`
	Detection  []string `json:"detection"`
	Recovery   []string `json:"recovery"`
	Status     Status   `json:"status"`
}

// Report is the result of validating a Table against a todo registry and
// scanned policy/planning test names.
type Report struct {
	Findings        []Finding                   `json:"findings"`
	Gaps            []Gap                       `json:"gaps"`
	NewGaps         []Gap                       `json:"new_gaps"`
	AllowlistedGaps []Gap                       `json:"allowlisted_gaps"`
	ByCategory      map[Category]CategoryReport `json:"by_category"`
	Rows            []BindingRow                `json:"rows"`
}

// Violations returns structural findings and gaps not covered by the current
// allowlist. A reviewed gap remains visible in Gaps and AllowlistedGaps.
func (r Report) Violations() []Finding {
	out := append([]Finding(nil), r.Findings...)
	for _, gap := range r.NewGaps {
		out = append(out, Finding{RiskID: gap.RiskID, Kind: string(gap.Kind), Code: gap.Code, Detail: gap.Detail, Reason: "risk has an unallowlisted evidence gap"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

// Load reads and validates the JSON shape of a checked-in table. Semantic
// references are resolved by Evaluate after the live registries are loaded.
func Load(path string) (Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Table{}, fmt.Errorf("read risk table %s: %w", path, err)
	}
	var table Table
	if err := json.Unmarshal(data, &table); err != nil {
		return Table{}, fmt.Errorf("parse risk table %s: %w", path, err)
	}
	if table.Version != tableVersion {
		return Table{}, fmt.Errorf("risk table version = %d, want %d", table.Version, tableVersion)
	}
	return table, nil
}

// JSON returns canonical indented JSON with a trailing newline.
func (t Table) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal risk table: %w", err)
	}
	return append(data, '\n'), nil
}

// BindingTableJSON returns the stable golden projection of a report.
func (r Report) BindingTableJSON() ([]byte, error) {
	type golden struct {
		Rows            []BindingRow `json:"rows"`
		Gaps            []Gap        `json:"gaps"`
		AllowlistedGaps []Gap        `json:"allowlisted_gaps"`
	}
	data, err := json.MarshalIndent(golden{Rows: r.Rows, Gaps: r.Gaps, AllowlistedGaps: r.AllowlistedGaps}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal binding table: %w", err)
	}
	return append(data, '\n'), nil
}

// Evaluate validates a table. todoTests maps todo IDs to their declared TEST
// field. testNames contains policy/planning Test/Fuzz/Benchmark declarations.
func Evaluate(table Table, todoTests map[string]string, testNames map[string]bool) Report {
	report := Report{ByCategory: map[Category]CategoryReport{Architecture: {Category: Architecture}, Product: {Category: Product}}}
	riskByID := make(map[string]Risk, len(table.Risks))
	for _, risk := range table.Risks {
		if risk.ID == "" {
			report.Findings = append(report.Findings, Finding{Code: "MISSING_RISK_ID", Reason: "risk has no stable id"})
			continue
		}
		if _, exists := riskByID[risk.ID]; exists {
			report.Findings = append(report.Findings, Finding{RiskID: risk.ID, Code: "DUPLICATE_RISK_ID", Reason: "risk id is declared more than once"})
			continue
		}
		if risk.Category != Architecture && risk.Category != Product {
			report.Findings = append(report.Findings, Finding{RiskID: risk.ID, Code: "INVALID_CATEGORY", Detail: string(risk.Category), Reason: "category must be ARCHITECTURE or PRODUCT"})
		}
		if strings.TrimSpace(risk.Statement) == "" || strings.TrimSpace(risk.Owner) == "" {
			report.Findings = append(report.Findings, Finding{RiskID: risk.ID, Code: "INCOMPLETE_RISK", Reason: "risk statement and owner are required"})
		}
		if risk.SourceDocument == "" || risk.SourceLine < 1 {
			report.Findings = append(report.Findings, Finding{RiskID: risk.ID, Code: "MISSING_SOURCE_CITATION", Reason: "risk must cite a source document and positive line"})
		}
		riskByID[risk.ID] = risk
		cat := report.ByCategory[risk.Category]
		cat.Total++
		report.ByCategory[risk.Category] = cat
	}

	bindingByRisk := make(map[string]Binding, len(table.Bindings))
	for _, binding := range table.Bindings {
		if _, ok := riskByID[binding.RiskID]; !ok {
			report.Findings = append(report.Findings, Finding{RiskID: binding.RiskID, Code: "UNKNOWN_RISK_BINDING", Reason: "binding references an undeclared risk"})
			continue
		}
		if _, exists := bindingByRisk[binding.RiskID]; exists {
			report.Findings = append(report.Findings, Finding{RiskID: binding.RiskID, Code: "DUPLICATE_RISK_BINDING", Reason: "risk has more than one binding row"})
			continue
		}
		bindingByRisk[binding.RiskID] = binding
	}
	for _, risk := range table.Risks {
		if _, ok := bindingByRisk[risk.ID]; ok || len(table.DefaultEvidence) == 0 {
			continue
		}
		bindingByRisk[risk.ID] = Binding{RiskID: risk.ID, Evidence: append([]Evidence(nil), table.DefaultEvidence...)}
	}

	allow := make(map[string]AllowlistedGap, len(table.Allowlist))
	for _, item := range table.Allowlist {
		if item.RiskID == "" || item.Owner == "" || item.Reason == "" || !validDate(item.ReviewDate) {
			report.Findings = append(report.Findings, Finding{RiskID: item.RiskID, Kind: item.Kind, Code: "INVALID_ALLOWLIST", Reason: "allowlisted gaps require risk, kind, owner, reason, and YYYY-MM-DD review date"})
			continue
		}
		kind := EvidenceKind(item.Kind)
		if !validKind(kind) {
			report.Findings = append(report.Findings, Finding{RiskID: item.RiskID, Kind: item.Kind, Code: "INVALID_ALLOWLIST", Reason: "allowlisted gap kind is not a binding stage"})
			continue
		}
		allow[gapKey(item.RiskID, kind, "MISSING_"+string(kind), "")] = item
	}

	for _, risk := range table.Risks {
		binding := bindingByRisk[risk.ID]
		stages := map[EvidenceKind][]string{Prevention: {}, Detection: {}, Recovery: {}}
		seenKinds := map[EvidenceKind]bool{}
		for _, evidence := range binding.Evidence {
			if !validKind(evidence.Kind) {
				report.Findings = append(report.Findings, Finding{RiskID: risk.ID, Kind: string(evidence.Kind), Code: "INVALID_EVIDENCE_KIND", Reason: "evidence kind must be PREVENTION, DETECTION, or RECOVERY"})
				continue
			}
			if (evidence.TodoID == "") == (evidence.TestName == "") {
				report.Findings = append(report.Findings, Finding{RiskID: risk.ID, Kind: string(evidence.Kind), Code: "INVALID_EVIDENCE_REFERENCE", Reason: "evidence must name exactly one todo id or test name"})
				continue
			}
			label := ""
			if evidence.TodoID != "" {
				test, ok := todoTests[evidence.TodoID]
				if !ok {
					report.Findings = append(report.Findings, Finding{RiskID: risk.ID, Kind: string(evidence.Kind), Code: "UNRESOLVED_TODO", Detail: evidence.TodoID, Reason: "todo evidence id is absent from the generated registry"})
					continue
				}
				if strings.TrimSpace(test) == "" {
					report.Findings = append(report.Findings, Finding{RiskID: risk.ID, Kind: string(evidence.Kind), Code: "TODO_MISSING_TEST", Detail: evidence.TodoID, Reason: "todo evidence id has no TEST field"})
					continue
				}
				label = "todo:" + evidence.TodoID + "=" + test
			} else {
				if !testNames[evidence.TestName] {
					report.Findings = append(report.Findings, Finding{RiskID: risk.ID, Kind: string(evidence.Kind), Code: "UNRESOLVED_TEST", Detail: evidence.TestName, Reason: "policy/planning test name is absent from the scanned tree"})
					continue
				}
				label = "test:" + evidence.TestName
			}
			if seenKinds[evidence.Kind] && contains(stages[evidence.Kind], label) {
				continue
			}
			seenKinds[evidence.Kind] = true
			stages[evidence.Kind] = append(stages[evidence.Kind], label)
		}

		for _, kind := range []EvidenceKind{Prevention, Detection, Recovery} {
			if len(stages[kind]) != 0 {
				continue
			}
			code := "MISSING_" + string(kind)
			gap := Gap{RiskID: risk.ID, Category: risk.Category, Kind: kind, Code: code, Owner: risk.Owner}
			if item, ok := allow[gapKey(risk.ID, kind, code, "")]; ok {
				gap.Allowlisted = true
				gap.Status = item.Status
				if gap.Status == "" {
					gap.Status = Partial
				}
				if item.Owner != risk.Owner {
					report.Findings = append(report.Findings, Finding{RiskID: risk.ID, Kind: string(kind), Code: "ALLOWLIST_OWNER_MISMATCH", Reason: "gap allowlist owner does not match the declared risk owner"})
				}
				report.AllowlistedGaps = append(report.AllowlistedGaps, gap)
			} else {
				report.NewGaps = append(report.NewGaps, gap)
			}
			report.Gaps = append(report.Gaps, gap)
		}

		row := BindingRow{RiskID: risk.ID, Category: risk.Category, Prevention: sorted(stages[Prevention]), Detection: sorted(stages[Detection]), Recovery: sorted(stages[Recovery]), Status: statusFor(risk.ID, report.NewGaps, report.AllowlistedGaps)}
		report.Rows = append(report.Rows, row)
	}

	for id, item := range allow {
		if !containsGap(report.Gaps, id) {
			report.Findings = append(report.Findings, Finding{RiskID: item.RiskID, Kind: item.Kind, Code: "STALE_ALLOWLIST", Reason: "allowlisted gap is not present in the current binding table"})
		}
	}
	for category, summary := range report.ByCategory {
		for _, gap := range report.Gaps {
			if gap.Category != category {
				continue
			}
			summary.GapCount++
			if !gap.Allowlisted {
				summary.NewGapCount++
			}
		}
		for _, row := range report.Rows {
			if row.Category == category && row.Status == Mitigated {
				summary.Bound++
			}
		}
		summary.Unbound = summary.Total - summary.Bound
		report.ByCategory[category] = summary
	}
	sort.Slice(report.Gaps, func(i, j int) bool { return report.Gaps[i].Key() < report.Gaps[j].Key() })
	sort.Slice(report.NewGaps, func(i, j int) bool { return report.NewGaps[i].Key() < report.NewGaps[j].Key() })
	sort.Slice(report.AllowlistedGaps, func(i, j int) bool { return report.AllowlistedGaps[i].Key() < report.AllowlistedGaps[j].Key() })
	sort.Slice(report.Rows, func(i, j int) bool { return report.Rows[i].RiskID < report.Rows[j].RiskID })
	sort.Slice(report.Findings, func(i, j int) bool { return report.Findings[i].Key() < report.Findings[j].Key() })
	return report
}

// EvaluateRepository loads a fixture, the real generated todo registry, and
// policy/planning test names from the repository.
func EvaluateRepository(root, tablePath, registryPath string) (Report, error) {
	table, err := Load(tablePath)
	if err != nil {
		return Report{}, err
	}
	todoTests, err := LoadTodoTests(registryPath)
	if err != nil {
		return Report{}, err
	}
	testNames, err := ScanTestNames(root)
	if err != nil {
		return Report{}, err
	}
	report := Evaluate(table, todoTests, testNames)
	report.Findings = append(report.Findings, ValidateCitations(root, table.Risks)...)
	sort.Slice(report.Findings, func(i, j int) bool { return report.Findings[i].Key() < report.Findings[j].Key() })
	return report, nil
}

// ValidateCitations verifies that every source citation points at the risk
// heading whose number and statement are declared in the table.
func ValidateCitations(root string, risks []Risk) []Finding {
	contents := map[string][]string{}
	var findings []Finding
	for _, risk := range risks {
		path := filepath.Join(root, filepath.FromSlash(risk.SourceDocument))
		lines, ok := contents[risk.SourceDocument]
		if !ok {
			data, err := os.ReadFile(path)
			if err != nil {
				findings = append(findings, Finding{RiskID: risk.ID, Code: "SOURCE_DOCUMENT_UNREADABLE", Detail: risk.SourceDocument, Reason: err.Error()})
				contents[risk.SourceDocument] = nil
				continue
			}
			lines = strings.Split(string(data), "\n")
			contents[risk.SourceDocument] = lines
		}
		if risk.SourceLine < 1 || risk.SourceLine > len(lines) {
			findings = append(findings, Finding{RiskID: risk.ID, Code: "SOURCE_LINE_MISMATCH", Detail: risk.SourceDocument, Reason: "source citation line is outside the cited document"})
			continue
		}
		match := riskHeadingRE.FindStringSubmatch(strings.TrimSpace(lines[risk.SourceLine-1]))
		wantNumber := strings.TrimPrefix(risk.ID, "RISK-")
		if match == nil || match[1] != strings.TrimLeft(wantNumber, "0") && !(match[1] == "0" && strings.TrimLeft(wantNumber, "0") == "") || strings.TrimSpace(match[2]) != strings.TrimSpace(risk.Statement) {
			findings = append(findings, Finding{RiskID: risk.ID, Code: "SOURCE_LINE_MISMATCH", Detail: risk.SourceDocument, Reason: "source citation does not match the declared risk heading"})
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].Key() < findings[j].Key() })
	return findings
}

// LoadTodoTests reads the generated registry without importing its generator.
func LoadTodoTests(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read todo registry %s: %w", path, err)
	}
	var rows []struct {
		ID   string `json:"id"`
		Test string `json:"test"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("parse todo registry %s: %w", path, err)
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		if row.ID != "" {
			out[row.ID] = row.Test
		}
	}
	return out, nil
}

// ScanTestNames scans only tools/policy and tools/planning *_test.go files.
func ScanTestNames(root string) (map[string]bool, error) {
	names := map[string]bool{}
	nameRE := regexp.MustCompile(`^\s*func\s+(Test|Fuzz|Benchmark)[A-Za-z0-9_]*\s*\(`)
	for _, dir := range []string{filepath.Join(root, "tools", "policy"), filepath.Join(root, "tools", "planning")} {
		if _, err := os.Stat(dir); err != nil {
			return nil, fmt.Errorf("stat test root %s: %w", dir, err)
		}
		err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				if info.Name() == "testdata" || info.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(info.Name(), "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, line := range strings.Split(string(data), "\n") {
				match := nameRE.FindStringSubmatch(line)
				if match == nil {
					continue
				}
				text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "func"))
				name, _, _ := strings.Cut(text, "(")
				names[strings.TrimSpace(name)] = true
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("scan test names below %s: %w", dir, err)
		}
	}
	return names, nil
}

func validKind(kind EvidenceKind) bool {
	return kind == Prevention || kind == Detection || kind == Recovery
}

func validDate(value string) bool {
	if len(value) != len("2006-01-02") {
		return false
	}
	_, err := fmt.Sscanf(value, "%d-%d-%d", new(int), new(int), new(int))
	return err == nil && value[4] == '-' && value[7] == '-'
}

func gapKey(riskID string, kind EvidenceKind, code, detail string) string {
	return strings.Join([]string{riskID, string(kind), code, detail}, "|")
}

func containsGap(gaps []Gap, key string) bool {
	for _, gap := range gaps {
		if gap.Key() == key {
			return true
		}
	}
	return false
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func sorted(values []string) []string {
	copyOf := append([]string(nil), values...)
	sort.Strings(copyOf)
	return copyOf
}

func statusFor(riskID string, newGaps, allowlisted []Gap) Status {
	for _, gap := range newGaps {
		if gap.RiskID == riskID {
			return Uncontrolled
		}
	}
	for _, gap := range allowlisted {
		if gap.RiskID == riskID {
			if gap.Status != "" {
				return gap.Status
			}
			return Partial
		}
	}
	return Mitigated
}
