package replay

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

func minimalRecord() Record {
	return Record{
		TenantID: uuid.New(), InstanceID: uuid.New(),
		WorkflowID: "wf", CompiledPlanDigest: "sha256:plan",
		HistoricalIntentID: "intent:1", FinalStatus: runtime.InstanceCompleted,
		Nodes: []NodeRecord{{
			Sequence: 1, NodeID: "a", Attempt: 1, StepType: workflow.StepEnd,
			RecordedAt: time.Unix(1, 0).UTC(),
		}},
	}
}

// TestRecord_ValidateRefusesEveryIncompleteShape walks the fields a replay
// cannot proceed without. Each one is a record a replay would otherwise
// consume and produce a plausible but wrong answer from.
func TestRecord_ValidateRefusesEveryIncompleteShape(t *testing.T) {
	if err := minimalRecord().Validate(); err != nil {
		t.Fatalf("a complete record was refused: %v", err)
	}
	for name, edit := range map[string]func(*Record){
		"no tenant":             func(r *Record) { r.TenantID = uuid.Nil },
		"no instance":           func(r *Record) { r.InstanceID = uuid.Nil },
		"no workflow":           func(r *Record) { r.WorkflowID = "" },
		"no plan digest":        func(r *Record) { r.CompiledPlanDigest = "" },
		"no historical intent":  func(r *Record) { r.HistoricalIntentID = "" },
		"undeclared status":     func(r *Record) { r.FinalStatus = "NOT_A_STATUS" },
		"no attempts":           func(r *Record) { r.Nodes = nil },
		"attempt names no node": func(r *Record) { r.Nodes[0].NodeID = "" },
		"attempt zero":          func(r *Record) { r.Nodes[0].Attempt = 0 },
		"sequence zero":         func(r *Record) { r.Nodes[0].Sequence = 0 },
		"no step type":          func(r *Record) { r.Nodes[0].StepType = "" },
		"no instant":            func(r *Record) { r.Nodes[0].RecordedAt = time.Time{} },
		"duplicate sequence": func(r *Record) {
			r.Nodes = append(r.Nodes, r.Nodes[0])
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec := minimalRecord()
			edit(&rec)
			err := rec.Validate()
			if CodeOf(err) != CodeRecordInvalid {
				t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeRecordInvalid)
			}
		})
	}
}

// TestRecord_OrderedIsByRecordedSequence checks that replay order comes from
// the recorded sequence and not from the order the slice happened to be built
// in, and that Ordered does not sort the caller's slice underneath it.
func TestRecord_OrderedIsByRecordedSequence(t *testing.T) {
	rec := minimalRecord()
	rec.Nodes = []NodeRecord{
		{Sequence: 3, NodeID: "c", Attempt: 1, StepType: workflow.StepEnd, RecordedAt: time.Unix(3, 0)},
		{Sequence: 1, NodeID: "a", Attempt: 1, StepType: workflow.StepTransform, RecordedAt: time.Unix(1, 0)},
		{Sequence: 2, NodeID: "b", Attempt: 1, StepType: workflow.StepTransform, RecordedAt: time.Unix(2, 0)},
	}
	var got []string
	for _, n := range rec.Ordered() {
		got = append(got, n.NodeID)
	}
	if !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("Ordered = %v", got)
	}
	if rec.Nodes[0].NodeID != "c" {
		t.Fatalf("Ordered sorted the caller's slice in place")
	}
}

// TestRecord_LookupsAndFrontier covers the accessors a replay reaches through:
// signal and timer resolution, the open frontier, and the pause reading.
func TestRecord_LookupsAndFrontier(t *testing.T) {
	rec := minimalRecord()
	rec.Signals = []SignalReceipt{{SignalID: "s1", NodeID: "a"}}
	rec.Timers = []TimerSettlement{{TimerID: "t1", NodeID: "a"}}
	rec.Frontier = []FrontierEntry{
		{NodeID: "b", State: "READY"},
		{NodeID: "a", State: "LEFT"},
		{NodeID: "c", State: "WAITING"},
	}

	if _, ok := rec.Signal("s1"); !ok {
		t.Fatalf("known signal not found")
	}
	if _, ok := rec.Signal("absent"); ok {
		t.Fatalf("unknown signal found")
	}
	if _, ok := rec.Timer("t1"); !ok {
		t.Fatalf("known timer not found")
	}
	if _, ok := rec.Timer("absent"); ok {
		t.Fatalf("unknown timer found")
	}
	if got := rec.OpenFrontier(); !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Fatalf("OpenFrontier = %v, want the two entries that have not left, sorted", got)
	}

	if rec.Paused() {
		t.Fatalf("a COMPLETED record reported itself paused")
	}
	for _, status := range []runtime.InstanceStatus{runtime.InstancePaused, runtime.InstancePauseRequested} {
		rec.FinalStatus = status
		if !rec.Paused() {
			t.Fatalf("%s did not report itself paused", status)
		}
	}
}

// TestRecord_CloneDoesNotShareBackingArrays is what makes concurrent and
// repeated replays safe: a clone a caller mutates must not be the record a
// replay reads.
func TestRecord_CloneDoesNotShareBackingArrays(t *testing.T) {
	rec := minimalRecord()
	rec.Nodes[0].RandomDraws = []string{"draw-1"}
	rec.Signals = []SignalReceipt{{SignalID: "s1"}}
	rec.Timers = []TimerSettlement{{TimerID: "t1"}}
	rec.Checkpoints = []Checkpoint{{Sequence: 1}}
	rec.Frontier = []FrontierEntry{{NodeID: "a", State: "READY"}}

	clone := rec.Clone()
	clone.Nodes[0].NodeID = "changed"
	clone.Nodes[0].RandomDraws[0] = "changed"
	clone.Signals[0].SignalID = "changed"
	clone.Timers[0].TimerID = "changed"
	clone.Checkpoints[0].Sequence = 99
	clone.Frontier[0].NodeID = "changed"

	switch {
	case rec.Nodes[0].NodeID != "a":
		t.Fatalf("clone shared the node slice")
	case rec.Nodes[0].RandomDraws[0] != "draw-1":
		t.Fatalf("clone shared a node's draws")
	case rec.Signals[0].SignalID != "s1":
		t.Fatalf("clone shared the signals")
	case rec.Timers[0].TimerID != "t1":
		t.Fatalf("clone shared the timers")
	case rec.Checkpoints[0].Sequence != 1:
		t.Fatalf("clone shared the checkpoints")
	case rec.Frontier[0].NodeID != "a":
		t.Fatalf("clone shared the frontier")
	}
}

// TestFrontierEntry_Open reads the one rule a frontier entry carries.
func TestFrontierEntry_Open(t *testing.T) {
	for state, want := range map[string]bool{
		"READY": true, "RUNNING": true, "WAITING": true, "BLOCKED": true,
		"LEFT": false, "": false,
	} {
		if got := (FrontierEntry{State: state}).Open(); got != want {
			t.Fatalf("state %q open = %v, want %v", state, got, want)
		}
	}
}
