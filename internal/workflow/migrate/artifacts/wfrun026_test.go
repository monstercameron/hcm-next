package artifacts

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// PRIMARY. WF-RUN-026's whole claim in one case: migrating a live instance
// onto a new epoch preserves the semantic identity, the owner and the
// deadline of every pending artifact it holds -- the timer wake, the signal
// subscription, the ready work, the approval and the child continuation --
// with the old artifact retired rather than left standing beside the new one,
// and with exactly one continuation recorded.
func TestWorkflowMigrationPreservesPendingTimerSignalWorkAndContinuationIdentity(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	receipt := s.mustRun(t)

	// Every declared kind is accounted for. An artifact that no handler
	// reported is an artifact the migration lost.
	for _, kind := range Kinds() {
		if len(receipt.OfKind(kind)) == 0 {
			t.Fatalf("the receipt says nothing about %s; the instance holds one", kind)
		}
	}

	// The timer wake: re-keyed by the target plan's requirement digest, at
	// exactly the instant already promised, on the new frontier node.
	timerEntry := entryFor(t, receipt, KindTimer, "DELAY@"+instantText(wakeInstant))
	if timerEntry.Disposition != Rekeyed {
		t.Fatalf("timer disposition = %q, want %q", timerEntry.Disposition, Rekeyed)
	}
	if !timerEntry.Deadline.Equal(wakeInstant) {
		t.Fatalf("the wake instant moved to %s", timerEntry.Deadline)
	}
	newTimerID := timer.TimerID(s.tenant, s.instance, toNode, targetRequirementDigest)
	if timerEntry.ToRef != newTimerID.String() {
		t.Fatalf("timer re-keyed to %s, want the derived %s", timerEntry.ToRef, newTimerID)
	}
	if got := s.ports.timers[newTimerID]; got.Key != targetRequirementDigest || !got.FiresAt.Equal(wakeInstant) {
		t.Fatalf("stored re-keyed timer = %+v", got)
	}
	oldTimerID := timer.TimerID(s.tenant, s.instance, fromNode, sourceRequirementDigest)
	if !s.ports.settled["TIMER:"+oldTimerID.String()] {
		t.Fatal("the old wake promise is still on the table beside the new one")
	}

	// The signal subscription: same signal, same correlation key, new node.
	sigEntry := entryFor(t, receipt, KindSignal, "promotion.finance_ack/employment:jane-doe-9001")
	if sigEntry.Disposition != Rekeyed {
		t.Fatalf("subscription disposition = %q", sigEntry.Disposition)
	}
	newSubID := SubscriptionID(s.tenant, s.instance, toNode, "promotion.finance_ack")
	if got := s.ports.subs[newSubID]; got.CorrelationKey != "employment:jane-doe-9001" {
		t.Fatalf("stored re-keyed subscription = %+v", got)
	}

	// The ready work: same eligibility instant, same priority.
	readyEntry := entryFor(t, receipt, KindReadyWork, "eligible@"+instantText(wakeInstant))
	if readyEntry.Disposition != Rekeyed || !readyEntry.Deadline.Equal(wakeInstant) {
		t.Fatalf("ready-work entry = %+v", readyEntry)
	}
	newReadyID := timer.ReadyWorkID(s.tenant, s.instance, toNode, 1)
	if got := s.ports.ready[newReadyID]; got.Priority != 100 || !got.EligibleAt.Equal(wakeInstant) {
		t.Fatalf("stored re-keyed ready work = %+v", got)
	}

	// The approval: identity, owner and deadline untouched.
	approval := s.ports.approvals[0]
	approvalEntry := entryFor(t, receipt, KindApproval, "work_item:"+approval.WorkItemID.String())
	if approvalEntry.Disposition != Carried {
		t.Fatalf("approval disposition = %q, want %q", approvalEntry.Disposition, Carried)
	}
	if approvalEntry.Owner != approval.Owner || !approvalEntry.Deadline.Equal(approval.DeadlineAt) {
		t.Fatalf("approval owner/deadline moved: %+v", approvalEntry)
	}

	// The child continuation: the child carried, and exactly one continuation
	// recorded under the new epoch.
	child := s.ports.children[0]
	entryFor(t, receipt, KindChildContinuation, "child:"+child.Child.String())
	awaiting := entryFor(t, receipt, KindChildContinuation, "awaiting")
	if awaiting.ToRef != toNode {
		t.Fatalf("the awaiting continuation names %q, want the new frontier node %q", awaiting.ToRef, toNode)
	}
	if total := totalContinuations(s); total != 1 {
		t.Fatalf("%d continuations recorded, want exactly 1", total)
	}

	// Ownership was verified, and carried rather than replaced.
	leaseEntry := entryFor(t, receipt, KindLease, "instance:"+s.instance.String())
	if leaseEntry.Owner != s.holder.HolderID() || leaseEntry.Disposition != Carried {
		t.Fatalf("lease entry = %+v", leaseEntry)
	}
}

