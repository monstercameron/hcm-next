package timeauth

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// Reason codes recorded on Evidence explaining a non-TRUSTED health
// decision. They are stable machine-readable tokens, never free text.
const (
	ReasonSourceUntrusted       = "SOURCE_SELF_REPORTED_UNTRUSTED"
	ReasonSourceDegraded        = "SOURCE_SELF_REPORTED_DEGRADED"
	ReasonUncertaintyUnknown    = "UNCERTAINTY_UNKNOWN"
	ReasonUncertaintyExceedsMax = "UNCERTAINTY_EXCEEDS_MAX"
	ReasonUncertaintyElevated   = "UNCERTAINTY_ELEVATED"
	ReasonBackwardJump          = "BACKWARD_JUMP"
	ReasonSkewExceedsMax        = "SKEW_EXCEEDS_MAX"
	ReasonSkewElevated          = "SKEW_ELEVATED"
)

// Evidence is one temporal-evidence record: the reading Monitor.Observe
// evaluated, the health it decided, why, and the drift it measured against
// the previous sample. It is the record a sensitive operation binds to (see
// TrustedInstant) and the artifact an auditor reviews afterward.
type Evidence struct {
	Source      string
	ObservedAt  values.Instant
	Uncertainty time.Duration
	Health      Health
	Reasons     []string

	HasPrevious        bool
	DriftFromMonotonic time.Duration
	BackwardJump       bool
}

// Options configures a Monitor's drift and uncertainty thresholds. A field
// left at zero uses its documented default.
type Options struct {
	// SoftSkew is the drift magnitude beyond which Observe degrades health
	// to at least DEGRADED.
	SoftSkew time.Duration
	// MaxSkew is the drift magnitude beyond which Observe forces UNTRUSTED.
	// Must be >= SoftSkew.
	MaxSkew time.Duration
	// SoftUncertainty is the declared uncertainty beyond which Observe
	// degrades health to at least DEGRADED.
	SoftUncertainty time.Duration
	// MaxUncertainty is the declared uncertainty beyond which Observe
	// forces UNTRUSTED. Must be >= SoftUncertainty.
	MaxUncertainty time.Duration
}

// Default thresholds: generous for a host wall clock under normal NTP
// discipline, while still catching a materially skewed or under-specified
// source.
const (
	DefaultSoftSkew        = 250 * time.Millisecond
	DefaultMaxSkew         = 2 * time.Second
	DefaultSoftUncertainty = 500 * time.Millisecond
	DefaultMaxUncertainty  = 5 * time.Second
)

func (o Options) withDefaults() Options {
	if o.SoftSkew <= 0 {
		o.SoftSkew = DefaultSoftSkew
	}
	if o.MaxSkew <= 0 {
		o.MaxSkew = DefaultMaxSkew
	}
	if o.SoftUncertainty <= 0 {
		o.SoftUncertainty = DefaultSoftUncertainty
	}
	if o.MaxUncertainty <= 0 {
		o.MaxUncertainty = DefaultMaxUncertainty
	}
	return o
}

// Monitor wraps a Clock and turns successive Samples into temporal
// Evidence, tracking drift between wall and monotonic readings across
// calls. A Monitor is safe for concurrent use.
type Monitor struct {
	mu    sync.Mutex
	clock Clock
	opts  Options
	last  *Sample
}

// NewMonitor constructs a Monitor over clock with the given thresholds.
func NewMonitor(clock Clock, opts Options) (*Monitor, error) {
	if clock == nil {
		return nil, newError("NewMonitor", ErrNilClock, "")
	}
	opts = opts.withDefaults()
	if opts.MaxSkew < opts.SoftSkew {
		return nil, newError("NewMonitor", ErrInvalidThresholds, "MaxSkew %s < SoftSkew %s", opts.MaxSkew, opts.SoftSkew)
	}
	if opts.MaxUncertainty < opts.SoftUncertainty {
		return nil, newError("NewMonitor", ErrInvalidThresholds, "MaxUncertainty %s < SoftUncertainty %s", opts.MaxUncertainty, opts.SoftUncertainty)
	}
	return &Monitor{clock: clock, opts: opts}, nil
}

