package execute

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// fakeCausalTimerReader is a TimerReader that always hands back the same
// drift-clean FIRED row, optionally carrying stored causal identity.
type fakeCausalTimerReader struct {
	row FiredTimer
	err error
}

func (f fakeCausalTimerReader) LoadTimer(context.Context, runtime.Executor, uuid.UUID, uuid.UUID) (FiredTimer, error) {
	return f.row, f.err
}

// recordingStarter implements Instrumentation plus the optional
// ResumeSpanStarter, recording every resume request and span ending.
type recordingStarter struct {
	NoopInstrumentation
	mu       sync.Mutex
	requests []ResumeSpanRequest
	endings  []spanEnding
}

type spanEnding struct {
	outcome string
	failed  bool
}

type recordingResumeSpan struct {
	rec *recordingStarter
}

func (s *recordingResumeSpan) End(outcome string, err error) {
	s.rec.mu.Lock()
	defer s.rec.mu.Unlock()
	s.rec.endings = append(s.rec.endings, spanEnding{outcome: outcome, failed: err != nil})
}

func (r *recordingStarter) StartResumeSpan(ctx context.Context, req ResumeSpanRequest) (context.Context, Span) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, req)
	return ctx, &recordingResumeSpan{rec: r}
}

func (r *recordingStarter) calls() []ResumeSpanRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ResumeSpanRequest(nil), r.requests...)
}

func (r *recordingStarter) ends() []spanEnding {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]spanEnding(nil), r.endings...)
}

var storedCausal = &runtime.CausalMetadata{
	CorrelationID: "corr-9", CausationID: "cause-9",
	LogicalOperationID: "logical-9", AttemptID: "attempt-9",
	TraceLink: &runtime.TraceLinkMetadata{
		TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", SpanID: "00f067aa0ba902b7",
		TraceFlags: 1, ExpiresAt: timerTestAt.Add(24 * time.Hour),
	},
}

func withCausal(row FiredTimer) FiredTimer {
	row.Causal = storedCausal
	return row
}

// resumeTimerDriver wires a Driver whose TimerReader serves row and whose
// instrumentation records resume spans, driving every timer resume through
// the injected Advance.
func resumeTimerDriver(reader TimerReader, starter *recordingStarter, advance AdvanceFunc) (*Driver, error) {
	return New(Options{
		DB:              oneBeginner{&memoryTx{}},
		Steps:           noStepRunner{},
		TimerReader:     reader,
		Instrumentation: starter,
		Advance:         advance,
	})
}

func completeAdvance(_ context.Context, _ runtime.Executor, in runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
	return runtime.AdvanceReceipt{
		TenantID: in.TenantID, InstanceID: in.InstanceID, NodeID: in.Outcome.NodeID,
		NewInstanceVersion: 4, Complete: true,
	}, nil
}

func timerResumeSelection(t *testing.T) (runtime.WorkflowSelection, version.CompiledVersion) {
	t.Helper()
	plan := waitFixturePlan(t)
	selection := runtime.WorkflowSelection{
		WorkflowID: "timer.resume.test", Pin: version.Pin{SemanticVersion: "1.0.0"}, Plan: plan,
	}
	record := version.CompiledVersion{
		WorkflowID: "timer.resume.test", SemanticVersion: "1.0.0",
		CompiledPlanDigest: plan.Digest(), Status: version.StatusActive,
	}
	return selection, record
}

func timerResumeRequest(selection runtime.WorkflowSelection, record version.CompiledVersion) ResumeTimerRequest {
	req := timerTestRequest()
	req.Start.Resolver = staticResolver{selection}
	req.Start.Versions = staticVersions{record}
	req.Start.StartIdempotencyKey = "start-timer-1"
	req.Start.CorrelationID = "corr-9"
	return req
}

