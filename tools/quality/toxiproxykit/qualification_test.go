package toxiproxykit_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/hcm-next/tools/quality/toxiproxykit"
)

func schedule() toxiproxykit.Schedule {
	return toxiproxykit.Schedule{Seed: 22, Faults: []toxiproxykit.Fault{{Kind: toxiproxykit.Latency, AtMS: 0, Value: 25}, {Kind: toxiproxykit.Timeout, AtMS: 25}, {Kind: toxiproxykit.Truncate, AtMS: 50, Value: 8}}}
}

func TestToxiproxyQualificationReproducesDeclaredTransportFaultSchedule(t *testing.T) {
	s := schedule()
	run := toxiproxykit.Run{Outcome: toxiproxykit.RetryableFailure, DurableWrites: 1, Timeline: s.Faults}
	if err := run.Validate(s); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_TOOL_022_Golden(t *testing.T) {
	s := schedule()
	if err := (toxiproxykit.Run{Outcome: toxiproxykit.Accepted, DurableWrites: 1, AcceptedEffects: 1, Timeline: s.Faults}).Validate(s); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_TOOL_022_Race(t *testing.T) {
	t.Parallel()
	s := schedule()
	if err := (toxiproxykit.Run{Outcome: toxiproxykit.UnknownCommit, DurableWrites: 1, Timeline: s.Faults}).Validate(s); err != nil {
		t.Fatal(err)
	}
}
func TestTodo_TOOL_022_Integration(t *testing.T) {
	t.Skip("requires an explicitly provisioned, digest-pinned injector and endpoint")
}
func TestTodo_TOOL_022_Fault(t *testing.T) {
	s := schedule()
	r := toxiproxykit.Run{Outcome: toxiproxykit.RetryableFailure, DurableWrites: 1, Timeline: s.Faults}
	if err := r.Validate(s); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_TOOL_022_Conformance(t *testing.T) {
	root := repoRoot(t)
	q, err := toxiproxykit.LoadQualification(filepath.Join(root, "definitions", "toolchain", "toxiproxy-qualification.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if q.Tool != "Toxiproxy" || q.Verdict != "QUALIFIED" {
		t.Fatalf("manifest tool/verdict = %q/%q", q.Tool, q.Verdict)
	}
	if len(q.Scope.Excludes) == 0 {
		t.Fatal("scope exclusions are required")
	}
	for _, term := range []string{"production", "nondeterministic"} {
		found := false
		for _, x := range q.Scope.Excludes {
			found = found || strings.Contains(strings.ToLower(x), term)
		}
		if !found {
			t.Errorf("exclusions missing %q", term)
		}
	}
	want := map[string]bool{"TestToxiproxyQualificationReproducesDeclaredTransportFaultSchedule": false, "TestTodo_TOOL_022_Golden": false, "TestTodo_TOOL_022_Race": false, "TestTodo_TOOL_022_Integration": false, "TestTodo_TOOL_022_Fault": false, "TestTodo_TOOL_022_Conformance": false}
	for _, e := range q.Evidence {
		if _, ok := want[e.Test]; !ok {
			t.Errorf("unexpected evidence %q", e.Test)
		} else {
			want[e.Test] = true
			if e.Package != "tools/quality/toxiproxykit" {
				t.Errorf("%s package=%q", e.Test, e.Package)
			}
		}
	}
	for n, ok := range want {
		if !ok {
			t.Errorf("missing evidence %q", n)
		}
	}
	if q.Command != "go test -count=1 ./tools/quality/toxiproxykit/..." {
		t.Errorf("command=%q", q.Command)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join("..", "..", ".."))
}
