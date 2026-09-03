package health

// State is one store's or the whole data plane's health verdict.
type State string

// Published states. There is no fourth "healthy on missing evidence" state:
// a Probe only ever reports [StateHealthy] when every dimension it checked
// was actually observed, within its declared bound, and free of error
// (DATA-020/STORE-003 GREEN).
const (
	// StateUnknown reports that evidence could not be collected at all --
	// a failed query, an unencodable snapshot. It outranks StateDegraded:
	// not knowing is treated as more severe than a known, bounded problem.
	StateUnknown State = "UNKNOWN"
	// StateDegraded reports that evidence was collected and it violates a
	// declared bound: a stale projection, an outbox row past its unacked-age
	// limit, a saturated row-count budget, an inconsistent checkpoint chain.
	StateDegraded State = "DEGRADED"
	// StateHealthy reports that every checked dimension was observed and
	// within its declared bound.
	StateHealthy State = "HEALTHY"
)

// rank orders states by severity for [combine]. An unrecognized value ranks
// as StateUnknown's severity rather than StateHealthy's: a State that is
// neither published value fails closed.
func (s State) rank() int {
	switch s {
	case StateHealthy:
		return 0
	case StateDegraded:
		return 1
	default:
		return 2
	}
}

// combine returns the more severe of a and b. A Snapshot's, or one store's,
// overall State is never better than the worst evidence that fed it.
func combine(a, b State) State {
	if b.rank() > a.rank() {
		return b
	}
	return a
}
