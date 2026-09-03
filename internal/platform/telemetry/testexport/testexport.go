// Package testexport builds OBS-015's deterministic in-memory structured-log
// exporter — the log half of the structured-logging spec's own "Testing
// contract" section ("The test harness supplies deterministic clocks and
// IDs plus in-memory log, trace and metric exporters. Tests assert exact
// records/spans/links/attributes...").
//
// OBS-023's own test matrix ("the in-memory exporter (OBS-015) proves the
// chain in tests") is what actually motivated building this now, ahead of
// OBS-015 itself: it is named and shaped so that whichever lane implements
// OBS-015's full contract (log AND trace AND metric, plus the negative
// fixtures OBS-015's own RED clause requires) can adopt it directly rather
// than reimplementing it a second time.
//
// The trace-side companion recorder lives at
// internal/platform/telemetry/otel/testexport, not here:
// definitions/architecture/dependency-roles.yaml (LIB-007) admits only
// internal/platform/telemetry/otel and internal/transport/otelmw to import
// go.opentelemetry.io/otel directly, and a real sdktrace.SpanExporter
// implementation cannot avoid that import. This package's own LogRecorder
// has no such dependency (it is a plain io.Writer over
// internal/platform/logging's JSON envelope), so it stays at this
// unprefixed path.
//
// Neither recorder applies privacy/cardinality policy of its own — that is
// internal/platform/telemetry's job (the Evaluator OBS-004 builds, and the
// filtering wrappers internal/platform/telemetry/otel already applies
// before any span reaches an exporter). This package only records,
// deterministically and in memory, whatever already-filtered signal
// reaches it.
package testexport

import (
	"bytes"
	"encoding/json"
	"sync"
)

// LogLine is one decoded structured-log envelope: the JSON object one
// internal/platform/logging.Handler record renders, as generic key/value
// pairs so a test can assert on exactly the fields it cares about without
// this package owning the envelope's Go struct shape.
type LogLine map[string]any

// LogRecorder is an io.Writer a [logging.Handler] (or any other
// newline-delimited-JSON emitter) writes envelope lines to. It decodes them
// back into inspectable [LogLine] values instead of leaving a test to parse
// raw bytes.
type LogRecorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// NewLogRecorder returns an empty LogRecorder.
func NewLogRecorder() *LogRecorder { return &LogRecorder{} }

// Write implements io.Writer.
func (r *LogRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

// Lines decodes every newline-delimited JSON object written so far, in
// write order. A line that fails to decode (should never happen against a
// well-formed logging.Handler) is silently omitted rather than panicking a
// test's assertion helper.
func (r *LogRecorder) Lines() []LogLine {
	r.mu.Lock()
	data := append([]byte(nil), r.buf.Bytes()...)
	r.mu.Unlock()

	var out []LogLine
	dec := json.NewDecoder(bytes.NewReader(data))
	for dec.More() {
		var line LogLine
		if err := dec.Decode(&line); err != nil {
			break
		}
		out = append(out, line)
	}
	return out
}

// LinesNamed returns every recorded line whose "message" field (the
// [log/slog] record's message, as internal/platform/logging.Handler's own
// envelope names it) equals event, in write order.
func (r *LogRecorder) LinesNamed(event string) []LogLine {
	var out []LogLine
	for _, line := range r.Lines() {
		if msg, _ := line["message"].(string); msg == event {
			out = append(out, line)
		}
	}
	return out
}

// Reset discards every recorded line.
func (r *LogRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf.Reset()
}
