package testexport

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
)

func TestTelemetryHarnessReturnsDeterministicLogsSpansMetricsAndDrops(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	h := NewHarness(HarnessConfig{Now: now, IDPrefix: "obs"})
	h.Log("workflow.advance", map[string]string{"outcome": "success"})
	span := h.StartSpan(string(telemetry.SpanWorkflowNode), map[string]string{"outcome": "success"})
	span.End(telemetry.OutcomeSuccess, nil)
	h.Metric("intent.created", 1, map[string]string{"cell_id": "cell-a", "outcome": "success"})
	h.Drop("span", "sampled")

	s := h.Snapshot()
	if got := s.Explain(); got != "logs=1,spans=1,metrics=1,drops=1" {
		t.Fatalf("snapshot = %q", got)
	}
	if !s.Logs[0].At.Equal(now) || s.Spans[0].Sequence <= s.Logs[0].Sequence || !s.Spans[0].Ended || s.Spans[0].Error {
		t.Fatalf("non-deterministic span snapshot: %+v", s.Spans)
	}
	if got := h.NextID(); got != "obs-000005" {
		t.Fatalf("next id = %q, want obs-000005", got)
	}
}

func TestTodo_OBS_015_Property(t *testing.T) {
	h := NewHarness(HarnessConfig{IDPrefix: "p"})
	attrs := map[string]string{"outcome": "success"}
	h.Log("event", attrs)
	attrs["outcome"] = "mutated"
	if h.Snapshot().Logs[0].Fields["outcome"] != "success" {
		t.Fatal("harness retained caller map")
	}
}

func TestTodo_OBS_015_Golden(t *testing.T) {
	h := NewHarness(HarnessConfig{IDPrefix: "g"})
	h.Log("one", nil)
	h.Metric("intent.created", 2, nil)
	if got := h.Snapshot().Explain(); got != "logs=1,spans=0,metrics=1,drops=0" {
		t.Fatalf("golden snapshot = %q", got)
	}
}

func TestTodo_OBS_015_Race(t *testing.T) {
	h := NewHarness(HarnessConfig{Capacity: 512})
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); h.Log("event", map[string]string{"outcome": "success"}) }()
	}
	wg.Wait()
	if got := len(h.Snapshot().Logs); got != 32 {
		t.Fatalf("concurrent log count = %d, want 32", got)
	}
}

func TestTodo_OBS_015_Security(t *testing.T) {
	allow, err := telemetry.DefaultAllowlist()
	if err != nil {
		t.Fatal(err)
	}
	h := NewHarness(HarnessConfig{})
	h.Log("secret", map[string]string{"password": "do-not-export"})
	if err := h.AssertNoProhibitedTelemetry(allow); err == nil {
		t.Fatal("prohibited field accepted by telemetry oracle")
	}
}

func TestTodo_OBS_015_Fault(t *testing.T) {
	h := NewHarness(HarnessConfig{Capacity: 1})
	h.Log("one", nil)
	h.Log("two", nil)
	if got := len(h.Snapshot().Drops); got != 1 || h.Snapshot().Drops[0].Reason != "capacity" {
		t.Fatalf("capacity drop = %+v", h.Snapshot().Drops)
	}
}

func TestTodo_OBS_015_Mutation(t *testing.T) {
	h := NewHarness(HarnessConfig{})
	h.Log("one", nil)
	first := h.Snapshot()
	first.Logs[0].Name = "mutated"
	if reflect.DeepEqual(first, h.Snapshot()) {
		t.Fatal("snapshot mutation changed exporter state")
	}
}
