package performance

import (
	"errors"
	"reflect"
	"strings"
	"sync"
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

// ---------------------------------------------------------------------------
// PERF-005: sizing model fixtures and tests.
// ---------------------------------------------------------------------------

const (
	// sizingMeasuredNumerator/Denominator model a representative 60% peak
	// load against each dimension's declared capacity: a fixed, documented
	// ratio derived from the envelope fixtures, not a magic per-field number.
	sizingMeasuredNumerator   = 3
	sizingMeasuredDenominator = 5

	// wantSizingDimensionCount is the fixed shape of every complete PERF-005
	// model built by sizingModelFromEnvelope: 6 storage + 3 timer + 2 classes
	// x 2 (throughput, memory).
	wantSizingDimensionCount = 13

	smallObjectClass = "SMALL_OBJECT"
	largeObjectClass = "LARGE_OBJECT"
)

func envelopeByTier(tb testing.TB, tier string) WorkloadEnvelope {
	tb.Helper()
	for _, envelope := range EnvelopeFixtures() {
		if envelope.Tier == tier {
			return envelope
		}
	}
	tb.Fatalf("no fixture envelope for tier %s", tier)
	return WorkloadEnvelope{}
}

func measuredValue(capacity int64) int64 {
	return capacity * sizingMeasuredNumerator / sizingMeasuredDenominator
}

func buildDimension(capacityValue int64, unit string) Dimension {
	return Dimension{Capacity: Limit{capacityValue, unit}, Measured: Limit{measuredValue(capacityValue), unit}}
}

// sizingModelFromEnvelope derives every PERF-005 dimension from a
// PERF-ENV-001 WorkloadEnvelope's own declared fields (never a bare
// literal), so the sizing model stays tied to the declared envelopes.
func sizingModelFromEnvelope(id string, envelope WorkloadEnvelope) SizingModel {
	return SizingModel{
		SchemaVersion: SchemaVersion,
		ID:            id,
		EnvelopeID:    envelope.ID,
		Storage: StorageSizing{
			Rows:           buildDimension(envelope.DatabaseRows.Value, "rows"),
			IndexBytes:     buildDimension(envelope.ObjectBytes.Value, "bytes"),
			WALBytes:       buildDimension(envelope.PayloadBytes.Value, "bytes"),
			Locks:          buildDimension(envelope.ConcurrentCommands.Value, "locks"),
			ConnectionPool: buildDimension(envelope.ConcurrentUsers.Value, "connections"),
			VacuumSeconds:  buildDimension(envelope.QueueAgeSeconds.Value*10, "seconds"),
		},
		Timers: TimerSizing{
			Backlog:      buildDimension(envelope.DatabaseRows.Value, "timers"),
			DrainRate:    buildDimension(envelope.CommandsPerMinute.Value, "timers/minute"),
			DrainSeconds: buildDimension(envelope.QueueAgeSeconds.Value, "seconds"),
		},
		Artifacts: ArtifactSizing{Classes: []ArtifactClass{
			{Name: smallObjectClass, Throughput: buildDimension(envelope.IntegrationOps.Value, "MBps"), Memory: buildDimension(envelope.MemoryBytes.Value, "bytes")},
			{Name: largeObjectClass, Throughput: buildDimension(envelope.AgentRuns.Value, "MBps"), Memory: buildDimension(envelope.CPUMillis.Value, "bytes")},
		}},
	}
}

func cloneSizingModel(m SizingModel) SizingModel {
	clone := m
	clone.Artifacts.Classes = append([]ArtifactClass(nil), m.Artifacts.Classes...)
	return clone
}

func hasSizingDiagnostic(diagnostics []Rejection, field, state string) bool {
	for _, d := range diagnostics {
		if d.Field == field && d.State == state {
			return true
		}
	}
	return false
}

// TestTodo_PERF_005 is the PRIMARY test: a healthy sizing model derived from
// a declared envelope produces a complete report, a measured budget breach
// and an undersized peak model each return a typed PERF_005_REJECTED naming
// the offending field, state and model version, and none of the three calls
// mutates its input -- the pure-computation half of RED's "persist zero
// authoritative rows, business events, outbox entries, human work and
// provider requests" (there is no store, file or clock in Size's signature
// for any of that to reach, and the inputs prove untouched below).
func TestTodo_PERF_005(t *testing.T) {
	baseline := sizingModelFromEnvelope("PLACEHOLDER_SIZING_LARGE", envelopeByTier(t, LargeTier))
	before := cloneSizingModel(baseline)

	report, err := Size(baseline)
	if err != nil {
		t.Fatalf("healthy sizing model rejected: %v", err)
	}
	if len(report.Dimensions) != wantSizingDimensionCount {
		t.Fatalf("healthy report has %d dimensions, want %d: %+v", len(report.Dimensions), wantSizingDimensionCount, report)
	}
	if report.Digest == "" {
		t.Fatalf("healthy report has no digest: %+v", report)
	}
	if !reflect.DeepEqual(before, baseline) {
		t.Fatalf("Size mutated its input model: before=%+v after=%+v", before, baseline)
	}

	// Seeded defect 1: a measured budget breach.
	breach := cloneSizingModel(baseline)
	breach.Storage.Rows.Measured.Value = breach.Storage.Rows.Capacity.Value + 1
	beforeBreach := cloneSizingModel(breach)
	_, err = Size(breach)
	var rejection *Rejection
	if !errors.As(err, &rejection) {
		t.Fatalf("measured budget breach did not return a typed rejection: %v", err)
	}
	if rejection.Field != "storage.rows.measured" || rejection.State != StateBreach || rejection.ModelVersion != SchemaVersion {
		t.Fatalf("breach rejection = %+v", rejection)
	}
	if !errors.Is(err, ErrSizingRejected) {
		t.Fatalf("breach error does not unwrap to ErrSizingRejected: %v", err)
	}
	if !reflect.DeepEqual(beforeBreach, breach) {
		t.Fatalf("Size mutated the breached model: before=%+v after=%+v", beforeBreach, breach)
	}

	// Seeded defect 2: an undersized peak model (the timer-drain floor).
	undersized := cloneSizingModel(baseline)
	undersized.Timers.Backlog.Capacity.Value = MinTimerBacklogCapacity - 1
	beforeUndersized := cloneSizingModel(undersized)
	_, err = Size(undersized)
	rejection = nil
	if !errors.As(err, &rejection) {
		t.Fatalf("undersized peak model did not return a typed rejection: %v", err)
	}
	if rejection.Field != "timers.backlog.capacity" || rejection.State != StateUndersized || rejection.ModelVersion != SchemaVersion {
		t.Fatalf("undersized rejection = %+v", rejection)
	}
	if !errors.Is(err, ErrSizingRejected) {
		t.Fatalf("undersized error does not unwrap to ErrSizingRejected: %v", err)
	}
	if !reflect.DeepEqual(beforeUndersized, undersized) {
		t.Fatalf("Size mutated the undersized model: before=%+v after=%+v", beforeUndersized, undersized)
	}
}

// TestTodo_PERF_005_Mutation flips exactly one dimension at a time off a
// known-healthy model and proves ValidateSizing catches that one breach
// independently -- never an aggregate pass/fail across the whole model.
func TestTodo_PERF_005_Mutation(t *testing.T) {
	baseline := sizingModelFromEnvelope("PLACEHOLDER_SIZING_MUTATION", envelopeByTier(t, LargeTier))
	if err := CheckSizing(baseline); err != nil {
		t.Fatalf("baseline sizing model should be healthy: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*SizingModel)
		field  string
		state  string
	}{
		{"schema_version", func(m *SizingModel) { m.SchemaVersion = 0 }, "schema_version", StateUnsupportedVersion},
		{"id", func(m *SizingModel) { m.ID = "" }, "id", StateMissing},
		{"envelope_id", func(m *SizingModel) { m.EnvelopeID = "" }, "envelope_id", StateMissing},
		{"rows_missing_capacity", func(m *SizingModel) { m.Storage.Rows.Capacity = Limit{} }, "storage.rows.capacity", StateMissing},
		{"rows_breach", func(m *SizingModel) { m.Storage.Rows.Measured.Value = m.Storage.Rows.Capacity.Value + 1 }, "storage.rows.measured", StateBreach},
		{"index_bytes_missing_measured", func(m *SizingModel) { m.Storage.IndexBytes.Measured = Limit{} }, "storage.index_bytes.measured", StateMissing},
		{"wal_breach", func(m *SizingModel) { m.Storage.WALBytes.Measured.Value = m.Storage.WALBytes.Capacity.Value * 2 }, "storage.wal_bytes.measured", StateBreach},
		{"locks_missing_capacity", func(m *SizingModel) { m.Storage.Locks.Capacity = Limit{} }, "storage.locks.capacity", StateMissing},
		{"pool_breach", func(m *SizingModel) {
			m.Storage.ConnectionPool.Measured.Value = m.Storage.ConnectionPool.Capacity.Value + 1
		}, "storage.connection_pool.measured", StateBreach},
		{"vacuum_missing_measured", func(m *SizingModel) { m.Storage.VacuumSeconds.Measured = Limit{} }, "storage.vacuum_seconds.measured", StateMissing},
		{"timer_backlog_undersized", func(m *SizingModel) { m.Timers.Backlog.Capacity.Value = MinTimerBacklogCapacity - 1 }, "timers.backlog.capacity", StateUndersized},
		{"timer_drain_rate_breach", func(m *SizingModel) { m.Timers.DrainRate.Measured.Value = m.Timers.DrainRate.Capacity.Value + 1 }, "timers.drain_rate.measured", StateBreach},
		{"timer_drain_seconds_missing_capacity", func(m *SizingModel) { m.Timers.DrainSeconds.Capacity = Limit{} }, "timers.drain_seconds.capacity", StateMissing},
		{"artifacts_empty", func(m *SizingModel) { m.Artifacts.Classes = nil }, "artifacts.classes", StateMissing},
		{"artifact_throughput_breach", func(m *SizingModel) {
			m.Artifacts.Classes[0].Throughput.Measured.Value = m.Artifacts.Classes[0].Throughput.Capacity.Value + 1
		}, "artifacts.classes." + smallObjectClass + ".throughput_mbps.measured", StateBreach},
		{"artifact_memory_missing_capacity", func(m *SizingModel) {
			m.Artifacts.Classes[1].Memory.Capacity = Limit{}
		}, "artifacts.classes." + largeObjectClass + ".memory_bytes.capacity", StateMissing},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mutated := cloneSizingModel(baseline)
			tc.mutate(&mutated)
			diagnostics := ValidateSizing(mutated)
			if !hasSizingDiagnostic(diagnostics, tc.field, tc.state) {
				t.Fatalf("mutation %s not caught: want field=%s state=%s, got %+v", tc.name, tc.field, tc.state, diagnostics)
			}
			if len(diagnostics) != 1 {
				t.Fatalf("mutation %s produced %d diagnostics, want exactly 1 (aggregate pass/fail leaking through): %+v", tc.name, len(diagnostics), diagnostics)
			}
		})
	}
}

