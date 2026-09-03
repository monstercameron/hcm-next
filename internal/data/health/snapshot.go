package health

import (
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/engines/canonicalbytes"
)

// SnapshotSchemaVersion tags Snapshot's own encoding, independent of any
// business payload schema.
const SnapshotSchemaVersion = 1

// FreshnessEvidence is how current one store's evidence is.
type FreshnessEvidence struct {
	// HasEvidence is false when the store carries no rows to measure
	// freshness from yet (an empty ledger, no registered projection, an
	// empty outbox). That is reported as State StateHealthy with a Reason,
	// never as missing evidence: a brand-new tenant's empty stores are not a
	// health problem.
	HasEvidence bool `json:"has_evidence"`
	// NewestAt is the newest observed instant this dimension measured
	// against (a ledger's newest recorded_at, or an outbox's oldest
	// still-unacked created_at). Zero when !HasEvidence.
	NewestAt time.Time `json:"newest_at,omitempty"`
	// AgeSeconds is Probe's clock minus NewestAt, in seconds. Zero when
	// !HasEvidence.
	AgeSeconds float64 `json:"age_seconds"`
	// LagSequences is a projection checkpoint's worst observed lag against
	// its stream head. -1 when lag is not the measure this store's
	// freshness uses (ledger, outbox).
	LagSequences int64 `json:"lag_sequences"`
	// LagThreshold is the Policy.MaxProjectionLagSequences bound this store
	// was evaluated against. -1 when lag is not this store's measure
	// (ledger, outbox) -- the "accepted staleness" DATA-020 GREEN requires
	// health to expose, for the lag-based dimension.
	LagThreshold int64 `json:"lag_threshold"`
	// AgeThresholdSeconds is the age-based bound (Policy.MaxLedgerSilence or
	// Policy.MaxOutboxUnackedAge) this store was evaluated against, in
	// seconds. 0 means no age bound is configured for this store -- the
	// "accepted staleness" DATA-020 GREEN requires health to expose, for the
	// age-based dimension.
	AgeThresholdSeconds float64 `json:"age_threshold_seconds"`
	State               State   `json:"state"`
	Reason              string  `json:"reason,omitempty"`
}

// SaturationEvidence is one store's row count against its declared budget.
type SaturationEvidence struct {
	RowCount int64 `json:"row_count"`
	// RowBudget is the caller-declared maximum (Policy.RowCountBudgets), or
	// 0 when the table has no declared budget.
	RowBudget int64   `json:"row_budget"`
	RowRatio  float64 `json:"row_ratio"`
	State     State   `json:"state"`
	Reason    string  `json:"reason,omitempty"`
}

// RecoveryEvidence is one store's recovery/consistency state.
type RecoveryEvidence struct {
	// HasEvidence is false when the store has no recovery-relevant rows yet
	// (no projection checkpoints registered for this tenant).
	HasEvidence bool `json:"has_evidence"`
	// Status is a projection checkpoint's own status column
	// (CURRENT/LAGGING/STALE/DISAGREEING/REBUILDING), taken from whichever
	// checkpoint is in the worst state. Empty when not applicable (outbox).
	Status string `json:"status,omitempty"`
	// ChainConsistent reports whether a projection checkpoint's recorded
	// digest still agrees with the ledger_event digest at the sequence it
	// claims to have applied. Trivially true where the check does not apply
	// (outbox).
	ChainConsistent bool `json:"chain_consistent"`
	// AbandonedCount is an outbox's count of rows that reached ABANDONED --
	// an effect that was never, and will never be, delivered. Always 0
	// where the check does not apply (projection).
	AbandonedCount int64 `json:"abandoned_count"`
	// LastReconcile carries whatever a caller-injected
	// ReconcileEvidencePort reported for the worst-lag checkpoint this pass
	// examined. nil when no port was injected, or the port reported no
	// prior attempt.
	LastReconcile *ReconcileEvidence `json:"last_reconcile,omitempty"`
	State         State              `json:"state"`
	Reason        string             `json:"reason,omitempty"`
}

