// Package report assembles discover.FileResult batches into the
// deterministic JSON/Markdown conformance report CONF-001 requires: stable
// ordering (documents, workflows, and checks all sorted), no wall-clock
// timestamps, and a manifest digest over the report content so two runs
// against the same inputs produce byte-identical output.
package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/conformance/checks"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/discover"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/model"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/runner"
	"github.com/monstercameron/human-capital-management-suite/tools/conformance/vocab"
)

// ToolVersion identifies the report format/generator. It is a constant,
// not a build timestamp, so it never breaks determinism.
const ToolVersion = "human-capital-management-suite/tools/conformance (CONF-001) v1"

// Report is the top-level, deterministically ordered conformance report for
// one run over one directory of reference workflow documents.
type Report struct {
	ToolVersion     string            `json:"tool_version"`
	ExecutionMode   string            `json:"execution_mode"`
	FixedClock      string            `json:"fixed_clock"`
	RootPath        string            `json:"root_path"`
	RuntimeSpecPath string            `json:"runtime_spec_path"`
	ContextSpecPath string            `json:"context_spec_path"`
	Vocabulary      VocabularySummary `json:"vocabulary"`
	Documents       []DocumentReport  `json:"documents"`
	ManifestDigest  string            `json:"manifest_digest"`
}

// VocabularySummary is the vocabulary the checks were evaluated against.
type VocabularySummary struct {
	Core       []string `json:"core"`
	Structural []string `json:"structural"`
	Retired    []string `json:"retired"`
}

// DocumentReport is one source *.md file's report.
type DocumentReport struct {
	SourcePath            string           `json:"source_path"`
	Title                 string           `json:"title,omitempty"`
	FoundationalQuestions []ScenarioReport `json:"foundational_questions,omitempty"`
	LegacyFixtures        []LegacyFixture  `json:"legacy_fixtures,omitempty"`
	Workflows             []WorkflowReport `json:"workflows"`
}

// LegacyFixture mirrors model.LegacyFixture for report output.
type LegacyFixture struct {
	Name             string `json:"name"`
	ContractPressure string `json:"contract_pressure"`
}

// ScenarioReport mirrors model.Scenario for report output.
type ScenarioReport struct {
	Index       int    `json:"index"`
	Description string `json:"description"`
}

// WorkflowReport is one workflow's PASS/FAIL/UNKNOWN check results plus the
// evidence extracted for it.
type WorkflowReport struct {
	ID           string           `json:"id"`
	Kind         string           `json:"kind"`
	Title        string           `json:"title,omitempty"`
	Summary      Summary          `json:"summary"`
	Checks       []CheckResult    `json:"checks"`
	Scenarios    []ScenarioReport `json:"scenarios,omitempty"`
	Capabilities []string         `json:"capabilities,omitempty"`
	Actors       []string         `json:"actors,omitempty"`
	Steps        []string         `json:"steps,omitempty"`
}

// Summary counts a workflow's checks by status.
type Summary struct {
	Pass    int `json:"pass"`
	Fail    int `json:"fail"`
	Unknown int `json:"unknown"`
}

// CheckResult mirrors checks.Result for report output.
type CheckResult struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Status string   `json:"status"`
	Detail string   `json:"detail"`
	Refs   []string `json:"refs,omitempty"`
}

// Build assembles a Report from a discover.Documents batch. repoRoot,
// refRoot, runtimeSpecPath, and contextSpecPath are all absolute paths;
// their repo-root-relative forms are what land in the report so the output
// never embeds a machine-specific absolute path.
func Build(repoRoot, refRoot, runtimeSpecPath, contextSpecPath string, v *vocab.Vocabulary, results []discover.FileResult, clk runner.Clock) Report {
	rpt := Report{
		ToolVersion:     ToolVersion,
		ExecutionMode:   "SIMULATE",
		FixedClock:      clk.Now().UTC().Format(time.RFC3339),
		RootPath:        relSlash(repoRoot, refRoot),
		RuntimeSpecPath: relSlash(repoRoot, runtimeSpecPath),
		ContextSpecPath: relSlash(repoRoot, contextSpecPath),
		Vocabulary: VocabularySummary{
			Core:       v.CoreNames(),
			Structural: v.StructuralNames(),
			Retired:    v.RetiredNames(),
		},
	}

	dr := runner.DocumentRunner{Vocab: v}
	for _, res := range results {
		if res.Err != nil {
			rpt.Documents = append(rpt.Documents, parseErrorDocument(res.RelPath, res.Err))
			continue
		}
		rpt.Documents = append(rpt.Documents, buildDocumentReport(dr, clk, res.Doc))
	}
	sort.Slice(rpt.Documents, func(i, j int) bool { return rpt.Documents[i].SourcePath < rpt.Documents[j].SourcePath })

	rpt.ManifestDigest = computeDigest(rpt)
	return rpt
}

