package telemetry_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

// Paths are relative to this package directory
// (internal/platform/telemetry), three levels above the repository root.
const (
	resourceContractPath = "../../../definitions/telemetry/resource-contract.yaml"
	policyContractPath   = "../../../definitions/telemetry/policy.yaml"
	metricsCatalogPath   = "../../../definitions/telemetry/metrics-catalog.yaml"
	dashboardsPath       = "../../../definitions/telemetry/dashboards.yaml"
	alertsPath           = "../../../definitions/telemetry/alerts.yaml"
)

// TestContractResourceYAMLMatchesCompiledDefaults proves
// definitions/telemetry/resource-contract.yaml and
// internal/platform/telemetry's compiled-in DefaultAllowlistDefinitions
// describe the same attribute allow-list (OBS-001 REFACTOR: the owned
// schema and the Go registry it generates must never silently drift).
func TestContractResourceYAMLMatchesCompiledDefaults(t *testing.T) {
	yamlContract, err := telemetry.LoadResourceContract(resourceContractPath)
	if err != nil {
		t.Fatalf("LoadResourceContract(%q) = %v", resourceContractPath, err)
	}
	if yamlContract.SchemaVersion != telemetry.ResourceSchemaVersion {
		t.Fatalf("YAML schema_version = %d, want %d (ResourceSchemaVersion)", yamlContract.SchemaVersion, telemetry.ResourceSchemaVersion)
	}

	fromYAML, err := yamlContract.ToAllowlist()
	if err != nil {
		t.Fatalf("ToAllowlist() = %v", err)
	}
	fromGo, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatalf("DefaultAllowlist() = %v", err)
	}

	yamlKeys := fromYAML.Keys()
	goKeys := fromGo.Keys()
	sort.Strings(yamlKeys)
	sort.Strings(goKeys)
	if !reflect.DeepEqual(yamlKeys, goKeys) {
		t.Fatalf("attribute key sets differ:\n  yaml: %v\n  go:   %v", yamlKeys, goKeys)
	}
}

// TestContractResourceYAMLAttributesMatchGoFieldByField compares every
// published attribute definition field by field between the YAML contract
// and the compiled-in Go defaults.
func TestContractResourceYAMLAttributesMatchGoFieldByField(t *testing.T) {
	yamlContract, err := telemetry.LoadResourceContract(resourceContractPath)
	if err != nil {
		t.Fatalf("LoadResourceContract(%q) = %v", resourceContractPath, err)
	}
	fromYAML, err := yamlContract.ToAllowlist()
	if err != nil {
		t.Fatalf("ToAllowlist() = %v", err)
	}
	for _, want := range telemetry.DefaultAllowlistDefinitions() {
		got, ok := fromYAML.Lookup(want.Key)
		if !ok {
			t.Fatalf("YAML contract is missing key %q, which the Go defaults publish", want.Key)
		}
		if got.Class != want.Class {
			t.Errorf("key %q: class = %q, want %q", want.Key, got.Class, want.Class)
		}
		if got.MaxCardinality != want.MaxCardinality {
			t.Errorf("key %q: max_cardinality = %d, want %d", want.Key, got.MaxCardinality, want.MaxCardinality)
		}
		gotSignals := append([]telemetry.SignalKind(nil), got.Signals...)
		wantSignals := append([]telemetry.SignalKind(nil), want.Signals...)
		sort.Slice(gotSignals, func(i, j int) bool { return gotSignals[i] < gotSignals[j] })
		sort.Slice(wantSignals, func(i, j int) bool { return wantSignals[i] < wantSignals[j] })
		if !reflect.DeepEqual(gotSignals, wantSignals) {
			t.Errorf("key %q: signals = %v, want %v", want.Key, gotSignals, wantSignals)
		}
	}
}

// TestContractPolicyYAMLMatchesCompiledDefaults proves
// definitions/telemetry/policy.yaml and internal/platform/telemetry's
// compiled-in DefaultExportPolicy/DefaultSamplingPolicy describe the same
// policy (OBS-004 REFACTOR).
func TestContractPolicyYAMLMatchesCompiledDefaults(t *testing.T) {
	yamlContract, err := telemetry.LoadPolicyContract(policyContractPath)
	if err != nil {
		t.Fatalf("LoadPolicyContract(%q) = %v", policyContractPath, err)
	}
	if yamlContract.Version != telemetry.DefaultPolicyVersion {
		t.Fatalf("YAML policy version = %d, want %d", yamlContract.Version, telemetry.DefaultPolicyVersion)
	}
	if yamlContract.CardinalityOverflowValue != "__overflow__" {
		t.Fatalf("cardinality_overflow_value = %q, want __overflow__ (matches CardinalityGovernor.Cap's bucket)", yamlContract.CardinalityOverflowValue)
	}

	fromYAML, err := yamlContract.ToExportPolicy()
	if err != nil {
		t.Fatalf("ToExportPolicy() = %v", err)
	}
	fromGo := telemetry.DefaultExportPolicy(telemetry.DefaultPolicyVersion)

	for _, class := range []telemetry.AttributeClass{telemetry.ClassOperationalPublic, telemetry.ClassOperationalRestricted, telemetry.ClassProhibited} {
		for _, sink := range []telemetry.SinkClass{telemetry.SinkMetricsBackend, telemetry.SinkLogBackend, telemetry.SinkTraceBackend} {
			if fromYAML.Allows(class, sink) != fromGo.Allows(class, sink) {
				t.Errorf("class %q sink %q: yaml allows=%v, go allows=%v", class, sink, fromYAML.Allows(class, sink), fromGo.Allows(class, sink))
			}
		}
	}

	yamlSampling := yamlContract.ToSamplingPolicy()
	goSampling := telemetry.DefaultSamplingPolicy()
	if yamlSampling != goSampling {
		t.Fatalf("sampling policy = %+v, want %+v", yamlSampling, goSampling)
	}

	gotClasses := append([]string(nil), yamlContract.Sampling.ForcedRetentionClasses...)
	sort.Strings(gotClasses)
	var wantClasses []string
	for _, c := range telemetry.DefaultForcedRetentionClasses() {
		wantClasses = append(wantClasses, string(c))
	}
	sort.Strings(wantClasses)
	if !reflect.DeepEqual(gotClasses, wantClasses) {
		t.Fatalf("forced_retention_classes = %v, want %v", gotClasses, wantClasses)
	}
}

