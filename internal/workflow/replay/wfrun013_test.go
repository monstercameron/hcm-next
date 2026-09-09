package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_WF_RUN_013 is WF-RUN-013's PRIMARY case: the deterministic REPLAY
// mode itself.
//
// It replays the Promotion reference run from its durable record and asserts
// the four properties the ticket's GREEN clause names, together:
//
//   - every node input came from the record (the trace's route keys and output
//     digests are the recorded ones, node for node, and the walk is in the
//     recorded sequence);
//   - the pinned historical version was used (the replay refuses a plan whose
//     digest is not the record's -- see the FAULT case -- and the trace pins
//     the digest it did run);
//   - nothing was regenerated and nothing reached out (the recorder turned
//     nothing away because nothing was attempted, and the run's clock is the
//     record's own instants);
//   - the comparison identifies agreement (the replayed trace reproduces the
//     digest the record pins).
func TestTodo_WF_RUN_013(t *testing.T) {
	plan := promotionPlan(t)
	rec := promotionRecord(t, plan)

	// Pin the record's trace digest to what a first replay produces, which is
	// how a real recorded run gets one: the run mints its trace, the trace's
	// digest is stored, and every later replay must reproduce it.
	first, err := newReplayer(t, plan, rec).Replay(context.Background())
	if err != nil {
		t.Fatalf("first replay: %v", err)
	}
	rec.TraceDigest = first.Trace.Digest()

	res, err := newReplayer(t, plan, rec).Replay(context.Background())
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if res.Status != StatusComplete {
		t.Fatalf("status = %s, want %s (divergence %v)", res.Status, StatusComplete, res.Divergence)
	}
	if res.Divergence != nil {
		t.Fatalf("clean replay reported a divergence: %s", res.Divergence)
	}
	if !res.DigestMatches || res.RecordedTraceDigest != res.Trace.Digest() {
		t.Fatalf("digest match=%v recorded=%s replayed=%s",
			res.DigestMatches, res.RecordedTraceDigest, res.Trace.Digest())
	}
	if res.Trace.TerminalCode != fixtureTerminal {
		t.Fatalf("terminal = %q, want %q", res.Trace.TerminalCode, fixtureTerminal)
	}
	if len(res.Trace.Frontier) != 0 {
		t.Fatalf("a completed replay left an open frontier: %v", res.Trace.Frontier)
	}

	// Every node input is the recorded one, in the recorded order.
	if len(res.Trace.Entries) != len(rec.Nodes) {
		t.Fatalf("replayed %d nodes, record holds %d: %v", len(res.Trace.Entries), len(rec.Nodes), nodeIDs(res.Trace))
	}
	for i, entry := range res.Trace.Entries {
		want := rec.Nodes[i]
		switch {
		case entry.NodeID != want.NodeID:
			t.Fatalf("entry %d node = %s, recorded %s", i, entry.NodeID, want.NodeID)
		case entry.Sequence != want.Sequence:
			t.Fatalf("entry %d sequence = %d, recorded %d", i, entry.Sequence, want.Sequence)
		case entry.RouteKey != want.RouteKey:
			t.Fatalf("entry %d route = %q, recorded %q", i, entry.RouteKey, want.RouteKey)
		case entry.OutputDigest != want.OutputDigest:
			t.Fatalf("entry %d output = %q, recorded %q", i, entry.OutputDigest, want.OutputDigest)
		case entry.Source != SourceRecordedOutput:
			t.Fatalf("entry %d source = %s, want %s", i, entry.Source, SourceRecordedOutput)
		case !entry.ObservedAt.Equal(want.RecordedAt):
			t.Fatalf("entry %d observed at %s, recorded %s", i, entry.ObservedAt, want.RecordedAt)
		case entry.TransitionDigest == "":
			t.Fatalf("entry %d carries no transition digest", i)
		}
	}
	if got := res.Trace.Entries[len(res.Trace.Entries)-1].CompletedState; got != string(frontier.NodeSucceeded) {
		t.Fatalf("terminal node completed %s, want %s", got, frontier.NodeSucceeded)
	}

	// Nothing was attempted, so nothing was refused: a replay that had reached
	// for an effect would have left a refusal behind.
	if len(res.Refusals) != 0 {
		t.Fatalf("replay attempted effects: %+v", res.Refusals)
	}
	if err := res.Trace.Verify(); err != nil {
		t.Fatalf("trace does not verify against its own digest: %v", err)
	}
}

