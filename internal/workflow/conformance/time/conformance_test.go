package time

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/workflow"
	"github.com/monstercameron/hcm-next/internal/workflow/simulate"
)

func mustSetup(t *testing.T, env *Environment) *Setup {
	t.Helper()
	setup, err := NewSetup(env)
	if err != nil {
		t.Fatalf("NewSetup: %v", err)
	}
	return setup
}

func mustRun(t *testing.T, setup *Setup) simulate.Receipt {
	t.Helper()
	receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return receipt
}

func traceFor(receipt simulate.Receipt, nodeID string) (simulate.NodeTrace, bool) {
	for _, trace := range receipt.Trace {
		if trace.NodeID == nodeID {
			return trace, true
		}
	}
	return simulate.NodeTrace{}, false
}

// TestTodo_CONF_011 is the PRIMARY conformance claim: the compiled
// time-punch/timecard/payroll-bridge reference walks through the real
// SIMULATE interpreter with its timezone and tzdb pin, classifies a clean
// punch as ACCEPTED, and independently observes the bridge without effects.
func TestTodo_CONF_011(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment())
	if err := simulate.Admit(setup.Plan); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	receipt := mustRun(t, setup)

	if receipt.Mode != workflow.ModeSimulate {
		t.Errorf("mode = %s, want %s", receipt.Mode, workflow.ModeSimulate)
	}
	if receipt.WorkflowID != WorkflowID {
		t.Errorf("workflow id = %q, want %q", receipt.WorkflowID, WorkflowID)
	}
	if receipt.PlanDigest != setup.Plan.Digest() {
		t.Errorf("receipt pins plan %q, plan digest is %q", receipt.PlanDigest, setup.Plan.Digest())
	}
	wantPath := []string{
		NodeReadDeviceAndClockContext,
		NodeBuildPunchProposal,
		NodeClassificationDecision,
		NodeObservePayrollBridge,
		NodeEndAcceptedBridgeConfirmed,
	}
	if got := receipt.NodeIDs(); len(got) != len(wantPath) {
		t.Fatalf("walked %d nodes %v, want %d %v", len(got), got, len(wantPath), wantPath)
	} else {
		for i := range wantPath {
			if got[i] != wantPath[i] {
				t.Fatalf("node %d = %q, want %q (whole path %v)", i, got[i], wantPath[i], got)
			}
		}
	}
	if receipt.Terminal.TerminalCode != "TIME_PUNCH_ACCEPTED_BRIDGE_CONFIRMED" {
		t.Fatalf("terminal code = %q, want TIME_PUNCH_ACCEPTED_BRIDGE_CONFIRMED", receipt.Terminal.TerminalCode)
	}
	if receipt.Lifecycle != (simulate.LifecycleState{
		RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED",
		ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING",
	}) {
		t.Fatalf("lifecycle = %+v, want simulated pending-observation state", receipt.Lifecycle)
	}
	wantObligations := []string{ObligationAttestation, ObligationCutoff, ObligationBridgeIntegrity, ObligationRecordsRetention}
	if !equalSets(receipt.Terminal.OutstandingObligationRefs, wantObligations) {
		t.Fatalf("outstanding obligations = %v, want %v", receipt.Terminal.OutstandingObligationRefs, wantObligations)
	}
	if counters := receipt.EffectCounters(); !counters.IsZero() {
		t.Fatalf("simulation counted effects: %v", counters.NonZero())
	}
	if err := receipt.ZeroEffect.Validate(); err != nil {
		t.Fatalf("zero-effect receipt: %v", err)
	}

	proposal, ok := traceFor(receipt, NodeBuildPunchProposal)
	if !ok {
		t.Fatalf("no trace entry for %s", NodeBuildPunchProposal)
	}
	for _, want := range []string{"America/Chicago", "tzdata2026b"} {
		if !strings.Contains(proposal.Detail, want) {
			t.Errorf("proposal detail %q does not preserve %q", proposal.Detail, want)
		}
	}
	classification, ok := traceFor(receipt, NodeClassificationDecision)
	if !ok || !strings.Contains(classification.Detail, "accepted") {
		t.Fatalf("classification trace = %+v, want ACCEPTED evidence", classification)
	}
	bridge, ok := traceFor(receipt, NodeObservePayrollBridge)
	if !ok || !strings.Contains(bridge.Detail, "PASS") {
		t.Fatalf("bridge trace = %+v, want PASS observation evidence", bridge)
	}
	if len(receipt.WorkItems) != 2 {
		t.Fatalf("work items = %d, want 2", len(receipt.WorkItems))
	}
	if err := receipt.Verify(); err != nil {
		t.Fatalf("receipt does not verify: %v", err)
	}
	second := mustRun(t, mustSetup(t, GoldenEnvironment()))
	if receipt.Digest() != second.Digest() {
		t.Fatalf("receipt digest drifted: %s vs %s", receipt.Digest(), second.Digest())
	}
}

