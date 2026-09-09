package health

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
)

// Querier is the minimal database capability a Probe reads through, matching
// internal/data/ledger.Querier's and internal/data/outbox.Querier's own shape.
// A pooled handle, a single connection and an open transaction all satisfy it.
type Querier = dbport.Querier

// PoolStats is a saturation snapshot of one connection pool, shaped after the
// pool's own accounting so PgxPoolStats can adapt it directly.
type PoolStats struct {
	AcquiredConns int32
	IdleConns     int32
	MaxConns      int32
	TotalConns    int32
}

// PoolStatsPort is the injected source of live connection-pool saturation.
// It is optional: a Probe built with none simply omits pool saturation from
// its Snapshot rather than reporting invented numbers (WithPoolStats).
type PoolStatsPort interface {
	Stats() PoolStats
}

// PgxPoolStats adapts a pgx-backed pool to PoolStatsPort, matching this
// repository's existing convention (internal/platform/bootstrap.DBPool) of a
// narrow, package-owned port over the pool rather than exposing the driver's
// own type on this package's public surface.
type PgxPoolStats struct{ Pool *pgxadapter.Pool }

// Stats implements PoolStatsPort.
func (p PgxPoolStats) Stats() PoolStats {
	s := p.Pool.Stats()
	return PoolStats{
		AcquiredConns: s.AcquiredConns,
		IdleConns:     s.IdleConns,
		MaxConns:      s.MaxConns,
		TotalConns:    s.TotalConns,
	}
}

// FakePoolStatsPort is a deterministic PoolStatsPort for tests.
type FakePoolStatsPort PoolStats

// Stats implements PoolStatsPort.
func (f FakePoolStatsPort) Stats() PoolStats { return PoolStats(f) }

// ReconcileEvidence is the outcome of the most recent out-of-band
// reconciliation attempt for one (tenant, projection, stream), when a
// caller has that evidence to offer (STORE-003: "last reconcile result").
type ReconcileEvidence struct {
	// Ran reports whether a reconcile attempt has ever been recorded for
	// this (tenant, projection, stream). Zero value means "none": Probe
	// then relies solely on the checkpoint's own status column and its
	// digest-versus-ledger chain-consistency check.
	Ran     bool
	At      time.Time
	Applied int
	// Err is the reconcile attempt's failure, or "" when it succeeded.
	Err string
}

// ReconcileEvidencePort is the optional injected source of out-of-band
// reconciler evidence. Nothing in this repository persists a reconcile-run
// log yet, so Probe works correctly with no port at all: recovery evidence
// is then derived entirely from projection_checkpoint's own status column
// and the ledger-digest chain-consistency check probe.go performs directly.
// A caller that does keep a reconcile log injects it here for a more
// precise recovery signal (WithReconcileEvidence).
type ReconcileEvidencePort interface {
	LastReconcile(ctx context.Context, tenant uuid.UUID, projectionName, streamKey string) (ReconcileEvidence, error)
}

// FakeReconcileEvidencePort is a deterministic ReconcileEvidencePort for
// tests: every call returns Result and Err, regardless of which
// (tenant, projection, stream) was asked about.
type FakeReconcileEvidencePort struct {
	Result ReconcileEvidence
	Err    error
}

// LastReconcile implements ReconcileEvidencePort.
func (f FakeReconcileEvidencePort) LastReconcile(context.Context, uuid.UUID, string, string) (ReconcileEvidence, error) {
	return f.Result, f.Err
}
