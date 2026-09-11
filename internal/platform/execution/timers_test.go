package execution

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// timerFixtureDataset is the tzdb and business-calendar release the fixture
// wake condition is resolved against.
var timerFixtureDataset = values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"}

const (
	timerFixtureWorkflowID = "hcmnext.workflows.test.timer_factory"
	timerFixtureNodeID     = "wait_effective_date"
)

var timerFixtureFireAt = time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)

// waitNodeFixture is a compiled WAIT node with the whole dataset provenance of
// its wake instant declared on it, exactly as internal/workflow's compiler
// emits from a WaitSpec.
func waitNodeFixture() workflow.CompiledNode {
	return workflow.CompiledNode{
		ID: timerFixtureNodeID, Type: workflow.StepWait,
		Wait: &workflow.CompiledWait{
			WakeKind:              workflow.WaitWakeAtInstant,
			WakeInstant:           timerFixtureFireAt.Format(time.RFC3339Nano),
			ZoneID:                "America/New_York",
			ZoneTzdbVersion:       "2026a",
			CalendarRef:           "us-federal",
			CalendarVersion:       "2026.1",
			ReferenceUpdatePolicy: "PIN",
		},
	}
}

func timerRequestFixture(node workflow.CompiledNode) execute.TimerRequest {
	return execute.TimerRequest{
		Continuation: runtime.ContinuationRecord{
			TenantID: uuid.New(), InstanceID: uuid.New(), TargetNodeID: timerFixtureNodeID,
		},
		Plan:      &workflow.CompiledWorkflow{WorkflowID: timerFixtureWorkflowID, Version: 1},
		Node:      node,
		CreatedAt: time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC),
	}
}

// recordingExecutor is a database that accepts one insert and remembers its
// arguments. It is enough to prove what the adapter writes without a server:
// timer.Scheduler.Schedule's happy path is a single INSERT whose affected-row
// count decides the outcome.
type recordingExecutor struct {
	sql      string
	args     []any
	affected int64
	err      error
}

func (e *recordingExecutor) Exec(_ context.Context, sql string, args ...any) (int64, error) {
	e.sql, e.args = sql, args
	if e.err != nil {
		return 0, e.err
	}
	return e.affected, nil
}

func (e *recordingExecutor) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("recordingExecutor: no query expected")
}

