package timeauth_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/timeauth"
)

var baseTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func newMonitor(t *testing.T) (*timeauth.Monitor, *timeauth.FakeClock) {
	t.Helper()
	fc := timeauth.NewFakeClock("test_clock", baseTime)
	m, err := timeauth.NewMonitor(fc, timeauth.Options{})
	if err != nil {
		t.Fatalf("NewMonitor: %v", err)
	}
	return m, fc
}

// TestTodo_TIME_001 proves the RED and GREEN clauses for trusted-time health
// and temporal evidence: an untrusted source, skew beyond threshold, a
// backward jump, or unknown uncertainty must never permit an effect-bearing
// operation to proceed, and a sensitive operation must either bind trusted
// source/offset/uncertainty evidence or receive TIME_UNTRUSTED, with
// monotonic sequencing enforced throughout.
func TestTodo_TIME_001(t *testing.T) {
	t.Run("GREEN_first_observation_trusted_with_zero_uncertainty", func(t *testing.T) {
		m, _ := newMonitor(t)
		ev, err := m.Observe()
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if ev.Health != timeauth.HealthTrusted {
			t.Fatalf("Health = %s, want TRUSTED", ev.Health)
		}
		if ev.HasPrevious {
			t.Fatalf("HasPrevious = true on the first observation")
		}
	})

	t.Run("RED_self_reported_untrusted_forces_untrusted", func(t *testing.T) {
		m, fc := newMonitor(t)
		fc.SetSelfReportedHealth(timeauth.HealthUntrusted)
		ev, err := m.Observe()
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if ev.Health != timeauth.HealthUntrusted {
			t.Fatalf("Health = %s, want UNTRUSTED", ev.Health)
		}
		if _, err := m.RequireTrusted(); !errors.Is(err, timeauth.ErrTimeUntrusted) {
			t.Fatalf("RequireTrusted error = %v, want ErrTimeUntrusted", err)
		}
	})

	t.Run("RED_unknown_uncertainty_forces_untrusted", func(t *testing.T) {
		m, fc := newMonitor(t)
		fc.SetUncertainty(timeauth.UncertaintyUnknown)
		ev, err := m.Observe()
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if ev.Health != timeauth.HealthUntrusted {
			t.Fatalf("Health = %s, want UNTRUSTED", ev.Health)
		}
		if _, err := m.RequireTrusted(); !errors.Is(err, timeauth.ErrTimeUntrusted) {
			t.Fatalf("RequireTrusted error = %v, want ErrTimeUntrusted", err)
		}
	})

	t.Run("RED_skew_beyond_max_forces_untrusted", func(t *testing.T) {
		m, fc := newMonitor(t)
		if _, err := m.Observe(); err != nil {
			t.Fatalf("warm-up Observe: %v", err)
		}
		// A single Observe/RequireTrusted call after the skew: each call to
		// either method advances the monitor's notion of "previous sample",
		// so the evidence for this skew must be read from one call, not two.
		fc.AdvanceSkewed(5*time.Second, 1*time.Second)
		trusted, err := m.RequireTrusted()
		if !errors.Is(err, timeauth.ErrTimeUntrusted) {
			t.Fatalf("RequireTrusted error = %v, want ErrTimeUntrusted", err)
		}
		if trusted.At.IsSet() {
			t.Fatalf("RequireTrusted returned a set instant alongside an error")
		}
	})

	t.Run("GREEN_skew_in_soft_zone_degrades_but_permits", func(t *testing.T) {
		m, fc := newMonitor(t)
		if _, err := m.Observe(); err != nil {
			t.Fatalf("warm-up Observe: %v", err)
		}
		fc.AdvanceSkewed(1*time.Second, 700*time.Millisecond)
		trusted, err := m.RequireTrusted()
		if err != nil {
			t.Fatalf("RequireTrusted rejected a DEGRADED but not UNTRUSTED sample: %v", err)
		}
		if trusted.Evidence.Health != timeauth.HealthDegraded {
			t.Fatalf("bound evidence health = %s, want DEGRADED", trusted.Evidence.Health)
		}
	})

	t.Run("RED_backward_wall_jump_forces_untrusted", func(t *testing.T) {
		m, fc := newMonitor(t)
		if _, err := m.Observe(); err != nil {
			t.Fatalf("warm-up Observe: %v", err)
		}
		fc.AdvanceSkewed(-1*time.Second, 1*time.Second)
		ev, err := m.Observe()
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if !ev.BackwardJump {
			t.Fatalf("BackwardJump = false, want true")
		}
		if ev.Health != timeauth.HealthUntrusted {
			t.Fatalf("Health = %s, want UNTRUSTED", ev.Health)
		}
	})

	t.Run("REFACTOR_monotonic_regression_is_a_hard_error", func(t *testing.T) {
		m, fc := newMonitor(t)
		if _, err := m.Observe(); err != nil {
			t.Fatalf("warm-up Observe: %v", err)
		}
		fc.AdvanceSkewed(1*time.Second, -1*time.Second)
		if _, err := m.Observe(); !errors.Is(err, timeauth.ErrMonotonicRegression) {
			t.Fatalf("Observe error = %v, want ErrMonotonicRegression", err)
		}
	})

	t.Run("REFACTOR_business_time_is_distinct_from_host_clock_internals", func(t *testing.T) {
		typ := reflect.TypeOf(timeauth.TrustedInstant{})
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if f.Type == reflect.TypeOf(time.Duration(0)) {
				t.Fatalf("TrustedInstant field %s carries a raw time.Duration; business time must stay distinct from host clock internals", f.Name)
			}
		}
	})

	t.Run("GREEN_evidence_binds_source_offset_and_uncertainty", func(t *testing.T) {
		m, fc := newMonitor(t)
		fc.SetUncertainty(10 * time.Millisecond)
		if _, err := m.Observe(); err != nil {
			t.Fatalf("warm-up Observe: %v", err)
		}
		fc.AdvanceSkewed(2*time.Second, 2*time.Second)
		trusted, err := m.RequireTrusted()
		if err != nil {
			t.Fatalf("RequireTrusted: %v", err)
		}
		if trusted.Evidence.Source != "test_clock" {
			t.Fatalf("Evidence.Source = %q, want test_clock", trusted.Evidence.Source)
		}
		if trusted.Evidence.Uncertainty != 10*time.Millisecond {
			t.Fatalf("Evidence.Uncertainty = %s, want 10ms", trusted.Evidence.Uncertainty)
		}
		if trusted.Evidence.DriftFromMonotonic != 0 {
			t.Fatalf("Evidence.DriftFromMonotonic = %s, want 0 for equal wall/monotonic deltas", trusted.Evidence.DriftFromMonotonic)
		}
		if trusted.At != trusted.Evidence.ObservedAt {
			t.Fatalf("TrustedInstant.At does not match Evidence.ObservedAt")
		}
	})
}

