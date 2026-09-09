package health

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
)

// identifierPattern bounds any registry-sourced string this package
// interpolates into SQL text (probeGeneric's table and tenant-scoping-column
// names). The disposition registry is a checked-in, reviewed file, not user
// input, but a Probe validates anyway rather than trusting that invariant
// silently forever.
var identifierPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func validIdentifier(name string) bool { return identifierPattern.MatchString(name) }

// Probe evaluates one tenant's data-plane health against the live
// PostgreSQL schema, per the disposition registry it was built with.
type Probe struct {
	db        Querier
	registry  DispositionRegistry
	policy    Policy
	clock     Clock
	pool      PoolStatsPort
	reconcile ReconcileEvidencePort
}

// Option configures a Probe.
type Option func(*Probe)

// WithClock replaces the production SystemClock.
func WithClock(c Clock) Option { return func(p *Probe) { p.clock = c } }

// WithPoolStats injects a connection-pool saturation source. Without one,
// Snapshot.Pool stays nil.
func WithPoolStats(port PoolStatsPort) Option { return func(p *Probe) { p.pool = port } }

// WithReconcileEvidence injects an out-of-band reconciler evidence source.
// Without one, recovery evidence is derived solely from
// projection_checkpoint's own status column and the ledger-digest
// chain-consistency check.
func WithReconcileEvidence(port ReconcileEvidencePort) Option {
	return func(p *Probe) { p.reconcile = port }
}