// TestTodo_CONF_011_Conformance proves every classification route is
// reachable and that review/rejection never becomes a silent ACCEPTED punch.
func TestTodo_CONF_011_Conformance(t *testing.T) {
	cases := []struct {
		name     string
		env      *Environment
		terminal string
		want     simulate.LifecycleState
	}{
		{"accepted", GoldenEnvironment(), NodeEndAcceptedBridgeConfirmed, simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_STARTED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"}},
		{"duplicate", DuplicatePunchEnvironment(), NodeEndDuplicatePunch, simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"}},
		{"post_lock", PostLockEditEnvironment(), NodeEndRejectedPostLockEdit, simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"}},
		{"stale_reopen", StaleReopenEnvironment(), NodeEndRejectedStaleReopen, simulate.LifecycleState{RequestState: "REJECTED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "NOT_APPLICABLE", ObligationState: "NOT_APPLICABLE"}},
		{"spoof_review", SpoofSuspectedEnvironment(), NodeEndReviewSpoofSuspected, reviewLifecycle()},
		{"offline_review", OfflineReplayEnvironment(), NodeEndReviewOfflineReplay, reviewLifecycle()},
		{"dst_fold_review", DSTFoldEnvironment(), NodeEndReviewDSTFold, reviewLifecycle()},
		{"clock_skew_review", ClockSkewEnvironment(), NodeEndReviewClockSkew, reviewLifecycle()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			receipt := mustRun(t, mustSetup(t, tc.env))
			if receipt.Terminal.NodeID != tc.terminal {
				t.Fatalf("terminal = %q, want %q (path %v)", receipt.Terminal.NodeID, tc.terminal, receipt.NodeIDs())
			}
			if receipt.Lifecycle != tc.want {
				t.Fatalf("lifecycle = %+v, want %+v", receipt.Lifecycle, tc.want)
			}
		})
	}
}

func reviewLifecycle() simulate.LifecycleState {
	return simulate.LifecycleState{RequestState: "SIMULATED", ExecutionState: "NOT_PLANNED", BusinessState: "NOT_ACHIEVED", ConsistencyState: "PENDING_OBSERVATION", ObligationState: "PENDING"}
}

// TestTodo_CONF_011_Recovery proves bridge FAIL/PARTIAL/UNKNOWN outcomes are
// separately retained as degraded or unknown evidence and use bounded retry.
func TestTodo_CONF_011_Recovery(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      *Environment
		terminal string
	}{
		{"fail", DegradedBridgeEnvironment(), NodeEndBridgeDegradedRepair},
		{"partial", func() *Environment {
			e := GoldenEnvironment()
			e.ObserveOutcome = workflow.OutcomePartial
			e.ObserveWatermark = ""
			return e
		}(), NodeEndBridgeDegradedRepair},
		{"unknown", func() *Environment {
			e := GoldenEnvironment()
			e.ObserveOutcome = workflow.OutcomeUnknown
			e.ObserveWatermark = ""
			return e
		}(), NodeEndUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setup := mustSetup(t, tc.env)
			receipt := mustRun(t, setup)
			if receipt.Terminal.NodeID != tc.terminal {
				t.Fatalf("terminal = %q, want %q", receipt.Terminal.NodeID, tc.terminal)
			}
			if receipt.Terminal.NodeID == NodeEndAcceptedBridgeConfirmed {
				t.Fatal("a non-PASS bridge observation was reported as confirmed")
			}
			if tc.terminal == NodeEndBridgeDegradedRepair {
				if !equalSets(receipt.Terminal.OutstandingObligationRefs, []string{ObligationBridgeIntegrity, ObligationRecordsRetention}) {
					t.Fatalf("degraded obligations = %v, want bridge integrity and retention", receipt.Terminal.OutstandingObligationRefs)
				}
				node, ok := setup.Plan.Node(NodeEndBridgeDegradedRepair)
				if !ok || node.Terminal == nil || len(node.Terminal.RepairRefs) != 1 {
					t.Fatalf("degraded terminal has no bounded repair reference")
				}
			}
		})
	}
	node, ok := mustSetup(t, DegradedBridgeEnvironment()).Plan.Node(NodeObservePayrollBridge)
	if !ok || node.Retry == nil || node.Retry.MaxAttempts == 0 || node.Observe == nil || node.Observe.RetryExhaustionRoute != NodeEndBridgeDegradedRepair {
		t.Fatalf("bridge observation retry is not bounded to repair: %+v", node)
	}
}

// TestTodo_CONF_011_Mutation proves route precedence and proves the tzdb pin
// participates in the proposal identity, so a tzdb change cannot silently
// reinterpret an existing punch.
func TestTodo_CONF_011_Mutation(t *testing.T) {
	inputs := func(values ...bool) simulate.Bag {
		return simulate.Bag{
			"duplicate_punch_detected":      simulate.NewBool(values[0]),
			"post_lock_edit_attempted":      simulate.NewBool(values[1]),
			"timecard_reopened_stale":       simulate.NewBool(values[2]),
			"shared_device_spoof_suspected": simulate.NewBool(values[3]),
			"offline_replay_detected":       simulate.NewBool(values[4]),
			"dst_fold_ambiguous":            simulate.NewBool(values[5]),
			"clock_skew_within_tolerance":   simulate.NewBool(values[6]),
		}
	}
	precedence := []struct {
		name string
		bits []bool
		want string
	}{
		{"duplicate_wins", []bool{true, true, true, true, true, true, false}, RouteDuplicatePunch},
		{"post_lock_wins", []bool{false, true, true, true, true, true, false}, RouteRejectedPostLockEdit},
		{"stale_wins", []bool{false, false, true, true, true, true, false}, RouteRejectedStaleReopen},
		{"spoof_wins", []bool{false, false, false, true, true, true, false}, RouteReviewSpoofSuspected},
		{"offline_wins", []bool{false, false, false, false, true, true, false}, RouteReviewOfflineReplay},
		{"dst_wins", []bool{false, false, false, false, false, true, false}, RouteReviewDSTFold},
		{"skew_review", []bool{false, false, false, false, false, false, false}, RouteReviewClockSkew},
		{"accepted", []bool{false, false, false, false, false, false, true}, RouteAccepted},
	}
	for _, tc := range precedence {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (Decisions{}).Decide(context.Background(), simulate.DecisionRequest{Decision: workflow.CompiledDecision{RuleRef: RuleClassifyPunchIntegrity}, Inputs: inputs(tc.bits...)})
			if err != nil {
				t.Fatalf("Decide: %v", err)
			}
			if got.RouteKey != tc.want {
				t.Fatalf("route = %q, want %q", got.RouteKey, tc.want)
			}
		})
	}

	first := mustRun(t, mustSetup(t, GoldenEnvironment()))
	changedEnv := GoldenEnvironment()
	changedEnv.TZDBVersion = "tzdata2026c"
	second := mustRun(t, mustSetup(t, changedEnv))
	firstProposal, ok := traceFor(first, NodeBuildPunchProposal)
	if !ok {
		t.Fatal("golden receipt has no proposal trace")
	}
	secondProposal, ok := traceFor(second, NodeBuildPunchProposal)
	if !ok {
		t.Fatal("changed-tzdb receipt has no proposal trace")
	}
	if firstProposal.OutputDigest == secondProposal.OutputDigest {
		t.Fatal("changing tzdb_version did not change the proposal digest")
	}
}

