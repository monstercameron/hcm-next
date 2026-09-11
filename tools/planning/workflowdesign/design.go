// Package workflowdesign owns the WF-DISC-005 design record contract. It
// loads machine-readable WorkflowDesignRecords, requires every execution
// dimension with explicit NOT_APPLICABLE reason codes where a dimension does
// not apply, binds records with a stable canonical digest, and emits stable
// Go and Protobuf registries. It is kernel-pure: file parsing, validation
// and text emission only, no database, network or mutable global.
package workflowdesign

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowarchetypes"
)

// Execution dispositions. DIRECT executes a governed capability with no
// durable workflow; WORKFLOW runs the full recipe.
const (
	DispositionDirect   = "DIRECT"
	DispositionWorkflow = "WORKFLOW"
)

// NotApplicable marks a dimension the design provably does not need. It is
// accepted only with an explicit reason code; a bare marker is a finding.
const NotApplicable = "NOT_APPLICABLE"

// Dimension is one execution dimension: substantive text, a list of engines,
// or an explicit NOT_APPLICABLE with a reason code.
type Dimension struct {
	Value  string   `yaml:"value,omitempty" json:"value,omitempty"`
	Reason string   `yaml:"reason,omitempty" json:"reason,omitempty"`
	Items  []string `yaml:"items,omitempty" json:"items,omitempty"`
}

// UnmarshalYAML accepts a scalar (value), a sequence (items) or a mapping
// with value and reason keys.
func (d *Dimension) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var value string
		if err := node.Decode(&value); err != nil {
			return err
		}
		d.Value = value
		return nil
	case yaml.SequenceNode:
		var items []string
		if err := node.Decode(&items); err != nil {
			return err
		}
		d.Items = items
		return nil
	case yaml.MappingNode:
		var mapped struct {
			Value  string `yaml:"value"`
			Reason string `yaml:"reason"`
		}
		if err := node.Decode(&mapped); err != nil {
			return err
		}
		d.Value = mapped.Value
		d.Reason = mapped.Reason
		return nil
	default:
		return fmt.Errorf("workflowdesign: dimension must be text, a list or a value/reason mapping")
	}
}

// DesignRecord is one machine-readable high-level workflow design.
// Definition optionally binds the record to one accepted BusinessIntent
// by stable semantic identity (definition_ref with version); display
// names must never be join keys, so the design join requires it.
type DesignRecord struct {
	Intent         string    `yaml:"intent" json:"intent"`
	Definition     string    `yaml:"definition,omitempty" json:"definition,omitempty"`
	Disposition    string    `yaml:"disposition" json:"disposition"`
	Archetype      string    `yaml:"archetype" json:"archetype"`
	DomainProfile  string    `yaml:"domain_profile" json:"domain_profile"`
	InputBoundary  string    `yaml:"input_boundary" json:"input_boundary"`
	SnapshotPolicy string    `yaml:"snapshot_policy" json:"snapshot_policy"`
	Engines        Dimension `yaml:"engines" json:"engines"`
	HumanWork      Dimension `yaml:"human_work" json:"human_work"`
	Writes         Dimension `yaml:"writes" json:"writes"`
	Waits          Dimension `yaml:"waits" json:"waits"`
	Invalidators   Dimension `yaml:"invalidators" json:"invalidators"`
	Reconciliation Dimension `yaml:"reconciliation" json:"reconciliation"`
	Correction     Dimension `yaml:"correction" json:"correction"`
	Completion     string    `yaml:"completion" json:"completion"`
}

// Finding is one exact record diagnostic located by intent.
type Finding struct {
	Intent string `json:"intent,omitempty"`
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Detail string `json:"detail"`
}

// Report is the complete validation result.
type Report struct {
	Records  []DesignRecord `json:"records"`
	Findings []Finding      `json:"findings,omitempty"`
	Digest   string         `json:"digest"`
}

// OK reports whether the report holds no findings.
func (r Report) OK() bool { return len(r.Findings) == 0 }

