package artifacts

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/wait"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

// fixedInstant is the clock every fixture stamps. Nothing in this package
// reads a wall clock, so a test that wants a time has to say which one.
var fixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

// wakeInstant is the instant every fixture timer promises. It is deliberately
// far from fixedInstant so a test can tell a preserved deadline from a
// rescheduled one at a glance.
var wakeInstant = time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)

const (
	fromNode = "build_proposal"
	toNode   = "raise_threshold"
)

// --- the in-memory double every fast case drives ----------------------------

// memoryPorts is a whole [Ports] set over maps. It exists because every
// property this ticket is about -- what is carried, what is re-keyed, what is
// deduplicated, what refuses, and what a partial failure leaves behind -- is a
// property of the handlers rather than of PostgreSQL, and proving it a
// thousand times over in a property or model-based case should not need a
// thousand transactions. The pgtest cases prove the durable ports agree.
//
// It is safe for concurrent use so the race case can hammer it.
type memoryPorts struct {
	mu sync.Mutex

	lease *LeaseRow

	timers    map[uuid.UUID]TimerRow
	subs      map[uuid.UUID]SubscriptionRow
	ready     map[uuid.UUID]ReadyRow
	approvals []ApprovalRow
	children  []ChildRow

	// settled records every predecessor artifact this package retired, keyed
	// "kind:id", so a test can prove the old promise was not left standing
	// beside the new one.
	settled map[string]bool
	// continuations counts inserts by derived identity, which is how the
	// "zero duplicate continuation" claim is checked without a database.
	continuations map[string]int

	// fail maps a "port.method" name onto the error that call returns. It is
	// the fault seam for the storage-failure cases.
	fail map[string]error
	// calls records every mutating call in order.
	calls []string
}

func newMemoryPorts() *memoryPorts {
	return &memoryPorts{
		timers: map[uuid.UUID]TimerRow{}, subs: map[uuid.UUID]SubscriptionRow{},
		ready: map[uuid.UUID]ReadyRow{}, settled: map[string]bool{},
		continuations: map[string]int{}, fail: map[string]error{},
	}
}

func (m *memoryPorts) ports() Ports {
	return Ports{
		Leases: m, Timers: m, Signals: m, Children: m, Continuations: m,
		ReadyWork: readyPort{m: m},
		Approvals: approvalPort{m: m},
	}
}

func (m *memoryPorts) record(name string) error {
	m.calls = append(m.calls, name)
	return m.fail[name]
}

// Calls returns every mutating call this double saw, in order.
func (m *memoryPorts) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

func (m *memoryPorts) Current(_ context.Context, _ Executor, _, _ uuid.UUID) (LeaseRow, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("leases.Current"); err != nil {
		return LeaseRow{}, false, err
	}
	if m.lease == nil {
		return LeaseRow{}, false, nil
	}
	return *m.lease, true, nil
}

func (m *memoryPorts) Pending(_ context.Context, _ Executor, _, _ uuid.UUID) ([]TimerRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("timers.Pending"); err != nil {
		return nil, err
	}
	out := make([]TimerRow, 0, len(m.timers))
	for _, row := range m.timers {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TimerID.String() < out[j].TimerID.String() })
	return out, nil
}

func (m *memoryPorts) Schedule(_ context.Context, _ Executor, _, _ uuid.UUID, row TimerRow, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("timers.Schedule"); err != nil {
		return false, err
	}
	if _, exists := m.timers[row.TimerID]; exists {
		return true, nil
	}
	row.Version = 1
	m.timers[row.TimerID] = row
	return false, nil
}

func (m *memoryPorts) Cancel(_ context.Context, _ Executor, _, timerID uuid.UUID, version uint64, _ time.Time, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("timers.Cancel"); err != nil {
		return err
	}
	row, ok := m.timers[timerID]
	if !ok {
		return fmt.Errorf("no such timer %s", timerID)
	}
	if row.Version != version {
		return fmt.Errorf("timer %s expected version %d, stored %d", timerID, version, row.Version)
	}
	delete(m.timers, timerID)
	m.settled["TIMER:"+timerID.String()] = true
	return nil
}

func (m *memoryPorts) Open(_ context.Context, _ Executor, _, _ uuid.UUID) ([]SubscriptionRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("signals.Open"); err != nil {
		return nil, err
	}
	out := make([]SubscriptionRow, 0, len(m.subs))
	for _, row := range m.subs {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SubscriptionID.String() < out[j].SubscriptionID.String() })
	return out, nil
}

