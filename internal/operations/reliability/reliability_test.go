package reliability_test

import (
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/operations/reliability"
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
	x := map[string]reliability.Measurement{"pilot.read.availability": {ObservedAt: now.Add(-time.Minute), Good: 1000, Valid: 1000, Total: 1000, LatencyP95Ms: 500}, "pilot.write.availability": {ObservedAt: now.Add(-time.Minute), Good: 992, Valid: 1000, Total: 1000, LatencyP95Ms: 900}}
	got := reliability.Evaluate(*m, x, now)
	if got[0].Status != reliability.StatusHealthy || got[1].Status != reliability.StatusAtRisk {
		t.Fatalf("results=%+v", got)
	}
}
func TestTodo_OPS_001_UnknownTelemetry(t *testing.T) {
	m, _ := reliability.Load(filepath.Join(root(t), reliability.DefaultManifestPath))
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	got := reliability.Evaluate(*m, map[string]reliability.Measurement{"pilot.read.availability": {ObservedAt: now.Add(-10 * time.Minute), Good: 100, Valid: 100, Total: 100}}, now)
	if got[0].Status != reliability.StatusUnknown {
		t.Fatalf("stale result=%+v", got)
	}
}
