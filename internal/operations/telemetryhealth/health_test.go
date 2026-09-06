package telemetryhealth

import (
	"sync"
	"testing"
	"time"
)

func healthPolicy() Policy {
	return Policy{ID: "telemetry-pipeline", Version: "v1", StalenessBound: 5 * time.Minute, IncidentAfter: 10 * time.Minute, MaxQueueDepth: 100, MaxDropCount: 0, MaxExportErrors: 0}
}

func healthyObservation(now time.Time) Observation {
	return Observation{CollectorOK: true, ExporterOK: true, PolicyOK: true, Watermark: now.Add(-time.Minute), ObservedAt: now.Add(-time.Minute), FailureSince: now}
}

func TestTodo_OPS_003(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	got, err := Evaluate(healthPolicy(), healthyObservation(now), now)
	if err != nil || got.Status != StatusHealthy || got.EvidenceQuality != EvidenceComplete || got.Incident != nil {
		t.Fatalf("healthy=%+v err=%v", got, err)
	}
	failing := healthyObservation(now)
	failing.ExporterOK = false
	failing.FailureSince = now.Add(-11 * time.Minute)
	got, err = Evaluate(healthPolicy(), failing, now)
	if err != nil || got.Status != StatusUnknown || got.Incident == nil || !got.Incident.Bounded {
		t.Fatalf("failure=%+v err=%v", got, err)
	}
}

func TestTodo_OPS_003_Property(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	for _, edit := range []func(*Observation){func(o *Observation) { o.CollectorOK = false }, func(o *Observation) { o.ExporterOK = false }, func(o *Observation) { o.PolicyOK = false }, func(o *Observation) { o.Watermark = now.Add(-6 * time.Minute) }, func(o *Observation) { o.DropCount = 1 }} {
		o := healthyObservation(now)
		edit(&o)
		got, err := Evaluate(healthPolicy(), o, now)
		if err != nil || got.Status != StatusUnknown || got.EvidenceQuality == EvidenceComplete {
			t.Fatalf("observation=%+v result=%+v err=%v", o, got, err)
		}
	}
}

func TestTodo_OPS_003_Golden(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	o := healthyObservation(now)
	o.ExporterOK = false
	o.FailureSince = now.Add(-5 * time.Minute)
	got, err := Evaluate(healthPolicy(), o, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Incident != nil || got.Reason != "telemetry exporter is unhealthy" {
		t.Fatalf("bounded interval violated: %+v", got)
	}
}

func TestTodo_OPS_003_Race(t *testing.T) {
	now := time.Now().UTC()
	p := healthPolicy()
	o := healthyObservation(now)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := Evaluate(p, o, now); err != nil || got.Status != StatusHealthy {
				t.Errorf("result=%+v err=%v", got, err)
			}
		}()
	}
	wg.Wait()
}
