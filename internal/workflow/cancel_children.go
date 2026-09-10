package workflow

import (
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/subworkflow"
)

// PropagatingChild is one child facing parent cancellation: its pinned
// ref, state, stoppability, whether it detached, its detached obligation
// and whether its outcome is mandatory for closure.
type PropagatingChild struct {
	Ref         subworkflow.ChildRef   `json:"ref"`
	State       subworkflow.ChildState `json:"state"`
	Cancellable bool                   `json:"cancellable"`
	Detached    bool                   `json:"detached"`
	Obligation  subworkflow.Obligation `json:"obligation"`
	Mandatory   bool                   `json:"mandatory"`
}

// ParentCancellationRequest closes one parent over its children.
type ParentCancellationRequest struct {
	RunID    string             `json:"run_id"`
	Revision string             `json:"revision"`
	Children []PropagatingChild `json:"children"`
}

// ChildPropagationOutcome reconciles every child: per-child reports, the
// mandatory completion and the retained detached obligations.
type ChildPropagationOutcome struct {
	Decision    CancellationDecision         `json:"decision"`
	Reports     []ChildCancellation          `json:"reports"`
	Completion  subworkflow.ParentCompletion `json:"completion"`
	Obligations []subworkflow.Obligation     `json:"obligations"`
}

// PropagateCancellationToChildren cancels one parent across its children.
// Detached children keep accountable owners: an ownerless detached child
// refuses closure. Unknown mandatory states refuse; unknown optional
// states repair. Child truth is reconciled, never overwritten.
func PropagateCancellationToChildren(req ParentCancellationRequest) (ChildPropagationOutcome, error) {
	if strings.TrimSpace(req.RunID) == "" || strings.TrimSpace(req.Revision) == "" {
		return ChildPropagationOutcome{}, errors.New("workflow: PropagateCancellationToChildren needs run and revision")
	}
	var outcome ChildPropagationOutcome
	var truths []subworkflow.ChildTruth
	var mandatory []subworkflow.ChildRef
	repair := false
	compensated := false
	coords := false
	for _, child := range req.Children {
		if child.Detached {
			if child.Obligation.Child != child.Ref || strings.TrimSpace(child.Obligation.Correlation) == "" ||
				strings.TrimSpace(child.Obligation.IdempotencyKey) == "" {
				return ChildPropagationOutcome{}, errors.New("workflow: detached child lost its accountable owner")
			}
			outcome.Obligations = append(outcome.Obligations, child.Obligation)
		}
		report, err := subworkflow.PropagateCancellation(subworkflow.CancellableChild{
			Ref: child.Ref, State: child.State, Cancellable: child.Cancellable,
		})
		if err != nil {
			if child.Mandatory {
				return ChildPropagationOutcome{}, err
			}
			repair = true
			continue
		}
		outcome.Reports = append(outcome.Reports, ChildCancellation{Ref: child.Ref, Report: report})
		truths = append(truths, subworkflow.ChildTruth{Child: child.Ref, Outcome: report, Mandatory: child.Mandatory})
		if child.Mandatory {
			mandatory = append(mandatory, child.Ref)
		}
		if report == subworkflow.ReportCompensated {
			compensated = true
		}
		if report == subworkflow.ReportCannotCancel {
			coords = true
		}
	}
	completion, err := subworkflow.CompleteParent(truths, mandatory)
	if err != nil {
		return ChildPropagationOutcome{}, err
	}
	outcome.Completion = completion
	switch {
	case repair:
		outcome.Decision = RepairRequired
	case coords:
		outcome.Decision = CannotCancel
	case compensated:
		outcome.Decision = CompensationRequired
	default:
		outcome.Decision = Cancelled
	}
	return outcome, nil
}