// TestTodo_WF_RUN_013_Golden pins the Promotion fixture's replayed trace, byte
// for byte, so that a change to the compiled plan, to the frontier's
// advancement rules or to this package's own canonicalization is a diff a
// reviewer sees rather than a digest that silently moved.
func TestTodo_WF_RUN_013_Golden(t *testing.T) {
	plan := promotionPlan(t)
	res, err := newReplayer(t, plan, promotionRecord(t, plan)).Replay(context.Background())
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	got, err := res.Trace.JSON()
	if err != nil {
		t.Fatalf("render trace: %v", err)
	}

	path := filepath.Join("testdata", "wfrun013_promotion_trace.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("replayed trace differs from %s\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}

	// The golden's own digest field is the contract; assert it separately so a
	// failure says which half moved.
	var rendered struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal(want, &rendered); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if rendered.Digest != res.Trace.Digest() {
		t.Fatalf("golden digest %s, replayed %s", rendered.Digest, res.Trace.Digest())
	}
}

// TestTodo_WF_RUN_013_Property is the ticket's determinism property: replaying
// twice is byte-identical.
//
// It replays the same record eight times through freshly built replayers and
// requires every rendering to be the same bytes. Two separate replayers rather
// than one called twice is the stronger statement: it proves the determinism
// is in the record and the plan, not in a cache the first call warmed.
func TestTodo_WF_RUN_013_Property(t *testing.T) {
	plan := promotionPlan(t)
	rec := promotionRecord(t, plan)

	var want []byte
	for i := 0; i < 8; i++ {
		res, err := newReplayer(t, plan, rec).Replay(context.Background())
		if err != nil {
			t.Fatalf("replay %d: %v", i, err)
		}
		got, err := res.Trace.JSON()
		if err != nil {
			t.Fatalf("render %d: %v", i, err)
		}
		if want == nil {
			want = got
			continue
		}
		if !bytes.Equal(want, got) {
			t.Fatalf("replay %d rendered different bytes\n--- first ---\n%s\n--- now ---\n%s", i, want, got)
		}
	}

	// The same record replayed through a source the caller then mutates must
	// still produce the first answer: MemorySource clones, so a caller cannot
	// change a replay after the fact.
	src := NewMemorySource(rec)
	held := src.Record()
	held.Nodes[0].OutputDigest = "sha256:tampered"
	r, err := New(Options{
		Plan: plan, Source: src, Contract: replayContract(t),
		Definition: replayDefinition(), Instance: replayInstance(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := r.Replay(context.Background())
	if err != nil {
		t.Fatalf("replay after caller mutation: %v", err)
	}
	got, err := res.Trace.JSON()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("a caller mutating its own copy changed the replay")
	}
}

// TestTodo_WF_RUN_013_Race drives eight concurrent replays of one record and
// requires the serial answer byte for byte, and requires the shared record to
// come back unmutated.
func TestTodo_WF_RUN_013_Race(t *testing.T) {
	plan := promotionPlan(t)
	rec := promotionRecord(t, plan)
	before, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}

	serial, err := newReplayer(t, plan, rec).Replay(context.Background())
	if err != nil {
		t.Fatalf("serial replay: %v", err)
	}
	want, err := serial.Trace.JSON()
	if err != nil {
		t.Fatalf("render serial: %v", err)
	}

	// One replayer, eight callers: Replay must hold no state across calls.
	shared := newReplayer(t, plan, rec)
	const n = 8
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
		outs [][]byte
	)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			res, err := shared.Replay(context.Background())
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			b, err := res.Trace.JSON()
			if err != nil {
				errs = append(errs, err)
				return
			}
			outs = append(outs, b)
		}()
	}
	wg.Wait()
	if len(errs) != 0 {
		t.Fatalf("concurrent replays failed: %v", errs)
	}
	for i, got := range outs {
		if !bytes.Equal(want, got) {
			t.Fatalf("concurrent replay %d differs from the serial one\n%s", i, got)
		}
	}

	after, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("concurrent replays mutated the shared record")
	}
}

