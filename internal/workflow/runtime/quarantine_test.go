package runtime_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

func quarantineSpec(node, key string, terminal runtime.RetryRoute) runtime.QuarantineSpec {
	return runtime.QuarantineSpec{
		NodeID: node, WorkflowID: "wf/poison", Attempts: 4, LastError: "ledger persistent 503",
		IdempotencyKey: key, Owner: "operator-1", SLA: 30 * time.Minute,
		NextAction: "await-reconciliation", Terminal: terminal,
	}
}

func terminalRoute(reason, repairRoute string) runtime.RetryRoute {
	return runtime.RetryRoute{Decision: runtime.RouteTerminal, Reason: reason, RepairRoute: repairRoute}
}

// TestTodo_WF_RUN_007 is the WF-RUN-007 primary test: exhausted nodes
// land in quarantine with everything retained and route the workflow to
// BLOCKED, REPAIR_REQUIRED or QUARANTINED — never success, never dropped.
func TestTodo_WF_RUN_007(t *testing.T) {
	ledger := runtime.NewQuarantineLedger()

	// Budget exhaustion carries its repair route: REPAIR_REQUIRED.
	repair, err := runtime.Admit(quarantineSpec("node/commit", "idem-1",
		terminalRoute(runtime.ReasonBudgetExhausted, "operations.repair.retry_budget")))
	if err != nil {
		t.Fatal(err)
	}
	if repair.Route != runtime.WorkflowRepairRequired || repair.NextAction != "repair:operations.repair.retry_budget" {
		t.Fatalf("exhausted route = %+v, want REPAIR_REQUIRED with the repair action", repair)
	}
	if repair.Attempts != 4 || repair.LastError == "" || repair.Owner == "" || repair.SLA != 30*time.Minute || repair.Digest == "" {
		t.Fatalf("quarantined work drops retention: %+v", repair)
	}
	if err := repair.Verify(); err != nil {
		t.Fatalf("fresh record does not verify: %v", err)
	}

	// Permanent failure needs an operator: BLOCKED.
	blocked, err := runtime.Admit(quarantineSpec("node/validate", "idem-2",
		terminalRoute(runtime.ReasonNonretryable, "")))
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Route != runtime.WorkflowBlocked || blocked.NextAction != "operator-decision" {
		t.Fatalf("permanent route = %+v, want BLOCKED with operator decision", blocked)
	}

	// Ambiguity waits under quarantine with its next action, never success.
	spec := quarantineSpec("node/commit-ambiguous", "idem-3", terminalRoute(runtime.ReasonBudgetExhausted, "operations.repair.retry_budget"))
	spec.Ambiguous = true
	ambiguous, err := runtime.Admit(spec)
	if err != nil {
		t.Fatal(err)
	}
	if ambiguous.Route != runtime.WorkflowQuarantined || ambiguous.NextAction != "await-reconciliation" {
		t.Fatalf("ambiguous route = %+v, want QUARANTINED awaiting reconciliation", ambiguous)
	}

	for _, work := range []runtime.QuarantinedWork{repair, blocked, ambiguous} {
		if filed, err := ledger.File(work); err != nil || filed.Digest != work.Digest {
			t.Fatalf("file = %+v, %v", filed, err)
		}
	}
	// Refiling is idempotent: the stored record returns, nothing duplicates.
	first, err := ledger.File(repair)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != repair.Digest || len(ledger.Keys()) != 3 {
		t.Fatalf("refile duplicated: %+v", first)
	}
	got, err := ledger.Get("idem-1")
	if err != nil || got.Route != runtime.WorkflowRepairRequired {
		t.Fatalf("get = %+v, %v; quarantined work must be retained", got, err)
	}
	// No route in the vocabulary reports success.
	for _, route := range []string{repair.Route, blocked.Route, ambiguous.Route} {
		if route == "SUCCESS" || route == "COMPLETED" || route == "" {
			t.Fatalf("quarantine reports %q", route)
		}
	}
}

func TestTodo_WF_RUN_007_Golden(t *testing.T) {
	work, err := runtime.Admit(quarantineSpec("node/commit", "idem-golden",
		terminalRoute(runtime.ReasonBudgetExhausted, "operations.repair.retry_budget")))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "node: %s workflow: %s attempts: %d\n", work.NodeID, work.WorkflowID, work.Attempts)
	fmt.Fprintf(&b, "error: %s ambiguous: %v\n", work.LastError, work.Ambiguous)
	fmt.Fprintf(&b, "idempotency: %s owner: %s sla: %s\n", work.IdempotencyKey, work.Owner, work.SLA)
	fmt.Fprintf(&b, "action: %s repair: %s route: %s\n", work.NextAction, work.RepairRoute, work.Route)
	fmt.Fprintf(&b, "digest: %s\n", work.Digest)
	got := b.String()
	path := filepath.Join("testdata", "wfrun007_quarantine.golden")
	if os.Getenv("HCMNEXT_UPDATE_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote golden %s", path)
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (set HCMNEXT_UPDATE_GOLDEN=1 to create it)", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden %s mismatch\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}

