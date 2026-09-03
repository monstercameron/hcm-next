package testexport

import "testing"

// TestLogRecorderLinesAndFilter proves the recorder decodes newline-
// delimited JSON envelopes in write order, preserves typed values, and
// LinesNamed filters on the envelope's "message" field.
func TestLogRecorderLinesAndFilter(t *testing.T) {
	rec := NewLogRecorder()
	n, err := rec.Write([]byte(`{"message":"start","n":1}` + "\n"))
	if err != nil || n != len(`{"message":"start","n":1}`+"\n") {
		t.Fatalf("Write returned (%d, %v), want (full length, nil)", n, err)
	}
	if _, err := rec.Write([]byte(`{"message":"step","n":2}` + "\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	lines := rec.Lines()
	if len(lines) != 2 {
		t.Fatalf("Lines() = %d, want 2", len(lines))
	}
	if lines[0]["message"] != "start" || lines[1]["message"] != "step" {
		t.Errorf("Lines() messages = [%v %v], want [start step]", lines[0]["message"], lines[1]["message"])
	}
	if v, _ := lines[0]["n"].(float64); v != 1 {
		t.Errorf("Lines()[0][n] = %v, want 1", lines[0]["n"])
	}

	named := rec.LinesNamed("step")
	if len(named) != 1 || named[0]["message"] != "step" {
		t.Errorf("LinesNamed(step) = %#v, want exactly the step line", named)
	}
	if got := rec.LinesNamed("absent"); len(got) != 0 {
		t.Errorf("LinesNamed(absent) = %#v, want none", got)
	}
}

// TestLogRecorderMalformedLineOmittedNotPanicking proves a line that fails
// to decode is omitted without panicking the assertion helper: well-formed
// lines written before it stay readable, and the bad line stops any later
// line from being decoded (the decoder breaks on first error).
func TestLogRecorderMalformedLineOmittedNotPanicking(t *testing.T) {
	rec := NewLogRecorder()
	_, _ = rec.Write([]byte(`{"message":"ok"}` + "\n"))
	_, _ = rec.Write([]byte(`{broken json` + "\n"))
	_, _ = rec.Write([]byte(`{"message":"never"}` + "\n"))

	lines := rec.Lines()
	if len(lines) != 1 || lines[0]["message"] != "ok" {
		t.Errorf("Lines() = %#v, want just the line before the malformed one", lines)
	}
}

// TestLogRecorderReset proves Reset clears every recorded line.
func TestLogRecorderReset(t *testing.T) {
	rec := NewLogRecorder()
	_, _ = rec.Write([]byte(`{"message":"x"}` + "\n"))
	rec.Reset()
	if got := len(rec.Lines()); got != 0 {
		t.Errorf("Lines() after Reset = %d, want 0", got)
	}
}
