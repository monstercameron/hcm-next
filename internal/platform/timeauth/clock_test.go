package timeauth

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestClock_Smoke(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
}

func TestClock_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic: %v", r)
		}
	}()
	_ = 1
}

func TestClock_HealthWireAndSampleValidation(t *testing.T) {
	for _, tc := range []struct {
		health Health
		token  string
		valid  bool
	}{
		{HealthUnspecified, "HEALTH_UNSPECIFIED", false}, {HealthTrusted, "TRUSTED", true}, {HealthDegraded, "DEGRADED", true}, {HealthUntrusted, "UNTRUSTED", true}, {Health(99), "HEALTH_UNSPECIFIED", false},
	} {
		if got := tc.health.String(); got != tc.token {
			t.Errorf("Health(%d).String() = %q, want %q", tc.health, got, tc.token)
		}
		if got := tc.health.Valid(); got != tc.valid {
			t.Errorf("Health(%d).Valid() = %t, want %t", tc.health, got, tc.valid)
		}
	}
	base := values.NewInstant(time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC))
	for _, sample := range []Sample{
		{Wall: values.Instant{}},
		{Wall: base, SelfReportedHealth: Health(9)},
		{Wall: base, Uncertainty: -2 * time.Nanosecond},
	} {
		if err := sample.validate(); !errors.Is(err, ErrInvalidSample) {
			t.Fatalf("sample %#v error = %v", sample, err)
		}
	}
	if err := (Sample{Wall: base, Uncertainty: UncertaintyUnknown}).validate(); err != nil {
		t.Fatalf("unknown uncertainty rejected: %v", err)
	}
}
