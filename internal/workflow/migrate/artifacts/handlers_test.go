package artifacts

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/kernel/values"
	"github.com/monstercameron/hcm-next/internal/workflow/lease"
	"github.com/monstercameron/hcm-next/internal/workflow/steps/wait"
	"github.com/monstercameron/hcm-next/internal/workflow/timer"
)

func runHandler(t *testing.T, h Handler, s *scenario) ([]Entry, error) {
	t.Helper()
	return h.Migrate(context.Background(), nil, s.scope, s.ports.ports())
}

// --- lease ------------------------------------------------------------------

func TestLeaseHandler_CarriesTheHolderAndItsWindow(t *testing.T) {
	t.Parallel()
	s := newScenario(t)
	entries, err := runHandler(t, leaseHandler{}, s)
	if err != nil {
		t.Fatalf("lease handler: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("lease handler produced %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Disposition != Carried {
		t.Fatalf("lease disposition = %q, want %q", e.Disposition, Carried)
	}
	if e.Owner != s.holder.HolderID() {
		t.Fatalf("lease owner = %q, want %q", e.Owner, s.holder.HolderID())
	}
	if !e.Deadline.Equal(s.ports.lease.ExpiresAt) {
		t.Fatalf("lease deadline = %s, want %s", e.Deadline, s.ports.lease.ExpiresAt)
	}
	if e.FromRef != e.ToRef {
		t.Fatal("a carried lease reported two different references")
	}
}

func TestLeaseHandler_RefusesEveryStaleFence(t *testing.T) {
	t.Parallel()
	cases := map[string]func(s *scenario){
		"token behind the resource": func(s *scenario) { s.scope.Fence.Token = 6 },
		"token ahead of the resource": func(s *scenario) {
			s.scope.Fence.Token = 8
		},
		"a different lease id": func(s *scenario) { s.scope.Fence.LeaseID = uuid.New() },
		"a different holder": func(s *scenario) {
			s.scope.Fence.Holder = lease.Identity{WorkloadRef: "workload:other", InstanceRef: "replica-9"}
		},
		"a lapsed window": func(s *scenario) {
			s.ports.lease.ExpiresAt = s.scope.MigratedAt.Add(-time.Second)
		},
		"no fence against a live lease": func(s *scenario) { s.scope.Fence = lease.Fence{} },
		"a fence against no lease":      func(s *scenario) { s.ports.lease = nil },
		"a malformed fence": func(s *scenario) {
			s.scope.Fence.Holder = lease.Identity{WorkloadRef: "bare-hostname", InstanceRef: "r"}
		},
	}
	for name, break_ := range cases {
		s := newScenario(t)
		break_(s)
		_, err := runHandler(t, leaseHandler{}, s)
		if err == nil {
			t.Fatalf("%s was accepted", name)
		}
		if got := CodeOf(err); got != CodeStaleLease {
			t.Fatalf("%s refused with %q, want %q", name, got, CodeStaleLease)
		}
		if got := KindOf(err); got != KindLease {
			t.Fatalf("%s refused under kind %q, want %q", name, got, KindLease)
		}
	}
}

// An unheld instance migrated with no fence is the ordinary case for an
// operator-driven migration of a paused instance: nobody is working it.
func TestLeaseHandler_UnheldInstanceWithNoFenceCarriesNothing(t *testing.T) {
	t.Parallel()
	s := newScenario(t)
	s.ports.lease = nil
	s.scope.Fence = lease.Fence{}
	entries, err := runHandler(t, leaseHandler{}, s)
	if err != nil {
		t.Fatalf("lease handler: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("an unheld instance produced %d lease entries, want 0", len(entries))
	}
}

// --- timers -----------------------------------------------------------------

func TestTimerHandler_RekeysWithoutMovingTheWakeInstant(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	before := s.ports.timers
	oldID := timer.TimerID(s.tenant, s.instance, fromNode, sourceRequirementDigest)
	if _, ok := before[oldID]; !ok {
		t.Fatal("fixture did not seed the source timer")
	}

	entries, err := runHandler(t, timerHandler{}, s)
	if err != nil {
		t.Fatalf("timer handler: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("timer handler produced %d entries, want 1", len(entries))
	}
	e := entries[0]
	if e.Disposition != Rekeyed {
		t.Fatalf("timer disposition = %q, want %q", e.Disposition, Rekeyed)
	}
	if !e.Deadline.Equal(wakeInstant) {
		t.Fatalf("re-keying moved the wake instant to %s, want %s", e.Deadline, wakeInstant)
	}
	wantID := timer.TimerID(s.tenant, s.instance, toNode, targetRequirementDigest)
	if e.ToRef != wantID.String() {
		t.Fatalf("re-keyed timer id = %s, want the derived %s", e.ToRef, wantID)
	}
	if !s.ports.settled["TIMER:"+oldID.String()] {
		t.Fatal("the predecessor promise was left standing beside the re-keyed one")
	}
	next, ok := s.ports.timers[wantID]
	if !ok {
		t.Fatal("no timer was written under the new epoch")
	}
	if !next.FiresAt.Equal(wakeInstant) || next.NodeID != toNode || next.Key != targetRequirementDigest {
		t.Fatalf("re-keyed timer = %+v", next)
	}
}

func TestTimerHandler_RefusesARequirementThatMovesTheInstant(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	s.scope.Requirements[sourceRequirementDigest] = wait.TimerRequirement{
		Digest: targetRequirementDigest, FireAt: values.NewInstant(wakeInstant.Add(time.Minute)),
	}
	_, err := runHandler(t, timerHandler{}, s)
	if got := CodeOf(err); got != CodeWakeInstantMoved {
		t.Fatalf("refused with %q, want %q (err %v)", got, CodeWakeInstantMoved, err)
	}
}

func TestTimerHandler_RefusesARelocationWithNoReplacementRequirement(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	s.scope.Requirements = nil
	_, err := runHandler(t, timerHandler{}, s)
	if got := CodeOf(err); got != CodeNotRelocatable {
		t.Fatalf("refused with %q, want %q (err %v)", got, CodeNotRelocatable, err)
	}
}

func TestTimerHandler_RefusesARequirementWithNoDigest(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	s.scope.Requirements[sourceRequirementDigest] = wait.TimerRequirement{FireAt: values.NewInstant(wakeInstant)}
	_, err := runHandler(t, timerHandler{}, s)
	if got := CodeOf(err); got != CodeInvalidRequest {
		t.Fatalf("refused with %q, want %q (err %v)", got, CodeInvalidRequest, err)
	}
}

// A timer raised on a node the migration is not moving off is untouched,
// whatever the requirement map happens to say about it.
func TestTimerHandler_CarriesTimersOnOtherNodes(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	otherID := timer.TimerID(s.tenant, s.instance, "simulate_comp", "3333")
	s.ports.timers[otherID] = TimerRow{
		TimerID: otherID, NodeID: "simulate_comp", Key: "3333", Kind: "DEADLINE",
		FiresAt: wakeInstant.Add(time.Hour), Version: 1,
	}
	entries, err := runHandler(t, timerHandler{}, s)
	if err != nil {
		t.Fatalf("timer handler: %v", err)
	}
	carried := 0
	for _, e := range entries {
		if e.Disposition == Carried {
			carried++
			if e.FromRef != otherID.String() {
				t.Fatalf("the carried entry is not the off-frontier timer: %+v", e)
			}
		}
	}
	if carried != 1 {
		t.Fatalf("%d carried timers, want 1", carried)
	}
	if _, ok := s.ports.timers[otherID]; !ok {
		t.Fatal("an off-frontier timer was retired")
	}
}

func TestTimerHandler_SameNodeMigrationCarriesEverything(t *testing.T) {
	t.Parallel()
	s := newScenario(t)
	entries, err := runHandler(t, timerHandler{}, s)
	if err != nil {
		t.Fatalf("timer handler: %v", err)
	}
	for _, e := range entries {
		if e.Disposition != Carried {
			t.Fatalf("a same-node migration produced %q for %s", e.Disposition, e.Identity)
		}
	}
	if len(s.ports.settled) != 0 {
		t.Fatalf("a same-node migration retired %v", s.ports.settled)
	}
}

// --- signal subscriptions ---------------------------------------------------

func TestSignalHandler_RekeepsTheSignalAndCorrelationKey(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	oldID := SubscriptionID(s.tenant, s.instance, fromNode, "promotion.finance_ack")

	entries, err := runHandler(t, signalHandler{}, s)
	if err != nil {
		t.Fatalf("signal handler: %v", err)
	}
	if len(entries) != 1 || entries[0].Disposition != Rekeyed {
		t.Fatalf("signal entries = %+v", entries)
	}
	if entries[0].Identity != "promotion.finance_ack/employment:jane-doe-9001" {
		t.Fatalf("the subscription's semantic identity changed: %q", entries[0].Identity)
	}
	wantID := SubscriptionID(s.tenant, s.instance, toNode, "promotion.finance_ack")
	next, ok := s.ports.subs[wantID]
	if !ok {
		t.Fatal("no subscription was opened under the new epoch")
	}
	if next.SignalName != "promotion.finance_ack" || next.CorrelationKey != "employment:jane-doe-9001" {
		t.Fatalf("re-keyed subscription = %+v", next)
	}
	if !s.ports.settled["SIGNAL:"+oldID.String()] {
		t.Fatal("the predecessor subscription was left open beside the re-keyed one")
	}
}

// --- ready work -------------------------------------------------------------

func TestReadyWorkHandler_PreservesEligibilityAndPriority(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	s.scope.To.Attempt = 2
	oldID := timer.ReadyWorkID(s.tenant, s.instance, fromNode, 1)

	entries, err := runHandler(t, readyWorkHandler{}, s)
	if err != nil {
		t.Fatalf("ready-work handler: %v", err)
	}
	if len(entries) != 1 || entries[0].Disposition != Rekeyed {
		t.Fatalf("ready-work entries = %+v", entries)
	}
	if !entries[0].Deadline.Equal(wakeInstant) {
		t.Fatalf("re-keying moved the eligibility instant to %s", entries[0].Deadline)
	}
	wantID := timer.ReadyWorkID(s.tenant, s.instance, toNode, 2)
	next, ok := s.ports.ready[wantID]
	if !ok {
		t.Fatal("no ready work was enqueued under the new epoch")
	}
	if next.Priority != 100 || !next.EligibleAt.Equal(wakeInstant) || next.Attempt != 2 {
		t.Fatalf("re-keyed ready work = %+v", next)
	}
	if !s.ports.settled["READY_WORK:"+oldID.String()] {
		t.Fatal("the predecessor ready work was left standing beside the re-keyed one")
	}
}

// --- approvals --------------------------------------------------------------

func TestApprovalHandler_CarriesOwnerAndDeadline(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	entries, err := runHandler(t, approvalHandler{}, s)
	if err != nil {
		t.Fatalf("approval handler: %v", err)
	}
	if len(entries) != 1 || entries[0].Disposition != Carried {
		t.Fatalf("approval entries = %+v", entries)
	}
	want := s.ports.approvals[0]
	if entries[0].Owner != want.Owner {
		t.Fatalf("approval owner = %q, want %q", entries[0].Owner, want.Owner)
	}
	if !entries[0].Deadline.Equal(want.DeadlineAt) {
		t.Fatalf("approval deadline = %s, want %s", entries[0].Deadline, want.DeadlineAt)
	}
	if entries[0].FromRef != entries[0].ToRef {
		t.Fatal("a carried approval reported two different work-item identities")
	}
}

func TestApprovalHandler_RefusesToRelocateARoutedApproval(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	s.ports.approvals[0].NodeID = fromNode
	_, err := runHandler(t, approvalHandler{}, s)
	if got := CodeOf(err); got != CodeNotRelocatable {
		t.Fatalf("refused with %q, want %q (err %v)", got, CodeNotRelocatable, err)
	}
	if got := KindOf(err); got != KindApproval {
		t.Fatalf("refused under kind %q, want %q", got, KindApproval)
	}
}

// --- child continuations ----------------------------------------------------

func TestChildHandler_RecordsExactlyOneContinuationHoweverManyChildren(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	for i := 2; i <= 5; i++ {
		s.ports.children = append(s.ports.children, ChildRow{
			Child: uuid.New(), ParentNodeID: "simulate_comp", Ordinal: i, Mode: "AWAIT",
		})
	}
	entries, err := runHandler(t, childHandler{}, s)
	if err != nil {
		t.Fatalf("child handler: %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("%d entries for five children plus the awaiting record, want 6", len(entries))
	}
	if total := totalContinuations(s); total != 1 {
		t.Fatalf("five awaited children recorded %d continuations, want exactly 1", total)
	}
	awaiting := entryFor(t, Receipt{Entries: entries}, KindChildContinuation, "awaiting")
	if awaiting.Disposition != Rekeyed || awaiting.ToRef != toNode {
		t.Fatalf("awaiting entry = %+v", awaiting)
	}
}

func TestChildHandler_ReplayRecordsNoSecondContinuation(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	for i := 0; i < 3; i++ {
		if _, err := runHandler(t, childHandler{}, s); err != nil {
			t.Fatalf("child handler run %d: %v", i, err)
		}
	}
	if total := totalContinuations(s); total != 3 {
		t.Fatalf("the double counted %d inserts, want 3 -- the identity, not the count, is what dedupes", total)
	}
	if len(s.ports.continuations) != 1 {
		t.Fatalf("three runs produced %d distinct continuation identities, want 1: %v",
			len(s.ports.continuations), s.ports.continuations)
	}
}

func TestChildHandler_RefusesToRelocateAnAwaitedChild(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	s.ports.children[0].ParentNodeID = fromNode
	_, err := runHandler(t, childHandler{}, s)
	if got := CodeOf(err); got != CodeNotRelocatable {
		t.Fatalf("refused with %q, want %q (err %v)", got, CodeNotRelocatable, err)
	}
	if got := KindOf(err); got != KindChildContinuation {
		t.Fatalf("refused under kind %q, want %q", got, KindChildContinuation)
	}
}

// A DETACH child reports back nowhere, so it never binds the parent's node
// and never produces an awaiting record.
func TestChildHandler_DetachedChildIsCarriedAcrossARelocation(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	s.ports.children = []ChildRow{{Child: uuid.New(), ParentNodeID: fromNode, Ordinal: 1, Mode: "DETACH"}}
	entries, err := runHandler(t, childHandler{}, s)
	if err != nil {
		t.Fatalf("child handler: %v", err)
	}
	if len(entries) != 1 || entries[0].Disposition != Carried {
		t.Fatalf("entries = %+v", entries)
	}
	if totalContinuations(s) != 0 {
		t.Fatal("a detached child produced an awaiting continuation")
	}
}

func totalContinuations(s *scenario) int {
	total := 0
	for _, n := range s.ports.continuations {
		total += n
	}
	return total
}
