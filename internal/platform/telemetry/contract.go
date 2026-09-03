package telemetry

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// This file loads definitions/telemetry/*.yaml — the checked-in, owned
// schema OBS-001/004/005 publish — into the same typed contracts this
// package uses at runtime. A composition root (outside this lane) may call
// these loaders with the deployed schema path to build its Allowlist,
// ExportPolicy and metric catalog directly from the published contract
// (OBS-001 REFACTOR: "generate signal-specific Go definitions and policy
// registries from the owned schema"). This package's own compiled-in
// defaults (DefaultAllowlist, P1ACellMetrics, ...) are tested against a
// load of the real repository file so the two can never silently drift.

// ResourceFieldContract is one Resource field entry in
// resource-contract.yaml.
type ResourceFieldContract struct {
	Name     string `yaml:"name"`
	Required bool   `yaml:"required"`
}

// ContextKeyContract is one context-propagation key entry in
// resource-contract.yaml.
type ContextKeyContract struct {
	Name      string `yaml:"name"`
	Required  bool   `yaml:"required"`
	MaxLength int    `yaml:"max_length"`
}

// AttributeContract is one attribute-allow-list entry in
// resource-contract.yaml.
type AttributeContract struct {
	Key            string   `yaml:"key"`
	Class          string   `yaml:"class"`
	Signals        []string `yaml:"signals"`
	MaxCardinality int      `yaml:"max_cardinality"`
	Description    string   `yaml:"description"`
}

// ResourceContract is the parsed form of
// definitions/telemetry/resource-contract.yaml.
type ResourceContract struct {
	Version                int                     `yaml:"version"`
	SchemaVersion          int                     `yaml:"schema_version"`
	ResourceFields         []ResourceFieldContract `yaml:"resource_fields"`
	ContextPropagationKeys []ContextKeyContract    `yaml:"context_propagation_keys"`
	AttributeAllowlist     []AttributeContract     `yaml:"attribute_allowlist"`
}

// LoadResourceContract reads and parses resource-contract.yaml at path.
func LoadResourceContract(path string) (ResourceContract, error) {
	var c ResourceContract
	if err := readYAML(path, &c); err != nil {
		return ResourceContract{}, err
	}
	return c, nil
}

// ToAllowlist compiles the contract's attribute_allowlist section into an
// Allowlist, exactly as NewAllowlist would from Go literals.
func (c ResourceContract) ToAllowlist() (*Allowlist, error) {
	defs := make([]AttributeDefinition, 0, len(c.AttributeAllowlist))
	for _, a := range c.AttributeAllowlist {
		signals := make([]SignalKind, 0, len(a.Signals))
		for _, s := range a.Signals {
			signals = append(signals, SignalKind(s))
		}
		defs = append(defs, AttributeDefinition{
			Key:            a.Key,
			Class:          AttributeClass(a.Class),
			Signals:        signals,
			MaxCardinality: a.MaxCardinality,
			Description:    a.Description,
		})
	}
	return NewAllowlist(defs...)
}

// ExportPolicyEntryContract is one export_policy entry in policy.yaml.
type ExportPolicyEntryContract struct {
	Class string   `yaml:"class"`
	Sinks []string `yaml:"sinks"`
}

// SamplingContract is the sampling section of policy.yaml.
type SamplingContract struct {
	Version                int      `yaml:"version"`
	SuccessSampleRate      float64  `yaml:"success_sample_rate"`
	ForcedRetentionClasses []string `yaml:"forced_retention_classes"`
}

// PolicyContract is the parsed form of definitions/telemetry/policy.yaml.
type PolicyContract struct {
	Version                  int                         `yaml:"version"`
	ExportPolicy             []ExportPolicyEntryContract `yaml:"export_policy"`
	CardinalityOverflowValue string                      `yaml:"cardinality_overflow_value"`
	Sampling                 SamplingContract            `yaml:"sampling"`
}

// LoadPolicyContract reads and parses policy.yaml at path.
func LoadPolicyContract(path string) (PolicyContract, error) {
	var c PolicyContract
	if err := readYAML(path, &c); err != nil {
		return PolicyContract{}, err
	}
	return c, nil
}

// ToExportPolicy compiles the contract's export_policy section.
func (c PolicyContract) ToExportPolicy() (ExportPolicy, error) {
	allowed := make(map[AttributeClass][]SinkClass, len(c.ExportPolicy))
	for _, e := range c.ExportPolicy {
		sinks := make([]SinkClass, 0, len(e.Sinks))
		for _, s := range e.Sinks {
			sinks = append(sinks, SinkClass(s))
		}
		allowed[AttributeClass(e.Class)] = sinks
	}
	return NewExportPolicy(c.Version, allowed)
}

// ToSamplingPolicy compiles the contract's sampling section.
func (c PolicyContract) ToSamplingPolicy() SamplingPolicy {
	return SamplingPolicy{Version: c.Sampling.Version, SuccessSampleRate: c.Sampling.SuccessSampleRate}
}

// MetricContract is one metrics entry in metrics-catalog.yaml.
type MetricContract struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"`
	Unit        string   `yaml:"unit"`
	Version     int      `yaml:"version"`
	Labels      []string `yaml:"labels"`
	Description string   `yaml:"description"`
}