// Observe takes one Sample from the clock and returns the temporal evidence
// for it. Health is never TRUSTED unless every check Observe knows how to
// run passed. A self-reported UNTRUSTED source, a backward wall jump, an
// unknown or over-max uncertainty, and drift beyond MaxSkew all force
// health to UNTRUSTED regardless of any other input; a source claiming
// TRUSTED cannot override a symptom Observe measures directly.
//
// A monotonic regression between this sample and the previous one is not
// classified into Health at all: it is a broken source contract, and
// Observe returns ErrMonotonicRegression instead of evidence.
func (m *Monitor) Observe() (Evidence, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sample := m.clock.Sample()
	if err := sample.validate(); err != nil {
		return Evidence{}, err
	}

	health := HealthTrusted
	var reasons []string
	add := func(h Health, reason string) {
		health = worseHealth(health, h)
		reasons = append(reasons, reason)
	}

	switch sample.SelfReportedHealth {
	case HealthUntrusted:
		add(HealthUntrusted, ReasonSourceUntrusted)
	case HealthDegraded:
		add(HealthDegraded, ReasonSourceDegraded)
	}

	switch {
	case sample.Uncertainty == UncertaintyUnknown:
		add(HealthUntrusted, ReasonUncertaintyUnknown)
	case sample.Uncertainty > m.opts.MaxUncertainty:
		add(HealthUntrusted, ReasonUncertaintyExceedsMax)
	case sample.Uncertainty > m.opts.SoftUncertainty:
		add(HealthDegraded, ReasonUncertaintyElevated)
	}

	var hasPrevious bool
	var drift time.Duration
	var backwardJump bool
	if m.last != nil {
		hasPrevious = true
		prev := *m.last
		monotonicDelta := sample.Monotonic - prev.Monotonic
		if monotonicDelta < 0 {
			return Evidence{}, newError("Observe", ErrMonotonicRegression,
				"monotonic reading moved backward by %s", -monotonicDelta)
		}
		wallDelta := sample.Wall.Time().Sub(prev.Wall.Time())
		drift = wallDelta - monotonicDelta
		if wallDelta < 0 {
			backwardJump = true
			add(HealthUntrusted, ReasonBackwardJump)
		}
		absDrift := drift
		if absDrift < 0 {
			absDrift = -absDrift
		}
		switch {
		case absDrift > m.opts.MaxSkew:
			add(HealthUntrusted, ReasonSkewExceedsMax)
		case absDrift > m.opts.SoftSkew:
			add(HealthDegraded, ReasonSkewElevated)
		}
	}

	m.last = &sample

	return Evidence{
		Source:             sample.Source,
		ObservedAt:         sample.Wall,
		Uncertainty:        sample.Uncertainty,
		Health:             health,
		Reasons:            reasons,
		HasPrevious:        hasPrevious,
		DriftFromMonotonic: drift,
		BackwardJump:       backwardJump,
	}, nil
}

// TrustedInstant is the only way a sensitive operation should read time: a
// business-effective instant bound to the temporal evidence that justified
// trusting it. It intentionally carries no monotonic reading or other host
// clock internal: business-effective time (values.Instant) stays a
// distinct type from host clock time (Sample.Monotonic, a time.Duration
// tick count), so replaying or auditing a decision never requires or
// exposes the host's raw monotonic counter.
type TrustedInstant struct {
	At       values.Instant
	Evidence Evidence
}

// RequireTrusted observes the clock and returns a TrustedInstant, or
// ErrTimeUntrusted if the observed health is UNTRUSTED. TRUSTED and
// DEGRADED both bind: DEGRADED evidence still carries every reason that
// contributed to it, so a caller with a stricter risk tier can inspect
// Evidence.Health itself and refuse further, but the platform-wide floor is
// that UNTRUSTED time never reaches an approval, ledger signature, cutoff,
// or token-expiry decision.
func (m *Monitor) RequireTrusted() (TrustedInstant, error) {
	ev, err := m.Observe()
	if err != nil {
		return TrustedInstant{}, err
	}
	if ev.Health == HealthUntrusted {
		return TrustedInstant{}, fmt.Errorf("%w: %s", ErrTimeUntrusted, strings.Join(ev.Reasons, ","))
	}
	return TrustedInstant{At: ev.ObservedAt, Evidence: ev}, nil
}