// TestTodo_PERF_005_Race proves determinism under concurrency: real
// goroutines evaluating the same model produce byte-identical reports, with
// no -race detector available on windows/arm64 locally.
func TestTodo_PERF_005_Race(t *testing.T) {
	model := sizingModelFromEnvelope("PLACEHOLDER_SIZING_RACE", envelopeByTier(t, PeakTier))
	const workers = 50
	results := make([]SizingReport, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = Size(model)
		}(i)
	}
	wg.Wait()

	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d unexpected error: %v", i, errs[i])
		}
		if !reflect.DeepEqual(results[0], results[i]) {
			t.Fatalf("concurrent Size produced divergent verdicts: worker0=%+v worker%d=%+v", results[0], i, results[i])
		}
	}
}

// TestTodo_PERF_005_Integration drives the sizing model from
// EnvelopeFixtures() rather than magic numbers, and proves every declared
// PERF-ENV-001 envelope yields a complete sizing with headroom on every
// dimension.
func TestTodo_PERF_005_Integration(t *testing.T) {
	for _, envelope := range EnvelopeFixtures() {
		model := sizingModelFromEnvelope("PLACEHOLDER_SIZING_"+envelope.Tier, envelope)
		report, err := Size(model)
		if err != nil {
			t.Fatalf("envelope %s produced a rejected sizing model: %v", envelope.ID, err)
		}
		if report.EnvelopeID != envelope.ID {
			t.Fatalf("envelope %s: report envelope id = %q", envelope.ID, report.EnvelopeID)
		}
		if len(report.Dimensions) != wantSizingDimensionCount {
			t.Fatalf("envelope %s produced %d dimensions, want %d", envelope.ID, len(report.Dimensions), wantSizingDimensionCount)
		}
		for _, dim := range report.Dimensions {
			if dim.HeadroomRatio <= 0 {
				t.Fatalf("envelope %s dimension %s has no headroom: %+v", envelope.ID, dim.Field, dim)
			}
			if dim.HardLimit != dim.Capacity {
				t.Fatalf("envelope %s dimension %s hard limit != capacity: %+v", envelope.ID, dim.Field, dim)
			}
			if dim.RolloverAt <= 0 || dim.RolloverAt > dim.Capacity {
				t.Fatalf("envelope %s dimension %s rollover point out of range: %+v", envelope.ID, dim.Field, dim)
			}
		}
	}
}

func BenchmarkTodo_PERF_005(b *testing.B) {
	model := sizingModelFromEnvelope("PLACEHOLDER_SIZING_BENCH", envelopeByTier(b, LargeTier))
	for b.Loop() {
		_, _ = Size(model)
	}
}
