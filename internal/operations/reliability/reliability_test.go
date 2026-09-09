package reliability_test

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/reliability"
)

func root(t *testing.T) string {
	_, f, _, _ := runtime.Caller(0)
	d := filepath.Dir(f)
	for {
		if filepath.Base(d) == "hcm-next" {
			return d
		}
		d = filepath.Dir(d)
	}
}
func TestTodo_OPS_001(t *testing.T) {
	m, e := reliability.Load(filepath.Join(root(t), reliability.DefaultManifestPath))
	if e != nil {
		t.Fatal(e)
	}
	if r := reliability.Validate(m, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)); !r.Ready() {
		t.Fatalf("manifest rejected: %v", r.Diagnostics)
	}
}
func TestTodo_OPS_001_Golden(t *testing.T) {
	m, _ := reliability.Load(filepath.Join(root(t), reliability.DefaultManifestPath))
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-time.Hour)
	x := map[string]reliability.Measurement{"pilot.read.availability": {WindowStart: windowStart, WindowEnd: now, ObservedAt: now.Add(-time.Minute), Good: 1000, Valid: 1000, Total: 1000, LatencyP95Ms: 500}, "pilot.write.availability": {WindowStart: windowStart, WindowEnd: now, ObservedAt: now.Add(-time.Minute), Good: 992, Valid: 1000, Total: 1000, LatencyP95Ms: 900}}
	got := reliability.Evaluate(*m, x, now)
	if got[0].Status != reliability.StatusHealthy || got[1].Status != reliability.StatusAtRisk {
		t.Fatalf("results=%+v", got)
	}
}
func TestTodo_OPS_001_UnknownTelemetry(t *testing.T) {
	m, _ := reliability.Load(filepath.Join(root(t), reliability.DefaultManifestPath))
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	got := reliability.Evaluate(*m, map[string]reliability.Measurement{"pilot.read.availability": {WindowStart: now.Add(-time.Hour), WindowEnd: now, ObservedAt: now.Add(-10 * time.Minute), Good: 100, Valid: 100, Total: 100}}, now)
	if got[0].Status != reliability.StatusUnknown {
		t.Fatalf("stale result=%+v", got)
	}
}

func TestTodo_OPS_001_WindowAndActionValidation(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	m := &reliability.Manifest{Version: 1, Module: "pilot", EffectiveAt: now.Format(time.RFC3339), Owner: "ops", SLIs: []reliability.SLI{{ID: "read", Version: "v1", Capability: "read", Query: "good/valid", Denominator: "valid", Window: "1h", StalenessBound: "5m", Owner: "ops", AvailabilityTarget: .99, LatencyTargetMs: 100}}, SLOs: []reliability.SLO{{ID: "read-slo", Version: "v1", SLI: "read", Target: .99, LatencyTargetMs: 100, Window: "1h", Owner: "ops", BreachAction: "freeze", AtRiskAction: "sample"}}, Actions: []reliability.BudgetAction{{ID: "half", Version: "v1", Threshold: .5, Action: "sample", Owner: "ops"}}}
	if got := reliability.Validate(m, now); !got.Ready() {
		t.Fatalf("valid manifest rejected: %+v", got.Diagnostics)
	}
	m.SLIs[0].Denominator = ""
	if got := reliability.Validate(m, now); got.Ready() {
		t.Fatal("missing denominator must be rejected")
	}
}

func BenchmarkTodo_OPS_001(b *testing.B) {
	m, err := reliability.Load(filepath.Join(root(&testing.T{}), reliability.DefaultManifestPath))
	if err != nil {
		b.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	measurements := map[string]reliability.Measurement{"pilot.read.availability": {WindowStart: now.Add(-time.Hour), WindowEnd: now, ObservedAt: now.Add(-time.Minute), Good: 1000, Valid: 1000, Total: 1000}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = reliability.Evaluate(*m, measurements, now)
	}
}
