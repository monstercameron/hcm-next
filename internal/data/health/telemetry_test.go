package health_test

import (
	"testing"

	"github.com/monstercameron/hcm-next/internal/data/health"
	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
)

// TestSnapshotTelemetrySamples_CompatibleWithCatalog proves the package
// doc's "Telemetry compatibility" claim: every sample TelemetrySamples
// emits names an existing internal/platform/telemetry P1ACellMetrics entry,
// with only labels that metric actually declares -- never a name or a label
// telemetry's own catalog does not already publish.
func TestSnapshotTelemetrySamples_CompatibleWithCatalog(t *testing.T) {
	catalog := telemetry.CatalogByName(telemetry.MetricCatalog())

	snap := health.Snapshot{
		Stores: []health.StoreHealth{
			{
				Table: "projection_checkpoint", DataRole: "PROJECTION", State: health.StateHealthy,
			},
			{
				Table: "outbox", DataRole: "OUTBOX", State: health.StateDegraded,
				Freshness: &health.FreshnessEvidence{HasEvidence: true, AgeSeconds: 42, LagSequences: -1, LagThreshold: -1},
			},
		},
	}

	samples := snap.TelemetrySamples("cell-local")
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want 2 (one edge.parity, one outbox.lag)", len(samples))
	}

	for _, s := range samples {
		def, ok := catalog[s.Metric]
		if !ok {
			t.Fatalf("sample names metric %q, which is not in telemetry.MetricCatalog()", s.Metric)
		}
		if def.Type != telemetry.MetricGauge {
			t.Fatalf("metric %q is type %q in the catalog, expected this package to only reuse gauges", s.Metric, def.Type)
		}
		allowed := make(map[string]bool, len(def.Labels))
		for _, l := range def.Labels {
			allowed[l] = true
		}
		for k := range s.Labels {
			if !allowed[k] {
				t.Fatalf("sample for metric %q carries label %q, which is not declared on that catalog entry (labels: %v)", s.Metric, k, def.Labels)
			}
		}
	}

	// An empty cellID must yield no samples: cell_id is a required label on
	// both source metrics, so this package must never emit one without it.
	if got := snap.TelemetrySamples(""); got != nil {
		t.Fatalf("TelemetrySamples(\"\") = %v, want nil", got)
	}
}