// PROPERTY. Over randomized artifact sets: nothing is lost, nothing is
// duplicated, no deadline moves, and the receipt is canonically ordered and
// deterministically digested.
func TestTodo_WF_RUN_026_Property(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(20260905))
	nodes := []string{fromNode, "simulate_comp", "evaluate_band"}

	for i := 0; i < 200; i++ {
		s := newScenario(t).relocate()
		s.ports.timers = map[uuid.UUID]TimerRow{}
		s.ports.subs = map[uuid.UUID]SubscriptionRow{}
		s.ports.ready = map[uuid.UUID]ReadyRow{}
		s.ports.approvals = nil
		s.ports.children = nil

		wantTimers := map[string]time.Time{}
		for n := 0; n < rng.Intn(4); n++ {
			node := nodes[rng.Intn(len(nodes))]
			fires := wakeInstant.Add(time.Duration(rng.Intn(1000)) * time.Minute)
			key := fmt.Sprintf("%064d", rng.Int63())
			id := timer.TimerID(s.tenant, s.instance, node, key)
			s.ports.timers[id] = TimerRow{
				TimerID: id, NodeID: node, Key: key, Kind: "DELAY", FiresAt: fires, Version: 1,
			}
			if node == fromNode {
				s.scope.Requirements[key] = wait.TimerRequirement{
					Digest: fmt.Sprintf("%064d", rng.Int63()), FireAt: values.NewInstant(fires),
				}
			}
			wantTimers["DELAY@"+instantText(fires)] = fires
		}
		for n := 0; n < rng.Intn(3); n++ {
			node := nodes[rng.Intn(len(nodes))]
			name := fmt.Sprintf("signal.%d", n)
			id := SubscriptionID(s.tenant, s.instance, node, name)
			s.ports.subs[id] = SubscriptionRow{
				SubscriptionID: id, NodeID: node, SignalName: name,
				CorrelationKey: "corr", Version: 1,
			}
		}

		inputTimers := len(s.ports.timers)
		inputSubs := len(s.ports.subs)
		receipt := s.mustRun(t)

		// One entry per input artifact: nothing dropped, nothing invented.
		if got := len(receipt.OfKind(KindTimer)); got != inputTimers {
			t.Fatalf("iteration %d: %d timers in, %d timer entries out", i, inputTimers, got)
		}
		if got := len(receipt.OfKind(KindSignal)); got != inputSubs {
			t.Fatalf("iteration %d: %d subscriptions in, %d entries out", i, inputSubs, got)
		}
		// The number of live timers never grows: a re-key retires its
		// predecessor in the same breath.
		if got := len(s.ports.timers); got != inputTimers {
			t.Fatalf("iteration %d: %d timers before, %d after", i, inputTimers, got)
		}
		if got := len(s.ports.subs); got != inputSubs {
			t.Fatalf("iteration %d: %d subscriptions before, %d after", i, inputSubs, got)
		}
		// No deadline moved.
		for _, e := range receipt.OfKind(KindTimer) {
			want, ok := wantTimers[e.Identity]
			if !ok {
				t.Fatalf("iteration %d: receipt invented timer identity %q", i, e.Identity)
			}
			if !e.Deadline.Equal(want) {
				t.Fatalf("iteration %d: %q moved from %s to %s", i, e.Identity, want, e.Deadline)
			}
		}
		// Canonical order, and a digest that is a pure function of the receipt.
		last := -1
		for _, e := range receipt.Entries {
			if e.Kind.order() < last {
				t.Fatalf("iteration %d: entries out of handler order: %v", i, identities(receipt))
			}
			last = e.Kind.order()
		}
		if computeReceiptDigest(receipt) != receipt.Digest() {
			t.Fatalf("iteration %d: the receipt digest is not a function of the receipt", i)
		}
	}
}