// TestTodo_CONF_011_Race is an additional deterministic concurrent walk check
// matching the other conformance fixtures' evidence discipline.
func TestTodo_CONF_011_Race(t *testing.T) {
	const n = 16
	digests := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			setup, err := NewSetup(GoldenEnvironment())
			if err != nil {
				errs[i] = err
				return
			}
			receipt, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
			if err != nil {
				errs[i] = err
				return
			}
			digests[i] = receipt.Digest()
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if digests[i] != digests[0] {
			t.Fatalf("run %d digest %q differs from run 0 digest %q", i, digests[i], digests[0])
		}
	}
}

// TestTodo_CONF_011_HiddenEffect proves the simulator refuses a compiled
// plan whose node has acquired a write effect, even if the rest of the plan
// was originally admitted as zero-effect.
func TestTodo_CONF_011_HiddenEffect(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment())
	setup.Plan.Nodes[0].EffectClass = capability.EffectExternalMutation
	if err := simulate.Admit(setup.Plan); err == nil {
		t.Fatal("Admit accepted a plan containing a hidden write effect")
	} else if code := simulate.CodeOf(err); code != simulate.CodeWriteEffectInSimulate {
		t.Fatalf("Admit refusal code = %q, want %q (%v)", code, simulate.CodeWriteEffectInSimulate, err)
	}
	_, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options)
	if err == nil {
		t.Fatal("Run accepted a plan containing a hidden write effect")
	}
	if !errors.Is(err, simulate.ErrSimulate) {
		t.Fatalf("refusal does not unwrap to ErrSimulate: %v", err)
	}
}

func equalSets(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(want))
	for _, value := range want {
		seen[value] = true
	}
	for _, value := range got {
		if !seen[value] {
			return false
		}
		delete(seen, value)
	}
	return len(seen) == 0
}