// TestTodo_WF_RUN_013_Fault is the ticket's FAULT case: a record missing an
// artifact the walk reaches for yields a [Divergence] and the typed
// [CodeArtifactUnavailable] refusal, never a panic and never a silently
// shorter run.
func TestTodo_WF_RUN_013_Fault(t *testing.T) {
	plan := promotionPlan(t)

	t.Run("a record that ends the instance without reaching a terminal", func(t *testing.T) {
		rec := promotionRecord(t, plan)
		rec.Nodes = rec.Nodes[:3] // the output of build_proposal onward is gone
		res, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeArtifactUnavailable {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeArtifactUnavailable)
		}
		if !errors.Is(err, ErrReplay) {
			t.Fatalf("error does not unwrap to ErrReplay: %v", err)
		}
		if res.Divergence == nil || res.Divergence.Field != FieldArtifact {
			t.Fatalf("divergence = %v, want an artifact divergence", res.Divergence)
		}
		if res.Divergence.NodeID != workflow.PromotionNodeBuildProposal {
			t.Fatalf("divergence names %q, want %q", res.Divergence.NodeID, workflow.PromotionNodeBuildProposal)
		}
		if res.Status != StatusDiverged {
			t.Fatalf("status = %s, want %s", res.Status, StatusDiverged)
		}
		// The partial walk survives: an investigator gets the three nodes that
		// did replay, not just an error string.
		if len(res.Trace.Entries) != 3 {
			t.Fatalf("partial trace holds %d entries, want 3", len(res.Trace.Entries))
		}
	})

	t.Run("an attempt naming a signal the record does not hold", func(t *testing.T) {
		rec := promotionRecord(t, plan)
		rec.Nodes[1].SignalID = "signal:absent"
		res, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeArtifactUnavailable {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeArtifactUnavailable)
		}
		if NodeOf(err) != workflow.PromotionNodeSimulateComp {
			t.Fatalf("refusal names %q, want %q", NodeOf(err), workflow.PromotionNodeSimulateComp)
		}
		if res.Divergence == nil || res.Divergence.Recorded != "signal:absent" {
			t.Fatalf("divergence = %v", res.Divergence)
		}
	})

	t.Run("an attempt naming a timer the record does not hold", func(t *testing.T) {
		rec := promotionRecord(t, plan)
		rec.Nodes[1].TimerID = "timer:absent"
		_, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeArtifactUnavailable {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeArtifactUnavailable)
		}
	})

	t.Run("a plan that is not the one the record pins", func(t *testing.T) {
		rec := promotionRecord(t, plan)
		rec.CompiledPlanDigest = "sha256:" + strings.Repeat("9", 64)
		_, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodePlanMismatch {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodePlanMismatch)
		}
	})

	t.Run("a source that cannot answer", func(t *testing.T) {
		boom := errors.New("storage is down")
		r, err := New(Options{
			Plan:     plan,
			Source:   sourceFunc(func(context.Context) (Record, error) { return Record{}, boom }),
			Contract: replayContract(t), Definition: replayDefinition(), Instance: replayInstance(),
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		_, err = r.Replay(context.Background())
		if CodeOf(err) != CodeSourceFailed || !errors.Is(err, boom) {
			t.Fatalf("code = %q, err = %v", CodeOf(err), err)
		}
	})
}

// TestTodo_WF_RUN_013_Recovery is the ticket's RECOVERY case: a replay of a
// paused instance stops at the frontier the record still holds open, with a
// typed status, rather than walking past a pause the historical run never
// walked past.
func TestTodo_WF_RUN_013_Recovery(t *testing.T) {
	plan := promotionPlan(t)
	rec := pausedPromotionRecord(t, plan)

	res, err := newReplayer(t, plan, rec).Replay(context.Background())
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Status != StatusPausedAtFrontier {
		t.Fatalf("status = %s, want %s (divergence %v)", res.Status, StatusPausedAtFrontier, res.Divergence)
	}
	if res.Divergence != nil {
		t.Fatalf("paused replay reported a divergence: %s", res.Divergence)
	}
	want := []string{workflow.PromotionNodeBuildProposal}
	if !reflect.DeepEqual(res.Trace.Frontier, want) {
		t.Fatalf("frontier = %v, want %v", res.Trace.Frontier, want)
	}
	if res.Trace.TerminalCode != "" {
		t.Fatalf("a paused replay reached a terminal: %q", res.Trace.TerminalCode)
	}
	if len(res.Trace.Entries) != 3 {
		t.Fatalf("replayed %d nodes, want 3: %v", len(res.Trace.Entries), nodeIDs(res.Trace))
	}

	// A PAUSE_REQUESTED instance is the same answer: a standing request is an
	// overlay on a live instance, and a replay of one stops where it stopped.
	rec.FinalStatus = runtime.InstancePauseRequested
	res, err = newReplayer(t, plan, rec).Replay(context.Background())
	if err != nil || res.Status != StatusPausedAtFrontier {
		t.Fatalf("PAUSE_REQUESTED replay: status=%s err=%v", res.Status, err)
	}

	// A frontier the record contradicts is a divergence, not a shrug.
	rec.Frontier = []FrontierEntry{{
		NodeID: workflow.PromotionNodeObserveDrift, State: "READY", Sequence: 4,
		EnteredAt: fixtureStart,
	}}
	res, err = newReplayer(t, plan, rec).Replay(context.Background())
	if CodeOf(err) != CodeDivergence {
		t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeDivergence)
	}
	if res.Divergence == nil || res.Divergence.Field != FieldOpenFrontier {
		t.Fatalf("divergence = %v, want an open-frontier divergence", res.Divergence)
	}
	if res.Divergence.NodeID != workflow.PromotionNodeObserveDrift {
		t.Fatalf("divergence names %q, want %q", res.Divergence.NodeID, workflow.PromotionNodeObserveDrift)
	}
}