// GOLDEN. A fixed instance with fixed identities produces a byte-stable
// receipt. The pinned digest is what makes a silent change to the contract --
// a renamed disposition, a reordered handler, a dropped field -- fail here
// rather than in production.
func TestTodo_WF_RUN_026_Golden(t *testing.T) {
	t.Parallel()
	s := goldenScenario(t)
	receipt := s.mustRun(t)

	wantEntries := []string{
		"LEASE/instance:33333333-3333-3333-3333-333333333333=CARRIED",
		"TIMER/DELAY@2026-10-01T09:30:00Z=REKEYED",
		"SIGNAL_SUBSCRIPTION/promotion.finance_ack/employment:jane-doe-9001=REKEYED",
		"READY_WORK/eligible@2026-10-01T09:30:00Z=REKEYED",
		"APPROVAL/work_item:44444444-4444-4444-4444-444444444444=CARRIED",
		"CHILD_CONTINUATION/awaiting=REKEYED",
		"CHILD_CONTINUATION/child:55555555-5555-5555-5555-555555555555=CARRIED",
	}
	got := make([]string, 0, len(receipt.Entries))
	for _, e := range receipt.Entries {
		got = append(got, string(e.Kind)+"/"+e.Identity+"="+string(e.Disposition))
	}
	if len(got) != len(wantEntries) {
		t.Fatalf("receipt entries =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(wantEntries, "\n"))
	}
	for i := range got {
		if got[i] != wantEntries[i] {
			t.Fatalf("entry %d = %q, want %q", i, got[i], wantEntries[i])
		}
	}
	if receipt.Digest() != goldenReceiptDigest {
		t.Fatalf("receipt digest = %q, want the pinned %q", receipt.Digest(), goldenReceiptDigest)
	}
}

// goldenReceiptDigest is the pinned content identity of the golden receipt.
// It moves only when the contract moves, which is exactly when a reader
// should be made to look.
const goldenReceiptDigest = "f247c6b7eb3cdf3b33b60afd839537306fd3db8dfa765e062d8246cfbff74b54"

// goldenScenario is newScenario with every identity pinned, so the receipt it
// produces is byte-stable across runs and machines.
func goldenScenario(t *testing.T) *scenario {
	t.Helper()
	s := newScenario(t)
	s.tenant = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	s.instance = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	leaseID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	s.ports.lease = &LeaseRow{
		LeaseID: leaseID, Holder: s.holder.HolderID(), FenceToken: 7,
		ExpiresAt: fixedInstant.Add(time.Hour),
	}
	s.ports.timers = map[uuid.UUID]TimerRow{}
	timerID := timer.TimerID(s.tenant, s.instance, fromNode, sourceRequirementDigest)
	s.ports.timers[timerID] = TimerRow{
		TimerID: timerID, NodeID: fromNode, Key: sourceRequirementDigest,
		Kind: "DELAY", FiresAt: wakeInstant, Version: 1,
	}
	s.ports.subs = map[uuid.UUID]SubscriptionRow{}
	subID := SubscriptionID(s.tenant, s.instance, fromNode, "promotion.finance_ack")
	s.ports.subs[subID] = SubscriptionRow{
		SubscriptionID: subID, NodeID: fromNode, SignalName: "promotion.finance_ack",
		CorrelationKey: "employment:jane-doe-9001", Version: 1,
	}
	s.ports.ready = map[uuid.UUID]ReadyRow{}
	readyID := timer.ReadyWorkID(s.tenant, s.instance, fromNode, 1)
	s.ports.ready[readyID] = ReadyRow{
		ReadyWorkID: readyID, NodeID: fromNode, Attempt: 1, Priority: 100,
		State: "READY", EligibleAt: wakeInstant, Version: 1,
	}
	s.ports.approvals = []ApprovalRow{{
		WorkItemID: uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		NodeID:     "end_requires_finance_approval",
		Owner:      "PRINCIPAL:principal:finance-partner-1", Status: "ASSIGNED",
		DeadlineAt: fixedInstant.Add(48 * time.Hour),
	}}
	s.ports.children = []ChildRow{{
		Child:        uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		ParentNodeID: "simulate_comp", Ordinal: 1, Mode: "AWAIT",
	}}

	s.scope.TenantID, s.scope.InstanceID = s.tenant, s.instance
	s.scope.Fence = lease.Fence{
		TenantID: s.tenant,
		Resource: lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: s.instance.String()},
		LeaseID:  leaseID, Holder: s.holder, Token: 7,
	}
	return s.relocate()
}