// MetricsCatalogContract is the parsed form of
// definitions/telemetry/metrics-catalog.yaml.
type MetricsCatalogContract struct {
	Version int              `yaml:"version"`
	Cell    string           `yaml:"cell"`
	Metrics []MetricContract `yaml:"metrics"`
}

// LoadMetricsCatalogContract reads and parses metrics-catalog.yaml at path.
func LoadMetricsCatalogContract(path string) (MetricsCatalogContract, error) {
	var c MetricsCatalogContract
	if err := readYAML(path, &c); err != nil {
		return MetricsCatalogContract{}, err
	}
	return c, nil
}

// ToMetricDefinitions compiles the contract's metrics section.
func (c MetricsCatalogContract) ToMetricDefinitions() []MetricDefinition {
	out := make([]MetricDefinition, 0, len(c.Metrics))
	for _, m := range c.Metrics {
		out = append(out, MetricDefinition{
			Name: m.Name, Type: MetricType(m.Type), Unit: m.Unit, Version: m.Version,
			Labels: append([]string(nil), m.Labels...), Description: m.Description,
		})
	}
	return out
}

// PanelContract is one panel entry in dashboards.yaml.
type PanelContract struct {
	Title       string   `yaml:"title"`
	Metric      string   `yaml:"metric"`
	Aggregation string   `yaml:"aggregation"`
	GroupBy     []string `yaml:"group_by"`
}

// DashboardContract is one dashboard entry in dashboards.yaml.
type DashboardContract struct {
	ID      string          `yaml:"id"`
	Title   string          `yaml:"title"`
	Version int             `yaml:"version"`
	Panels  []PanelContract `yaml:"panels"`
}

// DashboardsContract is the parsed form of
// definitions/telemetry/dashboards.yaml.
type DashboardsContract struct {
	Version    int                 `yaml:"version"`
	Dashboards []DashboardContract `yaml:"dashboards"`
}

// LoadDashboardsContract reads and parses dashboards.yaml at path.
func LoadDashboardsContract(path string) (DashboardsContract, error) {
	var c DashboardsContract
	if err := readYAML(path, &c); err != nil {
		return DashboardsContract{}, err
	}
	return c, nil
}

// ToDashboards compiles the contract's dashboards section.
func (c DashboardsContract) ToDashboards() []Dashboard {
	out := make([]Dashboard, 0, len(c.Dashboards))
	for _, d := range c.Dashboards {
		panels := make([]DashboardPanel, 0, len(d.Panels))
		for _, p := range d.Panels {
			panels = append(panels, DashboardPanel{
				Title: p.Title, Metric: p.Metric, Aggregation: Aggregation(p.Aggregation),
				GroupBy: append([]string(nil), p.GroupBy...),
			})
		}
		out = append(out, Dashboard{ID: d.ID, Title: d.Title, Version: d.Version, Panels: panels})
	}
	return out
}

// AlertContract is one alert entry in alerts.yaml.
type AlertContract struct {
	ID           string  `yaml:"id"`
	Version      int     `yaml:"version"`
	Metric       string  `yaml:"metric"`
	Numerator    string  `yaml:"numerator"`
	Denominator  string  `yaml:"denominator"`
	Condition    string  `yaml:"condition"`
	Threshold    float64 `yaml:"threshold"`
	For          string  `yaml:"for"`
	MaxStaleness string  `yaml:"max_staleness"`
	Severity     string  `yaml:"severity"`
	Description  string  `yaml:"description"`
}

// AlertsContract is the parsed form of definitions/telemetry/alerts.yaml.
type AlertsContract struct {
	Version int             `yaml:"version"`
	Alerts  []AlertContract `yaml:"alerts"`
}

// LoadAlertsContract reads and parses alerts.yaml at path.
func LoadAlertsContract(path string) (AlertsContract, error) {
	var c AlertsContract
	if err := readYAML(path, &c); err != nil {
		return AlertsContract{}, err
	}
	return c, nil
}

// ToAlertRules compiles the contract's alerts section. A malformed
// duration string fails the whole load rather than silently producing a
// zero (always-firing, or never-evaluating) duration.
func (c AlertsContract) ToAlertRules() ([]AlertRule, error) {
	out := make([]AlertRule, 0, len(c.Alerts))
	for _, a := range c.Alerts {
		forDur, err := time.ParseDuration(a.For)
		if err != nil {
			return nil, fmt.Errorf("telemetry: alert %q field \"for\" %q: %w", a.ID, a.For, err)
		}
		var staleness time.Duration
		if a.MaxStaleness != "" {
			staleness, err = time.ParseDuration(a.MaxStaleness)
			if err != nil {
				return nil, fmt.Errorf("telemetry: alert %q field \"max_staleness\" %q: %w", a.ID, a.MaxStaleness, err)
			}
		}
		out = append(out, AlertRule{
			ID: a.ID, Version: a.Version, Metric: a.Metric, Numerator: a.Numerator, Denominator: a.Denominator,
			Condition: a.Condition, Threshold: a.Threshold, For: forDur, MaxStaleness: staleness,
			Severity: AlertSeverity(a.Severity), Description: a.Description,
		})
	}
	return out, nil
}

func readYAML(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("telemetry: reading %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		return fmt.Errorf("telemetry: parsing %s: %w", path, err)
	}
	return nil
}
