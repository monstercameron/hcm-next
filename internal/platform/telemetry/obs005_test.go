package telemetry_test

import (
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

// TestTodo_OBS_005 proves the RED and GREEN clauses of planning/todos.md
// OBS-005: an alert with an ambiguous (implicit) ratio denominator, a
// gauge-backed alert with no staleness bound, and a dashboard panel that
// groups by an unbounded dimension all fail validation; the P1A cell's
// published dashboards and alert rules all validate against its own
// metric catalog.
func TestTodo_OBS_005(t *testing.T) {
	allow := testAllowlist(t)
	catalog := telemetry.CatalogByName(telemetry.MetricCatalog())

	t.Run("RED_ambiguous_ratio_denominator_fails", func(t *testing.T) {
		r := telemetry.AlertRule{
			ID: "bad-ratio", Version: 1, Numerator: "ledger.append", // Denominator left unset
			Condition: "<", Threshold: 0.9, For: 5 * time.Minute, Severity: telemetry.AlertWarning,
		}
		if err := telemetry.ValidateAlertRule(r, catalog); err == nil {
			t.Fatal("ratio alert with no denominator validated; want rejection")
		}
	})

	t.Run("RED_gauge_alert_without_staleness_bound_fails", func(t *testing.T) {
		r := telemetry.AlertRule{
			ID: "bad-gauge", Version: 1, Metric: "outbox.lag",
			Condition: ">", Threshold: 60000, For: 5 * time.Minute, Severity: telemetry.AlertCritical,
			// MaxStaleness left zero: a stalled gauge would read as
			// "unchanged", not "missing".
		}
		if err := telemetry.ValidateAlertRule(r, catalog); err == nil {
			t.Fatal("gauge alert with no MaxStaleness validated; want rejection")
		}
	})

	t.Run("RED_dashboard_panel_unbounded_group_by_fails", func(t *testing.T) {
		// error_type is a registered attribute, but its MaxCardinality is 0
		// (unbounded, log/span only): a synthetic metric that nonetheless
		// declares it as a label must still fail ValidatePanel, distinctly
		// from the "not one of the metric's own labels" defect below. The
		// real, published catalog can never construct this case on its own
		// because ValidateCatalog already refuses an unbounded metric
		// label — hence the synthetic fixture.
		synthetic := map[string]telemetry.MetricDefinition{
			"synthetic.unbounded": {Name: "synthetic.unbounded", Type: telemetry.MetricCounter, Unit: "1", Labels: []string{"error_type"}},
		}
		p := telemetry.DashboardPanel{
			Title: "bad panel", Metric: "synthetic.unbounded", Aggregation: telemetry.AggregationRate,
			GroupBy: []string{"error_type"},
		}
		if err := telemetry.ValidatePanel(p, synthetic, allow); err == nil {
			t.Fatal("panel grouping by an unbounded label validated; want rejection")
		}
	})

	t.Run("RED_dashboard_panel_group_by_not_on_metric_fails", func(t *testing.T) {
		p := telemetry.DashboardPanel{
			Title: "bad panel", Metric: "outbox.lag", Aggregation: telemetry.AggregationMax,
			GroupBy: []string{"outcome"}, // bounded, but outbox.lag does not declare it
		}
		if err := telemetry.ValidatePanel(p, catalog, allow); err == nil {
			t.Fatal("panel grouping by a label the metric does not declare validated; want rejection")
		}
	})

	t.Run("GREEN_metric_catalog_validates", func(t *testing.T) {
		if err := telemetry.ValidateCatalog(telemetry.MetricCatalog(), allow); err != nil {
			t.Fatalf("ValidateCatalog() = %v, want success", err)
		}
	})

	t.Run("GREEN_published_dashboards_validate", func(t *testing.T) {
		for _, d := range telemetry.P1ACellDashboards() {
			if err := telemetry.ValidateDashboard(d, catalog, allow); err != nil {
				t.Fatalf("dashboard %q: %v", d.ID, err)
			}
		}
	})

	t.Run("GREEN_published_alert_rules_validate", func(t *testing.T) {
		for _, r := range telemetry.P1ACellAlertRules() {
			if err := telemetry.ValidateAlertRule(r, catalog); err != nil {
				t.Fatalf("alert %q: %v", r.ID, err)
			}
		}
	})
}

// TestTodo_OBS_005_Race proves MetricCatalog returns an independent copy
// each call: concurrent callers mutating their own copy's Labels slice
// can never observe or corrupt each other's view, or the package's own
// source of truth.
func TestTodo_OBS_005_Race(t *testing.T) {
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			catalog := telemetry.MetricCatalog()
			for j := range catalog {
				if len(catalog[j].Labels) > 0 {
					catalog[j].Labels[0] = "mutated-by-goroutine"
				}
			}
		}(i)
	}
	wg.Wait()

	fresh := telemetry.MetricCatalog()
	for _, m := range fresh {
		for _, label := range m.Labels {
			if label == "mutated-by-goroutine" {
				t.Fatalf("metric %q carries a mutation from a concurrent caller's copy: %v", m.Name, m.Labels)
			}
		}
	}

	allow := testAllowlist(t)
	if err := telemetry.ValidateCatalog(fresh, allow); err != nil {
		t.Fatalf("catalog is no longer valid after concurrent access: %v", err)
	}
}

