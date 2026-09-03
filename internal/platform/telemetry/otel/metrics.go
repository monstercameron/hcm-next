package otel

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/monstercameron/hcm-next/internal/platform/telemetry"
)

// Catalog metric names (telemetry.MetricCatalog()). Declared as constants so
// newMetrics's exhaustiveness check and every Record* method reference the
// same literal the catalog itself publishes, rather than a second
// hand-typed copy that could drift from it.
const (
	metricIntentCreated   = "intent.created"
	metricIntentSimulated = "intent.simulated"
	metricLedgerAppend    = "ledger.append"
	metricOutboxLag       = "outbox.lag"
	metricEdgeParity      = "edge.parity"
)

// Metrics registers exactly the P1A cell's five catalog instruments
// (telemetry.MetricCatalog) against a metric.Meter and exposes one typed,
// narrow recording method per instrument. It is the only way this package
// lets a caller record a metric: there is no generic "record an arbitrary
// named metric with arbitrary labels" method, so cardinality/label
// governance stays centralized in the Evaluator rather than something each
// call site could invent its own (weaker) version of
// (internal/platform/telemetry/policy.go OBS-004 REFACTOR).
type Metrics struct {
	eval *telemetry.Evaluator

	intentCreated   metric.Int64Counter
	intentSimulated metric.Int64Counter
	ledgerAppend    metric.Int64Counter
	outboxLag       metric.Float64Gauge
	edgeParity      metric.Float64Gauge

	mu      sync.Mutex
	emitted map[string]struct{}
}

// newMetrics builds every instrument telemetry.MetricCatalog() declares. It
// fails if the catalog names an instrument this switch does not know how to
// construct, or if this switch expects an instrument the catalog no longer
// declares: either direction is the catalog and this adapter drifting apart,
// which OBS-002's "registers exactly the metric catalog's instruments"
// requirement exists to catch at construction time rather than silently at
// the completeness checker, much later.
func newMetrics(meter metric.Meter, eval *telemetry.Evaluator) (*Metrics, error) {
	m := &Metrics{eval: eval, emitted: make(map[string]struct{})}
	seen := make(map[string]bool, len(telemetry.P1ACellMetrics))

	for _, def := range telemetry.MetricCatalog() {
		seen[def.Name] = true
		switch def.Name {
		case metricIntentCreated:
			c, err := meter.Int64Counter(def.Name, metric.WithUnit(def.Unit), metric.WithDescription(def.Description))
			if err != nil {
				return nil, fmt.Errorf("otel: creating counter %q: %w", def.Name, err)
			}
			m.intentCreated = c
		case metricIntentSimulated:
			c, err := meter.Int64Counter(def.Name, metric.WithUnit(def.Unit), metric.WithDescription(def.Description))
			if err != nil {
				return nil, fmt.Errorf("otel: creating counter %q: %w", def.Name, err)
			}
			m.intentSimulated = c
		case metricLedgerAppend:
			c, err := meter.Int64Counter(def.Name, metric.WithUnit(def.Unit), metric.WithDescription(def.Description))
			if err != nil {
				return nil, fmt.Errorf("otel: creating counter %q: %w", def.Name, err)
			}
			m.ledgerAppend = c
		case metricOutboxLag:
			g, err := meter.Float64Gauge(def.Name, metric.WithUnit(def.Unit), metric.WithDescription(def.Description))
			if err != nil {
				return nil, fmt.Errorf("otel: creating gauge %q: %w", def.Name, err)
			}
			m.outboxLag = g
		case metricEdgeParity:
			g, err := meter.Float64Gauge(def.Name, metric.WithUnit(def.Unit), metric.WithDescription(def.Description))
			if err != nil {
				return nil, fmt.Errorf("otel: creating gauge %q: %w", def.Name, err)
			}
			m.edgeParity = g
		default:
			return nil, fmt.Errorf("otel: metric catalog declares %q, which this adapter does not know how to register (update internal/platform/telemetry/otel/metrics.go)", def.Name)
		}
	}
	for _, want := range []string{metricIntentCreated, metricIntentSimulated, metricLedgerAppend, metricOutboxLag, metricEdgeParity} {
		if !seen[want] {
			return nil, fmt.Errorf("otel: metric catalog no longer declares %q, which this adapter expects to register", want)
		}
	}
	return m, nil
}