func (e *recordingExecutor) QueryRow(context.Context, string, ...any) dbport.Row {
	return errRow{errors.New("recordingExecutor: no row query expected")}
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

var _ runtime.Executor = (*recordingExecutor)(nil)

func TestNewTimerFactoryRefusesAnUndeclaredDataset(t *testing.T) {
	if _, err := NewTimerFactory(TimerFactoryConfig{}); err == nil {
		t.Fatal("NewTimerFactory accepted a factory with no tzdb or calendar release")
	}
}

// TestTimerFactoryWritesThePromiseTheWakeConditionDescribes is the adapter's
// whole contract: the durable row's key is the wake requirement's content
// digest and its instant is the compiled wake instant, so the zone, tzdb
// release, business calendar and reference-update policy behind that instant
// are pinned by digest rather than copied into columns.
func TestTimerFactoryWritesThePromiseTheWakeConditionDescribes(t *testing.T) {
	factory, err := NewTimerFactory(TimerFactoryConfig{Dataset: timerFixtureDataset})
	if err != nil {
		t.Fatalf("NewTimerFactory: %v", err)
	}
	req := timerRequestFixture(waitNodeFixture())
	ex := &recordingExecutor{affected: 1}

	handle, err := factory.CreateTimer(context.Background(), ex, req)
	if err != nil {
		t.Fatalf("CreateTimer: %v", err)
	}
	if handle.NodeID != timerFixtureNodeID {
		t.Fatalf("handle names node %q, want %q", handle.NodeID, timerFixtureNodeID)
	}
	if !handle.FiresAt.Equal(timerFixtureFireAt) {
		t.Fatalf("handle fires at %s, want the compiled wake instant %s", handle.FiresAt, timerFixtureFireAt)
	}
	if handle.Key == "" {
		t.Fatal("handle carries no durable key")
	}
	if handle.Replay {
		t.Fatal("a first promise reported itself a replay")
	}
	want := timer.TimerID(req.Continuation.TenantID, req.Continuation.InstanceID, timerFixtureNodeID, handle.Key)
	if handle.TimerID != want {
		t.Fatalf("timer id = %s, want the id derived from the wake requirement digest %s", handle.TimerID, want)
	}
	if !strings.Contains(ex.sql, "INSERT INTO workflow_timer") {
		t.Fatalf("the adapter wrote %q, want an insert into workflow_timer", ex.sql)
	}
}

// TestTimerFactoryIsDeterministicForOneWakeRequirement is what makes a
// replayed advancement address the promise it already made rather than mint a
// second one: identical inputs derive the identical durable id and key.
func TestTimerFactoryIsDeterministicForOneWakeRequirement(t *testing.T) {
	factory, err := NewTimerFactory(TimerFactoryConfig{Dataset: timerFixtureDataset})
	if err != nil {
		t.Fatalf("NewTimerFactory: %v", err)
	}
	req := timerRequestFixture(waitNodeFixture())
	first, err := factory.CreateTimer(context.Background(), &recordingExecutor{affected: 1}, req)
	if err != nil {
		t.Fatalf("first CreateTimer: %v", err)
	}
	second, err := factory.CreateTimer(context.Background(), &recordingExecutor{affected: 1}, req)
	if err != nil {
		t.Fatalf("second CreateTimer: %v", err)
	}
	if first.TimerID != second.TimerID || first.Key != second.Key {
		t.Fatalf("two calls derived %s/%s and %s/%s; a replay must address one promise",
			first.TimerID, first.Key, second.TimerID, second.Key)
	}
}

// TestTimerFactoryRefusesWhatItCannotPromise covers every input the adapter
// must reject before it writes anything: a continuation with no pinned plan,
// a node that is not a WAIT, and a WAIT whose wake condition does not parse.
func TestTimerFactoryRefusesWhatItCannotPromise(t *testing.T) {
	factory, err := NewTimerFactory(TimerFactoryConfig{Dataset: timerFixtureDataset})
	if err != nil {
		t.Fatalf("NewTimerFactory: %v", err)
	}
	notAWait := waitNodeFixture()
	notAWait.Type = workflow.StepTransform

	noBinding := waitNodeFixture()
	noBinding.Wait = nil

	badInstant := waitNodeFixture()
	badInstant.Wait.WakeInstant = "the day after tomorrow"

	for name, mutate := range map[string]func(*execute.TimerRequest){
		"no pinned plan":          func(r *execute.TimerRequest) { r.Plan = nil },
		"node is not a WAIT":      func(r *execute.TimerRequest) { r.Node = notAWait },
		"node has no binding":     func(r *execute.TimerRequest) { r.Node = noBinding },
		"unparsable wake instant": func(r *execute.TimerRequest) { r.Node = badInstant },
	} {
		t.Run(name, func(t *testing.T) {
			req := timerRequestFixture(waitNodeFixture())
			mutate(&req)
			ex := &recordingExecutor{affected: 1}
			if _, err := factory.CreateTimer(context.Background(), ex, req); err == nil {
				t.Fatalf("CreateTimer accepted %s", name)
			}
			if ex.sql != "" {
				t.Fatalf("a refused promise still wrote %q", ex.sql)
			}
		})
	}
}

// TestTimerFactorySatisfiesTheDriverPort is the reason this adapter exists: it
// is the production value an execute.Options carries as Timers, so a shipped
// binary can park an instance on a durable timer.
func TestTimerFactorySatisfiesTheDriverPort(t *testing.T) {
	factory, err := NewTimerFactory(TimerFactoryConfig{Dataset: timerFixtureDataset})
	if err != nil {
		t.Fatalf("NewTimerFactory: %v", err)
	}
	// The factory is a struct value, so a runtime nil comparison could
	// never fail; the assignment itself is the port assertion.
	var _ execute.TimerFactory = factory
}

func TestWaitAttemptNamesTheContinuationActivation(t *testing.T) {
	if got := waitAttempt(2); got != 2 {
		t.Fatalf("wait attempt = %d, want 2", got)
	}
	if got := waitAttempt(0); got != 1 {
		t.Fatalf("untracked activation attempt = %d, want 1", got)
	}
}