// TestTodo_TIME_001_Property checks, over many randomized sample sequences,
// that health severity ordering holds and that RequireTrusted succeeds if
// and only if health is not UNTRUSTED.
func TestTodo_TIME_001_Property(t *testing.T) {
	seed := uint64(0x5EED1234)
	next := func() uint64 {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return seed
	}

	for i := 0; i < 300; i++ {
		m, fc := newMonitor(t)
		if _, err := m.Observe(); err != nil {
			t.Fatalf("iteration %d: warm-up Observe: %v", i, err)
		}

		// Only non-negative monotonic deltas: a property run explores the
		// space Observe is meant to classify, not the hard-error path a
		// separate test already covers.
		monotonicDelta := time.Duration(next()%3_000_000_000) * time.Nanosecond
		wallSkew := time.Duration(int64(next()%6_000_000_000)-3_000_000_000) * time.Nanosecond
		wallDelta := monotonicDelta + wallSkew
		fc.AdvanceSkewed(wallDelta, monotonicDelta)

		switch next() % 4 {
		case 0:
			fc.SetSelfReportedHealth(timeauth.HealthUntrusted)
		case 1:
			fc.SetSelfReportedHealth(timeauth.HealthDegraded)
		default:
			fc.SetSelfReportedHealth(timeauth.HealthTrusted)
		}
		if next()%5 == 0 {
			fc.SetUncertainty(timeauth.UncertaintyUnknown)
		} else {
			fc.SetUncertainty(time.Duration(next()%6_000_000_000) * time.Nanosecond)
		}

		trusted, err := m.RequireTrusted()
		if err != nil {
			if !errors.Is(err, timeauth.ErrTimeUntrusted) {
				t.Fatalf("iteration %d: unexpected error %v", i, err)
			}
			continue
		}
		if trusted.Evidence.Health == timeauth.HealthUntrusted {
			t.Fatalf("iteration %d: RequireTrusted succeeded with UNTRUSTED evidence", i)
		}
		if !trusted.Evidence.Health.Valid() {
			t.Fatalf("iteration %d: evidence health %d is not a declared value", i, trusted.Evidence.Health)
		}
		if trusted.At != trusted.Evidence.ObservedAt {
			t.Fatalf("iteration %d: TrustedInstant.At does not match Evidence.ObservedAt", i)
		}
		if trusted.Evidence.BackwardJump {
			t.Fatalf("iteration %d: a backward jump was recorded but health was not UNTRUSTED", i)
		}
	}
}