// TestTodo_WF_RUN_013_Security is the ticket's RED clause read as a threat:
// replay must not produce an effect, a task or a message, and an adapter that
// would reach out must be refused rather than merely unused by luck.
func TestTodo_WF_RUN_013_Security(t *testing.T) {
	plan := promotionPlan(t)
	rec := promotionRecord(t, plan)
	reaching := &reachingAdapter{}

	r, err := New(Options{
		Plan: plan, Source: NewMemorySource(rec), Contract: replayContract(t),
		Definition: replayDefinition(), Instance: replayInstance(), Adapter: reaching,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// A full replay never invokes the adapter at all.
	if _, err := r.Replay(context.Background()); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if reaching.calls() != 0 {
		t.Fatalf("replay invoked the live adapter %d times", reaching.calls())
	}

	// And every attempt a composed handler could make through the gate is
	// refused, naming the node, without reaching the adapter.
	gate := r.EffectGate()
	for _, attempt := range EffectAttempts() {
		err := gate.Invoke(context.Background(), workflow.PromotionNodeSnapshotWorker, attempt)
		if attempt == AttemptGovernedRead {
			if err != nil {
				t.Fatalf("a governed read was refused: %v", err)
			}
			continue
		}
		if CodeOf(err) != CodeEffectForbidden {
			t.Fatalf("%s: code = %q (%v), want %s", attempt, CodeOf(err), err, CodeEffectForbidden)
		}
		if NodeOf(err) != workflow.PromotionNodeSnapshotWorker {
			t.Fatalf("%s: refusal names %q, want the node", attempt, NodeOf(err))
		}
	}
	if reaching.calls() != 0 {
		t.Fatalf("the gate delegated to the live adapter %d times", reaching.calls())
	}

	// The refusals a composed handler provoked survive into the next result,
	// so a report says what was attempted rather than only that it failed.
	if got := len(r.Refusals()); got != len(EffectAttempts())-1 {
		t.Fatalf("the replayer recorded %d refusals, want %d", got, len(EffectAttempts())-1)
	}
	after, err := r.Replay(context.Background())
	if err != nil {
		t.Fatalf("replay after the refused attempts: %v", err)
	}
	if len(after.Refusals) != len(EffectAttempts())-1 {
		t.Fatalf("the result carried %d refusals: %+v", len(after.Refusals), after.Refusals)
	}
	for _, ref := range after.Refusals {
		if ref.Code != CodeEffectForbidden || ref.NodeID != workflow.PromotionNodeSnapshotWorker {
			t.Fatalf("refusal = %+v", ref)
		}
	}

	// A recorder records what it turned away, so a refusal is evidence rather
	// than only a returned error.
	recorder := NewRecorder(rec)
	if err := recorder.Attempt(context.Background(), workflow.PromotionNodeEndApproval, AttemptMessageSend); err == nil {
		t.Fatalf("a message send was admitted")
	}
	refusals := recorder.Refusals()
	if len(refusals) != 1 || refusals[0].NodeID != workflow.PromotionNodeEndApproval ||
		refusals[0].Attempt != AttemptMessageSend || refusals[0].Code != CodeEffectForbidden {
		t.Fatalf("refusals = %+v", refusals)
	}
}

// TestTodo_WF_RUN_013_Mutation is the ticket's MUTATION case: each of the
// guarantees below, weakened by one edit, must fail.
func TestTodo_WF_RUN_013_Mutation(t *testing.T) {
	plan := promotionPlan(t)
	base := promotionRecord(t, plan)

	t.Run("a tampered output digest breaks the pinned trace digest", func(t *testing.T) {
		clean, err := newReplayer(t, plan, base).Replay(context.Background())
		if err != nil {
			t.Fatalf("clean replay: %v", err)
		}
		rec := base.Clone()
		rec.TraceDigest = clean.Trace.Digest()
		rec.Nodes[2].OutputDigest = "sha256:" + strings.Repeat("f", 64)

		res, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeDivergence {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeDivergence)
		}
		if res.Divergence == nil || res.Divergence.Field != FieldTraceDigest {
			t.Fatalf("divergence = %v, want a trace-digest divergence", res.Divergence)
		}
		if res.DigestMatches {
			t.Fatalf("a tampered record still reported a digest match")
		}
	})

	t.Run("a rerouted decision no longer reaches the recorded terminal", func(t *testing.T) {
		rec := base.Clone()
		rec.Nodes[4].RouteKey = "WITHIN_THRESHOLD"
		rec.Nodes[5].NodeID = workflow.PromotionNodeEndApproval
		res, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeDivergence {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeDivergence)
		}
		if res.Divergence == nil || res.Divergence.Field != FieldFrontier {
			t.Fatalf("divergence = %v, want a frontier divergence", res.Divergence)
		}
		if res.Divergence.NodeID != workflow.PromotionNodeEndApproval {
			t.Fatalf("divergence names %q", res.Divergence.NodeID)
		}
	})

	t.Run("an undeclared route key cannot be routed", func(t *testing.T) {
		// build_proposal is a TRANSFORM, whose declared outcomes are SUCCEEDED
		// and FAILED and which has no default route: an outcome outside that
		// set is unroutable rather than defaulted. raise_threshold, the
		// DECISION beside it, deliberately behaves differently -- an
		// undeclared key there takes the declared default_route, and the
		// divergence then surfaces at the next node as a frontier divergence,
		// which is what the "rerouted decision" case above covers.
		rec := base.Clone()
		rec.Nodes[3].RouteKey = "NOT_A_DECLARED_ROUTE"
		res, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeDivergence {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeDivergence)
		}
		if res.Divergence == nil || res.Divergence.Field != FieldRoute {
			t.Fatalf("divergence = %v, want a route divergence", res.Divergence)
		}
	})

	t.Run("a step type the plan does not declare for that node", func(t *testing.T) {
		rec := base.Clone()
		rec.Nodes[0].StepType = workflow.StepApproval
		res, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeDivergence {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeDivergence)
		}
		if res.Divergence == nil || res.Divergence.Replayed != string(workflow.StepCapability) {
			t.Fatalf("divergence = %v", res.Divergence)
		}
	})

	t.Run("a node the plan does not declare at all", func(t *testing.T) {
		rec := base.Clone()
		rec.Nodes[0].NodeID = "no_such_node"
		res, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeDivergence {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeDivergence)
		}
		if res.Divergence == nil || res.Divergence.NodeID != "no_such_node" {
			t.Fatalf("divergence = %v", res.Divergence)
		}
	})

	t.Run("a terminal the record contradicts", func(t *testing.T) {
		rec := base.Clone()
		rec.TerminalCode = "SOMETHING_ELSE"
		res, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeDivergence {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeDivergence)
		}
		if res.Divergence == nil || res.Divergence.Field != FieldTerminal {
			t.Fatalf("divergence = %v, want a terminal divergence", res.Divergence)
		}
	})

	t.Run("an edited trace no longer verifies", func(t *testing.T) {
		res, err := newReplayer(t, plan, base).Replay(context.Background())
		if err != nil {
			t.Fatalf("Replay: %v", err)
		}
		tampered := res.Trace
		tampered.TerminalCode = "EDITED"
		if err := tampered.Verify(); err == nil {
			t.Fatalf("an edited trace verified against its own digest")
		}
	})

	t.Run("two attempts sharing a recorded sequence are refused", func(t *testing.T) {
		rec := base.Clone()
		rec.Nodes[1].Sequence = rec.Nodes[0].Sequence
		_, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeRecordInvalid {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeRecordInvalid)
		}
	})

	t.Run("a record that names no historical intent is refused", func(t *testing.T) {
		rec := base.Clone()
		rec.HistoricalIntentID = ""
		_, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeRecordInvalid {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeRecordInvalid)
		}
	})

	t.Run("an attempt with no recorded instant is refused", func(t *testing.T) {
		rec := base.Clone()
		rec.Nodes[0].RecordedAt = time.Time{}
		_, err := newReplayer(t, plan, rec).Replay(context.Background())
		if CodeOf(err) != CodeRecordInvalid {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeRecordInvalid)
		}
	})

	t.Run("a bound smaller than the record refuses rather than truncating", func(t *testing.T) {
		r, err := New(Options{
			Plan: plan, Source: NewMemorySource(base), MaxSteps: 2,
			Contract: replayContract(t), Definition: replayDefinition(), Instance: replayInstance(),
		})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if _, err := r.Replay(context.Background()); CodeOf(err) != CodeStepBudgetExceeded {
			t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeStepBudgetExceeded)
		}
	})
}

