// Package presentation compiles lifecycle dimensions into a truthful,
// participant-facing projection. It never owns lifecycle or authorization.
package presentation

import (
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"slices"
)

type Authorization string

const (
	AuthorizationUnknown Authorization = "UNKNOWN"
	AuthorizationAllowed Authorization = "ALLOWED"
	AuthorizationDenied  Authorization = "DENIED"
	AuthorizationExpired Authorization = "EXPIRED"
)

type Freshness string

const (
	FreshnessUnknown Freshness = "UNKNOWN"
	FreshnessFresh   Freshness = "FRESH"
	FreshnessStale   Freshness = "STALE"
	FreshnessPartial Freshness = "PARTIAL"
)

type OperationalState string

const (
	OperationalReady    OperationalState = "READY"
	OperationalLoading  OperationalState = "LOADING"
	OperationalWaiting  OperationalState = "WAITING"
	OperationalDegraded OperationalState = "DEGRADED"
	OperationalUnknown  OperationalState = "UNKNOWN"
)

type State string

const (
	StateLoading           State = "LOADING"
	StateEmpty             State = "EMPTY"
	StateDraft             State = "DRAFT"
	StateValidationBlocked State = "VALIDATION_BLOCKED"
	StateGovernanceDenied  State = "GOVERNANCE_DENIED"
	StateSimulationReady   State = "SIMULATION_READY"
	StateSubmitted         State = "SUBMITTED"
	StateRunning           State = "RUNNING"
	StateWaiting           State = "WAITING"
	StateStaleReplan       State = "STALE_REPLAN"
	StatePartialDegraded   State = "PARTIAL_DEGRADED"
	StateUnknownAmbiguous  State = "UNKNOWN_AMBIGUOUS"
	StateRepairRequired    State = "REPAIR_REQUIRED"
	StateCompleted         State = "COMPLETED"
	StateCancelled         State = "CANCELLED"
	StateCorrected         State = "CORRECTED"
	StateSuperseded        State = "SUPERSEDED"
)

type Action string

const (
	ActionWait         Action = "WAIT"
	ActionRefresh      Action = "REFRESH"
	ActionRequestHelp  Action = "REQUEST_HELP"
	ActionCancelIfSafe Action = "CANCEL_IF_SAFE"
	ActionCorrect      Action = "CORRECT"
	ActionOpenRepair   Action = "OPEN_REPAIR"
	ActionReview       Action = "REVIEW"
	ActionSubmit       Action = "SUBMIT"
	ActionSaveDraft    Action = "SAVE_DRAFT"
	ActionReplan       Action = "REPLAN"
	ActionAppeal       Action = "APPEAL"
	ActionInvestigate  Action = "INVESTIGATE"
)

// Input contains every truth-bearing dimension used by this projection.
type Input struct {
	Dimensions           lifecycle.Dimensions
	Authorization        Authorization
	Freshness            Freshness
	Operational          OperationalState
	HasResult            bool
	HasValidationErrors  bool
	HasSimulation        bool
	HasEvidence          bool
	ExternalOutcomeKnown bool
	EvidenceRefs         []string
}

type ActionDescriptor struct {
	Action  Action
	Enabled bool
	Reason  string
}
type Presentation struct {
	State        State
	Dimensions   lifecycle.Dimensions
	EvidenceRefs []string
	Actions      []ActionDescriptor
}

// Resolve is deterministic and copies all caller-owned slices.
func Resolve(in Input) Presentation {
	p := Presentation{State: stateFor(in), Dimensions: in.Dimensions, EvidenceRefs: slices.Clone(in.EvidenceRefs)}
	if p.State == StateGovernanceDenied {
		p.Dimensions = lifecycle.Dimensions{}
		p.EvidenceRefs = nil
		return p
	}
	for _, a := range actionsFor(p.State) {
		d := ActionDescriptor{Action: a, Enabled: true}
		if in.Authorization != AuthorizationAllowed {
			d.Enabled = false
			d.Reason = "Action unavailable until authorization and current evidence are established"
		} else if in.Freshness != FreshnessFresh && !safeWhenStale(a) {
			d.Enabled = false
			d.Reason = "Action unavailable until authorization and current evidence are established"
		} else if in.StateActionBlocked(a) {
			d.Enabled = false
			d.Reason = "Action is not safe in the current state"
		}
		p.Actions = append(p.Actions, d)
	}
	return p
}

