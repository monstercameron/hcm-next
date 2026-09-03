// Package health owns the data plane's own health probe (owner: data plane;
// phase: P1A; DATA-020, STORE-003).
//
// A Probe reads the live PostgreSQL schema for one tenant, driven by
// STORE-001's physical storage-disposition registry
// (definitions/storage/storage-disposition.yaml, or this package's own
// DefaultDispositionRegistry when that file is not yet present in a
// checkout -- see LoadOrDefaultDispositionRegistry), and reports three
// independent dimensions per store:
//
//   - Freshness: how current the store's evidence is -- the age of the
//     newest ledger append, a projection checkpoint's lag versus its
//     stream's head, or the age of the oldest not-yet-delivered outbox row.
//   - Saturation: row count against a caller-declared budget, plus
//     connection-pool utilization from an injected PoolStatsPort.
//   - Recovery: a projection checkpoint's own status column, whether its
//     recorded digest still agrees with the ledger event it claims to have
//     applied, permanently abandoned outbox rows, and (when a caller injects
//     one) the last out-of-band reconcile attempt's outcome.
//
// Every dimension and every store rolls up into one overall [State]:
// [StateHealthy], [StateDegraded] or [StateUnknown]. The aggregate is never
// [StateHealthy] on missing or errored evidence (DATA-020/STORE-003 GREEN,
// "never HEALTHY on missing evidence"): a query that fails is reported as
// [StateUnknown] for that store, not silently skipped, and [StateUnknown]
// always outranks [StateDegraded], which always outranks [StateHealthy]
// when Run rolls per-store results into the Snapshot's own State.
//
// Run never returns a Go error. Every failure it encounters -- a bad query,
// an unencodable snapshot -- becomes part of the returned Snapshot itself
// (an UNKNOWN store, or an UNKNOWN overall state), so a caller always gets a
// deterministic, JSON-serializable, digest-bound Snapshot to publish or
// alert on, never a bare error that would leave a status page with nothing
// to render.
//
// # Clock and connectivity
//
// Every wall-clock read goes through an injected [Clock]: production code
// uses SystemClock, tests use [FakeClock] so a "stale projection" or an
// "outbox row past its unacked-age threshold" scenario is exact and
// reproducible rather than racing real time.
//
// # Telemetry compatibility
//
// This package does not define its own metric catalog. Where a signal it
// already computes matches an existing entry in
// internal/platform/telemetry's P1ACellMetrics -- a projection's
// checkpoint-vs-head parity is exactly "edge.parity" (1 in parity, 0 not),
// and an outbox's oldest-unacked age is exactly "outbox.lag" (already a
// millisecond gauge) -- [Snapshot.TelemetrySamples] emits values under
// those names and labels so a caller feeding them to telemetry never has to
// register a second, competing definition for the same fact. Dimensions
// telemetry's catalog does not yet name (ledger freshness, row-count
// saturation, recovery state) are reported only in the Snapshot itself, not
// forced into an ill-fitting existing metric.
package health
