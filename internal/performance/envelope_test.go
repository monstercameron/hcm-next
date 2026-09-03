package performance

import (
	"errors"
	"testing"
)

func TestTodo_PERF_ENV_001(t *testing.T) {
	for _, e := range Fixtures() {
		if err := e.Validate(); err != nil {
			t.Errorf("%s: Validate() = %v", e.ID, err)
		}
		if err := Check(e, DefaultHardLimits); err != nil {
			t.Errorf("%s: Check() = %v", e.ID, err)
		}
	}
}

func TestTodo_PERF_ENV_001_MissingDimension(t *testing.T) {
	base := Small
	cases := map[string]func(*Envelope){
		"tenants":     func(e *Envelope) { e.Tenants = 0 },
		"tier":        func(e *Envelope) { e.Tier = "" },
		"concurrency": func(e *Envelope) { e.ConcurrentUsers = 0 },
		"commands":    func(e *Envelope) { e.CommandsPerSecond = 0 },
		"fanout":      func(e *Envelope) { e.Fanout = 0 },
		"payload":     func(e *Envelope) { e.PayloadBytes = 0 },
		"object":      func(e *Envelope) { e.Objects = 0 },
		"integration": func(e *Envelope) { e.Integrations = 0 },
		"agent":       func(e *Envelope) { e.AgentRuns = 0 },
		"peak":        func(e *Envelope) { e.PeakMultiplier = 0 },
		"degradation": func(e *Envelope) { e.Degradation = "" },
	}
	for name, mutate := range cases {
		e := base
		mutate(&e)
		if err := e.Validate(); !errors.Is(err, ErrInvalidEnvelope) {
			t.Errorf("%s: Validate() = %v, want ErrInvalidEnvelope", name, err)
		}
	}
}

func TestTodo_PERF_ENV_001_HardLimit(t *testing.T) {
	e := Small
	e.Fanout = DefaultHardLimits.Fanout + 1
	r := DefaultHardLimits.Check(e)
	if r.OK() || len(r.Violations) != 1 || r.Violations[0].Field != "fanout" {
		t.Fatalf("unexpected result: %+v", r)
	}
	if err := Check(e, DefaultHardLimits); !errors.Is(err, ErrHardLimit) {
		t.Fatalf("Check() = %v, want ErrHardLimit", err)
	}
}

func FuzzTodo_PERF_ENV_001(f *testing.F) {
	f.Add("small", 1.5)
	f.Add("peak", 4.0)
	f.Fuzz(func(t *testing.T, tier string, multiplier float64) {
		e := Small
		e.Tier = Tier(tier)
		e.PeakMultiplier = multiplier
		_ = e.Validate() // malformed input must return an error, never panic.
	})
}

func BenchmarkTodo_PERF_ENV_001(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Check(Peak, DefaultHardLimits)
	}
}