// StoreHealth is one table's evaluated health, echoing the disposition
// registry facts a reader needs to interpret it (DataRole, RetentionClass,
// RebuildSource) alongside whichever of the three evidence dimensions apply.
type StoreHealth struct {
	Table          string `json:"table"`
	DataRole       string `json:"data_role"`
	RetentionClass string `json:"retention_class"`
	// RebuildSource echoes the registry's own field: non-empty names the
	// append-only table this store can be reconstructed from.
	RebuildSource string `json:"rebuild_source,omitempty"`
	// Freshness, Saturation and Recovery are nil when that dimension does
	// not apply to this store (a CONTROL/REGISTRY table Probe only checks
	// for saturation gets a nil Freshness and nil Recovery, not a
	// fabricated StateHealthy for a dimension nothing measured).
	Freshness  *FreshnessEvidence  `json:"freshness,omitempty"`
	Saturation *SaturationEvidence `json:"saturation,omitempty"`
	Recovery   *RecoveryEvidence   `json:"recovery,omitempty"`
	State      State               `json:"state"`
	Reasons    []string            `json:"reasons,omitempty"`
}

// PoolSaturation is the data plane's shared connection-pool saturation, from
// an injected PoolStatsPort. It is reported once per Snapshot rather than
// once per store: a connection pool is not itself one of the tables the
// disposition registry names.
type PoolSaturation struct {
	Acquired int32   `json:"acquired"`
	Idle     int32   `json:"idle"`
	Max      int32   `json:"max"`
	Total    int32   `json:"total"`
	Ratio    float64 `json:"ratio"`
	State    State   `json:"state"`
	Reason   string  `json:"reason,omitempty"`
}

// Snapshot is one Probe.Run evaluation: every store the disposition
// registry named, this Probe's overall verdict, and a digest binding the
// two together.
type Snapshot struct {
	SchemaVersion   int             `json:"schema_version"`
	Tenant          uuid.UUID       `json:"tenant"`
	GeneratedAt     time.Time       `json:"generated_at"`
	RegistryVersion int             `json:"registry_version"`
	Pool            *PoolSaturation `json:"pool,omitempty"`
	// Stores is sorted by Table, ascending, so two evaluations of the same
	// evidence always produce the same JSON and the same digest regardless
	// of the disposition registry's own row order.
	Stores  []StoreHealth `json:"stores"`
	State   State         `json:"state"`
	Reasons []string      `json:"reasons,omitempty"`
	// Digest is "sha256:<hex>" over every field above, computed by
	// ComputeDigest. Two Snapshots built from identical evidence at the
	// identical instant carry the same Digest; any difference in state,
	// evidence or generation time changes it.
	Digest string `json:"digest"`
}

// TelemetrySample is one value this package believes is safe and correct to
// hand to internal/platform/telemetry under an existing catalog entry's own
// name, type and labels -- see the package doc's "Telemetry compatibility"
// section. It carries no envelope (correlation id, resource, ...): a caller
// wires that through telemetry.BuildEnvelope itself.
type TelemetrySample struct {
	Metric string
	Value  float64
	Labels map[string]string
}

// TelemetrySamples returns this Snapshot's values that already match an
// existing internal/platform/telemetry P1ACellMetrics entry: a projection
// store's checkpoint-vs-head parity as "edge.parity" (1 in parity, 0 not;
// labels cell_id, edge), and an outbox store's oldest-unacked age as
// "outbox.lag" (milliseconds; label cell_id) -- exactly that metric's own
// declared unit. cellID must be the bounded deployment-cell identifier
// telemetry's own "cell_id" attribute expects; an empty cellID yields no
// samples, since cell_id is a required label on both source metrics.
func (s Snapshot) TelemetrySamples(cellID string) []TelemetrySample {
	if cellID == "" {
		return nil
	}
	var out []TelemetrySample
	for _, st := range s.Stores {
		switch st.DataRole {
		case "PROJECTION":
			parity := 0.0
			if st.State == StateHealthy {
				parity = 1
			}
			out = append(out, TelemetrySample{
				Metric: "edge.parity",
				Value:  parity,
				Labels: map[string]string{"cell_id": cellID, "edge": st.Table},
			})
		case "OUTBOX":
			if st.Freshness != nil && st.Freshness.HasEvidence {
				out = append(out, TelemetrySample{
					Metric: "outbox.lag",
					Value:  st.Freshness.AgeSeconds * 1000,
					Labels: map[string]string{"cell_id": cellID},
				})
			}
		}
	}
	return out
}

