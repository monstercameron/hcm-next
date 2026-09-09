package testexport

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry"
)

// HarnessConfig supplies the only nondeterministic inputs a telemetry test
// normally has: time and identifiers. The harness never consults wall clock
// or random sources when these are omitted.
type HarnessConfig struct {
	Now      time.Time
	IDPrefix string
	Capacity int
}

type LogRecord struct {
	Sequence uint64
	At       time.Time
	Name     string
	Fields   map[string]string
}

type SpanRecord struct {
	Sequence uint64
	At       time.Time
	Name     string
	Attrs    map[string]string
	Outcome  telemetry.Outcome
	Error    bool
	Ended    bool
}

type MetricPoint struct {
	Sequence uint64
	At       time.Time
	Name     string
	Value    float64
	Labels   map[string]string
}

type DropRecord struct {
	Sequence uint64
	At       time.Time
	Signal   string
	Reason   string
}

// Snapshot is a deep-copy, stable view of all three signal types and drops.
type Snapshot struct {
	Logs    []LogRecord
	Spans   []SpanRecord
	Metrics []MetricPoint
	Drops   []DropRecord
}

// Harness is a bounded, concurrency-safe in-memory exporter trio. It stores
// only already-filtered test signals and never talks to a collector.
type Harness struct {
	mu       sync.Mutex
	next     uint64
	now      time.Time
	prefix   string
	capacity int
	logs     []LogRecord
	spans    []SpanRecord
	metrics  []MetricPoint
	drops    []DropRecord
}

// NewHarness returns an isolated deterministic exporter harness.
func NewHarness(cfg HarnessConfig) *Harness {
	if cfg.Now.IsZero() {
		cfg.Now = time.Unix(0, 0).UTC()
	}
	if cfg.IDPrefix == "" {
		cfg.IDPrefix = "test"
	}
	if cfg.Capacity <= 0 {
		cfg.Capacity = 1024
	}
	return &Harness{now: cfg.Now.UTC(), prefix: cfg.IDPrefix, capacity: cfg.Capacity}
}

// NextID returns a deterministic per-harness identifier.
func (h *Harness) NextID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	return fmt.Sprintf("%s-%06d", h.prefix, h.next)
}

// Log records one structured event with sorted, copied fields.
func (h *Harness) Log(name string, fields map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.logs) >= h.capacity {
		h.dropLocked("log", "capacity")
		return
	}
	h.logs = append(h.logs, LogRecord{Sequence: h.sequenceLocked(), At: h.now, Name: name, Fields: copyFields(fields)})
}

// StartSpan opens one in-memory span. End it exactly once through End.
func (h *Harness) StartSpan(name string, attrs map[string]string) *SpanHandle {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.spans) >= h.capacity {
		h.dropLocked("span", "capacity")
		return &SpanHandle{h: h, dropped: true}
	}
	record := SpanRecord{Sequence: h.sequenceLocked(), At: h.now, Name: name, Attrs: copyFields(attrs)}
	h.spans = append(h.spans, record)
	return &SpanHandle{h: h, index: len(h.spans) - 1}
}

// SpanHandle completes a span without exposing mutable exporter state.
type SpanHandle struct {
	h       *Harness
	index   int
	dropped bool
	once    sync.Once
}

// End marks the span complete. Repeated End calls are ignored.
func (s *SpanHandle) End(outcome telemetry.Outcome, err error) {
	if s == nil || s.h == nil {
		return
	}
	s.once.Do(func() {
		s.h.mu.Lock()
		defer s.h.mu.Unlock()
		if s.dropped || s.index < 0 || s.index >= len(s.h.spans) {
			return
		}
		s.h.spans[s.index].Outcome = outcome
		s.h.spans[s.index].Error = err != nil
		s.h.spans[s.index].Ended = true
	})
}

// Metric records a metric point with copied labels.
func (h *Harness) Metric(name string, value float64, labels map[string]string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.metrics) >= h.capacity {
		h.dropLocked("metric", "capacity")
		return
	}
	h.metrics = append(h.metrics, MetricPoint{Sequence: h.sequenceLocked(), At: h.now, Name: name, Value: value, Labels: copyFields(labels)})
}

// Drop records a deterministic drop reason rather than silently losing it.
func (h *Harness) Drop(signal, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.dropLocked(signal, reason)
}

func (h *Harness) dropLocked(signal, reason string) {
	if len(h.drops) < h.capacity {
		h.drops = append(h.drops, DropRecord{Sequence: h.sequenceLocked(), At: h.now, Signal: signal, Reason: reason})
	}
}

func (h *Harness) sequenceLocked() uint64 {
	h.next++
	return h.next
}

// Snapshot returns records ordered by their deterministic sequence number.
func (h *Harness) Snapshot() Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return Snapshot{
		Logs: cloneLogs(h.logs), Spans: cloneSpans(h.spans), Metrics: cloneMetrics(h.metrics), Drops: append([]DropRecord(nil), h.drops...),
	}
}

// Reset isolates a subsequent assertion window.
func (h *Harness) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next = 0
	h.logs, h.spans, h.metrics, h.drops = nil, nil, nil, nil
}

// AssertNoProhibitedTelemetry rejects unknown or signal-incompatible keys in
// all recorded attributes. It is intentionally an oracle, not a redactor.
func (h *Harness) AssertNoProhibitedTelemetry(allow *telemetry.Allowlist) error {
	snapshot := h.Snapshot()
	for _, record := range snapshot.Logs {
		if err := checkFields(allow, telemetry.SignalLog, record.Name, record.Fields); err != nil {
			return err
		}
	}
	for _, record := range snapshot.Spans {
		if err := checkFields(allow, telemetry.SignalSpan, record.Name, record.Attrs); err != nil {
			return err
		}
	}
	for _, record := range snapshot.Metrics {
		if err := checkFields(allow, telemetry.SignalMetric, record.Name, record.Labels); err != nil {
			return err
		}
	}
	return nil
}

func checkFields(allow *telemetry.Allowlist, signal telemetry.SignalKind, name string, fields map[string]string) error {
	for key := range fields {
		if allow == nil || !allow.AllowsSignal(key, signal) {
			return fmt.Errorf("telemetry: prohibited %s field %q on %q", signal, key, name)
		}
	}
	return nil
}

func copyFields(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(in))
	for _, key := range keys {
		out[key] = in[key]
	}
	return out
}

func cloneLogs(in []LogRecord) []LogRecord {
	out := append([]LogRecord(nil), in...)
	for i := range out {
		out[i].Fields = copyFields(out[i].Fields)
	}
	return out
}

func cloneSpans(in []SpanRecord) []SpanRecord {
	out := append([]SpanRecord(nil), in...)
	for i := range out {
		out[i].Attrs = copyFields(out[i].Attrs)
	}
	return out
}

func cloneMetrics(in []MetricPoint) []MetricPoint {
	out := append([]MetricPoint(nil), in...)
	for i := range out {
		out[i].Labels = copyFields(out[i].Labels)
	}
	return out
}

// Explain returns the signal counts in a stable format for test diagnostics.
func (s Snapshot) Explain() string {
	return strings.Join([]string{
		fmt.Sprintf("logs=%d", len(s.Logs)), fmt.Sprintf("spans=%d", len(s.Spans)),
		fmt.Sprintf("metrics=%d", len(s.Metrics)), fmt.Sprintf("drops=%d", len(s.Drops)),
	}, ",")
}
