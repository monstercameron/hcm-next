package timeauth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/hcm-next/internal/platform/timeauth"
)

// clampNanos folds an arbitrary int64 into a bounded, sane nanosecond range
// so fuzz-supplied deltas cannot overflow time.Duration arithmetic and
// obscure the invariant under test with an unrelated wraparound.
func clampNanos(n int64) int64 {
	const bound = int64(10 * time.Hour)
	m := n % bound
	if m < 0 {
		m = -m
	}
	return m
}

// FuzzTodo_TIME_001 proves Monitor.Observe never panics for arbitrary
// sample sequences, that any error it returns is one of the two documented
// causes, and that the health invariants (a backward jump always yields
// UNTRUSTED, a declared health is always one of the three known values,
// and RequireTrusted refuses exactly when health is UNTRUSTED) hold for
// every input.
func FuzzTodo_TIME_001(f *testing.F) {
	f.Add(int64(0), int64(0), int64(0), uint8(0))
	f.Add(int64(1_000_000_000), int64(1_000_000_000), int64(0), uint8(1))
	f.Add(int64(-1_000_000_000), int64(1_000_000_000), int64(-1), uint8(2))
	f.Add(int64(1_000_000_000), int64(-1_000_000_000), int64(0), uint8(3))
	f.Add(int64(5_000_000_000), int64(100_000_000), int64(6_000_000_000), uint8(255))

	f.Fuzz(func(t *testing.T, wallDeltaNanos, monotonicDeltaNanos, uncertaintyNanos int64, selfHealth uint8) {
		wallDelta := time.Duration(clampNanos(wallDeltaNanos)) * time.Nanosecond
		if wallDeltaNanos < 0 {
			wallDelta = -wallDelta
		}
		monotonicDelta := time.Duration(clampNanos(monotonicDeltaNanos)) * time.Nanosecond
		if monotonicDeltaNanos < 0 {
			monotonicDelta = -monotonicDelta
		}
		var uncertainty time.Duration
		if uncertaintyNanos == 0 {
			uncertainty = timeauth.UncertaintyUnknown
		} else {
			uncertainty = time.Duration(clampNanos(uncertaintyNanos)) * time.Nanosecond
		}
		health := timeauth.Health(selfHealth % 4)

		// setup builds an identically configured, freshly warmed-up monitor
		// so that the Observe-based checks below and the RequireTrusted-
		// based checks each see their own single post-warm-up sample: every
		// call to either method advances "the previous sample," so sharing
		// one monitor across both checks would make the second call
		// measure drift against the first call's own sample instead of
		// against the fuzzed scenario.
		setup := func(t *testing.T) (*timeauth.Monitor, *timeauth.FakeClock) {
			t.Helper()
			fc := timeauth.NewFakeClock("fuzz_clock", baseTime)
			m, err := timeauth.NewMonitor(fc, timeauth.Options{})
			if err != nil {
				t.Fatalf("NewMonitor: %v", err)
			}
			if _, err := m.Observe(); err != nil {
				t.Fatalf("warm-up Observe: %v", err)
			}
			fc.AdvanceSkewed(wallDelta, monotonicDelta)
			fc.SetUncertainty(uncertainty)
			fc.SetSelfReportedHealth(health)
			return m, fc
		}

		m1, _ := setup(t)
		ev, err := m1.Observe()
		if err != nil {
			if !errors.Is(err, timeauth.ErrMonotonicRegression) {
				t.Fatalf("unexpected Observe error: %v", err)
			}
			if monotonicDelta >= 0 {
				t.Fatalf("ErrMonotonicRegression returned for a non-negative monotonic delta %s", monotonicDelta)
			}
			return
		}

		if !ev.Health.Valid() {
			t.Fatalf("Health %d is not a declared value", ev.Health)
		}
		if ev.BackwardJump && ev.Health != timeauth.HealthUntrusted {
			t.Fatalf("BackwardJump=true but Health=%s, want UNTRUSTED", ev.Health)
		}
		if ev.Health != timeauth.HealthTrusted && len(ev.Reasons) == 0 {
			t.Fatalf("Health=%s carries no Reasons", ev.Health)
		}

		m2, _ := setup(t)
		trusted, rtErr := m2.RequireTrusted()
		if ev.Health == timeauth.HealthUntrusted {
			if !errors.Is(rtErr, timeauth.ErrTimeUntrusted) {
				t.Fatalf("RequireTrusted error = %v, want ErrTimeUntrusted for UNTRUSTED evidence", rtErr)
			}
		} else {
			if rtErr != nil {
				t.Fatalf("RequireTrusted error = %v, want nil for %s evidence", rtErr, ev.Health)
			}
			if trusted.At != trusted.Evidence.ObservedAt {
				t.Fatalf("TrustedInstant.At does not match Evidence.ObservedAt")
			}
		}
	})
}
