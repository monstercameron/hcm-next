package timeauth

import (
	"sync"
	"time"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
)

// FakeClock is a manually driven Clock for tests. It never reads the host
// clock, so a test can construct an exact skew, backward-jump, or unknown-
// uncertainty scenario deterministically, and can compose it into any
// package that needs a trusted-time source in its own tests.
type FakeClock struct {
	mu          sync.Mutex
	source      string
	wall        time.Time
	monotonic   time.Duration
	uncertainty time.Duration
	health      Health
}

// NewFakeClock starts a fake clock at start with the given source name,
// TRUSTED health, and zero uncertainty.
func NewFakeClock(source string, start time.Time) *FakeClock {
	return &FakeClock{source: source, wall: start.UTC(), health: HealthTrusted, uncertainty: 0}
}

// Sample implements Clock.
func (f *FakeClock) Sample() Sample {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Sample{
		Source:             f.source,
		Wall:               values.NewInstant(f.wall),
		Monotonic:          f.monotonic,
		Uncertainty:        f.uncertainty,
		SelfReportedHealth: f.health,
	}
}

// Advance moves both the wall clock and the monotonic tick forward by d.
// This is the normal case: time passing without any skew.
func (f *FakeClock) Advance(d time.Duration) {
	f.AdvanceSkewed(d, d)
}

// AdvanceSkewed moves the wall clock forward by wallDelta and the monotonic
// tick forward by monotonicDelta independently, so a test can construct an
// exact drift between the two, a backward wall jump (a negative wallDelta),
// or a monotonic regression (a negative monotonicDelta) without either
// being confused for the other.
func (f *FakeClock) AdvanceSkewed(wallDelta, monotonicDelta time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wall = f.wall.Add(wallDelta)
	f.monotonic += monotonicDelta
}

// SetUncertainty sets the uncertainty the next Sample reports. Pass
// UncertaintyUnknown to simulate a source that cannot bound its own error.
func (f *FakeClock) SetUncertainty(u time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uncertainty = u
}

// SetSelfReportedHealth sets the health the next Sample self-reports.
func (f *FakeClock) SetSelfReportedHealth(h Health) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.health = h
}
