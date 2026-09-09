package replay

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestStoreSource_RefusesUnusableWiring covers the two checks that run before
// any statement is issued, so a misconfigured source fails at the caller
// rather than inside a query.
func TestStoreSource_RefusesUnusableWiring(t *testing.T) {
	if _, err := (StoreSource{TenantID: uuid.New(), InstanceID: uuid.New()}).Load(context.Background()); CodeOf(err) != CodeSourceFailed {
		t.Fatalf("no executor: code = %q (%v)", CodeOf(err), err)
	}
	// A non-nil executor is enough to get past the first check; the ids are
	// then what is missing.
	src := StoreSource{Executor: unusedExecutor{}}
	if _, err := src.Load(context.Background()); CodeOf(err) != CodeSourceFailed {
		t.Fatalf("no ids: code = %q (%v)", CodeOf(err), err)
	}
}

// TestCausalOrder_IsCausalNotChronological is the finding this source exists
// to survive: a driver draining a whole run against one clock stamps every
// continuation with the same instant, so ordering by that instant alone would
// replay the graph in alphabetical order. The ledger's causation is read
// instead.
func TestCausalOrder_IsCausalNotChronological(t *testing.T) {
	at := time.Unix(1, 0).UTC()
	// Deliberately fed in reverse, all at one instant, with node ids whose
	// alphabetical order is the opposite of their causal order.
	conts := []continuationRow{
		{sourceNodeID: "zulu", sourceAttempt: 1, targetNodeID: "zulu",
			kind: frontier.IntentComplete, terminalCode: "DONE", recordedAt: at},
		{sourceNodeID: "mike", sourceAttempt: 1, targetNodeID: "zulu",
			kind: frontier.IntentReady, routeKey: "SUCCEEDED", recordedAt: at},
		{sourceNodeID: "alpha", sourceAttempt: 1, targetNodeID: "mike",
			kind: frontier.IntentReady, routeKey: "SUCCEEDED", recordedAt: at},
	}
	_, keys := causalOrder(conts)
	want := []string{"alpha\x001", "mike\x001", "zulu\x001"}
	if len(keys) != len(want) {
		t.Fatalf("causalOrder returned %d groups, want %d", len(keys), len(want))
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("group %d = %q, want %q", i, keys[i], want[i])
		}
	}
}

// TestCausalOrder_TiesFallBackToTheLedger checks the deterministic tie-break
// between two genuinely independent groups.
func TestCausalOrder_TiesFallBackToTheLedger(t *testing.T) {
	early := time.Unix(1, 0).UTC()
	late := time.Unix(2, 0).UTC()
	conts := []continuationRow{
		{sourceNodeID: "b", sourceAttempt: 1, targetNodeID: "b2", kind: frontier.IntentReady, recordedAt: late},
		{sourceNodeID: "a", sourceAttempt: 1, targetNodeID: "a2", kind: frontier.IntentReady, recordedAt: early},
	}
	_, keys := causalOrder(conts)
	if keys[0] != "a\x001" {
		t.Fatalf("the earlier instant did not win the tie: %v", keys)
	}

	// Identical instants fall back to the ledger's own row order.
	conts[0].recordedAt = early
	_, keys = causalOrder(conts)
	if keys[0] != "b\x001" {
		t.Fatalf("an exact tie did not fall back to ledger order: %v", keys)
	}
}

// TestCausalOrder_ACycleStillEmitsEveryGroup keeps a contradictory ledger from
// silently losing attempts: everything is emitted, and the replay then
// diverges visibly at the first node out of place.
func TestCausalOrder_ACycleStillEmitsEveryGroup(t *testing.T) {
	at := time.Unix(1, 0).UTC()
	conts := []continuationRow{
		{sourceNodeID: "a", sourceAttempt: 1, targetNodeID: "b", kind: frontier.IntentReady, recordedAt: at},
		{sourceNodeID: "b", sourceAttempt: 1, targetNodeID: "a", kind: frontier.IntentReady, recordedAt: at},
	}
	_, keys := causalOrder(conts)
	if len(keys) != 2 {
		t.Fatalf("a cycle dropped a group: %v", keys)
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Fatalf("a cycle emitted a group twice: %v", keys)
		}
		seen[k] = true
	}
}