// LoadRecords parses a design record sidecar document.
func LoadRecords(path string) ([]DesignRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("workflowdesign: read records: %w", err)
	}
	var document struct {
		Records []DesignRecord `yaml:"records"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("workflowdesign: parse records: %w", err)
	}
	if len(document.Records) == 0 {
		return nil, errors.New("workflowdesign: sidecar holds no records")
	}
	return document.Records, nil
}

// ValidateRecords checks every record against the contract and binds the set
// with a canonical digest that is stable across compilations.
func ValidateRecords(records []DesignRecord) Report {
	known := workflowarchetypes.KnownArchetypes()
	report := Report{Records: append([]DesignRecord(nil), records...)}
	seen := map[string]bool{}
	for index := range report.Records {
		record := &report.Records[index]
		if record.Intent == "" {
			report.add(Finding{Code: "MISSING_INTENT", Field: "intent", Detail: "record names no intent"})
			continue
		}
		if seen[record.Intent] {
			report.add(Finding{Intent: record.Intent, Code: "DUPLICATE_RECORD", Detail: "intent has more than one design record"})
		}
		seen[record.Intent] = true
		validateRecord(&report, record, known)
	}
	sort.Slice(report.Findings, func(i, j int) bool {
		if report.Findings[i].Intent != report.Findings[j].Intent {
			return report.Findings[i].Intent < report.Findings[j].Intent
		}
		return report.Findings[i].Code < report.Findings[j].Code
	})
	if report.Findings == nil {
		report.Findings = []Finding{}
	}
	report.Digest = digestRecords(report.Records)
	return report
}

func (r *Report) add(finding Finding) {
	r.Findings = append(r.Findings, finding)
}

// definitionRefRe matches a versioned BusinessIntent definition_ref:
// hcmnext.<domain>.<verb_noun>/v<version>.
var definitionRefRe = regexp.MustCompile(`^hcmnext\.[a-z0-9_]+\.[a-z0-9_]+/v[0-9]+$`)

// ValidDefinition reports whether ref is a versioned definition_ref.
// The design join shares this predicate so display names can never
// become join keys in either package.
func ValidDefinition(ref string) bool { return definitionRefRe.MatchString(ref) }

func validateRecord(report *Report, record *DesignRecord, known map[string]bool) {
	if record.Definition != "" && !definitionRefRe.MatchString(record.Definition) {
		report.add(Finding{Intent: record.Intent, Code: "INVALID_DEFINITION", Field: "definition", Detail: "definition must be a versioned definition_ref like hcmnext.people.change_manager/v1"})
	}
	switch record.Disposition {
	case DispositionDirect, DispositionWorkflow:
	case "":
		report.add(Finding{Intent: record.Intent, Code: "MISSING_EXECUTION_DISPOSITION", Field: "disposition", Detail: "record names no execution disposition"})
	default:
		report.add(Finding{Intent: record.Intent, Code: "INVALID_EXECUTION_DISPOSITION", Field: "disposition", Detail: "disposition must be DIRECT or WORKFLOW"})
	}
	if record.Archetype == "" {
		report.add(Finding{Intent: record.Intent, Code: "MISSING_ARCHETYPE", Field: "archetype", Detail: "record names no archetype"})
	} else if !known[record.Archetype] {
		report.add(Finding{Intent: record.Intent, Code: "UNKNOWN_ARCHETYPE", Field: "archetype", Detail: "archetype " + record.Archetype + " is not defined"})
	}
	checkText(report, record.Intent, "domain_profile", record.DomainProfile, "MISSING_DOMAIN_PROFILE")
	checkText(report, record.Intent, "input_boundary", record.InputBoundary, "MISSING_INPUT_BOUNDARY")
	checkText(report, record.Intent, "snapshot_policy", record.SnapshotPolicy, "MISSING_SNAPSHOT_POLICY")
	checkText(report, record.Intent, "completion", record.Completion, "MISSING_COMPLETION")
	checkDimension(report, record.Intent, "engines", record.Engines, "MISSING_ENGINES")
	checkDimension(report, record.Intent, "human_work", record.HumanWork, "MISSING_HUMAN_WORK")
	checkDimension(report, record.Intent, "writes", record.Writes, "MISSING_WRITES")
	checkDimension(report, record.Intent, "waits", record.Waits, "MISSING_WAITS")
	checkDimension(report, record.Intent, "invalidators", record.Invalidators, "MISSING_INVALIDATORS")
	checkDimension(report, record.Intent, "reconciliation", record.Reconciliation, "MISSING_RECONCILIATION")
	checkDimension(report, record.Intent, "correction", record.Correction, "MISSING_CORRECTION")
}

func checkText(report *Report, intent, field, value, code string) {
	if strings.TrimSpace(value) == "" {
		report.add(Finding{Intent: intent, Code: code, Field: field, Detail: "record states no " + strings.ReplaceAll(field, "_", " ")})
	}
}

func checkDimension(report *Report, intent, field string, dimension Dimension, code string) {
	if dimension.Value == NotApplicable {
		if strings.TrimSpace(dimension.Reason) == "" {
			report.add(Finding{Intent: intent, Code: "BARE_NOT_APPLICABLE", Field: field, Detail: "NOT_APPLICABLE needs an explicit reason code"})
		}
		return
	}
	if strings.TrimSpace(dimension.Value) == "" && len(nonBlank(dimension.Items)) == 0 {
		report.add(Finding{Intent: intent, Code: code, Field: field, Detail: "record states no " + strings.ReplaceAll(field, "_", " ")})
	}
}

func nonBlank(values []string) []string {
	var out []string
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, value)
		}
	}
	return out
}

func digestRecords(records []DesignRecord) string {
	ordered := append([]DesignRecord(nil), records...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Intent < ordered[j].Intent })
	var lines []string
	for _, record := range ordered {
		lines = append(lines, strings.Join([]string{
			record.Intent, record.Definition, record.Disposition, record.Archetype, record.DomainProfile,
			record.InputBoundary, record.SnapshotPolicy, dimensionDigest(record.Engines),
			dimensionDigest(record.HumanWork), dimensionDigest(record.Writes),
			dimensionDigest(record.Waits), dimensionDigest(record.Invalidators),
			dimensionDigest(record.Reconciliation), dimensionDigest(record.Correction),
			record.Completion,
		}, "\x00"))
	}
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func dimensionDigest(dimension Dimension) string {
	items := append([]string(nil), dimension.Items...)
	sort.Strings(items)
	return dimension.Value + "\x01" + dimension.Reason + "\x01" + strings.Join(items, ",")
}

// EmitGoRegistry renders the stable Go design registry for records.
func EmitGoRegistry(records []DesignRecord) (string, error) {
	ordered := append([]DesignRecord(nil), records...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Intent < ordered[j].Intent })
	var rendered strings.Builder
	rendered.WriteString("// Code generated by the workflowdesign command; DO NOT EDIT.\n\npackage registry\n\n// RecordCount is the number of design records in this registry.\nconst RecordCount = " + strconv.Itoa(len(ordered)) + "\n\n// Intents lists every registered intent in canonical order.\nvar Intents = []string{\n")
	for _, record := range ordered {
		rendered.WriteString("\t" + strconv.Quote(record.Intent) + ",\n")
	}
	rendered.WriteString("}\n")
	return rendered.String(), nil
}

// EmitProtoRegistry renders the stable Protobuf design registry schema. The
// schema is record-independent by design: record data lives in the Go
// registry while Protobuf carries the wire contract both must satisfy.
func EmitProtoRegistry(records []DesignRecord) (string, error) {
	if len(records) == 0 {
		return "", errors.New("workflowdesign: no records to cover")
	}
	var rendered strings.Builder
	rendered.WriteString("// Code generated by the workflowdesign command; DO NOT EDIT.\n\nsyntax = \"proto3\";\n\npackage hcmnext.workflow.design.v1;\n\n// DesignRecord is one machine-readable high-level workflow design.\nmessage DesignRecord {\n  string intent = 1;\n  string disposition = 2;\n  string archetype = 3;\n  string domain_profile = 4;\n  string input_boundary = 5;\n  string snapshot_policy = 6;\n  repeated string engines = 7;\n  string human_work = 8;\n  string human_work_reason = 9;\n  string writes = 10;\n  string waits = 11;\n  string invalidators = 12;\n  string reconciliation = 13;\n  string correction = 14;\n  string completion = 15;\n}\n\n// DesignRegistry carries every registered intent in canonical order.\nmessage DesignRegistry {\n  repeated DesignRecord records = 1;\n}\n")
	return rendered.String(), nil
}