func TestTodo_WF_RUN_007_Race(t *testing.T) {
	ledger := runtime.NewQuarantineLedger()
	const workers = 16
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			work, err := runtime.Admit(quarantineSpec(fmt.Sprintf("node/%d", i), fmt.Sprintf("race-%d", i),
				terminalRoute(runtime.ReasonBudgetExhausted, "operations.repair.retry_budget")))
			if err != nil {
				errs <- err
				return
			}
			if _, err := ledger.File(work); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent quarantine = %v", err)
	}
	if len(ledger.Keys()) != workers {
		t.Fatalf("ledger holds %d records, want %d", len(ledger.Keys()), workers)
	}
}

func TestTodo_WF_RUN_007_Fault(t *testing.T) {
	// A RETRY route is never poison: only terminal exhaustion quarantines.
	if _, err := runtime.Admit(quarantineSpec("node/live", "idem-live",
		runtime.RetryRoute{Decision: runtime.RouteRetry, NextDelay: time.Second, NextAttempt: 2})); err == nil {
		t.Fatal("retryable work quarantined")
	}
	// Identity, attempts and node binding are required.
	spec := quarantineSpec("", "idem-x", terminalRoute(runtime.ReasonNonretryable, ""))
	if _, err := runtime.Admit(spec); err == nil {
		t.Fatal("anonymous node quarantined")
	}
	spec = quarantineSpec("node/x", "", terminalRoute(runtime.ReasonNonretryable, ""))
	if _, err := runtime.Admit(spec); err == nil {
		t.Fatal("identity-less quarantine accepted")
	}
	spec = quarantineSpec("node/x", "idem-x", terminalRoute(runtime.ReasonNonretryable, ""))
	spec.Attempts = 0
	if _, err := runtime.Admit(spec); err == nil {
		t.Fatal("zero-attempt quarantine accepted")
	}
	// Ambiguity without owner and action is refused; blocked work without
	// an owner is refused.
	spec = quarantineSpec("node/x", "idem-x", terminalRoute(runtime.ReasonBudgetExhausted, "r"))
	spec.Ambiguous = true
	spec.Owner = ""
	if _, err := runtime.Admit(spec); err == nil {
		t.Fatal("owner-less ambiguity quarantined")
	}
	spec = quarantineSpec("node/x", "idem-x", terminalRoute(runtime.ReasonNonretryable, ""))
	spec.Owner = ""
	if _, err := runtime.Admit(spec); err == nil {
		t.Fatal("owner-less block accepted")
	}
	// Missing records are refused, never reported as success.
	if _, err := runtime.NewQuarantineLedger().Get("idem/ghost"); err == nil {
		t.Fatal("ghost record returned")
	}
}

func TestTodo_WF_RUN_007_Mutation(t *testing.T) {
	// Mutant 1: an edited record breaks its seal.
	work, err := runtime.Admit(quarantineSpec("node/commit", "idem-m1",
		terminalRoute(runtime.ReasonBudgetExhausted, "operations.repair.retry_budget")))
	if err != nil {
		t.Fatal(err)
	}
	work.NextAction = "retry-silently"
	if err := work.Verify(); err == nil {
		t.Fatal("edited record verifies")
	}
	if _, err := runtime.NewQuarantineLedger().File(work); err == nil {
		t.Fatal("edited record filed")
	}
	// Mutant 2: DO_NOT_RETRY lands BLOCKED with an operator decision,
	// never retried, never success.
	blocked, err := runtime.Admit(quarantineSpec("node/guard", "idem-m2",
		terminalRoute(runtime.ReasonDoNotRetry, "")))
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Route != runtime.WorkflowBlocked {
		t.Fatalf("do-not-retry route = %s, want BLOCKED", blocked.Route)
	}
	// Mutant 3: deadline exhaustion is terminal and quarantinable.
	tired, err := runtime.Admit(quarantineSpec("node/slow", "idem-m3",
		terminalRoute(runtime.ReasonDeadlineExceeded, "")))
	if err != nil {
		t.Fatal(err)
	}
	if tired.Route != runtime.WorkflowBlocked {
		t.Fatalf("deadline route = %s, want BLOCKED", tired.Route)
	}
	// Mutant 4: attempts exhaustion quarantines with its count retained.
	spent, err := runtime.Admit(quarantineSpec("node/flaky", "idem-m4",
		terminalRoute(runtime.ReasonAttemptsExhausted, "")))
	if err != nil {
		t.Fatal(err)
	}
	if spent.Attempts != 4 || spent.Route != runtime.WorkflowBlocked {
		t.Fatalf("spent record = %+v, want attempts retained under BLOCKED", spent)
	}
}