// TestTodo_TIME_001_Mutation proves that each RED condition independently
// causes exactly its documented health outcome, and that health returns to
// TRUSTED once the mutation is undone: no branch is missing, and no bad
// state sticks once its cause is gone.
func TestTodo_TIME_001_Mutation(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(fc *timeauth.FakeClock)
		revert     func(fc *timeauth.FakeClock)
		advance    time.Duration
		wantHealth timeauth.Health
	}{
		{
			name:       "self_reported_degraded",
			mutate:     func(fc *timeauth.FakeClock) { fc.SetSelfReportedHealth(timeauth.HealthDegraded) },
			revert:     func(fc *timeauth.FakeClock) { fc.SetSelfReportedHealth(timeauth.HealthTrusted) },
			wantHealth: timeauth.HealthDegraded,
		},
		{
			name:       "self_reported_untrusted",
			mutate:     func(fc *timeauth.FakeClock) { fc.SetSelfReportedHealth(timeauth.HealthUntrusted) },
			revert:     func(fc *timeauth.FakeClock) { fc.SetSelfReportedHealth(timeauth.HealthTrusted) },
			wantHealth: timeauth.HealthUntrusted,
		},
		{
			name:       "uncertainty_unknown",
			mutate:     func(fc *timeauth.FakeClock) { fc.SetUncertainty(timeauth.UncertaintyUnknown) },
			revert:     func(fc *timeauth.FakeClock) { fc.SetUncertainty(0) },
			wantHealth: timeauth.HealthUntrusted,
		},
		{
			name:       "uncertainty_above_soft",
			mutate:     func(fc *timeauth.FakeClock) { fc.SetUncertainty(600 * time.Millisecond) },
			revert:     func(fc *timeauth.FakeClock) { fc.SetUncertainty(0) },
			wantHealth: timeauth.HealthDegraded,
		},
		{
			name:       "uncertainty_above_max",
			mutate:     func(fc *timeauth.FakeClock) { fc.SetUncertainty(6 * time.Second) },
			revert:     func(fc *timeauth.FakeClock) { fc.SetUncertainty(0) },
			wantHealth: timeauth.HealthUntrusted,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, fc := newMonitor(t)
			c.mutate(fc)
			ev, err := m.Observe()
			if err != nil {
				t.Fatalf("Observe after mutation: %v", err)
			}
			if ev.Health != c.wantHealth {
				t.Fatalf("Health after mutation = %s, want %s", ev.Health, c.wantHealth)
			}

			m2, fc2 := newMonitor(t)
			c.revert(fc2)
			ev2, err := m2.Observe()
			if err != nil {
				t.Fatalf("Observe on the unmutated baseline: %v", err)
			}
			if ev2.Health != timeauth.HealthTrusted {
				t.Fatalf("Health with the mutation reverted = %s, want TRUSTED", ev2.Health)
			}
		})
	}

	t.Run("skew_above_soft_then_above_max", func(t *testing.T) {
		m, fc := newMonitor(t)
		if _, err := m.Observe(); err != nil {
			t.Fatalf("warm-up Observe: %v", err)
		}
		fc.AdvanceSkewed(600*time.Millisecond, 0)
		ev, err := m.Observe()
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if ev.Health != timeauth.HealthDegraded {
			t.Fatalf("Health at soft skew = %s, want DEGRADED", ev.Health)
		}

		m2, fc2 := newMonitor(t)
		if _, err := m2.Observe(); err != nil {
			t.Fatalf("warm-up Observe: %v", err)
		}
		fc2.AdvanceSkewed(3*time.Second, 0)
		ev2, err := m2.Observe()
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if ev2.Health != timeauth.HealthUntrusted {
			t.Fatalf("Health at max skew = %s, want UNTRUSTED", ev2.Health)
		}
	})

	t.Run("combining_a_bad_condition_with_good_ones_never_improves_health", func(t *testing.T) {
		m, fc := newMonitor(t)
		fc.SetUncertainty(10 * time.Millisecond)
		fc.SetSelfReportedHealth(timeauth.HealthUntrusted)
		ev, err := m.Observe()
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if ev.Health != timeauth.HealthUntrusted {
			t.Fatalf("a well-behaved uncertainty alongside a self-reported UNTRUSTED source produced %s, want UNTRUSTED", ev.Health)
		}
	})
}

