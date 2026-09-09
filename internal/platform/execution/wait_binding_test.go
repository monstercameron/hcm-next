package execution

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func effectiveDateWaitNode() workflow.CompiledNode {
	return workflow.CompiledNode{
		ID:   "wait_effective_date",
		Type: workflow.StepWait,
		Wait: &workflow.CompiledWait{
			WakeKind: workflow.WaitWakeAtLocalDate, WakeLocalDate: WakeFromProposalEffectiveDate,
			ZoneID: "America/New_York", ZoneTzdbVersion: "2026a",
			CalendarRef: "us-federal", CalendarVersion: "2026.1", ReferenceUpdatePolicy: "PIN",
		},
	}
}

func proposalEffectiveAt(t *testing.T, at time.Time) runtime.ProposalBinding {
	t.Helper()
	interval, err := values.NewOpenInstantInterval(values.NewInstant(at))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	return runtime.ProposalBinding{Revision: intent.ProposalRevision{EffectiveTime: interval}}
}

// A WAIT node that wakes on the proposal's effective date is bound from the
// proposal binding the driver presents: the durable, digest-bound fact every
// Start and Resume carries, so the promise never depends on process memory.
func TestBindWaitNodeTakesTheEffectiveDateFromTheProposalBinding(t *testing.T) {
	t.Parallel()
	factory, err := NewTimerFactory(TimerFactoryConfig{Dataset: values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"}})
	if err != nil {
		t.Fatalf("NewTimerFactory: %v", err)
	}
	at := time.Date(2026, 12, 1, 5, 0, 0, 0, time.UTC)
	bound, err := factory.bindWaitNode(execute.TimerRequest{Node: effectiveDateWaitNode(), Proposal: proposalEffectiveAt(t, at)})
	if err != nil {
		t.Fatalf("bindWaitNode: %v", err)
	}
	if bound.Wait.WakeLocalDate != "2026-12-01" {
		t.Fatalf("wake local date = %q, want 2026-12-01", bound.Wait.WakeLocalDate)
	}
	if bound.Wait.ZoneID != "America/New_York" || bound.Wait.CalendarRef != "us-federal" {
		t.Fatalf("binding altered the declared zone or calendar: %+v", bound.Wait)
	}
}

// The composition-local map is only a fallback for a caller with no
// proposal; the proposal outranks it, and a placeholder neither can fill is
// refused with the node named rather than parsed as a date.
func TestBindWaitNodePrefersTheProposalAndRefusesAnUnboundPlaceholder(t *testing.T) {
	t.Parallel()
	dates := &sync.Map{}
	instance := uuid.New()
	fallbackDate, err := values.NewLocalDate(2027, time.January, 15)
	if err != nil {
		t.Fatalf("NewLocalDate: %v", err)
	}
	dates.Store(instance.String(), fallbackDate)
	factory, err := NewTimerFactory(TimerFactoryConfig{
		Dataset: values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"}, EffectiveDates: dates,
	})
	if err != nil {
		t.Fatalf("NewTimerFactory: %v", err)
	}
	continuation := runtime.ContinuationRecord{InstanceID: instance}

	fromMap, err := factory.bindWaitNode(execute.TimerRequest{Node: effectiveDateWaitNode(), Continuation: continuation})
	if err != nil {
		t.Fatalf("fallback bindWaitNode: %v", err)
	}
	if fromMap.Wait.WakeLocalDate != "2027-01-15" {
		t.Fatalf("fallback wake local date = %q, want 2027-01-15", fromMap.Wait.WakeLocalDate)
	}

	fromProposal, err := factory.bindWaitNode(execute.TimerRequest{
		Node: effectiveDateWaitNode(), Continuation: continuation,
		Proposal: proposalEffectiveAt(t, time.Date(2026, 12, 1, 5, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("proposal bindWaitNode: %v", err)
	}
	if fromProposal.Wait.WakeLocalDate != "2026-12-01" {
		t.Fatalf("the map outranked the proposal: %q", fromProposal.Wait.WakeLocalDate)
	}

	bare, err := NewTimerFactory(TimerFactoryConfig{Dataset: values.DatasetVersions{TzdbVersion: "2026a", CalendarVersion: "2026.1"}})
	if err != nil {
		t.Fatalf("NewTimerFactory: %v", err)
	}
	if _, err := bare.bindWaitNode(execute.TimerRequest{Node: effectiveDateWaitNode()}); err == nil {
		t.Fatal("an unbound placeholder was accepted")
	}

	literal := effectiveDateWaitNode()
	literal.Wait.WakeLocalDate = "2026-10-01"
	same, err := bare.bindWaitNode(execute.TimerRequest{Node: literal})
	if err != nil || same.Wait.WakeLocalDate != "2026-10-01" {
		t.Fatalf("a literal wake date was altered: %+v, err=%v", same.Wait, err)
	}
}