// TestResumeTimerLinksAdvancementToStoredTimerCausal proves the OBS-013
// wiring: a fired timer carrying stored causal identity opens exactly one
// resume span addressed with that identity, and the advancement itself is
// unaffected by the linking.
func TestResumeTimerLinksAdvancementToStoredTimerCausal(t *testing.T) {
	selection, record := timerResumeSelection(t)
	starter := &recordingStarter{}
	driver, err := resumeTimerDriver(fakeCausalTimerReader{row: withCausal(firedRow())}, starter, completeAdvance)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result, err := driver.ResumeTimer(context.Background(), timerResumeRequest(selection, record))
	if err != nil {
		t.Fatalf("ResumeTimer: %v", err)
	}
	if result.Status != StatusComplete {
		t.Fatalf("result status = %v, want COMPLETE", result.Status)
	}
	calls := starter.calls()
	if len(calls) != 1 {
		t.Fatalf("resume spans opened = %d, want exactly 1", len(calls))
	}
	got := calls[0]
	if got.Causal == nil || got.Causal.CorrelationID != "corr-9" || got.Causal.LogicalOperationID != "logical-9" {
		t.Fatalf("resume span did not carry the stored causal identity: %+v", got.Causal)
	}
	if got.Causal.TraceLink == nil || got.Causal.TraceLink.TraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("resume span dropped the stored trace link: %+v", got.Causal)
	}
	if got.TimerID != timerTestID.String() || got.InstanceID != timerTestInstance.String() || got.NodeID != waitFixtureNode {
		t.Fatalf("resume span misaddressed: %+v", got)
	}
	ends := starter.ends()
	if len(ends) != 1 || ends[0].outcome != OutcomeSuccess || ends[0].failed {
		t.Fatalf("resume span endings = %+v, want one SUCCESS", ends)
	}
}

// TestResumeTimerWithoutStoredCausalAdvancesUnlinked proves the nil path:
// a timer stored before causal metadata existed opens no resume span, and
// its receipt matches the linked run exactly — linking never changes
// business behavior.
func TestResumeTimerWithoutStoredCausalAdvancesUnlinked(t *testing.T) {
	selection, record := timerResumeSelection(t)
	linkedStarter := &recordingStarter{}
	linkedDriver, err := resumeTimerDriver(fakeCausalTimerReader{row: withCausal(firedRow())}, linkedStarter, completeAdvance)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	linked, err := linkedDriver.ResumeTimer(context.Background(), timerResumeRequest(selection, record))
	if err != nil {
		t.Fatalf("linked ResumeTimer: %v", err)
	}
	if len(linkedStarter.calls()) != 1 {
		t.Fatal("linked run opened no resume span")
	}
	plainStarter := &recordingStarter{}
	plainDriver, err := resumeTimerDriver(fakeCausalTimerReader{row: firedRow()}, plainStarter, completeAdvance)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plain, err := plainDriver.ResumeTimer(context.Background(), timerResumeRequest(selection, record))
	if err != nil {
		t.Fatalf("unlinked ResumeTimer: %v", err)
	}
	if len(plainStarter.calls()) != 0 {
		t.Fatal("resume span opened with no stored causal identity")
	}
	// The receipt carries no span data, so equality here proves the
	// linking changed nothing observable about the business outcome.
	if !reflect.DeepEqual(linked, plain) {
		t.Fatalf("linked %+v differs from unlinked %+v: linking changed business behavior", linked, plain)
	}
}