// TestNodeRecords_ReadsTheLedgerAndTheExecutionTable checks the reconstruction
// itself: sequence, route key, await, failure, terminal code and the node
// executions that are correctly absent.
func TestNodeRecords_ReadsTheLedgerAndTheExecutionTable(t *testing.T) {
	tenant, instance := uuid.New(), uuid.New()
	at := time.Unix(1, 0).UTC()
	mk := func(nodeID string, step workflow.StepType, status runtime.NodeStatus, digest string) runtime.NodeExecution {
		e := runtime.NewNodeExecution(tenant, instance, nodeID, 1, step, status)
		e.OutputArtifactRef = digest
		return e
	}
	execs := []runtime.NodeExecution{
		mk("gate", workflow.StepApproval, runtime.NodeWaiting, ""),
		mk("work", workflow.StepCapability, runtime.NodeFailed, "sha256:work"),
		mk("done", workflow.StepEnd, runtime.NodeSucceeded, "sha256:done"),
		// A node the frontier settled without driving: it raised no
		// continuation, so it must not appear as a recorded attempt.
		mk("skipped", workflow.StepTransform, runtime.NodeSkipped, ""),
	}
	conts := []continuationRow{
		{sourceNodeID: "gate", sourceAttempt: 1, targetNodeID: "gate",
			kind: frontier.IntentWorkItemRequired, ref: "requirement:1", recordedAt: at},
		{sourceNodeID: "gate", sourceAttempt: 1, targetNodeID: "work",
			kind: frontier.IntentReady, routeKey: "APPROVED", recordedAt: at},
		{sourceNodeID: "work", sourceAttempt: 1, targetNodeID: "done",
			kind: frontier.IntentReady, routeKey: "FAILED", recordedAt: at},
		{sourceNodeID: "done", sourceAttempt: 1, targetNodeID: "done",
			kind: frontier.IntentComplete, terminalCode: "DONE", recordedAt: at},
	}

	nodes, terminal := nodeRecords(execs, conts)
	if len(nodes) != 3 {
		t.Fatalf("reconstructed %d attempts, want 3 (the settled node must not appear)", len(nodes))
	}
	if terminal != "DONE" {
		t.Fatalf("terminal = %q, want DONE", terminal)
	}
	for i, want := range []string{"gate", "work", "done"} {
		if nodes[i].NodeID != want {
			t.Fatalf("attempt %d = %s, want %s", i, nodes[i].NodeID, want)
		}
		if nodes[i].Sequence != i+1 {
			t.Fatalf("attempt %d carries sequence %d", i, nodes[i].Sequence)
		}
		if !nodes[i].RecordedAt.Equal(at) {
			t.Fatalf("attempt %d recorded at %s, want %s", i, nodes[i].RecordedAt, at)
		}
	}
	switch {
	case nodes[0].Await != frontier.AwaitWorkItem || nodes[0].AwaitRef != "requirement:1":
		t.Fatalf("the await was not read back: %+v", nodes[0])
	case nodes[0].RouteKey != "APPROVED":
		t.Fatalf("route key = %q, want APPROVED", nodes[0].RouteKey)
	case nodes[0].StepType != workflow.StepApproval:
		t.Fatalf("step type = %q", nodes[0].StepType)
	case !nodes[1].Failed || nodes[1].OutputDigest != "sha256:work":
		t.Fatalf("the failed attempt was not read back: %+v", nodes[1])
	case nodes[2].RouteKey != "":
		t.Fatalf("the END attempt carries a route key: %q", nodes[2].RouteKey)
	}
}

// TestAwaitOf maps every raised intent back to the marker that raised it.
func TestAwaitOf(t *testing.T) {
	for kind, want := range map[frontier.IntentKind]frontier.AwaitKind{
		frontier.IntentWorkItemRequired:           frontier.AwaitWorkItem,
		frontier.IntentSignalSubscriptionRequired: frontier.AwaitSignal,
		frontier.IntentTimerRequired:              frontier.AwaitTimer,
		frontier.IntentReady:                      frontier.AwaitNone,
		frontier.IntentComplete:                   frontier.AwaitNone,
	} {
		if got := awaitOf(kind); got != want {
			t.Fatalf("awaitOf(%s) = %q, want %q", kind, got, want)
		}
	}
}

// unusedExecutor satisfies [Executor] for the wiring checks that must refuse
// before any statement is issued. Every method fails loudly, so a check that
// silently started querying would fail this test rather than pass it.
type unusedExecutor struct{}

var _ Executor = unusedExecutor{}

func (unusedExecutor) Exec(context.Context, string, ...any) (int64, error) {
	panic("StoreSource issued a statement before validating its wiring")
}

func (unusedExecutor) Query(context.Context, string, ...any) (dbport.Rows, error) {
	panic("StoreSource issued a query before validating its wiring")
}

func (unusedExecutor) QueryRow(context.Context, string, ...any) dbport.Row {
	panic("StoreSource issued a query before validating its wiring")
}