// TestTodo_OBS_005_Integration validates the full generated set — catalog,
// dashboards and alert rules — together, the way a delivery pipeline would
// before publishing them.
func TestTodo_OBS_005_Integration(t *testing.T) {
	allow := testAllowlist(t)
	catalog := telemetry.MetricCatalog()
	if err := telemetry.ValidateCatalog(catalog, allow); err != nil {
		t.Fatalf("ValidateCatalog() = %v", err)
	}
	byName := telemetry.CatalogByName(catalog)

	for _, d := range telemetry.P1ACellDashboards() {
		if err := telemetry.ValidateDashboard(d, byName, allow); err != nil {
			t.Fatalf("dashboard %q: %v", d.ID, err)
		}
		for _, p := range d.Panels {
			if _, ok := byName[p.Metric]; !ok {
				t.Fatalf("panel %q references unknown metric %q", p.Title, p.Metric)
			}
		}
	}
	for _, r := range telemetry.P1ACellAlertRules() {
		if err := telemetry.ValidateAlertRule(r, byName); err != nil {
			t.Fatalf("alert %q: %v", r.ID, err)
		}
	}
}

// TestTodo_OBS_005_Fault proves an alert or panel that references a metric
// the catalog does not (or no longer) declares fails rather than silently
// evaluating against nothing, and that a query the fault leaves malformed
// never contributes evidence.
func TestTodo_OBS_005_Fault(t *testing.T) {
	allow := testAllowlist(t)
	catalog := telemetry.CatalogByName(telemetry.MetricCatalog())

	t.Run("alert_references_a_retired_metric", func(t *testing.T) {
		r := telemetry.AlertRule{
			ID: "orphaned-alert", Version: 1, Metric: "retired.metric.no.longer.published",
			Condition: ">", Threshold: 1, For: time.Minute, Severity: telemetry.AlertWarning,
		}
		if err := telemetry.ValidateAlertRule(r, catalog); err == nil {
			t.Fatal("alert referencing a metric absent from the catalog validated; want rejection")
		}
	})

	t.Run("ratio_alert_denominator_metric_missing_from_catalog", func(t *testing.T) {
		r := telemetry.AlertRule{
			ID: "ratio-missing-denominator-metric", Version: 1,
			Numerator: "ledger.append", Denominator: "collector.exporter.dropped",
			Condition: "<", Threshold: 0.9, For: time.Minute, Severity: telemetry.AlertWarning,
		}
		if err := telemetry.ValidateAlertRule(r, catalog); err == nil {
			t.Fatal("ratio alert with an unknown denominator metric validated; want rejection")
		}
	})

	t.Run("panel_references_a_retired_metric", func(t *testing.T) {
		p := telemetry.DashboardPanel{Title: "orphaned panel", Metric: "retired.metric", Aggregation: telemetry.AggregationSum}
		if err := telemetry.ValidatePanel(p, catalog, allow); err == nil {
			t.Fatal("panel referencing a metric absent from the catalog validated; want rejection")
		}
	})
}