func (m *memoryPorts) Subscribe(_ context.Context, _ Executor, _, _ uuid.UUID, row SubscriptionRow, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("signals.Subscribe"); err != nil {
		return false, err
	}
	if _, exists := m.subs[row.SubscriptionID]; exists {
		return true, nil
	}
	row.Version = 1
	m.subs[row.SubscriptionID] = row
	return false, nil
}

func (m *memoryPorts) Close(_ context.Context, _ Executor, _, subscriptionID uuid.UUID, version uint64, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("signals.Close"); err != nil {
		return err
	}
	row, ok := m.subs[subscriptionID]
	if !ok {
		return fmt.Errorf("no such subscription %s", subscriptionID)
	}
	if row.Version != version {
		return fmt.Errorf("subscription %s expected version %d, stored %d", subscriptionID, version, row.Version)
	}
	delete(m.subs, subscriptionID)
	m.settled["SIGNAL:"+subscriptionID.String()] = true
	return nil
}

func (m *memoryPorts) Enqueue(_ context.Context, _ Executor, _, _ uuid.UUID, row ReadyRow, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("ready.Enqueue"); err != nil {
		return false, err
	}
	if _, exists := m.ready[row.ReadyWorkID]; exists {
		return true, nil
	}
	row.Version = 1
	m.ready[row.ReadyWorkID] = row
	return false, nil
}

func (m *memoryPorts) Record(_ context.Context, _ Executor, in ContinuationIntent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("continuations.Record"); err != nil {
		return err
	}
	// The durable ledger's identity is (instance, node, attempt, node, READY);
	// this double keys the same way so a replay is a no-op here too.
	key := fmt.Sprintf("%s|%s|%d", in.InstanceID, in.NodeID, in.Attempt)
	m.continuations[key]++
	return nil
}

func (m *memoryPorts) Links(_ context.Context, _ Executor, _, _ uuid.UUID) ([]ChildRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.record("children.Links"); err != nil {
		return nil, err
	}
	return append([]ChildRow(nil), m.children...), nil
}

// approvalPort and readyPort are the two adapters Go's one-method-name-per-
// type rule forces out of memoryPorts itself: TimerPort and ReadyWorkPort
// both declare Pending, with different row types.
type approvalPort struct{ m *memoryPorts }

func (a approvalPort) Pending(_ context.Context, _ Executor, _, _ uuid.UUID) ([]ApprovalRow, error) {
	a.m.mu.Lock()
	defer a.m.mu.Unlock()
	if err := a.m.record("approvals.Pending"); err != nil {
		return nil, err
	}
	return append([]ApprovalRow(nil), a.m.approvals...), nil
}

type readyPort struct{ m *memoryPorts }

func (r readyPort) Pending(_ context.Context, _ Executor, _, _ uuid.UUID) ([]ReadyRow, error) {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	if err := r.m.record("ready.Pending"); err != nil {
		return nil, err
	}
	out := make([]ReadyRow, 0, len(r.m.ready))
	for _, row := range r.m.ready {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReadyWorkID.String() < out[j].ReadyWorkID.String() })
	return out, nil
}

func (r readyPort) Enqueue(ctx context.Context, ex Executor, tenantID, instanceID uuid.UUID, row ReadyRow, at time.Time) (bool, error) {
	return r.m.Enqueue(ctx, ex, tenantID, instanceID, row, at)
}

func (r readyPort) Cancel(_ context.Context, _ Executor, _, readyWorkID uuid.UUID, version uint64, _ time.Time) error {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	if err := r.m.record("ready.Cancel"); err != nil {
		return err
	}
	row, ok := r.m.ready[readyWorkID]
	if !ok {
		return fmt.Errorf("no such ready work %s", readyWorkID)
	}
	if row.Version != version {
		return fmt.Errorf("ready work %s expected version %d, stored %d", readyWorkID, version, row.Version)
	}
	delete(r.m.ready, readyWorkID)
	r.m.settled["READY_WORK:"+readyWorkID.String()] = true
	return nil
}

// --- scenario builder --------------------------------------------------------

// scenario is one instance with a set of pending artifacts and the scope that
// migrates it.
type scenario struct {
	tenant   uuid.UUID
	instance uuid.UUID
	ports    *memoryPorts
	scope    Scope
	holder   lease.Identity
}