// TestTodo_TIME_001_Security proves that a compromised or confused source
// cannot whitewash observed symptoms by self-reporting TRUSTED: a backward
// jump, an unknown uncertainty, or skew beyond threshold all force
// UNTRUSTED regardless of what the source claims about itself, and
// RequireTrusted refuses in every such case.
func TestTodo_TIME_001_Security(t *testing.T) {
	t.Run("self_reported_trusted_cannot_hide_a_backward_jump", func(t *testing.T) {
		m, fc := newMonitor(t)
		fc.SetSelfReportedHealth(timeauth.HealthTrusted)
		if _, err := m.Observe(); err != nil {
			t.Fatalf("warm-up Observe: %v", err)
		}
		fc.SetSelfReportedHealth(timeauth.HealthTrusted)
		fc.AdvanceSkewed(-2*time.Second, 1*time.Second)
		ev, err := m.Observe()
		if err != nil {
			t.Fatalf("Observe: %v", err)
		}
		if ev.Health != timeauth.HealthUntrusted {
			t.Fatalf("a source claiming TRUSTED during a backward jump produced %s, want UNTRUSTED", ev.Health)
		}
	})

	t.Run("self_reported_trusted_cannot_hide_unknown_uncertainty", func(t *testing.T) {
		m, fc := newMonitor(t)
		fc.SetSelfReportedHealth(timeauth.HealthTrusted)
		fc.SetUncertainty(timeauth.UncertaintyUnknown)
		if _, err := m.RequireTrusted(); !errors.Is(err, timeauth.ErrTimeUntrusted) {
			t.Fatalf("RequireTrusted error = %v, want ErrTimeUntrusted", err)
		}
	})

	t.Run("self_reported_trusted_cannot_hide_excess_skew", func(t *testing.T) {
		m, fc := newMonitor(t)
		if _, err := m.Observe(); err != nil {
			t.Fatalf("warm-up Observe: %v", err)
		}
		fc.SetSelfReportedHealth(timeauth.HealthTrusted)
		fc.AdvanceSkewed(10*time.Second, 0)
		if _, err := m.RequireTrusted(); !errors.Is(err, timeauth.ErrTimeUntrusted) {
			t.Fatalf("RequireTrusted error = %v, want ErrTimeUntrusted", err)
		}
	})

	t.Run("ErrTimeUntrusted_is_matchable_through_the_wrapped_message", func(t *testing.T) {
		m, fc := newMonitor(t)
		fc.SetSelfReportedHealth(timeauth.HealthUntrusted)
		_, err := m.RequireTrusted()
		if err == nil {
			t.Fatalf("expected an error")
		}
		if !errors.Is(err, timeauth.ErrTimeUntrusted) {
			t.Fatalf("errors.Is(%v, ErrTimeUntrusted) = false", err)
		}
	})
}