// RACE. Many callers running the same migration against one store must not
// produce two of anything. Each caller either wins the re-key or discovers
// the promise is already there; whichever way, one timer stands at the end
// and the predecessor is gone.
func TestTodo_WF_RUN_026_Race(t *testing.T) {
	t.Parallel()
	s := newScenario(t).relocate()
	oldTimerID := timer.TimerID(s.tenant, s.instance, fromNode, sourceRequirementDigest)
	newTimerID := timer.TimerID(s.tenant, s.instance, toNode, targetRequirementDigest)

	const callers = 8
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		succeeded int
		codes     []string
	)
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			_, err := s.run(t)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				succeeded++
				return
			}
			codes = append(codes, CodeOf(err))
		}()
	}
	wg.Wait()

	if succeeded == 0 {
		t.Fatalf("every concurrent caller failed: %v", codes)
	}
	for _, code := range codes {
		if code != CodeStorageFailed {
			t.Fatalf("a concurrent caller failed with %q; the only legal loss is a storage conflict", code)
		}
	}
	if _, ok := s.ports.timers[newTimerID]; !ok {
		t.Fatal("no timer stands under the new epoch after the race")
	}
	if _, ok := s.ports.timers[oldTimerID]; ok {
		t.Fatal("the predecessor timer survived the race")
	}
	if len(s.ports.timers) != 1 {
		t.Fatalf("%d timers stand after the race, want 1: %v", len(s.ports.timers), s.ports.timers)
	}
	if len(s.ports.continuations) != 1 {
		t.Fatalf("the race produced %d distinct continuation identities, want 1", len(s.ports.continuations))
	}
}

// INTEGRATION. The durable ports, against a real paused Promotion instance
// holding a real timer and a real approval work item, inside one transaction.
func TestTodo_WF_RUN_026_Integration(t *testing.T) {
	t.Parallel()
	env := newPromotionEnvironment(t)
	ctx := context.Background()

	var receipt Receipt
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		var err error
		receipt, err = Migrate(ctx, tx, Request{Scope: env.scope(), Ports: DefaultPorts()})
		return err
	})

	newTimerID := timer.TimerID(env.tenant, env.instance, workflow.PromotionNodeRaiseThreshold, targetRequirementDigest)
	newSubID := SubscriptionID(env.tenant, env.instance, workflow.PromotionNodeRaiseThreshold, "promotion.finance_ack")
	newReadyID := timer.ReadyWorkID(env.tenant, env.instance, workflow.PromotionNodeRaiseThreshold, 1)

	// The re-keyed timer promises the identical instant on the new node.
	var (
		firesAt time.Time
		nodeID  string
		state   string
	)
	env.db.QueryRow(ctx, `SELECT fires_at, node_id, timer_state FROM workflow_timer
		WHERE tenant_id = $1 AND timer_id = $2`, env.tenant, newTimerID).Scan(&firesAt, &nodeID, &state)
	if !firesAt.UTC().Equal(wakeInstant) || nodeID != workflow.PromotionNodeRaiseThreshold || state != runtimestate.TimerPending {
		t.Fatalf("re-keyed timer row = %s / %s / %s", firesAt.UTC(), nodeID, state)
	}
	// The predecessor is cancelled, not deleted: history is not rewritten.
	env.db.QueryRow(ctx, `SELECT timer_state FROM workflow_timer
		WHERE tenant_id = $1 AND timer_id = $2`, env.tenant, env.timerID).Scan(&state)
	if state != runtimestate.TimerCancelled {
		t.Fatalf("the predecessor timer is %s, want %s", state, runtimestate.TimerCancelled)
	}
	if n := env.countRows(t, `SELECT count(*) FROM workflow_timer
		WHERE tenant_id = $1 AND instance_id = $2 AND timer_state = 'PENDING'`,
		env.tenant, env.instance); n != 1 {
		t.Fatalf("%d pending timers stand after the migration, want 1", n)
	}

	// The re-keyed subscription keeps signal and correlation key; the old one
	// is closed.
	var signalName, correlation, subState string
	env.db.QueryRow(ctx, `SELECT signal_name, correlation_key, subscription_state
		FROM workflow_signal_subscription WHERE tenant_id = $1 AND subscription_id = $2`,
		env.tenant, newSubID).Scan(&signalName, &correlation, &subState)
	if signalName != "promotion.finance_ack" || correlation != "employment:jane-doe-9001" || subState != runtimestate.SubscriptionOpen {
		t.Fatalf("re-keyed subscription = %s / %s / %s", signalName, correlation, subState)
	}
	env.db.QueryRow(ctx, `SELECT subscription_state FROM workflow_signal_subscription
		WHERE tenant_id = $1 AND subscription_id = $2`, env.tenant, env.subID).Scan(&subState)
	if subState != runtimestate.SubscriptionCancelled {
		t.Fatalf("the predecessor subscription is %s, want %s", subState, runtimestate.SubscriptionCancelled)
	}

	// The re-keyed ready work keeps its eligibility instant.
	var eligible time.Time
	var priority int
	env.db.QueryRow(ctx, `SELECT eligible_at, priority FROM workflow_ready_work
		WHERE tenant_id = $1 AND ready_work_id = $2`, env.tenant, newReadyID).Scan(&eligible, &priority)
	if !eligible.UTC().Equal(wakeInstant) || priority != 100 {
		t.Fatalf("re-keyed ready work = %s / priority %d", eligible.UTC(), priority)
	}

	// The approval is untouched: same row, same owner, same deadline.
	var status, ownerRef string
	var deadline time.Time
	env.db.QueryRow(ctx, `SELECT status, owner_ref, deadline_at FROM work_item
		WHERE tenant_id = $1 AND work_item_id = $2`, env.tenant, env.workID).Scan(&status, &ownerRef, &deadline)
	if status != string(workitem.StatusCreated) || !deadline.UTC().Equal(fixedInstant.Add(48*time.Hour)) {
		t.Fatalf("the approval moved: %s / %s", status, deadline.UTC())
	}
	approvalEntry := entryFor(t, receipt, KindApproval, "work_item:"+env.workID.String())
	if approvalEntry.Owner != "POLICY_ROUTE:"+ownerRef {
		t.Fatalf("receipt owner %q does not match the stored owner %q", approvalEntry.Owner, ownerRef)
	}

	// Exactly one continuation ledger row, whatever a replay does.
	if n := env.countRows(t, `SELECT count(*) FROM workflow_continuation
		WHERE tenant_id = $1 AND instance_id = $2`, env.tenant, env.instance); n != 1 {
		t.Fatalf("%d continuation rows, want exactly 1", n)
	}
}

