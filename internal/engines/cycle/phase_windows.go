package cycle

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Phase/window validation is kept in this file so the base cycle contract can
// evolve independently from the executable phase graph.
var (
	ErrPhaseGap         = errors.New("cycle: phase window gap")
	ErrPhaseOverlap     = errors.New("cycle: phase window overlap")
	ErrPhaseUnreachable = errors.New("cycle: unreachable phase")
	ErrPhaseBoundary    = errors.New("cycle: ambiguous phase boundary")
	ErrCutoff           = errors.New("cycle: invalid cutoff policy")
)

// The cutoff vocabulary is intentionally small. A future calendar engine may
// add policies, but arbitrary strings must not silently change close behavior.
const (
	CutoffAtEnd    = "CLOSE_AT_END"
	CutoffAtCutoff = "CLOSE_AT_CUTOFF"
	CutoffOnSignal = "CLOSE_ON_SIGNAL"
)

// CompiledPhase is the immutable, executable view of a declared phase.
type CompiledPhase struct {
	ID, Name          string
	Start, End        time.Time
	AllowedOperations []string
	EntryConditions   []string
	ExitConditions    []string
	Obligations       []string
}

// PhaseGraph is a validated phase graph. Its windows are ordered and use
// half-open intervals [Start, End), making a shared boundary unambiguous.
type PhaseGraph struct {
	Phases []CompiledPhase
	Cutoff string
}

// CompiledPhaseGraph is retained as a descriptive alias for callers that use
// the longer engine terminology.
type CompiledPhaseGraph = PhaseGraph

func validCutoffPolicy(policy string) bool {
	switch strings.TrimSpace(policy) {
	case CutoffAtEnd, CutoffAtCutoff, CutoffOnSignal:
		return true
	default:
		return false
	}
}

// ValidatePhaseWindows validates phase coverage against the cycle periods.
// Every period must be covered exactly once by contiguous phase windows.
func ValidatePhaseWindows(c BusinessCycle) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if !validCutoffPolicy(c.Policies.Cutoff) {
		return fmt.Errorf("%w: %q", ErrCutoff, c.Policies.Cutoff)
	}

	seen := make(map[string]struct{}, len(c.Phases))
	for _, phase := range c.Phases {
		if _, ok := seen[phase.ID]; ok {
			return fmt.Errorf("%w: duplicate phase %q", ErrPhaseBoundary, phase.ID)
		}
		seen[phase.ID] = struct{}{}
		reachable := false
		for _, period := range c.Periods {
			if !phase.Start.Before(period.End) || !period.Start.Before(phase.End) {
				continue
			}
			reachable = true
			break
		}
		if !reachable {
			return fmt.Errorf("%w: %q", ErrPhaseUnreachable, phase.ID)
		}
	}

	for _, period := range c.Periods {
		windows := make([]Phase, 0, len(c.Phases))
		for _, phase := range c.Phases {
			if !phase.Start.Before(period.End) || !period.Start.Before(phase.End) {
				continue
			}
			windows = append(windows, phase)
		}
		sort.Slice(windows, func(i, j int) bool { return windows[i].Start.Before(windows[j].Start) })
		cursor := period.Start
		for _, phase := range windows {
			if phase.Start.After(cursor) {
				return fmt.Errorf("%w: period %q before phase %q", ErrPhaseGap, period.ID, phase.ID)
			}
			if phase.Start.Before(cursor) {
				return fmt.Errorf("%w: period %q at phase %q", ErrPhaseOverlap, period.ID, phase.ID)
			}
			cursor = phase.End
		}
		if !cursor.Equal(period.End) {
			return fmt.Errorf("%w: period %q after %s", ErrPhaseGap, period.ID, cursor)
		}
	}
	return nil
}

// CompilePhaseGraph validates and returns a detached executable phase graph.
func CompilePhaseGraph(c BusinessCycle) (PhaseGraph, error) {
	if err := ValidatePhaseWindows(c); err != nil {
		return PhaseGraph{}, err
	}
	phases := make([]CompiledPhase, len(c.Phases))
	for i, phase := range c.Phases {
		phases[i] = CompiledPhase{ID: phase.ID, Name: phase.Name, Start: phase.Start, End: phase.End}
	}
	sort.Slice(phases, func(i, j int) bool { return phases[i].Start.Before(phases[j].Start) })
	return PhaseGraph{Phases: phases, Cutoff: c.Policies.Cutoff}, nil
}

// PhaseAt returns the phase active at t, using the graph's half-open windows.
func (g PhaseGraph) PhaseAt(t time.Time) (CompiledPhase, bool) {
	for _, phase := range g.Phases {
		if !t.Before(phase.Start) && t.Before(phase.End) {
			return phase, true
		}
	}
	return CompiledPhase{}, false
}