// newScenario builds an instance holding one timer, one subscription, one
// ready-work row, one approval and one awaited child, all on fromNode, under
// a live lease the scope's fence matches.
func newScenario(t *testing.T) *scenario {
	t.Helper()
	tenant, instance := uuid.New(), uuid.New()
	holder := lease.Identity{WorkloadRef: "workload:hcmnext-workflow-runtime", InstanceRef: "replica-1"}
	leaseID := uuid.New()

	m := newMemoryPorts()
	m.lease = &LeaseRow{
		LeaseID: leaseID, Holder: holder.HolderID(), FenceToken: 7,
		ExpiresAt: fixedInstant.Add(time.Hour),
	}

	timerID := timer.TimerID(tenant, instance, fromNode, sourceRequirementDigest)
	m.timers[timerID] = TimerRow{
		TimerID: timerID, NodeID: fromNode, Key: sourceRequirementDigest,
		Kind: "DELAY", FiresAt: wakeInstant, Version: 1,
	}
	subID := SubscriptionID(tenant, instance, fromNode, "promotion.finance_ack")
	m.subs[subID] = SubscriptionRow{
		SubscriptionID: subID, NodeID: fromNode, SignalName: "promotion.finance_ack",
		CorrelationKey: "employment:jane-doe-9001", Version: 1,
	}
	readyID := timer.ReadyWorkID(tenant, instance, fromNode, 1)
	m.ready[readyID] = ReadyRow{
		ReadyWorkID: readyID, NodeID: fromNode, Attempt: 1, Priority: 100,
		State: "READY", EligibleAt: wakeInstant, Version: 1,
	}
	m.approvals = []ApprovalRow{{
		WorkItemID: uuid.New(), NodeID: "end_requires_finance_approval",
		Owner: "PRINCIPAL:principal:finance-partner-1", Status: "ASSIGNED",
		DeadlineAt: fixedInstant.Add(48 * time.Hour),
	}}
	m.children = []ChildRow{{
		Child: uuid.New(), ParentNodeID: "simulate_comp", Ordinal: 1, Mode: "AWAIT",
	}}

	return &scenario{
		tenant: tenant, instance: instance, ports: m, holder: holder,
		scope: Scope{
			TenantID: tenant, InstanceID: instance,
			From: Epoch{WorkflowVersion: 1, CompiledPlanDigest: "digest-source", NodeID: fromNode, Attempt: 1},
			To:   Epoch{WorkflowVersion: 2, CompiledPlanDigest: "digest-target", NodeID: fromNode, Attempt: 1},
			Fence: lease.Fence{
				TenantID: tenant,
				Resource: lease.Resource{Kind: lease.ResourceWorkflowInstance, ID: instance.String()},
				LeaseID:  leaseID, Holder: holder, Token: 7,
			},
			MigratedBy: "principal:migration-operator", MigratedAt: fixedInstant,
		},
	}
}

// relocate turns the scenario's migration into one that moves the frontier
// onto toNode and supplies the replacement wake requirement for the timer.
func (s *scenario) relocate() *scenario {
	s.scope.To.NodeID = toNode
	s.scope.Requirements = map[string]wait.TimerRequirement{
		sourceRequirementDigest: targetRequirementFor(toNode, wakeInstant),
	}
	return s
}

func (s *scenario) run(t *testing.T) (Receipt, error) {
	t.Helper()
	return Migrate(context.Background(), nil, Request{Scope: s.scope, Ports: s.ports.ports()})
}

func (s *scenario) mustRun(t *testing.T) Receipt {
	t.Helper()
	receipt, err := s.run(t)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return receipt
}

// --- wake requirements -------------------------------------------------------

// The two requirement digests the fixtures use. They are opaque 64-hex
// strings because a timer's key is a content digest and nothing here cares
// what it hashed, only that re-keying changes it.
const (
	sourceRequirementDigest = "1111111111111111111111111111111111111111111111111111111111111111"
	targetRequirementDigest = "2222222222222222222222222222222222222222222222222222222222222222"
)

// targetRequirementFor is the replacement wake requirement the target plan
// computes for one node: a different digest promising the identical instant.
func targetRequirementFor(node string, at time.Time) wait.TimerRequirement {
	return wait.TimerRequirement{
		WorkflowID: "hcmnext.promote_into_management", WorkflowVersion: 2, NodeID: node,
		FireAt: values.NewInstant(at), Digest: targetRequirementDigest,
	}
}

// --- assertions --------------------------------------------------------------

func entryFor(t *testing.T, r Receipt, kind Kind, identity string) Entry {
	t.Helper()
	for _, e := range r.Entries {
		if e.Kind == kind && e.Identity == identity {
			return e
		}
	}
	t.Fatalf("receipt carries no %s entry with identity %q; it carries %v", kind, identity, identities(r))
	return Entry{}
}

func identities(r Receipt) []string {
	out := make([]string, 0, len(r.Entries))
	for _, e := range r.Entries {
		out = append(out, string(e.Kind)+"/"+e.Identity)
	}
	return out
}
