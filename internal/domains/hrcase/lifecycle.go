package hrcase

import "fmt"

// Transition is an append-only lifecycle command result. Invalid commands
// return no event, work item, or outbox message.
type Transition struct {
	From, To CaseState
	Revision CaseRevision
}
type Emissions struct{ Events, Work, Outbox []any }
type Result struct {
	Revision   CaseRevision
	Transition Transition
	Emissions  Emissions
}

var allowed = map[CaseState]map[CaseState]bool{
	Draft: {Open: true}, Open: {Waiting: true, Paused: true, Resolved: true, Closed: true}, Waiting: {Open: true, Paused: true, Resolved: true},
	Paused: {Open: true, Waiting: true, Resolved: true}, Resolved: {Closed: true, Reopened: true, Appealed: true},
	Reopened: {Open: true, Waiting: true, Paused: true, Resolved: true}, Appealed: {Resolved: true, Closed: true}, Closed: {},
}

func TransitionAllowed(from, to CaseState) bool { return allowed[from][to] }

// Apply appends exactly one successor revision on success. The input and all
// prior revisions remain untouched; terminal revisions cannot be edited.
func Apply(current CaseRevision, to CaseState, disposition string) (Result, error) {
	if err := current.Validate(); err != nil {
		return Result{}, err
	}
	if terminal(current.State) {
		return Result{}, ErrTerminalCase
	}
	if !validState(to) || !TransitionAllowed(current.State, to) {
		return Result{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.State, to)
	}
	n := cloneRevision(current)
	n.Revision++
	n.Previous = current.Revision
	n.State = to
	n.Disposition = disposition
	n, err := NewCaseRevision(n)
	if err != nil {
		return Result{}, err
	}
	return Result{Revision: n, Transition: Transition{From: current.State, To: to, Revision: n}, Emissions: Emissions{Events: []any{Transition{From: current.State, To: to, Revision: n}}}}, nil
}
