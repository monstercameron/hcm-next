package slo

import (
	"testing"
	"time"
)

func testTarget() Target {
	return Target{ID: "slo-admission", Capability: "operations.admission", Indicator: "accepted work", Query: "accepted/eligible", Window: time.Minute, StalenessBound: time.Minute, Threshold: .99, Owner: "reliability", Version: "v1"}
}

func TestTodo_OPS_001(t *testing.T) {
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	target := testTarget()
	result, err := Evaluate(target, Observation{ObservedAt: at.Add(-10 * time.Second), WindowFrom: at.Add(-time.Minute), WindowTo: at, Total: 1000, Good: 985, Complete: true}, at)
	if err != nil || result.Status != AtRisk || result.Version != "v1" || result.Denominator != 1000 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestTodo_OPS_001_Golden(t *testing.T) {
	if err := (Manifest{Version: "v1", Targets: []Target{testTarget()}}).Validate(); err != nil {
		t.Fatal(err)
	}
	result, err := Evaluate(testTarget(), Observation{ObservedAt: time.Unix(0, 0), WindowFrom: time.Unix(-60, 0), WindowTo: time.Unix(0, 0), Total: 1, Good: 1, Complete: true}, time.Unix(0, 0))
	if err != nil || result.Status != Healthy || result.Reason != "TARGET_MET" {
		t.Fatalf("golden result=%+v err=%v", result, err)
	}
}

func BenchmarkTodo_OPS_001(b *testing.B) {
	target := testTarget()
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	observation := Observation{ObservedAt: at, WindowFrom: at.Add(-time.Minute), WindowTo: at, Total: 100, Good: 100, Complete: true}
	for i := 0; i < b.N; i++ {
		_, _ = Evaluate(target, observation, at)
	}
}
