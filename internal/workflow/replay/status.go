package replay

// Status is where a replay stopped. It is a typed outcome rather than a
// boolean pair because "the run ended", "the run is parked on a pause" and
// "the run still has open work" are three different answers an operator acts
// on differently, and collapsing any two of them would hide the one that
// matters.
type Status string

// The declared replay statuses.
const (
	// StatusComplete reports a replay that reached the instance's terminal.
	StatusComplete Status = "COMPLETE"
	// StatusPausedAtFrontier reports a replay of a PAUSED or PAUSE_REQUESTED
	// instance: it stopped at the frontier the record still holds open rather
	// than walking past a pause the historical run never walked past.
	StatusPausedAtFrontier Status = "PAUSED_AT_FRONTIER"
	// StatusOpenFrontier reports a replay that exhausted the recorded
	// attempts with the instance neither terminal nor paused -- a run still in
	// flight, or waiting on human work, a signal or a timer that had not
	// settled when the record was taken.
	StatusOpenFrontier Status = "OPEN_FRONTIER"
	// StatusDiverged reports a replay that stopped because it and the record
	// disagreed. The [Result] carries the [Divergence] that names where.
	StatusDiverged Status = "DIVERGED"
)

// Valid reports whether s names a declared status.
func (s Status) Valid() bool {
	switch s {
	case StatusComplete, StatusPausedAtFrontier, StatusOpenFrontier, StatusDiverged:
		return true
	default:
		return false
	}
}

// Statuses returns every declared status, in a fixed order.
func Statuses() []Status {
	return []Status{StatusComplete, StatusPausedAtFrontier, StatusOpenFrontier, StatusDiverged}
}