// TestTodo_WF_RUN_013_Conformance walks the INTENT-023 mode matrix and
// requires that exactly one of its twenty rows admits a replay.
//
// It is the "runs only under the REPLAY ModeContract" clause proved against
// the actual table rather than against the one row a test happened to pick:
// EXECUTE is refused in every environment, and so is every other mode.
func TestTodo_WF_RUN_013_Conformance(t *testing.T) {
	plan := promotionPlan(t)
	rec := promotionRecord(t, plan)
	def, inst := replayDefinition(), replayInstance()

	admitted := 0
	for _, contract := range intent.ModeContracts() {
		_, err := New(Options{
			Plan: plan, Source: NewMemorySource(rec),
			Contract: contract, Definition: def, Instance: inst,
		})
		if contract.Mode == intent.ModeReplay {
			if err != nil {
				t.Fatalf("REPLAY/%s was refused: %v", contract.Environment, err)
			}
			admitted++
			continue
		}
		if err == nil {
			t.Fatalf("%s/%s admitted a replay", contract.Mode, contract.Environment)
		}
		if CodeOf(err) != CodeModeRefused {
			t.Fatalf("%s/%s: code = %q (%v), want %s",
				contract.Mode, contract.Environment, CodeOf(err), err, CodeModeRefused)
		}
	}
	if admitted != len(intent.Environments()) {
		t.Fatalf("admitted %d contracts, want one per environment (%d)", admitted, len(intent.Environments()))
	}

	// A REPLAY contract whose guarantees have been loosened is refused too --
	// the mode's name is not the guarantee.
	base := replayContract(t)
	for _, tc := range []struct {
		name string
		edit func(intent.ModeContract) intent.ModeContract
		want string
	}{
		{"permits a domain commit", func(c intent.ModeContract) intent.ModeContract {
			c.AllowsDomainCommit = true
			return c
		}, CodeEffectForbidden},
		{"permits an external effect", func(c intent.ModeContract) intent.ModeContract {
			c.AllowsExternalEffect = true
			c.EffectCeiling = intent.EffectClassExternalMutation
			return c
		}, CodeEffectForbidden},
		{"permits a live approval", func(c intent.ModeContract) intent.ModeContract {
			c.AllowsApprovalConsumption = true
			return c
		}, CodeEffectForbidden},
		{"reads the wall clock", func(c intent.ModeContract) intent.ModeContract {
			c.Clock = intent.ClockWall
			return c
		}, CodeModeRefused},
		{"binds live adapters", func(c intent.ModeContract) intent.ModeContract {
			c.Adapters = intent.AdapterLive
			return c
		}, CodeModeRefused},
		{"requires no historical causation", func(c intent.ModeContract) intent.ModeContract {
			c.RequiresHistoricalCausation = false
			return c
		}, CodeModeRefused},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := replayDefinition()
			d.EffectClass = intent.EffectClassExternalMutation
			_, err := New(Options{
				Plan: plan, Source: NewMemorySource(rec),
				Contract: tc.edit(base), Definition: d, Instance: inst,
			})
			if CodeOf(err) != tc.want {
				t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, tc.want)
			}
		})
	}
}

