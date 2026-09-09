package app

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

func TestOutcomeReceiptFromExecution(t *testing.T) {
	inst := intent.Instance{IntentID: "intent-1"}
	receipt, ok, err := OutcomeReceiptFromExecution(inst, ExecutionResult{InstanceID: "workflow-1", VisitedNodes: []string{"end_complete"}}, time.Unix(1, 0))
	if err != nil || !ok {
		t.Fatalf("receipt = %+v, ok=%t, err=%v", receipt, ok, err)
	}
	if receipt.Dimensions.Execution != lifecycle.ExecutionCommitted || receipt.Reconciliation != intent.ReconciliationPass {
		t.Fatalf("receipt = %+v, want committed/pass", receipt)
	}
}

func TestOutcomeReceiptFromExecutionParked(t *testing.T) {
	_, ok, err := OutcomeReceiptFromExecution(intent.Instance{IntentID: "intent-1"}, ExecutionResult{Parked: true}, time.Unix(1, 0))
	if err != nil || ok {
		t.Fatalf("parked outcome = ok=%t, err=%v, want no receipt", ok, err)
	}
}

func TestOutcomeReceiptFromExecutionDerivesTerminalRefs(t *testing.T) {
	inst := intent.Instance{IntentID: "intent-1"}
	committed, ok, err := OutcomeReceiptFromExecution(inst, ExecutionResult{InstanceID: "workflow-1", VisitedNodes: []string{"execute_promotion", "end_complete"}}, time.Unix(1, 0))
	if err != nil || !ok {
		t.Fatalf("committed receipt = %+v, ok=%t, err=%v", committed, ok, err)
	}
	if committed.CommitReceiptRef != "receipt.promotion.execute/v1:workflow-1" || committed.RepairRef != "" {
		t.Fatalf("committed refs = %q / %q, want the plan's receipt ref qualified by the instance", committed.CommitReceiptRef, committed.RepairRef)
	}
	if err := committed.Validate(); err != nil {
		t.Fatalf("committed receipt must validate: %v", err)
	}

	repair, ok, err := OutcomeReceiptFromExecution(inst, ExecutionResult{InstanceID: "workflow-2", VisitedNodes: []string{"observe_payroll", "end_repair_plan"}}, time.Unix(1, 0))
	if err != nil || !ok {
		t.Fatalf("repair receipt = %+v, ok=%t, err=%v", repair, ok, err)
	}
	if repair.RepairRef != "repair.promotion.execute/v1:workflow-2" {
		t.Fatalf("repair refs = %q / %q, want the plan's repair ref qualified by the instance", repair.CommitReceiptRef, repair.RepairRef)
	}
	if err := repair.Validate(); err != nil {
		t.Fatalf("repair receipt must validate: %v", err)
	}

	supplied, _, err := OutcomeReceiptFromExecution(inst, ExecutionResult{InstanceID: "workflow-3", VisitedNodes: []string{"end_complete"}, CommitReceiptRef: "ledger:abc"}, time.Unix(1, 0))
	if err != nil || supplied.CommitReceiptRef != "ledger:abc" {
		t.Fatalf("adapter-supplied ref = %q, err=%v, want ledger:abc kept", supplied.CommitReceiptRef, err)
	}
}