// metricAttr classifies one metric label through eval and returns the
// attribute to attach plus whether it survived. A label the Evaluator drops
// is simply omitted from the recorded point rather than substituted with a
// placeholder: an omitted label still lets telemetry.Check see the metric
// name as emitted (see EmittedMetricNames), it just carries one fewer
// dimension, exactly like any other Evaluator-denied attribute elsewhere in
// this package.
func metricAttr(eval *telemetry.Evaluator, key, value string) (attribute.KeyValue, bool) {
	d := eval.EvaluateAttribute(telemetry.SignalMetric, key, value)
	if !d.Kept {
		return attribute.KeyValue{}, false
	}
	return attribute.String(key, d.Value), true
}

func (m *Metrics) markEmitted(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.emitted[name] = struct{}{}
}

// RecordIntentCreated records one BusinessIntent creation attempt.
func (m *Metrics) RecordIntentCreated(ctx context.Context, cellID, tenantClass, outcome string) {
	attrs := m.filterLabels(
		kv{"cell_id", cellID},
		kv{"tenant_class", tenantClass},
		kv{"outcome", outcome},
	)
	m.intentCreated.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricIntentCreated)
}

// RecordIntentSimulated records one BusinessIntent simulation run.
func (m *Metrics) RecordIntentSimulated(ctx context.Context, cellID, tenantClass, outcome string) {
	attrs := m.filterLabels(
		kv{"cell_id", cellID},
		kv{"tenant_class", tenantClass},
		kv{"outcome", outcome},
	)
	m.intentSimulated.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricIntentSimulated)
}

// RecordLedgerAppend records one ledger append attempt.
func (m *Metrics) RecordLedgerAppend(ctx context.Context, cellID, outcome string) {
	attrs := m.filterLabels(
		kv{"cell_id", cellID},
		kv{"outcome", outcome},
	)
	m.ledgerAppend.Add(ctx, 1, metric.WithAttributes(attrs...))
	m.markEmitted(metricLedgerAppend)
}

// RecordOutboxLag records the age, in milliseconds, of the oldest
// unpublished outbox entry.
func (m *Metrics) RecordOutboxLag(ctx context.Context, cellID string, ms float64) {
	attrs := m.filterLabels(kv{"cell_id", cellID})
	m.outboxLag.Record(ctx, ms, metric.WithAttributes(attrs...))
	m.markEmitted(metricOutboxLag)
}

// RecordEdgeParity records 1 when the named replication/consistency edge is
// in parity, 0 otherwise (telemetry.P1ACellMetrics's own description for
// edge.parity).
func (m *Metrics) RecordEdgeParity(ctx context.Context, cellID, edge string, inParity bool) {
	attrs := m.filterLabels(
		kv{"cell_id", cellID},
		kv{"edge", edge},
	)
	value := 0.0
	if inParity {
		value = 1.0
	}
	m.edgeParity.Record(ctx, value, metric.WithAttributes(attrs...))
	m.markEmitted(metricEdgeParity)
}

// kv is an unexported label-name/value pair; filterLabels is the only
// consumer.
type kv struct {
	key   string
	value string
}

func (m *Metrics) filterLabels(labels ...kv) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(labels))
	for _, l := range labels {
		if l.value == "" {
			continue
		}
		if attr, ok := metricAttr(m.eval, l.key, l.value); ok {
			out = append(out, attr)
		}
	}
	return out
}

// EmittedMetricNames implements telemetry.Sink's metric half: every catalog
// metric name at least one Record* call has been made for, in no particular
// order. It reflects that a *recording call happened*, independent of
// whether every label on that call survived the Evaluator — recording with
// a redacted label set is still telemetry being emitted, not telemetry
// being lost.
func (m *Metrics) EmittedMetricNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.emitted))
	for name := range m.emitted {
		out = append(out, name)
	}
	return out
}

// EmittedLogEventNames implements the other half of telemetry.Sink. This
// package emits no logs (that is internal/platform/logging's lane, and
// OBS-010's event-name registry it would report by does not exist yet), so
// it always reports none. A caller checking full OBS-006 completeness
// composes a telemetry.Sink that reports both halves once that registry
// lands; a metrics-only telemetry.RequiredSignals (omitting
// RequiredLogEvents) is what this package's own tests certify HEALTHY
// against.
func (m *Metrics) EmittedLogEventNames() []string { return nil }