func (in Input) StateActionBlocked(a Action) bool {
	if a == ActionSubmit && in.HasValidationErrors {
		return true
	}
	if (a == ActionCancelIfSafe || a == ActionCorrect) && in.Dimensions.Execution == lifecycle.ExecutionCommitted {
		return true
	}
	if a == ActionRefresh && in.Operational == OperationalLoading {
		return true
	}
	return false
}

func stateFor(in Input) State {
	if in.Authorization == AuthorizationDenied || in.Authorization == AuthorizationExpired {
		return StateGovernanceDenied
	}
	if in.Operational == OperationalLoading {
		return StateLoading
	}
	if in.Freshness == FreshnessStale {
		return StateStaleReplan
	}
	if in.Freshness == FreshnessPartial || in.Dimensions.Consistency == lifecycle.ConsistencyDegraded {
		return StatePartialDegraded
	}
	if in.Dimensions.Business == lifecycle.BusinessUnknown || in.Dimensions.Consistency == lifecycle.ConsistencyUnknown || !in.ExternalOutcomeKnown && in.Dimensions.Execution == lifecycle.ExecutionCommitted {
		return StateUnknownAmbiguous
	}
	if in.Dimensions.Execution == lifecycle.ExecutionRepairRequired || in.Dimensions.Consistency == lifecycle.ConsistencyRepairing {
		return StateRepairRequired
	}
	if in.HasValidationErrors {
		return StateValidationBlocked
	}
	if in.HasSimulation {
		return StateSimulationReady
	}
	if in.Dimensions.Execution == lifecycle.ExecutionExecuting {
		return StateRunning
	}
	if in.Dimensions.Consistency == lifecycle.ConsistencyPendingObservation || in.Dimensions.Obligation == lifecycle.ObligationPending {
		return StateWaiting
	}
	switch in.Dimensions.Request {
	case lifecycle.RequestUnspecified:
		return StateEmpty
	case lifecycle.RequestDraft:
		if !in.HasResult {
			return StateEmpty
		}
		return StateDraft
	case lifecycle.RequestSubmitted:
		return StateSubmitted
	case lifecycle.RequestClosed:
		return StateCompleted
	case lifecycle.RequestCancelled:
		return StateCancelled
	case lifecycle.RequestSuperseded:
		return StateSuperseded
	case lifecycle.RequestReopened:
		return StateCorrected
	}
	if !in.HasResult {
		return StateEmpty
	}
	return StateSubmitted
}

func safeWhenStale(a Action) bool {
	switch a {
	case ActionWait, ActionRefresh, ActionRequestHelp, ActionReview, ActionInvestigate, ActionOpenRepair, ActionReplan:
		return true
	}
	return false
}

func actionsFor(s State) []Action {
	switch s {
	case StateLoading:
		return []Action{ActionWait, ActionCancelIfSafe}
	case StateEmpty:
		return []Action{ActionRequestHelp}
	case StateDraft:
		return []Action{ActionSaveDraft, ActionSubmit}
	case StateValidationBlocked:
		return []Action{ActionCorrect, ActionSaveDraft}
	case StateGovernanceDenied:
		return nil
	case StateSimulationReady:
		return []Action{ActionSubmit, ActionReview}
	case StateSubmitted, StateRunning:
		return []Action{ActionWait, ActionCancelIfSafe, ActionReview}
	case StateWaiting:
		return []Action{ActionWait, ActionRefresh, ActionRequestHelp}
	case StateStaleReplan:
		return []Action{ActionRefresh, ActionReplan, ActionRequestHelp}
	case StatePartialDegraded:
		return []Action{ActionRefresh, ActionOpenRepair, ActionRequestHelp}
	case StateUnknownAmbiguous:
		return []Action{ActionInvestigate, ActionRefresh, ActionRequestHelp}
	case StateRepairRequired:
		return []Action{ActionOpenRepair, ActionReview, ActionRequestHelp}
	case StateCompleted:
		return []Action{ActionReview, ActionCorrect}
	case StateCancelled, StateCorrected, StateSuperseded:
		return []Action{ActionReview, ActionRequestHelp}
	default:
		return nil
	}
}

// Actions returns a defensive copy of the action descriptors.
func (p Presentation) ActionsCopy() []ActionDescriptor { return slices.Clone(p.Actions) }