// FAULT. A failure injected between two handlers leaves a half-migrated
// transaction the caller must roll back -- and rolling it back leaves the
// instance exactly as it was, with every promise it started with.
func TestTodo_WF_RUN_026_Fault(t *testing.T) {
	t.Parallel()
	env := newPromotionEnvironment(t)
	ctx := context.Background()

	barrier := &stopAt{kind: KindSignal}
	err := inTenantTxErr(env.conn, env.tenant, func(tx dbport.Tx) error {
		_, err := Migrate(ctx, tx, Request{Scope: env.scope(), Ports: DefaultPorts(), Barrier: barrier})
		return err
	})
	if CodeOf(err) != CodeBarrierFailed {
		t.Fatalf("refused with %q, want %q (err %v)", CodeOf(err), CodeBarrierFailed, err)
	}
	if KindOf(err) != KindSignal {
		t.Fatalf("refused under kind %q, want %q", KindOf(err), KindSignal)
	}

	// The timer handler had already re-keyed before the barrier fired. Because
	// the transaction rolled back, none of it stands.
	newTimerID := timer.TimerID(env.tenant, env.instance, workflow.PromotionNodeRaiseThreshold, targetRequirementDigest)
	if n := env.countRows(t, `SELECT count(*) FROM workflow_timer WHERE tenant_id = $1 AND timer_id = $2`,
		env.tenant, newTimerID); n != 0 {
		t.Fatal("a re-keyed timer survived a rolled-back migration")
	}
	var state string
	env.db.QueryRow(ctx, `SELECT timer_state FROM workflow_timer WHERE tenant_id = $1 AND timer_id = $2`,
		env.tenant, env.timerID).Scan(&state)
	if state != runtimestate.TimerPending {
		t.Fatalf("the original promise is %s after a rolled-back migration, want PENDING", state)
	}
	if n := env.countRows(t, `SELECT count(*) FROM workflow_continuation WHERE tenant_id = $1 AND instance_id = $2`,
		env.tenant, env.instance); n != 0 {
		t.Fatalf("%d continuations survived a rolled-back migration, want zero", n)
	}

	// The instance is still PAUSED on its old version: runnable, not stranded.
	var status string
	env.db.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`,
		env.tenant, env.instance).Scan(&status)
	if status != string(runtime.InstancePaused) {
		t.Fatalf("instance is %s after a rolled-back migration, want PAUSED", status)
	}
}

// SECURITY. Ownership is the gate. Every way a fence can fail to name the
// live lease refuses the whole run before any artifact is touched, and a
// stale lease is never carried onto the new epoch.
func TestTodo_WF_RUN_026_Security(t *testing.T) {
	t.Parallel()
	cases := map[string]func(s *scenario){
		"a superseded fence token": func(s *scenario) { s.scope.Fence.Token = 6 },
		"a forged holder": func(s *scenario) {
			s.scope.Fence.Holder = lease.Identity{WorkloadRef: "workload:attacker", InstanceRef: "r"}
		},
		"a forged lease id":                 func(s *scenario) { s.scope.Fence.LeaseID = uuid.New() },
		"a window that has already closed":  func(s *scenario) { s.ports.lease.ExpiresAt = fixedInstant },
		"no fence at all against a holder":  func(s *scenario) { s.scope.Fence = lease.Fence{} },
		"a fence against a released lease":  func(s *scenario) { s.ports.lease = nil },
		"a fence for another tenant":        func(s *scenario) { s.scope.Fence.TenantID = uuid.New() },
		"a fence naming another instance's": func(s *scenario) { s.scope.Fence.Resource.ID = uuid.NewString() },
	}
	for name, attack := range cases {
		s := newScenario(t).relocate()
		attack(s)
		_, err := s.run(t)
		if err == nil {
			t.Fatalf("%s was accepted", name)
		}
		if got := CodeOf(err); got != CodeStaleLease {
			// A fence for another tenant or resource is malformed against this
			// instance rather than stale in the token sense; both are refused
			// under the lease handler, and neither may proceed.
			t.Fatalf("%s refused with %q, want %q (err %v)", name, got, CodeStaleLease, err)
		}
		for _, call := range s.ports.Calls() {
			if call != "leases.Current" {
				t.Fatalf("%s touched %s before ownership was established", name, call)
			}
		}
	}
}

// CONFORMANCE. The WF-RUN-000 gate blocks scheduler code, so this package
// must contain no clock read, no ticker, no sleep and no goroutine of its
// own; and every declared kind must have exactly one contract and one
// handler, in one order.
func TestTodo_WF_RUN_026_Conformance(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}
	banned := map[string]bool{"Now": true, "Sleep": true, "Tick": true, "NewTicker": true, "NewTimer": true, "After": true}
	fset := token.NewFileSet()
	scanned := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, perr := parser.ParseFile(fset, name, nil, 0)
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		scanned++
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.GoStmt:
				t.Fatalf("%s starts a goroutine; this package runs nothing of its own", name)
			case *ast.SelectorExpr:
				ident, ok := node.X.(*ast.Ident)
				if ok && ident.Name == "time" && banned[node.Sel.Name] {
					t.Fatalf("%s calls time.%s; every instant is the caller's", name, node.Sel.Name)
				}
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("scanned no package source files; the conformance check proved nothing")
	}

	for _, kind := range Kinds() {
		if _, ok := ContractFor(kind); !ok {
			t.Fatalf("%s has no declared contract", kind)
		}
		found := false
		for _, h := range Handlers() {
			if h.Kind() == kind {
				if found {
					t.Fatalf("%s has two handlers", kind)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("%s has no handler", kind)
		}
	}
}

// RECOVERY. Two paths out of a failed migration, both of which leave zero
// duplicate continuation: re-running a migration whose artifacts are already
// under the new epoch deduplicates rather than doubling, and an instance that
// cannot be rolled back is durably marked REPAIR_REQUIRED.
func TestTodo_WF_RUN_026_Recovery(t *testing.T) {
	t.Parallel()
	env := newPromotionEnvironment(t)
	ctx := context.Background()

	// First run commits.
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		_, err := Migrate(ctx, tx, Request{Scope: env.scope(), Ports: DefaultPorts()})
		return err
	})

	// A replay of the same migration finds its own artifacts already there.
	// The source rows are settled now, so the second run has nothing left to
	// re-key -- and, crucially, records no second continuation.
	var replay Receipt
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		var err error
		replay, err = Migrate(ctx, tx, Request{Scope: env.scope(), Ports: DefaultPorts()})
		return err
	})
	if replay.Count(Rekeyed) != 1 {
		t.Fatalf("a replay re-keyed %d artifacts; only the awaiting continuation should restate itself",
			replay.Count(Rekeyed))
	}
	if n := env.countRows(t, `SELECT count(*) FROM workflow_continuation
		WHERE tenant_id = $1 AND instance_id = $2`, env.tenant, env.instance); n != 1 {
		t.Fatalf("%d continuation rows after a replay, want exactly 1", n)
	}
	if n := env.countRows(t, `SELECT count(*) FROM workflow_timer
		WHERE tenant_id = $1 AND instance_id = $2`, env.tenant, env.instance); n != 2 {
		t.Fatalf("%d timer rows after a replay, want 2 (the cancelled predecessor and its successor)", n)
	}

	// The other path: mark the instance REPAIR_REQUIRED durably.
	var record RepairRecord
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		var err error
		record, err = MarkRepairRequired(ctx, tx, RepairRequest{
			TenantID: env.tenant, InstanceID: env.instance,
			Reason: CodeNotRelocatable, Detail: "the approval could not follow the frontier",
			RecordedBy: "principal:migration-operator", RecordedAt: fixedInstant,
		})
		return err
	})
	if record.From != runtime.InstancePaused {
		t.Fatalf("the repair record says the instance was %s, want PAUSED", record.From)
	}
	if len(record.Digest()) != 64 {
		t.Fatalf("repair record digest = %q", record.Digest())
	}
	var status string
	env.db.QueryRow(ctx, `SELECT runtime_status FROM workflow_instance WHERE tenant_id = $1 AND instance_id = $2`,
		env.tenant, env.instance).Scan(&status)
	if status != string(runtime.InstanceRepairRequired) {
		t.Fatalf("instance is %s, want REPAIR_REQUIRED", status)
	}

	// Marking an already-marked instance again is idempotent: the state
	// machine treats a re-record of the same status as a no-op, so a retried
	// repair does not fail and does not invent a new history.
	inTenantTx(t, env.conn, env.tenant, func(tx dbport.Tx) error {
		again, err := MarkRepairRequired(ctx, tx, RepairRequest{
			TenantID: env.tenant, InstanceID: env.instance,
			Reason: CodeNotRelocatable, RecordedBy: "principal:migration-operator", RecordedAt: fixedInstant,
		})
		if err != nil {
			return err
		}
		if len(again.Path) != 1 || again.Path[0] != runtime.InstanceRepairRequired {
			t.Fatalf("a repeated repair walked %v, want a single idempotent step", again.Path)
		}
		return nil
	})
}

// MODEL_BASED. A small model of the artifact set drives random migration
// sequences against the store: after every step, the set of live timers the
// model predicts -- one per semantic identity, on the epoch's current node,
// at the instant it always promised -- must be exactly what the store holds.
func TestTodo_WF_RUN_026_ModelBased(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(9052026))
	nodes := []string{"n_a", "n_b", "n_c", "n_d"}

	for run := 0; run < 40; run++ {
		s := newScenario(t)
		s.ports.subs = map[uuid.UUID]SubscriptionRow{}
		s.ports.ready = map[uuid.UUID]ReadyRow{}
		s.ports.approvals = nil
		s.ports.children = nil
		s.ports.timers = map[uuid.UUID]TimerRow{}

		// model: semantic identity -> the node and instant it should be on.
		type modelTimer struct {
			node  string
			key   string
			fires time.Time
		}
		model := map[string]*modelTimer{}
		node := nodes[0]
		s.scope.From.NodeID, s.scope.To.NodeID = node, node

		for i := 0; i < 1+rng.Intn(3); i++ {
			fires := wakeInstant.Add(time.Duration(i+1) * time.Hour)
			key := fmt.Sprintf("%064d", rng.Int63())
			id := timer.TimerID(s.tenant, s.instance, node, key)
			s.ports.timers[id] = TimerRow{
				TimerID: id, NodeID: node, Key: key, Kind: "DELAY", FiresAt: fires, Version: 1,
			}
			model["DELAY@"+instantText(fires)] = &modelTimer{node: node, key: key, fires: fires}
		}

		for step := 0; step < 6; step++ {
			next := nodes[rng.Intn(len(nodes))]
			s.scope.From.NodeID = node
			s.scope.To.NodeID = next
			s.scope.From.CompiledPlanDigest = fmt.Sprintf("plan-%d", step)
			s.scope.To.CompiledPlanDigest = fmt.Sprintf("plan-%d", step+1)
			s.scope.Requirements = map[string]wait.TimerRequirement{}
			for _, m := range model {
				if m.node != node {
					continue
				}
				s.scope.Requirements[m.key] = wait.TimerRequirement{
					Digest: fmt.Sprintf("%064d", rng.Int63()), FireAt: values.NewInstant(m.fires),
				}
			}

			receipt, err := s.run(t)
			if err != nil {
				t.Fatalf("run %d step %d: %v", run, step, err)
			}

			// Apply the same step to the model.
			for _, m := range model {
				if m.node != node {
					continue
				}
				m.key = s.scope.Requirements[m.key].Digest
				m.node = next
			}
			node = next

			// The store must hold exactly what the model predicts.
			if len(s.ports.timers) != len(model) {
				t.Fatalf("run %d step %d: store holds %d timers, model holds %d",
					run, step, len(s.ports.timers), len(model))
			}
			for identity, m := range model {
				want := timer.TimerID(s.tenant, s.instance, m.node, m.key)
				got, ok := s.ports.timers[want]
				if !ok {
					t.Fatalf("run %d step %d: %s is not at its derived identity %s",
						run, step, identity, want)
				}
				if got.NodeID != m.node || !got.FiresAt.Equal(m.fires) {
					t.Fatalf("run %d step %d: %s = %+v, model says node %s at %s",
						run, step, identity, got, m.node, m.fires)
				}
			}
			if len(receipt.OfKind(KindTimer)) != len(model) {
				t.Fatalf("run %d step %d: receipt reported %d timers, model holds %d",
					run, step, len(receipt.OfKind(KindTimer)), len(model))
			}
		}
	}
}

// MUTATION. Each guard this package relies on is removed one at a time -- by
// presenting exactly the input it exists to catch -- and every one must
// refuse. A guard that does not bite here is a guard that is not there.
func TestTodo_WF_RUN_026_Mutation(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		mutate func(s *scenario)
		want   string
	}{
		"a wake requirement that reschedules the promise": {
			mutate: func(s *scenario) {
				s.scope.Requirements[sourceRequirementDigest] = wait.TimerRequirement{
					Digest: targetRequirementDigest, FireAt: values.NewInstant(wakeInstant.Add(time.Nanosecond)),
				}
			},
			want: CodeWakeInstantMoved,
		},
		"a wake requirement that resolved to nothing": {
			mutate: func(s *scenario) {
				s.scope.Requirements[sourceRequirementDigest] = wait.TimerRequirement{Digest: targetRequirementDigest}
			},
			want: CodeWakeInstantMoved,
		},
		"a requirement no compiler produced": {
			mutate: func(s *scenario) {
				s.scope.Requirements[sourceRequirementDigest] = wait.TimerRequirement{
					FireAt: values.NewInstant(wakeInstant),
				}
			},
			want: CodeInvalidRequest,
		},
		"a relocation with no replacement requirement": {
			mutate: func(s *scenario) { s.scope.Requirements = nil },
			want:   CodeNotRelocatable,
		},
		"an approval dragged off its node": {
			mutate: func(s *scenario) { s.ports.approvals[0].NodeID = fromNode },
			want:   CodeNotRelocatable,
		},
		"an awaited child dragged off its node": {
			mutate: func(s *scenario) { s.ports.children[0].ParentNodeID = fromNode },
			want:   CodeNotRelocatable,
		},
		"a migration to the epoch it is already on": {
			mutate: func(s *scenario) { s.scope.To = s.scope.From },
			want:   CodeInvalidRequest,
		},
		"a stale fence": {
			mutate: func(s *scenario) { s.scope.Fence.Token = 1 },
			want:   CodeStaleLease,
		},
	}
	for name, tc := range cases {
		s := newScenario(t).relocate()
		tc.mutate(s)
		_, err := s.run(t)
		if err == nil {
			t.Fatalf("%s was accepted", name)
		}
		if got := CodeOf(err); got != tc.want {
			t.Fatalf("%s refused with %q, want %q (err %v)", name, got, tc.want, err)
		}
	}

	// And the guards that are not refusals: a same-node migration writes
	// nothing at all, which is what makes "carried" a claim rather than a
	// label.
	s := newScenario(t)
	if _, err := s.run(t); err != nil {
		t.Fatalf("a same-node migration: %v", err)
	}
	for _, call := range s.ports.Calls() {
		if strings.Contains(call, "Schedule") || strings.Contains(call, "Subscribe") ||
			strings.Contains(call, "Enqueue") || strings.Contains(call, "Cancel") ||
			strings.Contains(call, "Close") {
			t.Fatalf("a same-node migration issued %s", call)
		}
	}
	if !errors.Is(refuse(CodeStaleLease, KindLease, "", "x"), ErrArtifacts) {
		t.Fatal("refusals stopped unwrapping to the package sentinel")
	}
}
