package execute

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// WF-RUN-004's driver-facing half is a drift check and some wiring, and both
// are pure: no transaction, no clock, no database. These tests are therefore
// unit tests, and they use the repository's own compiled WAIT fixture rather
// than a hand-built plan so "is this node a WAIT" is answered by the real
// compiler.

const waitFixtureNode = "wait_for_effective_date"

func waitFixturePlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	def, err := workflow.LoadFile(filepath.Join("..", "testdata", "wait_signal_fixture.json"))
	if err != nil {
		t.Fatalf("load the WAIT/SIGNAL fixture definition: %v", err)
	}
	plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1B})
	if err != nil {
		t.Fatalf("compile the WAIT/SIGNAL fixture: %v", err)
	}
	return plan
}

var (
	timerTestTenant   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	timerTestInstance = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	timerTestID       = uuid.MustParse("33333333-3333-4333-8333-333333333333")
	timerTestAt       = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
)

func timerTestRequest() ResumeTimerRequest {
	return ResumeTimerRequest{
		Start:                   runtime.StartRequest{TenantID: timerTestTenant},
		InstanceID:              timerTestInstance,
		ExpectedInstanceVersion: 3,
		TimerID:                 timerTestID,
		Outcome:                 frontier.NodeOutcome{Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:wait"},
		RecordedAt:              timerTestAt,
	}
}

func firedRow() FiredTimer {
	return FiredTimer{
		TimerID: timerTestID, InstanceID: timerTestInstance, NodeID: waitFixtureNode,
		Key: "digest-of-the-wake-requirement", State: TimerStateFired, FiresAt: timerTestAt,
	}
}

func TestCheckTimerDrift_AcceptsASettledTimerAndTakesTheNodeFromTheRow(t *testing.T) {
	selection := runtime.WorkflowSelection{Plan: waitFixturePlan(t), WorkflowID: "wf"}
	req := timerTestRequest()

	outcome, refs, err := checkTimerDrift(req, selection, firedRow())
	if err != nil {
		t.Fatalf("a settled timer on a WAIT node was refused: %v", err)
	}
	if outcome.NodeID != waitFixtureNode {
		t.Fatalf("outcome node = %q, want the node the stored timer names (%q)", outcome.NodeID, waitFixtureNode)
	}
	if outcome.Outcome != workflow.OutcomeSucceeded || outcome.OutputDigest != "sha256:wait" {
		t.Fatalf("the caller's typed resolution was not carried through: %+v", outcome)
	}
	if refs.HumanTaskID != req.Refs.HumanTaskID {
		t.Fatalf("governance refs were rewritten: %+v", refs)
	}
}

// A TIMER_REVIEW_REQUIRED resolution is a failed attempt, not an outcome
// route, and it must still be accepted: it is how a WAIT node reaches its
// declared failure route for human review.
func TestCheckTimerDrift_AcceptsAFailedResolution(t *testing.T) {
	selection := runtime.WorkflowSelection{Plan: waitFixturePlan(t), WorkflowID: "wf"}
	req := timerTestRequest()
	req.Outcome = frontier.NodeOutcome{Failed: true, ErrorClass: "TIMER_REVIEW_REQUIRED"}

	outcome, _, err := checkTimerDrift(req, selection, firedRow())
	if err != nil {
		t.Fatalf("a failed WAIT resolution was refused: %v", err)
	}
	if !outcome.Failed || outcome.NodeID != waitFixtureNode {
		t.Fatalf("outcome = %+v, want a failed attempt on the stored timer's node", outcome)
	}
}

func TestCheckTimerDrift_RefusesEveryDisagreementWithTheStoredRow(t *testing.T) {
	selection := runtime.WorkflowSelection{Plan: waitFixturePlan(t), WorkflowID: "wf"}

	cases := map[string]struct {
		mutateReq func(*ResumeTimerRequest)
		mutateRow func(*FiredTimer)
		sentinel  error
	}{
		"a pending timer": {
			mutateRow: func(r *FiredTimer) { r.State = "PENDING" }, sentinel: ErrTimerDrift,
		},
		"a cancelled timer": {
			mutateRow: func(r *FiredTimer) { r.State = "CANCELLED" }, sentinel: ErrTimerDrift,
		},
		"a timer bound to another instance": {
			mutateRow: func(r *FiredTimer) { r.InstanceID = uuid.New() }, sentinel: ErrTimerDrift,
		},
		"a timer the reader substituted": {
			mutateRow: func(r *FiredTimer) { r.TimerID = uuid.New() }, sentinel: ErrTimerDrift,
		},
		"a node the plan does not declare": {
			mutateRow: func(r *FiredTimer) { r.NodeID = "no_such_node" }, sentinel: ErrTimerDrift,
		},
		"a node that is not a WAIT": {
			mutateRow: func(r *FiredTimer) { r.NodeID = "signal_ack_received" }, sentinel: ErrTimerDrift,
		},
		"an outcome naming another node": {
			mutateReq: func(q *ResumeTimerRequest) { q.Outcome.NodeID = "signal_ack_received" }, sentinel: ErrTimerDrift,
		},
		"a resolution still awaiting a timer": {
			mutateReq: func(q *ResumeTimerRequest) {
				q.Outcome = frontier.NodeOutcome{Await: frontier.AwaitTimer, AwaitRef: "digest"}
			},
			sentinel: ErrInvalidConfiguration,
		},
		"a resolution that neither completes nor fails": {
			mutateReq: func(q *ResumeTimerRequest) { q.Outcome = frontier.NodeOutcome{} },
			sentinel:  ErrInvalidConfiguration,
		},
	}
	for name, tc := range cases {
		req, row := timerTestRequest(), firedRow()
		if tc.mutateReq != nil {
			tc.mutateReq(&req)
		}
		if tc.mutateRow != nil {
			tc.mutateRow(&row)
		}
		_, _, err := checkTimerDrift(req, selection, row)
		if err == nil {
			t.Fatalf("%s was accepted", name)
		}
		if !errors.Is(err, tc.sentinel) {
			t.Fatalf("%s: err = %v, want %v", name, err, tc.sentinel)
		}
	}
}

func TestValidateResumeTimerConfig_RefusesIncompleteWiring(t *testing.T) {
	ctx := context.Background()
	var reader TimerReader = stubTimerReader{}

	if _, err := validateResumeTimerConfig(ctx, timerTestRequest(), nil); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("no TimerReader: err = %v, want ErrInvalidConfiguration", err)
	}
	for name, mutate := range map[string]func(*ResumeTimerRequest){
		"no tenant":            func(q *ResumeTimerRequest) { q.Start.TenantID = uuid.Nil },
		"no instance":          func(q *ResumeTimerRequest) { q.InstanceID = uuid.Nil },
		"no timer":             func(q *ResumeTimerRequest) { q.TimerID = uuid.Nil },
		"no expected version":  func(q *ResumeTimerRequest) { q.ExpectedInstanceVersion = 0 },
		"no workflow resolver": func(q *ResumeTimerRequest) { q.Start.Resolver = nil },
	} {
		req := timerTestRequest()
		mutate(&req)
		if _, err := validateResumeTimerConfig(ctx, req, reader); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("%s: err = %v, want ErrInvalidConfiguration", name, err)
		}
	}
}

