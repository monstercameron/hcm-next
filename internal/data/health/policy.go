package health

import "time"

// Policy is the caller-declared thresholds a Probe evaluates evidence
// against. Nothing here is invented by the probe itself: a bound a caller
// does not supply simply does not gate that dimension (a zero
// MaxProjectionLagSequences below is the one deliberate exception, documented
// on the field itself).
type Policy struct {
	// MaxProjectionLagSequences bounds how many events a projection
	// checkpoint may trail its stream head before Freshness reports
	// StateDegraded. Zero (the default) means a checkpoint must match its
	// stream head exactly to read as fresh, matching migration 00006's own
	// CURRENT status meaning "at the head, not merely close to it".
	MaxProjectionLagSequences int64
	// MaxOutboxUnackedAge bounds the age of the oldest outbox row that has
	// not yet reached DELIVERED or ABANDONED. Zero disables this check
	// (every age reads as fresh).
	MaxOutboxUnackedAge time.Duration
	// MaxLedgerSilence bounds how long a tenant's ledger may go without a
	// new append before Freshness reports StateDegraded. Zero disables the
	// check: a quiet ledger is not by itself unhealthy for every tenant.
	MaxLedgerSilence time.Duration
	// RowCountBudgets declares the maximum row count each table (by its
	// disposition-registry name) may carry before Saturation reports
	// StateDegraded. A table absent from this map has no declared budget
	// and its Saturation is never StateDegraded on count alone.
	RowCountBudgets map[string]int64
	// MaxPoolUtilization bounds AcquiredConns/MaxConns, as a fraction in
	// (0,1], before pool saturation reports StateDegraded. Zero disables the
	// check.
	MaxPoolUtilization float64
	// CellID is the bounded deployment-cell identifier
	// (internal/platform/telemetry's own "cell_id" attribute) this Probe's
	// tenant is served from. It is only used to label
	// Snapshot.TelemetrySamples; an empty CellID simply yields no samples.
	CellID string
}

// DefaultPolicy returns reasonable starting bounds: no projection lag
// tolerated, a five-minute outbox unacked-age limit, ledger silence never
// checked on its own, no row-count budgets and a 90% pool-utilization
// ceiling. Callers with different SLOs supply their own Policy rather than
// relying on these remaining stable across versions.
func DefaultPolicy() Policy {
	return Policy{
		MaxProjectionLagSequences: 0,
		MaxOutboxUnackedAge:       5 * time.Minute,
		MaxLedgerSilence:          0,
		RowCountBudgets:           map[string]int64{},
		MaxPoolUtilization:        0.9,
	}
}