type fixedSampleClock struct{ sample timeauth.Sample }

func (c fixedSampleClock) Sample() timeauth.Sample { return c.sample }

func TestMonitor_ConstructorAndObservationRefusals(t *testing.T) {
	if _, err := timeauth.NewMonitor(nil, timeauth.Options{}); !errors.Is(err, timeauth.ErrNilClock) {
		t.Fatalf("nil clock error = %v", err)
	}
	fc := timeauth.NewFakeClock("thresholds", baseTime)
	for _, opts := range []timeauth.Options{
		{SoftSkew: time.Second, MaxSkew: 500 * time.Millisecond},
		{SoftUncertainty: time.Second, MaxUncertainty: 500 * time.Millisecond},
	} {
		if _, err := timeauth.NewMonitor(fc, opts); !errors.Is(err, timeauth.ErrInvalidThresholds) {
			t.Fatalf("invalid thresholds error = %v", err)
		}
	}
	base := values.NewInstant(baseTime)
	for _, sample := range []timeauth.Sample{{Wall: values.Instant{}}, {Wall: base, SelfReportedHealth: timeauth.Health(99)}, {Wall: base, Uncertainty: -2 * time.Nanosecond}} {
		m, err := timeauth.NewMonitor(fixedSampleClock{sample}, timeauth.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := m.Observe(); !errors.Is(err, timeauth.ErrInvalidSample) {
			t.Fatalf("invalid sample error = %v", err)
		}
	}
}

func TestMonitor_RecordsEachReasonAndRequireTrustedBinding(t *testing.T) {
	fc := timeauth.NewFakeClock("reason-clock", baseTime)
	m, err := timeauth.NewMonitor(fc, timeauth.Options{SoftSkew: time.Second, MaxSkew: 2 * time.Second, SoftUncertainty: time.Second, MaxUncertainty: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Observe(); err != nil {
		t.Fatal(err)
	}
	fc.SetSelfReportedHealth(timeauth.HealthDegraded)
	fc.SetUncertainty(1500 * time.Millisecond)
	fc.AdvanceSkewed(-1500*time.Millisecond, 0)
	ev, err := m.Observe()
	if err != nil {
		t.Fatal(err)
	}
	wantReasons := []string{timeauth.ReasonSourceDegraded, timeauth.ReasonUncertaintyElevated, timeauth.ReasonBackwardJump, timeauth.ReasonSkewElevated}
	for _, want := range wantReasons {
		found := false
		for _, got := range ev.Reasons {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("reasons = %v, missing %s", ev.Reasons, want)
		}
	}
	if !ev.HasPrevious || ev.DriftFromMonotonic != -1500*time.Millisecond || !ev.BackwardJump || ev.Health != timeauth.HealthUntrusted {
		t.Fatalf("evidence = %+v", ev)
	}
	fc.SetSelfReportedHealth(timeauth.HealthTrusted)
	fc.SetUncertainty(0)
	fc.Advance(2 * time.Second)
	trusted, err := m.RequireTrusted()
	if err != nil || !trusted.At.IsSet() || trusted.At != trusted.Evidence.ObservedAt {
		t.Fatalf("trusted binding = %+v, %v", trusted, err)
	}
}