// snapshotDigestSchema names the canonical stream ComputeDigest writes
// through internal/engines/canonicalbytes.
const snapshotDigestSchema = "hcmnext.data.health.Snapshot"

// ComputeDigest computes the canonical digest over every field of s except
// s.Digest itself. Run calls it to populate Snapshot.Digest; a caller that
// receives a Snapshot from storage or over the wire can call it again to
// verify the snapshot was not altered in transit.
func ComputeDigest(s Snapshot) (string, error) {
	w := canonicalbytes.New(snapshotDigestSchema, SnapshotSchemaVersion).
		Int("schema_version", int64(s.SchemaVersion)).
		String("tenant", s.Tenant.String()).
		Int("generated_at_unix_ns", instantNanos(s.GeneratedAt)).
		Int("registry_version", int64(s.RegistryVersion))

	w.Bool("pool_present", s.Pool != nil)
	if s.Pool != nil {
		w.Int("pool.acquired", int64(s.Pool.Acquired)).
			Int("pool.idle", int64(s.Pool.Idle)).
			Int("pool.max", int64(s.Pool.Max)).
			Int("pool.total", int64(s.Pool.Total)).
			String("pool.state", string(s.Pool.State))
	}

	w.Count("stores", len(s.Stores))
	for _, st := range s.Stores {
		w.String("store.table", st.Table).
			String("store.data_role", st.DataRole).
			String("store.rebuild_source", st.RebuildSource).
			String("store.state", string(st.State))

		w.Bool("store.freshness_present", st.Freshness != nil)
		if f := st.Freshness; f != nil {
			w.Bool("store.freshness.has_evidence", f.HasEvidence).
				Int("store.freshness.newest_at_unix_ns", instantNanos(f.NewestAt)).
				Int("store.freshness.lag_sequences", f.LagSequences).
				Int("store.freshness.lag_threshold", f.LagThreshold).
				Int("store.freshness.age_threshold_ns", int64(f.AgeThresholdSeconds*float64(time.Second))).
				String("store.freshness.state", string(f.State))
		}

		w.Bool("store.saturation_present", st.Saturation != nil)
		if sat := st.Saturation; sat != nil {
			w.Int("store.saturation.row_count", sat.RowCount).
				Int("store.saturation.row_budget", sat.RowBudget).
				String("store.saturation.state", string(sat.State))
		}

		w.Bool("store.recovery_present", st.Recovery != nil)
		if rec := st.Recovery; rec != nil {
			w.Bool("store.recovery.has_evidence", rec.HasEvidence).
				String("store.recovery.status", rec.Status).
				Bool("store.recovery.chain_consistent", rec.ChainConsistent).
				Int("store.recovery.abandoned_count", rec.AbandonedCount).
				String("store.recovery.state", string(rec.State))
			w.Bool("store.recovery.last_reconcile_present", rec.LastReconcile != nil)
			if lr := rec.LastReconcile; lr != nil {
				w.Bool("store.recovery.last_reconcile.ran", lr.Ran).
					Int("store.recovery.last_reconcile.at_unix_ns", instantNanos(lr.At)).
					Int("store.recovery.last_reconcile.applied", int64(lr.Applied)).
					String("store.recovery.last_reconcile.err", lr.Err)
			}
		}

		w.SortedStrings("store.reasons", st.Reasons)
	}

	w.String("state", string(s.State)).
		SortedStrings("reasons", s.Reasons)

	return w.Digest()
}

func instantNanos(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixNano()
}
