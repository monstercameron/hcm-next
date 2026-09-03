package health

import "time"

// Clock is the injected source of "now" every freshness/staleness
// computation reads through, so a test can make a projection or an outbox
// row exactly as stale as a scenario requires instead of racing real time
// (see MEMORY "Browser pane starves timers" / this repo's own fixed-clock
// convention in internal/data/ledger's WithClock).
type Clock interface {
	Now() time.Time
}

// systemClock is the production Clock.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

// SystemClock is the production Clock: the host's wall clock, in UTC.
var SystemClock Clock = systemClock{}

// FakeClock is a deterministic, settable Clock for tests. The zero value is
// not usable; construct one with NewFakeClock.
type FakeClock struct{ t time.Time }

// NewFakeClock returns a FakeClock fixed at t.
func NewFakeClock(t time.Time) *FakeClock { return &FakeClock{t: t.UTC()} }

// Now implements Clock.
func (c *FakeClock) Now() time.Time { return c.t }

// Set moves the clock to an exact instant.
func (c *FakeClock) Set(t time.Time) { c.t = t.UTC() }

// Advance moves the clock forward by d (a negative d moves it backward).
func (c *FakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }
