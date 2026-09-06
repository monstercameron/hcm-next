package telemetry

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type SemanticKind string

const (
	SemanticEvent     SemanticKind = "event"
	SemanticSpan      SemanticKind = "span"
	SemanticMetric    SemanticKind = "metric"
	SemanticAttribute SemanticKind = "attribute"
)

// SemanticDefinition is the source-level shape checked by the CI gate. File
// and Symbol are evidence coordinates, not telemetry values.
type SemanticDefinition struct {
	File           string
	Symbol         string
	Kind           SemanticKind
	Name           string
	Signal         SignalKind
	Type           string
	Unit           string
	Description    string
	Owner          string
	Version        int
	MaxCardinality int
	Labels         []string
	PreviousName   string
	MigrationRef   string
}

type FindingCode string

const (
	FindingUnknownAttribute FindingCode = "unknown_attribute"
	FindingHighCardinality  FindingCode = "high_cardinality"
	FindingDynamicName      FindingCode = "dynamic_name"
	FindingMissingMetadata  FindingCode = "missing_metadata"
	FindingDuplicate        FindingCode = "duplicate_semantic_key"
	FindingBreakingRename   FindingCode = "breaking_rename"
	FindingUnknownMetric    FindingCode = "unknown_metric"
)

// Finding is a typed, exact CI defect. It never includes an observed value;
// high-cardinality values are represented by their key and bounded count.
type Finding struct {
	Code     FindingCode
	File     string
	Symbol   string
	Semantic string
	Detail   string
	Count    int
	Limit    int
}

func (f Finding) Error() string {
	return fmt.Sprintf("telemetry: %s at %s:%s (%s): %s", f.Code, f.File, f.Symbol, f.Semantic, f.Detail)
}

// FindingsError refuses a schema batch while retaining all typed findings.
type FindingsError struct{ Findings []Finding }

func (e *FindingsError) Error() string {
	if e == nil || len(e.Findings) == 0 {
		return "telemetry: no schema findings"
	}
	return e.Findings[0].Error()
}

// SchemaGate combines the pinned semantic registry and bounded cardinality
// state used by tests and CI checks.
type SchemaGate struct {
	allow   *Allowlist
	metrics map[string]MetricDefinition
	mu      sync.Mutex
	values  map[string]map[string]struct{}
}

func NewSchemaGate(allow *Allowlist, metrics []MetricDefinition) *SchemaGate {
	compiled := CatalogByName(metrics)
	return &SchemaGate{allow: allow, metrics: compiled, values: make(map[string]map[string]struct{})}
}

// Observe rejects unknown attributes and refuses the value that would exceed
// the registered cardinality bound. The value itself is never copied into a
// Finding.
func (g *SchemaGate) Observe(signal SignalKind, key, value string) (bool, Finding) {
	if g == nil || g.allow == nil {
		return false, Finding{Code: FindingUnknownAttribute, Semantic: key, Detail: "attribute registry unavailable"}
	}
	def, ok := g.allow.Lookup(key)
	if !ok || !g.allow.AllowsSignal(key, signal) {
		return false, Finding{Code: FindingUnknownAttribute, Semantic: key, Detail: "attribute is not registered for this signal"}
	}
	if def.MaxCardinality <= 0 {
		return true, Finding{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	set := g.values[key]
	if set == nil {
		set = make(map[string]struct{})
		g.values[key] = set
	}
	if _, exists := set[value]; !exists && len(set) >= def.MaxCardinality {
		return false, Finding{Code: FindingHighCardinality, Semantic: key, Detail: "value exceeds registered cardinality budget", Count: len(set) + 1, Limit: def.MaxCardinality}
	}
	set[value] = struct{}{}
	return true, Finding{}
}

// Lint validates semantic definitions against the pinned registry. Returned
// findings are sorted by source coordinates for reproducible CI output.
func (g *SchemaGate) Lint(defs []SemanticDefinition) []Finding {
	var findings []Finding
	seen := make(map[string]SemanticDefinition, len(defs))
	for _, def := range defs {
		key := string(def.Kind) + ":" + def.Name
		if _, exists := seen[key]; exists {
			findings = append(findings, Finding{Code: FindingDuplicate, File: def.File, Symbol: def.Symbol, Semantic: def.Name, Detail: "semantic name is declared more than once"})
		}
		seen[key] = def
		if def.Name == "" || strings.ContainsAny(def.Name, "{}$") {
			findings = append(findings, Finding{Code: FindingDynamicName, File: def.File, Symbol: def.Symbol, Semantic: def.Name, Detail: "semantic name must be static"})
		}
		if def.Version <= 0 || def.Owner == "" || def.Description == "" || (def.Kind == SemanticMetric && def.Unit == "") {
			findings = append(findings, Finding{Code: FindingMissingMetadata, File: def.File, Symbol: def.Symbol, Semantic: def.Name, Detail: "version, owner, description and metric unit are required"})
		}
		if def.PreviousName != "" && def.MigrationRef == "" {
			findings = append(findings, Finding{Code: FindingBreakingRename, File: def.File, Symbol: def.Symbol, Semantic: def.Name, Detail: "rename requires a compatibility migration reference"})
		}
		if def.Kind == SemanticAttribute {
			if g == nil || g.allow == nil || !g.allow.AllowsSignal(def.Name, def.Signal) {
				findings = append(findings, Finding{Code: FindingUnknownAttribute, File: def.File, Symbol: def.Symbol, Semantic: def.Name, Detail: "attribute is not registered for its signal"})
			}
		}
		if def.Kind == SemanticMetric {
			if g == nil {
				findings = append(findings, Finding{Code: FindingUnknownMetric, File: def.File, Symbol: def.Symbol, Semantic: def.Name, Detail: "metric registry unavailable"})
			} else if _, ok := g.metrics[def.Name]; !ok {
				findings = append(findings, Finding{Code: FindingUnknownMetric, File: def.File, Symbol: def.Symbol, Semantic: def.Name, Detail: "metric is not in the pinned catalog"})
			}
			for _, label := range def.Labels {
				if g == nil || g.allow == nil {
					continue
				}
				attr, ok := g.allow.Lookup(label)
				if !ok || attr.MaxCardinality <= 0 || !g.allow.AllowsSignal(label, SignalMetric) {
					findings = append(findings, Finding{Code: FindingHighCardinality, File: def.File, Symbol: def.Symbol, Semantic: label, Detail: "metric label is not bounded by the pinned registry"})
				}
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Symbol != findings[j].Symbol {
			return findings[i].Symbol < findings[j].Symbol
		}
		return findings[i].Code < findings[j].Code
	})
	return findings
}

// Refuse returns a typed error if linting found any incompatibility.
func (g *SchemaGate) Refuse(defs []SemanticDefinition) error {
	findings := g.Lint(defs)
	if len(findings) == 0 {
		return nil
	}
	return &FindingsError{Findings: findings}
}

// Explain is stable and suitable for CI output.
func (e *FindingsError) Explain() string {
	if e == nil {
		return ""
	}
	parts := make([]string, len(e.Findings))
	for i, finding := range e.Findings {
		parts[i] = finding.Error()
	}
	return strings.Join(parts, "\n")
}
