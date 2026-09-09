package replay

import (
	"context"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

// TestNew_RefusesUnusableWiring covers the two options a replayer cannot be
// built without.
func TestNew_RefusesUnusableWiring(t *testing.T) {
	plan := promotionPlan(t)
	rec := promotionRecord(t, plan)

	if _, err := New(Options{
		Source: NewMemorySource(rec), Contract: replayContract(t),
		Definition: replayDefinition(), Instance: replayInstance(),
	}); CodeOf(err) != CodeInvalidOptions {
		t.Fatalf("no plan: code = %q (%v)", CodeOf(err), err)
	}
	if _, err := New(Options{
		Plan: plan, Contract: replayContract(t),
		Definition: replayDefinition(), Instance: replayInstance(),
	}); CodeOf(err) != CodeInvalidOptions {
		t.Fatalf("no source: code = %q (%v)", CodeOf(err), err)
	}
}

// TestReplayer_ContractAndGateAreReadable checks the two accessors a composed
// caller uses to wire its handlers.
func TestReplayer_ContractAndGateAreReadable(t *testing.T) {
	plan := promotionPlan(t)
	r := newReplayer(t, plan, promotionRecord(t, plan))

	if got := r.Contract(); got.Mode.String() != "REPLAY" {
		t.Fatalf("Contract() = %s", got.Mode)
	}
	gate := r.EffectGate()
	if gate == nil {
		t.Fatalf("EffectGate returned nil")
	}
	if err := gate.Invoke(context.Background(), "a", AttemptDomainWrite); CodeOf(err) != CodeEffectForbidden {
		t.Fatalf("code = %q (%v)", CodeOf(err), err)
	}
}

// TestReplayer_ARecordWithNoTerminalAndNoPauseIsOpen covers the third clean
// status: a run still in flight when the record was taken is OPEN_FRONTIER,
// which is neither a completion nor a divergence.
func TestReplayer_ARecordWithNoTerminalAndNoPauseIsOpen(t *testing.T) {
	plan := promotionPlan(t)
	rec := pausedPromotionRecord(t, plan)
	rec.FinalStatus = "RUNNING"

	res, err := newReplayer(t, plan, rec).Replay(context.Background())
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Status != StatusOpenFrontier {
		t.Fatalf("status = %s, want %s (divergence %v)", res.Status, StatusOpenFrontier, res.Divergence)
	}
	if res.RecordedTraceDigest != "" || res.DigestMatches {
		t.Fatalf("a record pinning no digest reported a match")
	}
}

// TestOutcomeOf_CopiesTheRecordVerbatim is the heart of "take every node input
// from the recorded outputs": every field of the advancement input comes from
// the record, and none is derived.
func TestOutcomeOf_CopiesTheRecordVerbatim(t *testing.T) {
	nr := NodeRecord{
		NodeID: "a", RouteKey: "SUCCEEDED", OutputDigest: "sha256:a",
		Await: frontier.AwaitWorkItem, AwaitRef: "requirement:1",
		Failed: true, ErrorClass: "TIMEOUT",
		Terminal: frontier.TerminalResult{TerminalCode: "T", Asserted: true},
	}
	got := outcomeOf(nr)
	want := frontier.NodeOutcome{
		NodeID: "a", Outcome: "SUCCEEDED", OutputDigest: "sha256:a",
		Await: frontier.AwaitWorkItem, AwaitRef: "requirement:1",
		Failed: true, ErrorClass: "TIMEOUT",
		Terminal: frontier.TerminalResult{TerminalCode: "T", Asserted: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("outcomeOf = %+v, want %+v", got, want)
	}
}

// TestFirstDifferent names the one node a frontier divergence should point at,
// in both directions: a node the record expects and the replay does not have,
// and a node the replay produced that the record does not expect.
func TestFirstDifferent(t *testing.T) {
	for name, tc := range map[string]struct {
		recorded, replayed []string
		want               string
	}{
		"missing from the replay": {[]string{"a", "b"}, []string{"a"}, "b"},
		"extra in the replay":     {[]string{"a"}, []string{"a", "c"}, "c"},
		"identical":               {[]string{"a", "b"}, []string{"a", "b"}, ""},
		"both empty":              {nil, nil, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := firstDifferent(tc.recorded, tc.replayed); got != tc.want {
				t.Fatalf("firstDifferent = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestJoinIDsAndOnFrontier cover the two small readings the walk depends on.
func TestJoinIDsAndOnFrontier(t *testing.T) {
	if got := joinIDs([]string{"c", "a", "b"}); got != "a,b,c" {
		t.Fatalf("joinIDs = %q", got)
	}
	if got := joinIDs(nil); got != "" {
		t.Fatalf("joinIDs(nil) = %q", got)
	}
	// joinIDs must not sort the caller's slice.
	ids := []string{"c", "a"}
	_ = joinIDs(ids)
	if ids[0] != "c" {
		t.Fatalf("joinIDs sorted the caller's slice")
	}

	state := frontier.InstanceState{Frontier: []string{"a", "b"}}
	if !onFrontier(state, "b") || onFrontier(state, "z") {
		t.Fatalf("onFrontier disagrees with the state")
	}
	if got := firstOpen(state); got != "a" {
		t.Fatalf("firstOpen = %q", got)
	}
	if got := firstOpen(frontier.InstanceState{}); got != "" {
		t.Fatalf("firstOpen of an empty frontier = %q", got)
	}
}