// TestResumeSpanOutcomeMirrorsParkedAndFailedAdvancements proves the span
// ending tracks the advancement: PARKED when the advancement parks again,
// FAILURE with the error when the advancement errors.
func TestResumeSpanOutcomeMirrorsParkedAndFailedAdvancements(t *testing.T) {
	selection, record := timerResumeSelection(t)
	newDriver := func(starter *recordingStarter, adv AdvanceFunc) *Driver {
		driver, err := resumeTimerDriver(fakeCausalTimerReader{row: withCausal(firedRow())}, starter, adv)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return driver
	}
	parkedStarter := &recordingStarter{}
	parkedDriver := newDriver(parkedStarter, func(_ context.Context, _ runtime.Executor, in runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
		return runtime.AdvanceReceipt{
			TenantID: in.TenantID, InstanceID: in.InstanceID, NodeID: in.Outcome.NodeID,
			NewInstanceVersion: 4,
			Continuations:      []runtime.ContinuationRecord{{Kind: frontier.IntentTimerRequired}},
		}, nil
	})
	parked, err := parkedDriver.ResumeTimer(context.Background(), timerResumeRequest(selection, record))
	if err != nil {
		t.Fatalf("parking ResumeTimer: %v", err)
	}
	if parked.Status != StatusParked {
		t.Fatalf("status = %v, want PARKED", parked.Status)
	}
	ends := parkedStarter.ends()
	if len(ends) != 1 || ends[0].outcome != OutcomeParked || ends[0].failed {
		t.Fatalf("resume span endings = %+v, want one PARKED", ends)
	}
	boom := errors.New("advance exploded")
	failedStarter := &recordingStarter{}
	failedDriver := newDriver(failedStarter, func(context.Context, runtime.Executor, runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
		return runtime.AdvanceReceipt{}, boom
	})
	if _, err := failedDriver.ResumeTimer(context.Background(), timerResumeRequest(selection, record)); !errors.Is(err, boom) {
		t.Fatalf("ResumeTimer err = %v, want the advancement error", err)
	}
	ends = failedStarter.ends()
	if len(ends) != 1 || ends[0].outcome != OutcomeFailure || !ends[0].failed {
		t.Fatalf("resume span endings = %+v, want one failed FAILURE", ends)
	}
}

// failCommitTx is a memoryTx whose commit always fails, proving the
// resume span tracks failures even on paths whose error binds to a
// narrower scope than the advancement's own err.
type failCommitTx struct {
	*memoryTx
	err error
}

func (f failCommitTx) Commit(context.Context) error { return f.err }

type failBeginner struct {
	tx dbport.Tx
}

func (b failBeginner) Begin(context.Context) (dbport.Tx, error) { return b.tx, nil }

// TestResumeSpanMarksCommitFailure proves the deferred span ending sees
// past Go scoping: tx.Commit binds its error to the if statement, so the
// driver mirrors the failure onto the resume outcome explicitly.
func TestResumeSpanMarksCommitFailure(t *testing.T) {
	selection, record := timerResumeSelection(t)
	boom := errors.New("commit exploded")
	starter := &recordingStarter{}
	driver, err := New(Options{
		DB:              failBeginner{tx: failCommitTx{memoryTx: &memoryTx{}, err: boom}},
		Steps:           noStepRunner{},
		TimerReader:     fakeCausalTimerReader{row: withCausal(firedRow())},
		Instrumentation: starter,
		Advance:         completeAdvance,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := driver.ResumeTimer(context.Background(), timerResumeRequest(selection, record)); !errors.Is(err, boom) {
		t.Fatalf("ResumeTimer err = %v, want the commit error", err)
	}
	ends := starter.ends()
	if len(ends) != 1 || ends[0].outcome != OutcomeFailure || !ends[0].failed {
		t.Fatalf("resume span endings = %+v, want one failed FAILURE on commit error", ends)
	}
	if len(starter.calls()) != 1 {
		t.Fatal("resume span was never opened before the commit failed")
	}
}

// TestHumanWorkResumeOpensNoResumeSpan proves the scope boundary: a
// human-work Resume carries no stored causal identity, so it never opens a
// resume span however the instrumentation is configured.
func TestHumanWorkResumeOpensNoResumeSpan(t *testing.T) {
	fx := newResumeFixture()
	req := fx.req
	req.Start.Proposal.Revision.MaterialDigest.Digest = fx.item.ProposalRef
	starter := &recordingStarter{}
	driver, err := New(Options{
		DB: oneBeginner{&memoryTx{}}, Steps: noStepRunner{}, Items: fakeWorkItemReader{item: fx.item},
		Instrumentation: starter,
		Advance: func(_ context.Context, _ runtime.Executor, in runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
			return runtime.AdvanceReceipt{
				TenantID: in.TenantID, InstanceID: in.InstanceID, NodeID: in.Outcome.NodeID,
				NewInstanceVersion: 7, Complete: true,
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := driver.Resume(context.Background(), req); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(starter.calls()) != 0 {
		t.Fatal("human-work Resume opened a resume span with no stored causal identity")
	}
}
