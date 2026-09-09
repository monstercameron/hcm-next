package intent

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

func TestTodo_INTENT_008(t *testing.T) {
	policy := CompletionPolicy{ID: "promotion/v1", BusinessCompleteState: []lifecycle.BusinessState{lifecycle.BusinessCompleted},
		ConsistencyFloor: lifecycle.ConsistencyConsistent, RequireNoOpenRepair: true, RequireNoOpenObligation: true}
	facts := CompletionFacts{Dimensions: lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionCommitted,
		Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyConsistent, Obligation: lifecycle.ObligationSatisfied},
		EvidenceRefs: []string{"terminal:1"}, Owner: "principal:operator", ReopenConditions: []string{"new_observation"}}
	decision, err := EvaluateCompletion(policy, facts)
	if err != nil || decision.Status != CompletionClosed || len(decision.Satisfied) != 3 || len(decision.EvidenceRefs) != 1 {
		t.Fatalf("completion = %+v, err=%v", decision, err)
	}
	degraded := facts
	degraded.Dimensions.Consistency = lifecycle.ConsistencyDegraded
	degraded.OpenRepair = true
	_, refusalErr := EvaluateCompletion(policy, degraded)
	if !errors.Is(refusalErr, ErrCompletionRefused) {
		t.Fatalf("open repair error = %v, want typed refusal", refusalErr)
	}
	if refusal := new(CompletionRefusal); !errors.As(refusalErr, &refusal) || refusal.Dimension != lifecycle.DimensionConsistency {
		t.Fatalf("open repair refusal = %v, want ConsistencyState", refusalErr)
	}
}

func TestTodo_INTENT_008_Integration(t *testing.T) {
	policy := CompletionPolicy{ID: "promotion/degraded/v1", BusinessCompleteState: []lifecycle.BusinessState{lifecycle.BusinessCompleted},
		ConsistencyFloor: lifecycle.ConsistencyDegraded, RequireNoOpenRepair: true, RequireNoOpenObligation: true}
	decision, err := EvaluateCompletion(policy, CompletionFacts{Dimensions: lifecycle.Dimensions{
		Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionCommitted, Business: lifecycle.BusinessCompleted,
		Consistency: lifecycle.ConsistencyDegraded, Obligation: lifecycle.ObligationSatisfied}})
	if err != nil || decision.Status != CompletionClosedDegraded {
		t.Fatalf("degraded closure = %+v, err=%v, want CLOSED_DEGRADED", decision, err)
	}
}

func TestTodo_INTENT_008_Mutation(t *testing.T) {
	policy := CompletionPolicy{ID: "promotion/v1", BusinessCompleteState: []lifecycle.BusinessState{lifecycle.BusinessCompleted},
		ConsistencyFloor: lifecycle.ConsistencyConsistent, RequireNoOpenRepair: true, RequireNoOpenObligation: true}
	facts := CompletionFacts{Dimensions: lifecycle.Dimensions{Request: lifecycle.RequestApproved, Execution: lifecycle.ExecutionCommitted,
		Business: lifecycle.BusinessCompleted, Consistency: lifecycle.ConsistencyConsistent, Obligation: lifecycle.ObligationPending}}
	decision, err := EvaluateCompletion(policy, facts)
	if decision.Status != CompletionBlockedObligation || !errors.Is(err, ErrCompletionRefused) {
		t.Fatalf("pending obligation = %+v, err=%v, want BLOCKED_OBLIGATION refusal", decision, err)
	}
}
