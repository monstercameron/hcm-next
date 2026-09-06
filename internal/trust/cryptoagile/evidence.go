package cryptoagile

import (
	"fmt"
	"time"
)

// Evidence records one migration-plan window transition: the instant the
// plan entered window Index with ActiveSuiteID (and DualSuiteID, if that
// window declares one). It is the durable trail [Resume] reads back to pick
// an interrupted migration up from where it actually left off, rather than
// re-deriving state from wall-clock time alone - wall-clock time alone
// cannot distinguish "window 1's evidence was already recorded" from "it is
// about to be".
type Evidence struct {
	Index         int
	ActiveSuiteID string
	DualSuiteID   string
	At            time.Time
}

func evidenceForWindow(index int, w Window) Evidence {
	return Evidence{Index: index, ActiveSuiteID: w.ActiveSuiteID, DualSuiteID: w.DualSuiteID, At: w.Start}
}

// Resume computes the Evidence records that must exist for plan to be
// correctly positioned at now, given the trail already recorded. It returns
// only the NEW records - recorded is read, never mutated - so a caller
// appends the result to its own durable log and is caught up.
//
// This is the Recovery property: calling Resume(plan, nil, t) once, or
// calling it repeatedly with whatever prefix of its own prior output
// actually got durably recorded before each interruption, produces the
// identical final trail either way. A process that crashes after recording
// only part of what Resume told it to record simply calls Resume again
// later with what it did manage to persist; it never has to know it was
// interrupted "mid-window" as opposed to between windows - the recorded
// slice is the entire state Resume needs.
func Resume(plan MigrationPlan, recorded []Evidence, now time.Time) ([]Evidence, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	_, targetIndex, ok := plan.WindowAt(now)
	if !ok {
		return nil, fmt.Errorf("cryptoagile: no migration window covers %s", now.Format(time.RFC3339))
	}
	lastIndex := -1
	for _, ev := range recorded {
		if ev.Index > lastIndex {
			lastIndex = ev.Index
		}
	}
	if lastIndex > targetIndex {
		return nil, fmt.Errorf("cryptoagile: recorded evidence (window %d) is ahead of now (window %d)", lastIndex, targetIndex)
	}
	var next []Evidence
	for i := lastIndex + 1; i <= targetIndex; i++ {
		next = append(next, evidenceForWindow(i, plan.Windows[i]))
	}
	return next, nil
}
