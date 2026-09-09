package app

import (
	"sync/atomic"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/timeauth"
)

// HostClockSource is the source name a reading from this process's own wall
// clock is attributed to. It is evidence, not a trust decision: the monitor
// decides whether the reading is trustworthy.
const HostClockSource = "host_wall_clock"

// HostClockUncertainty is the bound this cell declares on its own wall clock.
//
// It is deliberately below timeauth's soft threshold: a host under normal NTP
// discipline is accurate to well inside this, and declaring a wider bound
// would mark every recording DEGRADED and make the signal useless. A host that
// is not under NTP discipline does not become trustworthy by being asked
// nicely - it drifts, and the monitor catches the drift against the monotonic
// reading regardless of what is declared here.
const HostClockUncertainty = 100 * time.Millisecond

// hostClock is the [timeauth.Clock] over this process's wall clock.
//
// The monotonic reading comes from a process-start baseline rather than from
// the wall clock, which is the whole point: a wall clock that jumps backwards
// is detectable only by comparing it against something that cannot.
type hostClock struct {
	now   func() time.Time
	start time.Time
	base  atomic.Int64
}

// newHostClock returns a clock over now, or over time.Now when now is nil.
func newHostClock(now func() time.Time) *hostClock {
	if now == nil {
		now = time.Now
	}
	c := &hostClock{now: now, start: now().UTC()}
	return c
}

// Sample implements [timeauth.Clock].
func (c *hostClock) Sample() timeauth.Sample {
	wall := c.now().UTC()
	// The monotonic tick is elapsed time since the process's first reading,
	// forced non-decreasing. A source that returned a decreasing tick would
	// break the monitor's contract outright rather than be classified, so this
	// clamps rather than reporting a regression it caused itself.
	tick := wall.Sub(c.start)
	for {
		previous := c.base.Load()
		if int64(tick) <= previous {
			tick = time.Duration(previous)
			break
		}
		if c.base.CompareAndSwap(previous, int64(tick)) {
			break
		}
	}
	return timeauth.Sample{
		Source:             HostClockSource,
		Wall:               values.NewInstant(wall),
		Monotonic:          tick,
		Uncertainty:        HostClockUncertainty,
		SelfReportedHealth: timeauth.HealthTrusted,
	}
}

// NewTrustedClock returns the recording clock a cell stamps its chronology
// with: the host wall clock, observed through a [timeauth.Monitor], so every
// recorded instant has passed a drift and uncertainty check.
//
// When the monitor refuses - the wall clock jumped backwards, drifted past the
// hard threshold, or the source declared itself untrusted - the returned clock
// answers with the unset instant. That is a deliberate fail-closed: the kernel
// refuses an instance whose recorded time is unset, so an untrusted clock
// stops the write instead of stamping the ledger with a time nobody vouches
// for. There is no error channel on [intent.Clock] to report it through, and
// adding one would let a caller choose to ignore it.
func NewTrustedClock(monitor *timeauth.Monitor) intent.Clock {
	return func() values.Instant {
		trusted, err := monitor.RequireTrusted()
		if err != nil {
			return values.Instant{}
		}
		return trusted.At
	}
}