// NewProbe builds a Probe over db (a connection, transaction or pool),
// evaluating registry under policy. Without WithClock it uses SystemClock.
func NewProbe(db Querier, registry DispositionRegistry, policy Policy, opts ...Option) *Probe {
	p := &Probe{db: db, registry: registry, policy: policy, clock: SystemClock}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Run evaluates every table the disposition registry names for tenant and
// returns a deterministic Snapshot. It never returns a Go error: any query
// or encoding failure is captured as StateUnknown, on the affected store or
// on the Snapshot as a whole, rather than aborting the evaluation
// (DATA-020/STORE-003 GREEN: "a probe error yields UNKNOWN, not HEALTHY").
func (p *Probe) Run(ctx context.Context, tenant uuid.UUID) Snapshot {
	now := p.clock.Now().UTC()
	snap := Snapshot{
		SchemaVersion:   SnapshotSchemaVersion,
		Tenant:          tenant,
		GeneratedAt:     now,
		RegistryVersion: p.registry.Version,
	}

	tables := append([]StoreDisposition(nil), p.registry.Tables...)
	sort.Slice(tables, func(i, j int) bool { return tables[i].Table < tables[j].Table })

	overall := StateHealthy
	var reasons []string
	for _, td := range tables {
		sh := p.probeTable(ctx, tenant, td, now)
		snap.Stores = append(snap.Stores, sh)
		overall = combine(overall, sh.State)
		for _, r := range sh.Reasons {
			reasons = append(reasons, td.Table+": "+r)
		}
	}

	if p.pool != nil {
		ps := p.probePool()
		snap.Pool = &ps
		overall = combine(overall, ps.State)
		if ps.Reason != "" {
			reasons = append(reasons, "pool: "+ps.Reason)
		}
	}

	sort.Strings(reasons)
	snap.Reasons = reasons
	snap.State = overall

	digest, err := ComputeDigest(snap)
	if err != nil {
		// A snapshot that cannot even be canonically encoded carries no
		// integrity binding; that is itself unknown-evidence, not a reason
		// to publish an undigested snapshot as though it were fine.
		snap.State = StateUnknown
		snap.Reasons = append(snap.Reasons, fmt.Sprintf("digest: %v", err))
		sort.Strings(snap.Reasons)
		digest = ""
	}
	snap.Digest = digest
	return snap
}

// probeTable dispatches to the store-specific check when Probe knows how to
// evaluate all three dimensions for td.Table, and otherwise falls back to a
// saturation-only check (probeGeneric): freshness and recovery are only
// reported where this package actually models them, never fabricated.
func (p *Probe) probeTable(ctx context.Context, tenant uuid.UUID, td StoreDisposition, now time.Time) StoreHealth {
	switch td.Table {
	case "ledger_event":
		return p.probeLedgerEvent(ctx, tenant, td, now)
	case "projection_checkpoint":
		return p.probeProjectionCheckpoint(ctx, tenant, td, now)
	case "outbox":
		return p.probeOutbox(ctx, tenant, td, now)
	default:
		return p.probeGeneric(ctx, tenant, td)
	}
}

func newStoreHealth(td StoreDisposition) StoreHealth {
	return StoreHealth{Table: td.Table, DataRole: td.DataRole, RetentionClass: td.RetentionClass, RebuildSource: td.RebuildSource}
}

// errorStoreHealth reports a probe failure as StateUnknown -- never as a
// silently-skipped or StateHealthy store.
func errorStoreHealth(td StoreDisposition, err error) StoreHealth {
	sh := newStoreHealth(td)
	sh.State = StateUnknown
	sh.Reasons = []string{fmt.Sprintf("probe error: %v", err)}
	return sh
}

func collectReasons(reasons ...string) []string {
	var out []string
	for _, r := range reasons {
		if r != "" {
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}

// saturation evaluates rowCount against td.Table's declared budget, if any.
func (p *Probe) saturation(td StoreDisposition, rowCount int64) *SaturationEvidence {
	// A two-value map lookup distinguishes "no budget declared" from "a
	// budget of exactly zero rows was declared": RowCountBudgets' zero value
	// for an absent key must never be confused with a caller who explicitly
	// declared that a table may hold no rows at all.
	budget, declared := p.policy.RowCountBudgets[td.Table]
	sat := &SaturationEvidence{RowCount: rowCount, RowBudget: budget, State: StateHealthy}
	if declared {
		if budget > 0 {
			sat.RowRatio = float64(rowCount) / float64(budget)
		}
		if rowCount > budget {
			sat.State = StateDegraded
			sat.Reason = fmt.Sprintf("row count %d exceeds budget %d", rowCount, budget)
		}
	}
	return sat
}

// probeLedgerEvent checks ledger_event's freshness (age of the newest
// append) and saturation. Recovery does not apply: the ledger is the
// append-only source of truth, so there is nothing for it to recover from.
func (p *Probe) probeLedgerEvent(ctx context.Context, tenant uuid.UUID, td StoreDisposition, now time.Time) StoreHealth {
	var rowCount int64
	var newestAt *time.Time
	if err := p.db.QueryRow(ctx, `SELECT count(*), max(recorded_at) FROM ledger_event WHERE tenant_id = $1`, tenant).
		Scan(&rowCount, &newestAt); err != nil {
		return errorStoreHealth(td, err)
	}

	fresh := &FreshnessEvidence{LagSequences: -1, LagThreshold: -1, AgeThresholdSeconds: p.policy.MaxLedgerSilence.Seconds(), State: StateHealthy}
	if newestAt != nil {
		fresh.HasEvidence = true
		fresh.NewestAt = *newestAt
		age := now.Sub(*newestAt)
		fresh.AgeSeconds = age.Seconds()
		if p.policy.MaxLedgerSilence > 0 && age > p.policy.MaxLedgerSilence {
			fresh.State = StateDegraded
			fresh.Reason = fmt.Sprintf("no ledger append in %s (limit %s)", age, p.policy.MaxLedgerSilence)
		}
	} else {
		fresh.Reason = "no ledger events recorded yet for this tenant"
	}

	sat := p.saturation(td, rowCount)

	sh := newStoreHealth(td)
	sh.Freshness = fresh
	sh.Saturation = sat
	sh.State = combine(fresh.State, sat.State)
	sh.Reasons = collectReasons(fresh.Reason, sat.Reason)
	return sh
}

type checkpointRow struct {
	projectionName string
	streamKey      string
	lastApplied    int64
	digest         *string
	status         string
	head           int64
}

// probeProjectionCheckpoint checks every checkpoint this tenant has
// registered: freshness is the worst observed lag against each checkpoint's
// stream head, recovery is degraded by any non-CURRENT status, any
// checkpoint digest that disagrees with the ledger_event it claims to have
// applied, or (when injected) a failed last-reconcile result, and
// saturation is the table's row count against its declared budget.
func (p *Probe) probeProjectionCheckpoint(ctx context.Context, tenant uuid.UUID, td StoreDisposition, now time.Time) StoreHealth {
	rows, err := p.db.Query(ctx, `
		SELECT pc.projection_name, pc.stream_key, pc.last_applied_sequence, pc.last_applied_digest, pc.status, sh.head_sequence
		FROM projection_checkpoint pc
		JOIN stream_head sh ON sh.tenant_id = pc.tenant_id AND sh.stream_key = pc.stream_key
		WHERE pc.tenant_id = $1`, tenant)
	if err != nil {
		return errorStoreHealth(td, err)
	}
	defer rows.Close()

	var all []checkpointRow
	for rows.Next() {
		var r checkpointRow
		if err := rows.Scan(&r.projectionName, &r.streamKey, &r.lastApplied, &r.digest, &r.status, &r.head); err != nil {
			return errorStoreHealth(td, err)
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		return errorStoreHealth(td, err)
	}

	var rowCount int64
	if err := p.db.QueryRow(ctx, `SELECT count(*) FROM projection_checkpoint WHERE tenant_id = $1`, tenant).Scan(&rowCount); err != nil {
		return errorStoreHealth(td, err)
	}
	sat := p.saturation(td, rowCount)

	fresh := &FreshnessEvidence{LagThreshold: p.policy.MaxProjectionLagSequences, State: StateHealthy}
	rec := &RecoveryEvidence{State: StateHealthy, ChainConsistent: true}

	if len(all) == 0 {
		fresh.Reason = "no projection checkpoints registered for this tenant"
		rec.Reason = fresh.Reason
	} else {
		fresh.HasEvidence = true
		var worstLag int64
		var worstProjection, worstStream string
		for _, r := range all {
			if lag := r.head - r.lastApplied; lag > worstLag {
				worstLag, worstProjection, worstStream = lag, r.projectionName, r.streamKey
			}

			rec.HasEvidence = true
			// Report the worst status seen: once any checkpoint disagrees
			// with CURRENT, that status wins and never gets overwritten by
			// a later, healthier row.
			if rec.Status == "" || rec.Status == projectionStatusCurrent {
				rec.Status = r.status
			}
			if r.status != projectionStatusCurrent {
				rec.State = StateDegraded
				rec.Reason = fmt.Sprintf("projection %s on stream %s has status %s", r.projectionName, r.streamKey, r.status)
			}

			if r.lastApplied > 0 && r.digest != nil {
				var ledgerDigest string
				derr := p.db.QueryRow(ctx, `SELECT digest FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2 AND sequence = $3`,
					tenant, r.streamKey, r.lastApplied).Scan(&ledgerDigest)
				if derr != nil {
					return errorStoreHealth(td, derr)
				}
				rec.HasEvidence = true
				if ledgerDigest != *r.digest {
					rec.ChainConsistent = false
					rec.State = StateDegraded
					rec.Reason = fmt.Sprintf("projection %s on stream %s: checkpoint digest disagrees with ledger_event at sequence %d",
						r.projectionName, r.streamKey, r.lastApplied)
				}
			}

			if p.reconcile != nil {
				ev, everr := p.reconcile.LastReconcile(ctx, tenant, r.projectionName, r.streamKey)
				if everr != nil {
					return errorStoreHealth(td, everr)
				}
				if ev.Ran {
					rec.HasEvidence = true
					evCopy := ev
					rec.LastReconcile = &evCopy
					if ev.Err != "" {
						rec.State = StateDegraded
						rec.Reason = fmt.Sprintf("projection %s on stream %s: last reconcile failed: %s", r.projectionName, r.streamKey, ev.Err)
					}
				}
			}
		}

		fresh.LagSequences = worstLag
		if worstLag > p.policy.MaxProjectionLagSequences {
			fresh.State = StateDegraded
			fresh.Reason = fmt.Sprintf("projection %s on stream %s is %d event(s) behind its stream head (limit %d)",
				worstProjection, worstStream, worstLag, p.policy.MaxProjectionLagSequences)
		}
	}

	sh := newStoreHealth(td)
	sh.Freshness = fresh
	sh.Saturation = sat
	sh.Recovery = rec
	sh.State = combine(combine(fresh.State, sat.State), rec.State)
	sh.Reasons = collectReasons(fresh.Reason, sat.Reason, rec.Reason)
	return sh
}

// projectionStatusCurrent mirrors internal/data/projection.StatusCurrent.
// It is redeclared locally rather than imported so this file does not take
// a dependency on internal/data/projection merely for one string constant;
// internal/data/projection/reconcile.go is still imported by this
// package's tests, which exercise real reconciliation against the same
// value.
const projectionStatusCurrent = "CURRENT"

// probeOutbox checks the outbox's freshness (age of the oldest row that has
// not yet reached DELIVERED or ABANDONED), recovery (any permanently
// ABANDONED row, plus injected reconcile evidence) and saturation.
func (p *Probe) probeOutbox(ctx context.Context, tenant uuid.UUID, td StoreDisposition, now time.Time) StoreHealth {
	var rowCount, abandonedCount int64
	var oldestUnacked *time.Time
	err := p.db.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE status = $2),
			min(created_at) FILTER (WHERE status NOT IN ($2, $3))
		FROM outbox WHERE tenant_id = $1`,
		tenant, outbox.StatusAbandoned, outbox.StatusDelivered).
		Scan(&rowCount, &abandonedCount, &oldestUnacked)
	if err != nil {
		return errorStoreHealth(td, err)
	}

	sat := p.saturation(td, rowCount)

	fresh := &FreshnessEvidence{LagSequences: -1, LagThreshold: -1, AgeThresholdSeconds: p.policy.MaxOutboxUnackedAge.Seconds(), State: StateHealthy}
	if oldestUnacked != nil {
		fresh.HasEvidence = true
		fresh.NewestAt = *oldestUnacked
		age := now.Sub(*oldestUnacked)
		fresh.AgeSeconds = age.Seconds()
		if p.policy.MaxOutboxUnackedAge > 0 && age > p.policy.MaxOutboxUnackedAge {
			fresh.State = StateDegraded
			fresh.Reason = fmt.Sprintf("oldest unacknowledged outbox row is %s old (limit %s)", age, p.policy.MaxOutboxUnackedAge)
		}
	} else {
		fresh.Reason = "no unacknowledged outbox rows"
	}

	rec := &RecoveryEvidence{HasEvidence: true, AbandonedCount: abandonedCount, ChainConsistent: true, State: StateHealthy}
	if abandonedCount > 0 {
		rec.State = StateDegraded
		rec.Reason = fmt.Sprintf("%d outbox row(s) were abandoned and never recovered", abandonedCount)
	}

	sh := newStoreHealth(td)
	sh.Freshness = fresh
	sh.Saturation = sat
	sh.Recovery = rec
	sh.State = combine(combine(fresh.State, sat.State), rec.State)
	sh.Reasons = collectReasons(fresh.Reason, sat.Reason, rec.Reason)
	return sh
}

// probeGeneric checks only saturation, for any registry table this package
// has no store-specific check for: a CONTROL or REGISTRY table's Freshness
// and Recovery stay nil rather than a fabricated StateHealthy.
func (p *Probe) probeGeneric(ctx context.Context, tenant uuid.UUID, td StoreDisposition) StoreHealth {
	if !validIdentifier(td.Table) {
		return errorStoreHealth(td, fmt.Errorf("disposition registry: table name %q is not a safe identifier", td.Table))
	}

	var q string
	args := []any{}
	if td.TenantScopingColumn != "" {
		if !validIdentifier(td.TenantScopingColumn) {
			return errorStoreHealth(td, fmt.Errorf("disposition registry: tenant scoping column %q is not a safe identifier", td.TenantScopingColumn))
		}
		q = fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s = $1`, td.Table, td.TenantScopingColumn)
		args = append(args, tenant)
	} else {
		q = fmt.Sprintf(`SELECT count(*) FROM %s`, td.Table)
	}

	var rowCount int64
	if err := p.db.QueryRow(ctx, q, args...).Scan(&rowCount); err != nil {
		return errorStoreHealth(td, err)
	}

	sat := p.saturation(td, rowCount)
	sh := newStoreHealth(td)
	sh.Saturation = sat
	sh.State = sat.State
	sh.Reasons = collectReasons(sat.Reason)
	return sh
}

// probePool evaluates the injected PoolStatsPort against
// Policy.MaxPoolUtilization.
func (p *Probe) probePool() PoolSaturation {
	stats := p.pool.Stats()
	ps := PoolSaturation{Acquired: stats.AcquiredConns, Idle: stats.IdleConns, Max: stats.MaxConns, Total: stats.TotalConns, State: StateHealthy}
	if stats.MaxConns > 0 {
		ps.Ratio = float64(stats.AcquiredConns) / float64(stats.MaxConns)
		if p.policy.MaxPoolUtilization > 0 && ps.Ratio > p.policy.MaxPoolUtilization {
			ps.State = StateDegraded
			ps.Reason = fmt.Sprintf("pool utilization %.2f exceeds limit %.2f", ps.Ratio, p.policy.MaxPoolUtilization)
		}
	}
	return ps
}
