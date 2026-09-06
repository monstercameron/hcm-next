package cryptoagile

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Window is one contiguous span of a migration timeline during which
// exactly one suite is active and, optionally, a second is dual-signed
// alongside it. Window covers the half-open interval [Start, End): End is
// exclusive, and the zero End marks an open-ended window - "still current,
// no declared end" - which only a plan's last window may use.
type Window struct {
	Start         time.Time
	End           time.Time
	ActiveSuiteID string
	DualSuiteID   string
}

func (w Window) openEnded() bool { return w.End.IsZero() }

func (w Window) covers(t time.Time) bool {
	if t.Before(w.Start) {
		return false
	}
	if w.openEnded() {
		return true
	}
	return t.Before(w.End)
}

// MigrationPlan is the declared sequence of windows a crypto-agile migration
// moves through. It is inert data: [DualSigner] reads the window in effect
// to decide what to sign, and [Resume] reads the whole plan to decide what
// evidence a resumed process still owes.
type MigrationPlan struct {
	Windows []Window
}

// Plan validation failures. RED in the todo is exactly ErrPlanOverlap and
// ErrPlanGap: a hand-edited schedule that leaves two windows both claiming
// a moment, or neither claiming it.
var (
	ErrPlanEmpty       = errors.New("cryptoagile: migration plan has no windows")
	ErrPlanUnsorted    = errors.New("cryptoagile: migration plan windows must be sorted by start time")
	ErrPlanBadWindow   = errors.New("cryptoagile: migration plan window has no active suite or a backwards span")
	ErrPlanOpenNotLast = errors.New("cryptoagile: only the last migration plan window may be open-ended")
	ErrPlanOverlap     = errors.New("cryptoagile: migration plan windows overlap")
	ErrPlanGap         = errors.New("cryptoagile: migration plan windows leave a gap between them")
)

// Validate refuses an empty plan, a window missing its active suite, a
// window whose end is not after its start, more than one open-ended
// window, an unsorted window sequence, and - the invariant the todo names
// explicitly - any pair of consecutive windows that overlap or leave a gap.
// A valid plan's windows tile the timeline from the first window's start
// onward with no seam: every instant at or after that start belongs to
// exactly one window.
func (p MigrationPlan) Validate() error {
	if len(p.Windows) == 0 {
		return ErrPlanEmpty
	}
	for i, w := range p.Windows {
		if strings.TrimSpace(w.ActiveSuiteID) == "" {
			return fmt.Errorf("%w: window %d has no active suite", ErrPlanBadWindow, i)
		}
		if !w.openEnded() && !w.Start.Before(w.End) {
			return fmt.Errorf("%w: window %d start %s is not before end %s", ErrPlanBadWindow, i, w.Start, w.End)
		}
		if w.openEnded() && i != len(p.Windows)-1 {
			return fmt.Errorf("%w: window %d is open-ended but not the last window", ErrPlanOpenNotLast, i)
		}
		if i == 0 {
			continue
		}
		prev := p.Windows[i-1]
		if !prev.Start.Before(w.Start) {
			return fmt.Errorf("%w: window %d does not start after window %d", ErrPlanUnsorted, i, i-1)
		}
		switch {
		case prev.End.Before(w.Start):
			return fmt.Errorf("%w: window %d ends %s, window %d starts %s", ErrPlanGap, i-1, prev.End, i, w.Start)
		case prev.End.After(w.Start):
			return fmt.Errorf("%w: window %d ends %s, window %d starts %s", ErrPlanOverlap, i-1, prev.End, i, w.Start)
		}
	}
	return nil
}

// WindowAt returns the window covering t and its index, or ok=false when no
// window covers it (t is before the plan's first window starts).
func (p MigrationPlan) WindowAt(t time.Time) (w Window, index int, ok bool) {
	for i, win := range p.Windows {
		if win.covers(t) {
			return win, i, true
		}
	}
	return Window{}, -1, false
}