type stubTimerReader struct{}

func (stubTimerReader) LoadTimer(context.Context, runtime.Executor, uuid.UUID, uuid.UUID) (FiredTimer, error) {
	return FiredTimer{}, errors.New("stub timer reader must not be called")
}

var _ TimerReader = stubTimerReader{}

// A fence without its verifier (or the other way round) is a half-configured
// safety property, which is worse than none: it would read as fenced and
// check nothing.
func TestNew_RefusesAFenceWithoutItsVerifier(t *testing.T) {
	fence := &runtime.Fence{
		ResourceKind: "WORKFLOW_INSTANCE", ResourceID: timerTestInstance.String(),
		LeaseID: uuid.New(), HolderID: "workload:w#replica:1", Token: 1,
	}
	base := Options{DB: stubBeginner{}, Steps: stubStepRunner{}}

	withFence := base
	withFence.Fence = fence
	if _, err := New(withFence); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("a fence with no verifier was accepted: %v", err)
	}

	withVerifier := base
	withVerifier.FenceVerifier = stubVerifier{}
	if _, err := New(withVerifier); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("a verifier with no fence was accepted: %v", err)
	}

	both := base
	both.Fence, both.FenceVerifier = fence, stubVerifier{}
	if _, err := New(both); err != nil {
		t.Fatalf("a fence and its verifier together were refused: %v", err)
	}
}

type stubBeginner struct{}

func (stubBeginner) Begin(context.Context) (dbport.Tx, error) {
	return nil, errors.New("stub beginner must not be called")
}

var _ Beginner = stubBeginner{}

type stubStepRunner struct{}

func (stubStepRunner) Run(context.Context, StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("stub step runner must not be called")
}

var _ StepRunner = stubStepRunner{}

type stubVerifier struct{}

func (stubVerifier) VerifyFence(context.Context, runtime.Executor, uuid.UUID, runtime.Fence) error {
	return nil
}

var _ runtime.FenceVerifier = stubVerifier{}
