package timeauth

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Health is the trust state a Sample or an Evidence carries.
type Health uint8

// Declared health states. The zero value, HealthUnspecified, is never a
// legal verdict on Evidence; it may appear only as a Sample field the
// source chose not to set.
const (
	HealthUnspecified Health = iota
	HealthTrusted
	HealthDegraded
	HealthUntrusted
)

var healthWire = map[Health]string{
	HealthTrusted:   "TRUSTED",
	HealthDegraded:  "DEGRADED",
	HealthUntrusted: "UNTRUSTED",
}

// String returns the stable wire token.
func (h Health) String() string {
	if w, ok := healthWire[h]; ok {
		return w
	}
	return "HEALTH_UNSPECIFIED"
}

// Valid reports whether h is a declared health state.
func (h Health) Valid() bool {
	_, ok := healthWire[h]
	return ok
}

// severity orders health states so combining several conditions never
// silently improves the outcome: TRUSTED < DEGRADED < UNTRUSTED.
func (h Health) severity() int {
	switch h {
	case HealthDegraded:
		return 1
	case HealthUntrusted:
		return 2
	default:
		return 0
	}
}

// worseHealth returns whichever of a, b is at least as severe.
func worseHealth(a, b Health) Health {
	if a.severity() >= b.severity() {
		return a
	}
	return b
}

// UncertaintyUnknown is the sentinel a Sample uses to declare that its
// source cannot bound its own error. An unknown uncertainty is never
// silently treated as zero: Monitor.Observe always classifies it UNTRUSTED.
const UncertaintyUnknown time.Duration = -1

// Sample is one reading from a Clock: a wall-clock estimate, a monotonic
// tick count, a declared uncertainty bound, and the source's own opinion of
// its health. Nothing in a Sample is business-effective time by itself; see
// TrustedInstant for the type that is.
type Sample struct {
	// Source names the origin of the reading, for example
	// "host_wall_clock", "ntp", or "external_time_authority". It is
	// evidence, not a trust decision.
	Source string

	// Wall is the source's estimate of the current UTC instant.
	Wall values.Instant

	// Monotonic is a tick count from a source-defined, arbitrary but
	// non-decreasing origin. It is comparable only to another Monotonic
	// reading taken from the same Clock.
	Monotonic time.Duration

	// Uncertainty is the source's declared bound on how far Wall may be
	// from true UTC. UncertaintyUnknown declares that bound is unknown.
	Uncertainty time.Duration

	// SelfReportedHealth is the source's own opinion of its health. Monitor
	// treats it as one input among several, never as the sole authority: a
	// confused or compromised source claiming TRUSTED does not override
	// symptoms Monitor observes directly, such as a backward wall jump or
	// excess drift.
	SelfReportedHealth Health
}

func (s Sample) validate() error {
	if err := s.Wall.Validate(); err != nil {
		return newError("Sample.validate", ErrInvalidSample, "wall instant: %v", err)
	}
	if s.SelfReportedHealth != HealthUnspecified && !s.SelfReportedHealth.Valid() {
		return newError("Sample.validate", ErrInvalidSample,
			"self-reported health %d is not a declared value", uint8(s.SelfReportedHealth))
	}
	if s.Uncertainty != UncertaintyUnknown && s.Uncertainty < 0 {
		return newError("Sample.validate", ErrInvalidSample,
			"uncertainty %s is negative and not the unknown sentinel", s.Uncertainty)
	}
	return nil
}

// Clock is the trusted-time port. Every source of time the platform trusts
// implements this one method: the host wall clock, an NTP-disciplined
// source, an external attested time authority, or FakeClock in tests.
type Clock interface {
	Sample() Sample
}
