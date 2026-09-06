package performance

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// placeholderEnvelopeFixture is intentionally labelled: a human must replace
// these values with the selected partner's observed workload envelope.
func placeholderEnvelopeFixture(t *testing.T) WorkloadEnvelope {
	t.Helper()
	for _, envelope := range EnvelopeFixtures() {
		if envelope.Tier == MediumTier {
			return envelope
		}
	}
	t.Fatal("medium placeholder envelope missing")
	return WorkloadEnvelope{}
}

func forecastFixture(t *testing.T) CostForecastInput {
	t.Helper()
	envelope := placeholderEnvelopeFixture(t)
	return CostForecastInput{
		SchemaVersion: SchemaVersion,
		ID:            "PLACEHOLDER_FORECAST_V1",
		Envelope:      envelope,
		Usage: UsageForecast{
			Commands: 1000, WorkflowRuns: 100, ConnectorOps: 250,
			StoredGBMonths: 4, AgentRuns: 10, PeakCommands: 2000, PeakConcurrency: 500,
		},
		Rates: UnitRates{
			CentsPerCommand: 2, CentsPerWorkflowRun: 20, CentsPerConnectorOp: 5,
			CentsPerStoredGBMonth: 100, CentsPerAgentRun: 50,
		},
		Allocations: []Allocation{
			{ID: "PLACEHOLDER_CUSTOMER_LABOR", Category: "CUSTOMER_LABOR", AmountCents: 12000, Allocated: true, Source: "PLACEHOLDER_WEDGE_007_CUSTOMER_RATE"},
			{ID: "PLACEHOLDER_HCM_NEXT_COST", Category: "HCM_NEXT_COST_TO_SERVE", AmountCents: 12000, Allocated: true, Source: "PLACEHOLDER_WEDGE_007_SUPPORT_RATE"},
		},
		WedgeCost:   CostEvidenceSummary{CustomerLaborCents: 12000, HCMNextCostCents: 12000, SourceRef: "PLACEHOLDER_WEDGE_007_REPORT"},
		BudgetCents: 100000,
	}
}

func TestTodo_PERF_ENV_001(t *testing.T) {
	for _, envelope := range EnvelopeFixtures() {
		if err := Check(envelope); err != nil {
			t.Fatalf("placeholder %s rejected: %v", envelope.ID, err)
		}
	}
	missing := placeholderEnvelopeFixture(t)
	missing.TenantCount = Limit{}
	missing.DegradationPolicy = ""
	diagnostics := Validate(missing)
	if !hasDiagnostic(diagnostics, "tenant_count", "MISSING_OR_INVALID") || !hasDiagnostic(diagnostics, "degradation_policy", "MISSING") {
		t.Fatalf("missing dimensions were not rejected: %v", diagnostics)
	}
}

func TestTodo_PERF_ENV_001_Security(t *testing.T) {
	envelope := placeholderEnvelopeFixture(t)
	envelope.Tier = "UNTRUSTED_TIER"
	diagnostics := Validate(envelope)
	if !hasDiagnostic(diagnostics, "tier", "INVALID") {
		t.Fatalf("invalid tier was admitted: %v", diagnostics)
	}
	envelope.Tier = MediumTier
	envelope.DegradationPolicy = "PLACEHOLDER_FAIL_OPEN"
	if err := Check(envelope); err != nil {
		t.Fatalf("explicit degradation policy should be mechanically valid: %v", err)
	}
}

func FuzzTodo_PERF_ENV_001(f *testing.F) {
	f.Add(int64(1), "SMALL")
	f.Add(int64(-1), "UNKNOWN")
	f.Fuzz(func(t *testing.T, tenants int64, tier string) {
		envelope := placeholderEnvelopeFixture(t)
		envelope.TenantCount.Value = tenants
		envelope.Tier = tier
		_ = Validate(envelope)
	})
}

func BenchmarkTodo_PERF_ENV_001(b *testing.B) {
	envelope := EnvelopeFixtures()[1]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Validate(envelope)
	}
}

func TestTodo_PERF_007(t *testing.T) {
	report, err := Forecast(forecastFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalCostCents != 30150 || report.BudgetRemainingCents != 69850 || report.BudgetStatus != BudgetWithin || len(report.UnitCosts) != 5 || len(report.BudgetAlerts) != 0 {
		t.Fatalf("forecast totals = %+v", report)
	}
	if !report.Capacity.WithinEnvelope || report.Digest == "" {
		t.Fatalf("forecast capacity/digest = %+v", report)
	}
	input := forecastFixture(t)
	input.BudgetCents = 100
	alert, err := Forecast(input)
	if err != nil || alert.BudgetStatus != BudgetAlertStatus || len(alert.BudgetAlerts) != 1 {
		t.Fatalf("budget alert = %+v err=%v", alert, err)
	}
}

func TestTodo_PERF_007_Golden(t *testing.T) {
	first, err := Forecast(forecastFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Forecast(forecastFixture(t))
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("forecast is not deterministic: first=%+v second=%+v err=%v", first, second, err)
	}
}

func FuzzTodo_PERF_007(f *testing.F) {
	f.Add(int64(1000), int64(2))
	f.Add(int64(-1), int64(2))
	f.Fuzz(func(t *testing.T, commands, rate int64) {
		input := forecastFixture(t)
		input.Usage.Commands = commands
		input.Rates.CentsPerCommand = rate
		_, _ = Forecast(input)
	})
}

func TestTodo_PERF_007_Integration(t *testing.T) {
	input := forecastFixture(t)
	input.Allocations[1].Allocated = false
	if _, err := Forecast(input); !errors.Is(err, ErrInvalidForecast) {
		t.Fatalf("unallocated spend returned %v, want invalid forecast", err)
	}
	input = forecastFixture(t)
	input.Allocations[0].AmountCents++
	if _, err := Forecast(input); !errors.Is(err, ErrInvalidForecast) {
		t.Fatalf("unbalanced spend returned %v, want invalid forecast", err)
	}
}

func BenchmarkTodo_PERF_007(b *testing.B) {
	input := forecastFixture(&testing.T{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Forecast(input)
	}
}

func hasDiagnostic(diagnostics []Diagnostic, field, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Field == field && diagnostic.Code == code {
			return true
		}
	}
	return false
}

func TestPerformanceContractNames(t *testing.T) {
	if Version() != SchemaVersion || !strings.Contains(Explain(), "PERF-007") {
		t.Fatalf("contract description = %q", Explain())
	}
}