// TestTodo_WF_RUN_013_CausalSeparation is INTENT-023's causal-separation rule
// at this package's boundary: a replay must name the historical intent it
// re-derives, and must not be that intent.
func TestTodo_WF_RUN_013_CausalSeparation(t *testing.T) {
	plan := promotionPlan(t)
	rec := promotionRecord(t, plan)
	self := fixtureReplayIntent

	for _, tc := range []struct {
		name string
		inst intent.Instance
	}{
		{"names no historical intent", intent.Instance{
			IntentID: fixtureReplayIntent, ExecutionMode: intent.ModeReplay,
		}},
		{"names itself as the run it re-derives", intent.Instance{
			IntentID: fixtureReplayIntent, CausationID: &self, ExecutionMode: intent.ModeReplay,
		}},
		{"has no identity of its own", intent.Instance{
			CausationID: &self, ExecutionMode: intent.ModeReplay,
		}},
		{"declares a mode that is not REPLAY", intent.Instance{
			IntentID: fixtureReplayIntent, CausationID: &self, ExecutionMode: intent.ModeExecute,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(Options{
				Plan: plan, Source: NewMemorySource(rec), Contract: replayContract(t),
				Definition: replayDefinition(), Instance: tc.inst,
			})
			if CodeOf(err) != CodeCausalSeparation {
				t.Fatalf("code = %q (%v), want %s", CodeOf(err), err, CodeCausalSeparation)
			}
		})
	}
}

// TestReplayReadsNoWallClock checks this package's own source for a wall-clock
// read. "The clock comes from the record" is the kind of property that is true
// on the day it is written and quietly false a refactor later, so it is
// checked rather than asserted in a comment.
func TestReplayReadsNoWallClock(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, forbidden := range []string{"time.Now(", "rand.", "os.Getenv("} {
			if bytes.Contains(b, []byte(forbidden)) {
				t.Fatalf("%s reads %s; a replay's clock, randomness and configuration come from the record", name, forbidden)
			}
		}
	}
}

// reachingAdapter is the SECURITY case's live adapter: one that would reach a
// real destination if it were ever invoked. It counts its invocations so the
// test can assert the number is zero.
type reachingAdapter struct {
	mu sync.Mutex
	n  int
}

var _ NodeAdapter = (*reachingAdapter)(nil)

func (a *reachingAdapter) Invoke(context.Context, string, EffectAttempt) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n++
	return nil
}

func (a *reachingAdapter) calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n
}
