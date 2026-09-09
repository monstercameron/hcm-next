package telemetry_test

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

// businessSideEffectSpy is a test double standing in for every authority
// this package must never touch: the ledger, business-event log, outbox,
// human work queue and provider-request dispatcher. Check/telemetry.Sink
// have no method that could reach it; it exists only so a test can prove
// zero calls ever land here, however Check is implemented.
type businessSideEffectSpy struct {
	AuthoritativeRows int
	BusinessEvents    int
	OutboxEntries     int
	HumanWork         int
	ProviderRequests  int
}

func (s *businessSideEffectSpy) anyRecorded() bool {
	return s.AuthoritativeRows != 0 || s.BusinessEvents != 0 || s.OutboxEntries != 0 || s.HumanWork != 0 || s.ProviderRequests != 0
}

// TestTodo_OBS_006 proves the RED and GREEN clauses of planning/todos.md
// OBS-006: a malformed required-signal set is rejected with an
// OBS_006_REJECTED error naming the offending field/state/version, a
// missing or faulted signal never reports HEALTHY, and evaluating
// completeness never touches ledger/business-event/outbox/human-work/
// provider-request authority (Sink has no method that could).
func TestTodo_OBS_006(t *testing.T) {
	spy := &businessSideEffectSpy{}
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

	t.Run("RED_malformed_required_signals_rejected_with_exact_code", func(t *testing.T) {
		_, err := telemetry.Check(telemetry.RequiredSignals{Version: 0}, telemetry.FakeSink{}, telemetry.PipelineFaults{}, now)
		if err == nil {
			t.Fatal("Check() with Version 0 succeeded; want rejection")
		}
		rej, ok := err.(*telemetry.CompletenessError)
		if !ok {
			t.Fatalf("error is not *CompletenessError: %v", err)
		}
		if rej.Code != "OBS_006_REJECTED" || rej.Field != "required.version" || rej.State != "missing" {
			t.Fatalf("rejection = %+v, want code OBS_006_REJECTED field required.version state missing", rej)
		}
		if spy.anyRecorded() {
			t.Fatalf("rejection path recorded a business side effect: %+v", spy)
		}
	})

	t.Run("RED_seeded_defect_missing_signal_never_reports_healthy", func(t *testing.T) {
		required := telemetry.DefaultRequiredSignals()
		// Seeded defect: the sink is missing exactly one required metric
		// (as if a Collector/exporter silently dropped it). A buggy
		// completeness checker would still report HEALTHY because "most"
		// signals arrived; Check must not.
		incomplete := telemetry.FakeSink{
			Metrics:   required.RequiredMetrics[1:],
			LogEvents: required.RequiredLogEvents,
		}
		report, err := telemetry.Check(required, incomplete, telemetry.PipelineFaults{}, now)
		if err != nil {
			t.Fatalf("Check() = %v", err)
		}
		if report.Health == telemetry.HealthHealthy {
			t.Fatalf("missing signal %q still reported healthy", required.RequiredMetrics[0])
		}
		if len(report.MissingMetrics) != 1 || report.MissingMetrics[0] != required.RequiredMetrics[0] {
			t.Fatalf("MissingMetrics = %v, want exactly [%q]", report.MissingMetrics, required.RequiredMetrics[0])
		}
		if spy.anyRecorded() {
			t.Fatalf("degraded evaluation recorded a business side effect: %+v", spy)
		}
	})

	t.Run("RED_nil_sink_reports_unknown_not_healthy", func(t *testing.T) {
		required := telemetry.DefaultRequiredSignals()
		report, err := telemetry.Check(required, nil, telemetry.PipelineFaults{}, now)
		if err != nil {
			t.Fatalf("Check() = %v", err)
		}
		if report.Health != telemetry.HealthUnknown {
			t.Fatalf("Health = %v, want UNKNOWN for a completely lost sink", report.Health)
		}
	})

	t.Run("RED_redaction_failure_degrades_even_with_every_signal_present", func(t *testing.T) {
		required := telemetry.DefaultRequiredSignals()
		complete := telemetry.FakeSink{Metrics: required.RequiredMetrics, LogEvents: required.RequiredLogEvents}
		report, err := telemetry.Check(required, complete, telemetry.PipelineFaults{RedactionFailures: 1}, now)
		if err != nil {
			t.Fatalf("Check() = %v", err)
		}
		if report.Health == telemetry.HealthHealthy {
			t.Fatal("a reported redaction failure still evaluated as healthy")
		}
	})

	t.Run("GREEN_fully_observed_fault_free_evaluation_is_healthy", func(t *testing.T) {
		required := telemetry.DefaultRequiredSignals()
		complete := telemetry.FakeSink{Metrics: required.RequiredMetrics, LogEvents: required.RequiredLogEvents}
		report, err := telemetry.Check(required, complete, telemetry.PipelineFaults{}, now)
		if err != nil {
			t.Fatalf("Check() = %v", err)
		}
		if report.Health != telemetry.HealthHealthy {
			t.Fatalf("Health = %v, want HEALTHY", report.Health)
		}
		if len(report.MissingMetrics) != 0 || len(report.MissingLogEvents) != 0 {
			t.Fatalf("healthy report still names missing signals: %+v", report)
		}
		if !report.EvaluatedAt.Equal(now) {
			t.Fatalf("EvaluatedAt = %v, want %v", report.EvaluatedAt, now)
		}
	})
}

