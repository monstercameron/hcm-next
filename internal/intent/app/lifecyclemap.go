package app

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

// LifecycleColumns projects the five lifecycle dimensions onto the five
// columns migrations/00004 declares, in the order that migration declares them.
//
// It lives here rather than in a store adapter because the projection is a
// kernel fact, not a storage detail: intent_instance has exactly five state
// columns and no universal status column, and an adapter that invented a
// sixth, or collapsed two, would be contradicting the kernel rather than
// persisting it.
func LifecycleColumns(d lifecycle.Dimensions) (request, execution, business, consistency, obligation string) {
	return string(d.State(lifecycle.DimensionRequest)),
		string(d.State(lifecycle.DimensionExecution)),
		string(d.State(lifecycle.DimensionBusiness)),
		string(d.State(lifecycle.DimensionConsistency)),
		string(d.State(lifecycle.DimensionObligation))
}

// LifecycleFromColumns is the inverse. Every state has to parse: a row whose
// dimension does not name a known state is corrupt, not a default.
func LifecycleFromColumns(request, execution, business, consistency, obligation string) (lifecycle.Dimensions, error) {
	var (
		out lifecycle.Dimensions
		err error
	)
	if out.Request, err = lifecycle.ParseRequestState(lifecycle.StateID(request)); err != nil {
		return lifecycle.Dimensions{}, fmt.Errorf("app: request_state %q: %w", request, err)
	}
	if out.Execution, err = lifecycle.ParseExecutionState(lifecycle.StateID(execution)); err != nil {
		return lifecycle.Dimensions{}, fmt.Errorf("app: execution_state %q: %w", execution, err)
	}
	if out.Business, err = lifecycle.ParseBusinessState(lifecycle.StateID(business)); err != nil {
		return lifecycle.Dimensions{}, fmt.Errorf("app: business_state %q: %w", business, err)
	}
	if out.Consistency, err = lifecycle.ParseConsistencyState(lifecycle.StateID(consistency)); err != nil {
		return lifecycle.Dimensions{}, fmt.Errorf("app: consistency_state %q: %w", consistency, err)
	}
	if out.Obligation, err = lifecycle.ParseObligationState(lifecycle.StateID(obligation)); err != nil {
		return lifecycle.Dimensions{}, fmt.Errorf("app: obligation_state %q: %w", obligation, err)
	}
	return out, nil
}