func relSlash(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

func buildDocumentReport(dr runner.DocumentRunner, clk runner.Clock, doc *model.Document) DocumentReport {
	d := DocumentReport{SourcePath: doc.SourcePath, Title: doc.Title}
	for _, q := range doc.FoundationalQuestions {
		d.FoundationalQuestions = append(d.FoundationalQuestions, ScenarioReport{Index: q.Index, Description: q.Description})
	}
	for _, lf := range doc.LegacyFixtures {
		d.LegacyFixtures = append(d.LegacyFixtures, LegacyFixture{Name: lf.Name, ContractPressure: lf.ContractPressure})
	}
	for _, wf := range doc.Workflows {
		d.Workflows = append(d.Workflows, buildWorkflowReport(dr, clk, wf))
	}
	sort.Slice(d.Workflows, func(i, j int) bool { return d.Workflows[i].ID < d.Workflows[j].ID })
	return d
}

func buildWorkflowReport(dr runner.DocumentRunner, clk runner.Clock, wf *model.Workflow) WorkflowReport {
	// DocumentRunner.Simulate never errors (see runner package doc); every
	// document-shape problem is expressed as a check result instead.
	receipt, _ := dr.Simulate(wf, clk)

	wr := WorkflowReport{
		ID:           wf.ID,
		Kind:         wf.Kind,
		Title:        wf.Title,
		Capabilities: append([]string(nil), wf.Capabilities...),
		Actors:       append([]string(nil), wf.Actors...),
	}
	for _, sc := range wf.Scenarios {
		wr.Scenarios = append(wr.Scenarios, ScenarioReport{Index: sc.Index, Description: sc.Description})
	}
	for _, st := range wf.Steps {
		wr.Steps = append(wr.Steps, st.Text)
	}
	for _, c := range receipt.Checks {
		wr.Checks = append(wr.Checks, CheckResult{ID: c.ID, Name: c.Name, Status: string(c.Status), Detail: c.Detail, Refs: append([]string(nil), c.Refs...)})
		switch c.Status {
		case checks.Pass:
			wr.Summary.Pass++
		case checks.Fail:
			wr.Summary.Fail++
		default:
			wr.Summary.Unknown++
		}
	}
	return wr
}

func parseErrorDocument(relPath string, err error) DocumentReport {
	base := strings.TrimSuffix(filepath.Base(relPath), filepath.Ext(relPath))
	return DocumentReport{
		SourcePath: relPath,
		Workflows: []WorkflowReport{{
			ID:      base,
			Kind:    "PARSE_ERROR",
			Title:   base,
			Summary: Summary{Unknown: 1},
			Checks: []CheckResult{{
				ID:     "CHK-000",
				Name:   "DocumentParsed",
				Status: "UNKNOWN",
				Detail: fmt.Sprintf("document could not be parsed: %v", err),
			}},
		}},
	}
}

func computeDigest(r Report) string {
	r.ManifestDigest = ""
	data, err := json.Marshal(r)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// JSON renders the report as indented, deterministic JSON (struct field
// order is fixed by declaration order, and every slice inside Report is
// pre-sorted by Build).
func (r Report) JSON() ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// Markdown renders a human-readable summary of the report, built from the
// same sorted fields as JSON so it is equally deterministic.
func (r Report) Markdown() []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# Human Capital Management Suite Reference-Workflow Conformance Report\n\n")
	fmt.Fprintf(&b, "- Tool version: %s\n", r.ToolVersion)
	fmt.Fprintf(&b, "- Execution mode: %s\n", r.ExecutionMode)
	fmt.Fprintf(&b, "- Fixed clock: %s\n", r.FixedClock)
	fmt.Fprintf(&b, "- Root path: %s\n", r.RootPath)
	fmt.Fprintf(&b, "- Runtime spec: %s\n", r.RuntimeSpecPath)
	fmt.Fprintf(&b, "- Context spec: %s\n", r.ContextSpecPath)
	fmt.Fprintf(&b, "- Manifest digest: `%s`\n\n", r.ManifestDigest)

	fmt.Fprintf(&b, "## Vocabulary\n\n")
	fmt.Fprintf(&b, "- Core primitives: %s\n", strings.Join(r.Vocabulary.Core, ", "))
	fmt.Fprintf(&b, "- Structural primitives: %s\n", strings.Join(r.Vocabulary.Structural, ", "))
	fmt.Fprintf(&b, "- Retired names: %s\n\n", strings.Join(r.Vocabulary.Retired, ", "))

	for _, doc := range r.Documents {
		fmt.Fprintf(&b, "## %s\n\n", doc.SourcePath)
		for _, wf := range doc.Workflows {
			fmt.Fprintf(&b, "### %s (%s)\n\n", wf.ID, wf.Kind)
			fmt.Fprintf(&b, "Summary: PASS=%d FAIL=%d UNKNOWN=%d\n\n", wf.Summary.Pass, wf.Summary.Fail, wf.Summary.Unknown)
			b.WriteString("| Check | Status | Detail |\n| --- | --- | --- |\n")
			for _, c := range wf.Checks {
				fmt.Fprintf(&b, "| %s %s | %s | %s |\n", c.ID, c.Name, c.Status, escapePipe(c.Detail))
			}
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func escapePipe(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// WriteFiles writes the report to outDir as report.json, report.md, or
// both, depending on format ("json", "markdown", or "both").
func WriteFiles(outDir, format string, r Report) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if format == "json" || format == "both" {
		data, err := r.JSON()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(outDir, "report.json"), data, 0o644); err != nil {
			return err
		}
	}
	if format == "markdown" || format == "both" {
		if err := os.WriteFile(filepath.Join(outDir, "report.md"), r.Markdown(), 0o644); err != nil {
			return err
		}
	}
	return nil
}