// TestTodo_OBS_006_Integration exercises the full derivation: required
// signals are drawn from the live metric catalog, and a sink that emits
// exactly the published catalog plus every log event certifies healthy,
// while a sink also reporting clock skew and backend lag beyond bound
// degrades even though every named signal is present.
func TestTodo_OBS_006_Integration(t *testing.T) {
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	required := telemetry.DefaultRequiredSignals()

	catalogNames := map[string]bool{}
	for _, m := range telemetry.MetricCatalog() {
		catalogNames[m.Name] = true
	}
	for _, name := range required.RequiredMetrics {
		if !catalogNames[name] {
			t.Fatalf("required metric %q is not in the live metric catalog", name)
		}
	}

	complete := telemetry.FakeSink{Metrics: required.RequiredMetrics, LogEvents: required.RequiredLogEvents}

	healthy, err := telemetry.Check(required, complete, telemetry.PipelineFaults{}, now)
	if err != nil || healthy.Health != telemetry.HealthHealthy {
		t.Fatalf("Check() = %+v, %v, want a healthy report", healthy, err)
	}

	faulted, err := telemetry.Check(required, complete, telemetry.PipelineFaults{
		ClockSkew: 10 * time.Second, ClockSkewBound: time.Second,
		BackendLag: time.Minute, BackendLagBound: 5 * time.Second,
	}, now)
	if err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if faulted.Health != telemetry.HealthDegraded {
		t.Fatalf("Health = %v, want DEGRADED when clock skew and backend lag both exceed bound", faulted.Health)
	}
	if faulted.Faults.ClockSkew != 10*time.Second || faulted.Faults.BackendLag != time.Minute {
		t.Fatalf("Report did not carry the observed faults through: %+v", faulted.Faults)
	}
}

// TestTodo_OBS_006_Mutation flips exactly one required signal from present
// to absent and asserts Health flips from HEALTHY to DEGRADED, and flips
// a fault value across its bound and asserts the same.
func TestTodo_OBS_006_Mutation(t *testing.T) {
	now := time.Now()
	required := telemetry.DefaultRequiredSignals()

	t.Run("single_missing_log_event_flips_health", func(t *testing.T) {
		complete := telemetry.FakeSink{Metrics: required.RequiredMetrics, LogEvents: required.RequiredLogEvents}
		healthy, err := telemetry.Check(required, complete, telemetry.PipelineFaults{}, now)
		if err != nil || healthy.Health != telemetry.HealthHealthy {
			t.Fatalf("baseline Check() = %+v, %v, want healthy", healthy, err)
		}

		missingOne := telemetry.FakeSink{Metrics: required.RequiredMetrics, LogEvents: required.RequiredLogEvents[1:]}
		degraded, err := telemetry.Check(required, missingOne, telemetry.PipelineFaults{}, now)
		if err != nil {
			t.Fatalf("Check() = %v", err)
		}
		if degraded.Health != telemetry.HealthDegraded {
			t.Fatalf("removing one required log event did not degrade health: %+v", degraded)
		}
	})

	t.Run("backend_lag_exactly_at_bound_passes_one_over_degrades", func(t *testing.T) {
		complete := telemetry.FakeSink{Metrics: required.RequiredMetrics, LogEvents: required.RequiredLogEvents}
		atBound, err := telemetry.Check(required, complete, telemetry.PipelineFaults{BackendLag: 5 * time.Second, BackendLagBound: 5 * time.Second}, now)
		if err != nil || atBound.Health != telemetry.HealthHealthy {
			t.Fatalf("lag exactly at bound: %+v, %v, want healthy", atBound, err)
		}
		overBound, err := telemetry.Check(required, complete, telemetry.PipelineFaults{BackendLag: 5*time.Second + 1, BackendLagBound: 5 * time.Second}, now)
		if err != nil {
			t.Fatalf("Check() = %v", err)
		}
		if overBound.Health != telemetry.HealthDegraded {
			t.Fatalf("lag one unit over bound: %+v, want degraded", overBound)
		}
	})
}
