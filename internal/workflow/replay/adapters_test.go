package replay

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"
)

// TestRecorder_AdmitsOnlyGovernedReads is the recorder's whole job: reads are
// answered, everything else is refused and recorded.
func TestRecorder_AdmitsOnlyGovernedReads(t *testing.T) {
	r := NewRecorder(minimalRecord())
	ctx := context.Background()

	if err := r.Attempt(ctx, "a", AttemptGovernedRead); err != nil {
		t.Fatalf("a governed read was refused: %v", err)
	}
	if len(r.Refusals()) != 0 {
		t.Fatalf("a governed read was recorded as a refusal")
	}

	for _, attempt := range []EffectAttempt{
		AttemptDomainWrite, AttemptExternalCall, AttemptMessageSend, AttemptApprovalConsumption,
	} {
		err := r.Attempt(ctx, "a", attempt)
		if CodeOf(err) != CodeEffectForbidden {
			t.Fatalf("%s: code = %q (%v)", attempt, CodeOf(err), err)
		}
		if NodeOf(err) != "a" {
			t.Fatalf("%s: refusal names %q, want the node", attempt, NodeOf(err))
		}
	}
	refusals := r.Refusals()
	if len(refusals) != 4 {
		t.Fatalf("recorded %d refusals, want 4", len(refusals))
	}
	if refusals[0].Attempt != AttemptDomainWrite || refusals[3].Attempt != AttemptApprovalConsumption {
		t.Fatalf("refusals are not in attempt order: %+v", refusals)
	}

	// The returned slice is a copy: a caller cannot edit the evidence.
	refusals[0].NodeID = "edited"
	if r.Refusals()[0].NodeID != "a" {
		t.Fatalf("Refusals handed out its own backing array")
	}
}

// TestEffectAttempts_IsTheClosedVocabulary pins the five attempts, so a new
// one cannot be added without a test that says what refuses it.
func TestEffectAttempts_IsTheClosedVocabulary(t *testing.T) {
	want := []EffectAttempt{
		AttemptGovernedRead, AttemptDomainWrite, AttemptExternalCall,
		AttemptMessageSend, AttemptApprovalConsumption,
	}
	got := EffectAttempts()
	if len(got) != len(want) {
		t.Fatalf("EffectAttempts returned %d, want %d", len(got), len(want))
	}
	seen := map[EffectAttempt]bool{}
	for i, a := range got {
		if a != want[i] {
			t.Fatalf("attempt %d = %s, want %s", i, a, want[i])
		}
		if a == "" || seen[a] {
			t.Fatalf("attempt %d is empty or duplicated", i)
		}
		seen[a] = true
	}
}

// TestRecorder_AnswersOnlyFromTheRecord covers the three accessors that stand
// in for the world: the output, the clock and randomness. Each refuses rather
// than inventing a value.
func TestRecorder_AnswersOnlyFromTheRecord(t *testing.T) {
	rec := minimalRecord()
	rec.Nodes[0].OutputDigest = "sha256:a"
	rec.Nodes[0].RandomDraws = []string{"one", "two"}
	rec.Signals = []SignalReceipt{{SignalID: "s1", NodeID: "a"}}
	rec.Timers = []TimerSettlement{{TimerID: "t1", NodeID: "a"}}
	r := NewRecorder(rec)

	out, err := r.Output("a", 1)
	if err != nil || out.OutputDigest != "sha256:a" {
		t.Fatalf("Output = %+v err %v", out, err)
	}
	if _, err := r.Output("a", 2); CodeOf(err) != CodeArtifactUnavailable {
		t.Fatalf("an unrecorded attempt was answered: %v", err)
	}
	if _, err := r.Output("absent", 1); NodeOf(err) != "absent" {
		t.Fatalf("refusal names %q", NodeOf(err))
	}

	now, err := r.Now("a", 1)
	if err != nil || !now.Equal(rec.Nodes[0].RecordedAt.UTC()) {
		t.Fatalf("Now = %s err %v, want the recorded instant", now, err)
	}
	if _, err := r.Now("absent", 1); CodeOf(err) != CodeArtifactUnavailable {
		t.Fatalf("an unrecorded clock was answered: %v", err)
	}
	blank := minimalRecord()
	blank.Nodes[0].RecordedAt = time.Time{}
	if _, err := NewRecorder(blank).Now("a", 1); CodeOf(err) != CodeArtifactUnavailable {
		t.Fatalf("a record with no instant produced one: %v", err)
	}

	for _, want := range []string{"one", "two"} {
		got, err := r.Draw("a", 1)
		if err != nil || got != want {
			t.Fatalf("Draw = %q err %v, want %q", got, err, want)
		}
	}
	if _, err := r.Draw("a", 1); CodeOf(err) != CodeArtifactUnavailable {
		t.Fatalf("a third draw was invented: %v", err)
	}

	if _, err := r.Signal("a", "s1"); err != nil {
		t.Fatalf("recorded signal refused: %v", err)
	}
	if _, err := r.Signal("a", "absent"); CodeOf(err) != CodeArtifactUnavailable {
		t.Fatalf("an unrecorded signal was answered: %v", err)
	}
	if _, err := r.Timer("a", "t1"); err != nil {
		t.Fatalf("recorded timer refused: %v", err)
	}
	if _, err := r.Timer("a", "absent"); CodeOf(err) != CodeArtifactUnavailable {
		t.Fatalf("an unrecorded timer was answered: %v", err)
	}
}

// TestRecorder_IsSafeForConcurrentUse drives one recorder from many goroutines,
// which is what a caller wiring several nodes' adapters behind it needs.
func TestRecorder_IsSafeForConcurrentUse(t *testing.T) {
	r := NewRecorder(minimalRecord())
	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_ = r.Attempt(context.Background(), "node-"+strconv.Itoa(i), AttemptExternalCall)
			_, _ = r.Output("a", 1)
		}(i)
	}
	wg.Wait()
	if len(r.Refusals()) != n {
		t.Fatalf("recorded %d refusals, want %d", len(r.Refusals()), n)
	}
}

// TestGuardedAdapter_NeverDelegates is the structural half of the security
// property: the wrapped adapter is unreachable, refusal or not.
func TestGuardedAdapter_NeverDelegates(t *testing.T) {
	inner := &reachingAdapter{}
	g := guardedAdapter{recorder: NewRecorder(minimalRecord()), inner: inner}
	ctx := context.Background()

	if err := g.Invoke(ctx, "a", AttemptGovernedRead); err != nil {
		t.Fatalf("a governed read was refused: %v", err)
	}
	if err := g.Invoke(ctx, "a", AttemptExternalCall); CodeOf(err) != CodeEffectForbidden {
		t.Fatalf("code = %q (%v)", CodeOf(err), err)
	}
	if inner.calls() != 0 {
		t.Fatalf("the guard delegated %d times", inner.calls())
	}
}

// TestItoa covers the local formatter the draw key and the execution index are
// built from.
func TestItoa(t *testing.T) {
	for _, i := range []int{0, 1, 9, 10, 42, 1234567890, -1, -42} {
		if got, want := itoa(i), strconv.Itoa(i); got != want {
			t.Fatalf("itoa(%d) = %q, want %q", i, got, want)
		}
	}
}
