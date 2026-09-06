package timeauth

import (
	"testing"
	"time"
)

func TestFakeclock_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestFakeclock_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestFakeClock_ProducesExactDrivenSamples(t *testing.T) {
	start := time.Date(2026, 9, 3, 12, 0, 0, 123, time.FixedZone("offset", -5*60*60))
	f := NewFakeClock("source", start)
	first := f.Sample()
	if first.Source != "source" || !first.Wall.Time().Equal(start.UTC()) || first.Monotonic != 0 || first.Uncertainty != 0 || first.SelfReportedHealth != HealthTrusted {
		t.Fatalf("initial sample = %+v", first)
	}
	f.Advance(2 * time.Second)
	f.AdvanceSkewed(-time.Second, 3*time.Second)
	f.SetUncertainty(UncertaintyUnknown)
	f.SetSelfReportedHealth(HealthDegraded)
	second := f.Sample()
	if !second.Wall.Time().Equal(start.UTC().Add(time.Second)) || second.Monotonic != 5*time.Second || second.Uncertainty != UncertaintyUnknown || second.SelfReportedHealth != HealthDegraded {
		t.Fatalf("driven sample = %+v", second)
	}
}