// TestContractMetricsCatalogYAMLMatchesGoCatalog proves
// definitions/telemetry/metrics-catalog.yaml is exactly
// internal/platform/telemetry.MetricCatalog() (OBS-005 GREEN:
// "deterministic, validated by test").
func TestContractMetricsCatalogYAMLMatchesGoCatalog(t *testing.T) {
	yamlContract, err := telemetry.LoadMetricsCatalogContract(metricsCatalogPath)
	if err != nil {
		t.Fatalf("LoadMetricsCatalogContract(%q) = %v", metricsCatalogPath, err)
	}
	if yamlContract.Cell != "P1A" {
		t.Fatalf("cell = %q, want P1A", yamlContract.Cell)
	}
	fromYAML := yamlContract.ToMetricDefinitions()
	fromGo := telemetry.MetricCatalog()

	sortMetrics := func(m []telemetry.MetricDefinition) {
		sort.Slice(m, func(i, j int) bool { return m[i].Name < m[j].Name })
	}
	sortMetrics(fromYAML)
	sortMetrics(fromGo)
	if !reflect.DeepEqual(fromYAML, fromGo) {
		t.Fatalf("metrics-catalog.yaml does not match MetricCatalog():\n  yaml: %+v\n  go:   %+v", fromYAML, fromGo)
	}

	allow := testAllowlist(t)
	if err := telemetry.ValidateCatalog(fromYAML, allow); err != nil {
		t.Fatalf("ValidateCatalog(yaml-loaded catalog) = %v", err)
	}
}

// TestContractDashboardsYAMLMatchesGoAndValidates proves
// definitions/telemetry/dashboards.yaml matches
// internal/platform/telemetry.P1ACellDashboards() and validates against
// the live catalog.
func TestContractDashboardsYAMLMatchesGoAndValidates(t *testing.T) {
	yamlContract, err := telemetry.LoadDashboardsContract(dashboardsPath)
	if err != nil {
		t.Fatalf("LoadDashboardsContract(%q) = %v", dashboardsPath, err)
	}
	fromYAML := yamlContract.ToDashboards()
	fromGo := telemetry.P1ACellDashboards()
	if !reflect.DeepEqual(fromYAML, fromGo) {
		t.Fatalf("dashboards.yaml does not match P1ACellDashboards():\n  yaml: %+v\n  go:   %+v", fromYAML, fromGo)
	}

	allow := testAllowlist(t)
	catalog := telemetry.CatalogByName(telemetry.MetricCatalog())
	for _, d := range fromYAML {
		if err := telemetry.ValidateDashboard(d, catalog, allow); err != nil {
			t.Fatalf("dashboard %q loaded from YAML fails validation: %v", d.ID, err)
		}
	}
}

// TestContractAlertsYAMLMatchesGoAndValidates proves
// definitions/telemetry/alerts.yaml matches
// internal/platform/telemetry.P1ACellAlertRules() and validates against
// the live catalog.
func TestContractAlertsYAMLMatchesGoAndValidates(t *testing.T) {
	yamlContract, err := telemetry.LoadAlertsContract(alertsPath)
	if err != nil {
		t.Fatalf("LoadAlertsContract(%q) = %v", alertsPath, err)
	}
	fromYAML, err := yamlContract.ToAlertRules()
	if err != nil {
		t.Fatalf("ToAlertRules() = %v", err)
	}
	fromGo := telemetry.P1ACellAlertRules()
	if !reflect.DeepEqual(fromYAML, fromGo) {
		t.Fatalf("alerts.yaml does not match P1ACellAlertRules():\n  yaml: %+v\n  go:   %+v", fromYAML, fromGo)
	}

	catalog := telemetry.CatalogByName(telemetry.MetricCatalog())
	for _, r := range fromYAML {
		if err := telemetry.ValidateAlertRule(r, catalog); err != nil {
			t.Fatalf("alert %q loaded from YAML fails validation: %v", r.ID, err)
		}
	}
}
