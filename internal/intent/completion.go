package intent

import (
	"errors"
	"fmt"

	"github.com/monstercameron/hcm-next/internal/intent/lifecycle"
)

// CompletionStatus is the explicit closure-policy answer.
type CompletionStatus string

const (
	CompletionClosed            CompletionStatus = "CLOSED"
	CompletionClosedDegraded    CompletionStatus = "CLOSED_DEGRADED"
	CompletionOpenRepair        CompletionStatus = "OPEN_REPAIR"
	CompletionBlockedObligation CompletionStatus = "BLOCKED_OBLIGATION"
	CompletionUnknown           CompletionStatus = "UNKNOWN"
)

// CompletionPolicy is versioned policy data owned by an intent definition.
// It is passed explicitly so a caller cannot silently substitute a global
// default when a definition declares a different consistency floor.
type CompletionPolicy struct {
	ID                      string
	BusinessCompleteState   []lifecycle.BusinessState
	ConsistencyFloor        lifecycle.ConsistencyState
	RequireNoOpenRepair     bool
	RequireNoOpenObligation bool
}

// DefaultCompletionPolicy returns the conservative policy for a mutating
// definition. A caller may use a definition-specific policy when one is
// published; the default never treats DEGRADED or UNKNOWN as consistent.
func DefaultCompletionPolicy(def Definition) CompletionPolicy {
	policy := CompletionPolicy{
		ID:                      "hcmnext.completion.default/v1",
		BusinessCompleteState:   []lifecycle.BusinessState{lifecycle.BusinessCompleted, lifecycle.BusinessCorrected},
		ConsistencyFloor:        lifecycle.ConsistencyConsistent,
		RequireNoOpenRepair:     true,
		RequireNoOpenObligation: true,
	}
	if def.Family != FamilyChangeRequest {
		policy.ConsistencyFloor = lifecycle.ConsistencyNotApplicable
	}
	return policy
}

// CompletionFacts are the dimensions and durable open-work facts observed at
// the instant a scheduler or resume path reevaluates closure.
type CompletionFacts struct {
	Dimensions       lifecycle.Dimensions
	OpenObligation   bool
	OpenRepair       bool
	EvidenceRefs     []string
	Owner            string
	ReopenConditions []string
}

// CompletionRefusal names the exact unmet dimension or durable condition.
type CompletionRefusal struct {
	Dimension lifecycle.Dimension
	Reason    string
}

func (r *CompletionRefusal) Error() string {
	return fmt.Sprintf("intent completion refused at %s: %s", r.Dimension, r.Reason)
}

// Unwrap makes all policy refusals classifiable without matching prose.
func (r *CompletionRefusal) Unwrap() error { return ErrCompletionRefused }

var ErrCompletionRefused = errors.New("intent: completion policy refused closure")

// CompletionDecision is the durable decision a closure writer records.
type CompletionDecision struct {
	Status           CompletionStatus
	Satisfied        []lifecycle.Dimension
	Deferred         []lifecycle.Dimension
	EvidenceRefs     []string
	Owner            string
	ReopenConditions []string
}

// EvaluateCompletion applies the definition's completion policy. It never
// mutates workflow, observation, repair, or transaction history.
func EvaluateCompletion(policy CompletionPolicy, facts CompletionFacts) (CompletionDecision, error) {
	if err := facts.Dimensions.Validate(); err != nil {
		return CompletionDecision{Status: CompletionUnknown, Deferred: []lifecycle.Dimension{lifecycle.DimensionBusiness}}, &CompletionRefusal{Dimension: lifecycle.DimensionBusiness, Reason: err.Error()}
	}
	if len(policy.BusinessCompleteState) == 0 {
		policy.BusinessCompleteState = []lifecycle.BusinessState{lifecycle.BusinessCompleted}
	}
	if !containsBusiness(policy.BusinessCompleteState, facts.Dimensions.Business) {
		return CompletionDecision{Status: CompletionUnknown, Deferred: []lifecycle.Dimension{lifecycle.DimensionBusiness}}, &CompletionRefusal{Dimension: lifecycle.DimensionBusiness,
			Reason: "business outcome is not complete"}
	}
	if policy.RequireNoOpenObligation && (facts.OpenObligation || !obligationSettled(facts.Dimensions.Obligation)) {
		return CompletionDecision{Status: CompletionBlockedObligation, Deferred: []lifecycle.Dimension{lifecycle.DimensionObligation}}, &CompletionRefusal{Dimension: lifecycle.DimensionObligation,
			Reason: "an obligation remains open"}
	}
	if policy.RequireNoOpenRepair && facts.OpenRepair {
		return CompletionDecision{Status: CompletionOpenRepair, Deferred: []lifecycle.Dimension{lifecycle.DimensionConsistency}}, &CompletionRefusal{Dimension: lifecycle.DimensionConsistency,
			Reason: "a required repair remains open"}
	}
	if !meetsConsistencyFloor(facts.Dimensions.Consistency, policy.ConsistencyFloor) {
		status := CompletionUnknown
		if facts.Dimensions.Consistency == lifecycle.ConsistencyDegraded {
			status = CompletionOpenRepair
		}
		return CompletionDecision{Status: status, Deferred: []lifecycle.Dimension{lifecycle.DimensionConsistency}}, &CompletionRefusal{Dimension: lifecycle.DimensionConsistency,
			Reason: fmt.Sprintf("consistency %s is below policy floor %s", facts.Dimensions.Consistency, policy.ConsistencyFloor)}
	}
	status := CompletionClosed
	if facts.Dimensions.Consistency == lifecycle.ConsistencyDegraded {
		status = CompletionClosedDegraded
	}
	return CompletionDecision{Status: status,
		Satisfied:    []lifecycle.Dimension{lifecycle.DimensionBusiness, lifecycle.DimensionConsistency, lifecycle.DimensionObligation},
		EvidenceRefs: append([]string(nil), facts.EvidenceRefs...), Owner: facts.Owner,
		ReopenConditions: append([]string(nil), facts.ReopenConditions...)}, nil
}

func containsBusiness(states []lifecycle.BusinessState, state lifecycle.BusinessState) bool {
	for _, candidate := range states {
		if candidate == state {
			return true
		}
	}
	return false
}

func obligationSettled(state lifecycle.ObligationState) bool {
	return state == lifecycle.ObligationSatisfied || state == lifecycle.ObligationWaived || state == lifecycle.ObligationNotApplicable
}

func meetsConsistencyFloor(state, floor lifecycle.ConsistencyState) bool {
	if state == lifecycle.ConsistencyUnknown || state == lifecycle.ConsistencyPendingObservation || state == lifecycle.ConsistencyRepairing {
		return false
	}
	if floor == lifecycle.ConsistencyNotApplicable {
		return state == lifecycle.ConsistencyNotApplicable
	}
	return state == lifecycle.ConsistencyConsistent || (state == lifecycle.ConsistencyDegraded && floor == lifecycle.ConsistencyDegraded)
}
