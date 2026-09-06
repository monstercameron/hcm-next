package artifacts

import (
	"testing"
)

// Compile-time proof that every durable adapter satisfies the port it is
// wired into. A mismatch here is a build failure rather than a nil interface
// discovered at the first migration.
var (
	_ LeasePort        = storeLeases{}
	_ TimerPort        = storeTimers{}
	_ SignalPort       = storeSignals{}
	_ ReadyWorkPort    = storeReadyWork{}
	_ ApprovalPort     = storeApprovals{}
	_ ChildPort        = storeChildren{}
	_ ContinuationPort = storeContinuations{}
)

// DefaultPorts must wire the durable adapters, not a double. The behaviour of
// those adapters against real SQL is what TestTodo_WF_RUN_026_Integration
// proves; this only pins the wiring, which is the part a refactor silently
// breaks.
func TestDefaultPorts_WiresTheDurableAdapters(t *testing.T) {
	t.Parallel()
	ports := DefaultPorts()
	if _, ok := ports.Leases.(storeLeases); !ok {
		t.Fatalf("Leases is %T, want storeLeases", ports.Leases)
	}
	if _, ok := ports.Timers.(storeTimers); !ok {
		t.Fatalf("Timers is %T, want storeTimers", ports.Timers)
	}
	if _, ok := ports.Signals.(storeSignals); !ok {
		t.Fatalf("Signals is %T, want storeSignals", ports.Signals)
	}
	if _, ok := ports.ReadyWork.(storeReadyWork); !ok {
		t.Fatalf("ReadyWork is %T, want storeReadyWork", ports.ReadyWork)
	}
	if _, ok := ports.Approvals.(storeApprovals); !ok {
		t.Fatalf("Approvals is %T, want storeApprovals", ports.Approvals)
	}
	if _, ok := ports.Children.(storeChildren); !ok {
		t.Fatalf("Children is %T, want storeChildren", ports.Children)
	}
	if _, ok := ports.Continuations.(storeContinuations); !ok {
		t.Fatalf("Continuations is %T, want storeContinuations", ports.Continuations)
	}
}

// Two calls must not share state: every adapter is a zero-value struct over a
// zero-value store, so a caller may construct the set per transaction.
func TestDefaultPorts_IsStateless(t *testing.T) {
	t.Parallel()
	if DefaultPorts() != DefaultPorts() {
		t.Fatal("two DefaultPorts() sets compared unequal; an adapter carries state")
	}
}
