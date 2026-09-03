package lifecycle

import (
	"errors"
	"fmt"
	"sort"
)

// ErrUnknownState reports a token that does not name a state of its dimension.
var ErrUnknownState = errors.New("lifecycle: unknown state")

// StateID is the canonical wire spelling of one state, for example "DRAFT".
// Display labels are never state identity.
type StateID string

// Dimension names one of the five lifecycle dimensions. There are exactly five;
// see [AllDimensions].
type Dimension string

// The five dimensions. No sixth dimension exists, and no collapsed universal
// status exists.
const (
	DimensionRequest     Dimension = "RequestState"
	DimensionExecution   Dimension = "ExecutionState"
	DimensionBusiness    Dimension = "BusinessState"
	DimensionConsistency Dimension = "ConsistencyState"
	DimensionObligation  Dimension = "ObligationState"
)

// AllDimensions returns the five dimensions in their canonical order.
func AllDimensions() []Dimension {
	return []Dimension{
		DimensionRequest,
		DimensionExecution,
		DimensionBusiness,
		DimensionConsistency,
		DimensionObligation,
	}
}

// RequestState answers "where is the request itself?". Numeric identity matches
// hcmnext.intents.v1.RequestState.
type RequestState uint8

// RequestState values.
const (
	RequestUnspecified RequestState = 0
	RequestDraft       RequestState = 1
	RequestPreflighted RequestState = 2
	RequestSimulated   RequestState = 3
	RequestSubmitted   RequestState = 4
	RequestApproved    RequestState = 5
	RequestRejected    RequestState = 6
	RequestWithdrawn   RequestState = 7
	RequestCancelled   RequestState = 8
	RequestSuperseded  RequestState = 9
	RequestClosed      RequestState = 10
	RequestReopened    RequestState = 11
)

var requestNames = map[RequestState]StateID{
	RequestUnspecified: "UNSPECIFIED",
	RequestDraft:       "DRAFT",
	RequestPreflighted: "PREFLIGHTED",
	RequestSimulated:   "SIMULATED",
	RequestSubmitted:   "SUBMITTED",
	RequestApproved:    "APPROVED",
	RequestRejected:    "REJECTED",
	RequestWithdrawn:   "WITHDRAWN",
	RequestCancelled:   "CANCELLED",
	RequestSuperseded:  "SUPERSEDED",
	RequestClosed:      "CLOSED",
	RequestReopened:    "REOPENED",
}

// ExecutionState answers "what has the runtime done?". Numeric identity matches
// hcmnext.intents.v1.ExecutionState.
type ExecutionState uint8

// ExecutionState values.
const (
	ExecutionUnspecified    ExecutionState = 0
	ExecutionNotPlanned     ExecutionState = 1
	ExecutionScheduled      ExecutionState = 2
	ExecutionRevalidating   ExecutionState = 3
	ExecutionExecuting      ExecutionState = 4
	ExecutionCommitted      ExecutionState = 5
	ExecutionBlocked        ExecutionState = 6
	ExecutionRepairRequired ExecutionState = 7
)

var executionNames = map[ExecutionState]StateID{
	ExecutionUnspecified:    "UNSPECIFIED",
	ExecutionNotPlanned:     "NOT_PLANNED",
	ExecutionScheduled:      "SCHEDULED",
	ExecutionRevalidating:   "REVALIDATING",
	ExecutionExecuting:      "EXECUTING",
	ExecutionCommitted:      "COMMITTED",
	ExecutionBlocked:        "BLOCKED",
	ExecutionRepairRequired: "REPAIR_REQUIRED",
}

// BusinessState answers "did the business outcome happen?". Numeric identity
// matches hcmnext.intents.v1.BusinessState.
type BusinessState uint8

// BusinessState values.
const (
	BusinessUnspecified BusinessState = 0
	BusinessNotStarted  BusinessState = 1
	BusinessInProgress  BusinessState = 2
	BusinessCompleted   BusinessState = 3
	BusinessNotAchieved BusinessState = 4
	BusinessCorrected   BusinessState = 5
	BusinessUnknown     BusinessState = 6
)

var businessNames = map[BusinessState]StateID{
	BusinessUnspecified: "UNSPECIFIED",
	BusinessNotStarted:  "NOT_STARTED",
	BusinessInProgress:  "IN_PROGRESS",
	BusinessCompleted:   "COMPLETED",
	BusinessNotAchieved: "NOT_ACHIEVED",
	BusinessCorrected:   "CORRECTED",
	BusinessUnknown:     "UNKNOWN",
}

// ConsistencyState answers "does observed external state agree with intent?".
// REPAIRING is the reconciliation-in-progress value; there is no separate
// reconciliation dimension. Numeric identity matches
// hcmnext.intents.v1.ConsistencyState.
type ConsistencyState uint8

// ConsistencyState values.
const (
	ConsistencyUnspecified        ConsistencyState = 0
	ConsistencyNotApplicable      ConsistencyState = 1
	ConsistencyPendingObservation ConsistencyState = 2
	ConsistencyConsistent         ConsistencyState = 3
	ConsistencyDegraded           ConsistencyState = 4
	ConsistencyRepairing          ConsistencyState = 5
	ConsistencyUnknown            ConsistencyState = 6
)

var consistencyNames = map[ConsistencyState]StateID{
	ConsistencyUnspecified:        "UNSPECIFIED",
	ConsistencyNotApplicable:      "NOT_APPLICABLE",
	ConsistencyPendingObservation: "PENDING_OBSERVATION",
	ConsistencyConsistent:         "CONSISTENT",
	ConsistencyDegraded:           "DEGRADED",
	ConsistencyRepairing:          "REPAIRING",
	ConsistencyUnknown:            "UNKNOWN",
}

// ObligationState answers "are attached obligations discharged?". Numeric
// identity matches hcmnext.intents.v1.ObligationState, including the reserved
// gap at 6 where DISPUTED used to sit.
type ObligationState uint8

// ObligationState values. 6 is reserved and is never a legal value.
const (
	ObligationUnspecified   ObligationState = 0
	ObligationNotApplicable ObligationState = 1
	ObligationPending       ObligationState = 2
	ObligationSatisfied     ObligationState = 3
	ObligationOverdue       ObligationState = 4
	ObligationWaived        ObligationState = 5
	ObligationUnknown       ObligationState = 7
)

var obligationNames = map[ObligationState]StateID{
	ObligationUnspecified:   "UNSPECIFIED",
	ObligationNotApplicable: "NOT_APPLICABLE",
	ObligationPending:       "PENDING",
	ObligationSatisfied:     "SATISFIED",
	ObligationOverdue:       "OVERDUE",
	ObligationWaived:        "WAIVED",
	ObligationUnknown:       "UNKNOWN",
}

// String returns the canonical state id, or a diagnostic form for a value
// outside the declared set.
func (s RequestState) String() string { return nameOf(requestNames, uint8(s), "RequestState") }

// Valid reports whether s is a declared RequestState.
func (s RequestState) Valid() bool { _, ok := requestNames[s]; return ok }

// StateID returns the canonical state id.
func (s RequestState) StateID() StateID { return requestNames[s] }

// String returns the canonical state id.
func (s ExecutionState) String() string { return nameOf(executionNames, uint8(s), "ExecutionState") }

// Valid reports whether s is a declared ExecutionState.
func (s ExecutionState) Valid() bool { _, ok := executionNames[s]; return ok }

// StateID returns the canonical state id.
func (s ExecutionState) StateID() StateID { return executionNames[s] }

// String returns the canonical state id.
func (s BusinessState) String() string { return nameOf(businessNames, uint8(s), "BusinessState") }

// Valid reports whether s is a declared BusinessState.
func (s BusinessState) Valid() bool { _, ok := businessNames[s]; return ok }

// StateID returns the canonical state id.
func (s BusinessState) StateID() StateID { return businessNames[s] }

// String returns the canonical state id.
func (s ConsistencyState) String() string {
	return nameOf(consistencyNames, uint8(s), "ConsistencyState")
}

// Valid reports whether s is a declared ConsistencyState.
func (s ConsistencyState) Valid() bool { _, ok := consistencyNames[s]; return ok }

// StateID returns the canonical state id.
func (s ConsistencyState) StateID() StateID { return consistencyNames[s] }

// String returns the canonical state id.
func (s ObligationState) String() string { return nameOf(obligationNames, uint8(s), "ObligationState") }

// Valid reports whether s is a declared ObligationState.
func (s ObligationState) Valid() bool { _, ok := obligationNames[s]; return ok }

// StateID returns the canonical state id.
func (s ObligationState) StateID() StateID { return obligationNames[s] }

func nameOf[T ~uint8](table map[T]StateID, v uint8, dimension string) string {
	if id, ok := table[T(v)]; ok {
		return string(id)
	}
	return fmt.Sprintf("%s(%d)", dimension, v)
}

// ParseRequestState resolves a canonical state id.
func ParseRequestState(id StateID) (RequestState, error) { return parseState(requestNames, id) }

// ParseExecutionState resolves a canonical state id.
func ParseExecutionState(id StateID) (ExecutionState, error) { return parseState(executionNames, id) }

// ParseBusinessState resolves a canonical state id.
func ParseBusinessState(id StateID) (BusinessState, error) { return parseState(businessNames, id) }

// ParseConsistencyState resolves a canonical state id.
func ParseConsistencyState(id StateID) (ConsistencyState, error) {
	return parseState(consistencyNames, id)
}

// ParseObligationState resolves a canonical state id.
func ParseObligationState(id StateID) (ObligationState, error) {
	return parseState(obligationNames, id)
}

func parseState[T ~uint8](table map[T]StateID, id StateID) (T, error) {
	for v, name := range table {
		if name == id {
			return v, nil
		}
	}
	var zero T
	return zero, fmt.Errorf("%w: %q", ErrUnknownState, string(id))
}

func statesOf[T ~uint8](table map[T]StateID) []StateID {
	out := make([]StateID, 0, len(table))
	for _, id := range table {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// StatesOf lists the declared state ids of one dimension, sorted. UNSPECIFIED
// is included because it is a declarable wire value; it is nevertheless illegal
// for a persisted active instance (see [RuleUnspecifiedInvalid]).
func StatesOf(d Dimension) []StateID {
	switch d {
	case DimensionRequest:
		return statesOf(requestNames)
	case DimensionExecution:
		return statesOf(executionNames)
	case DimensionBusiness:
		return statesOf(businessNames)
	case DimensionConsistency:
		return statesOf(consistencyNames)
	case DimensionObligation:
		return statesOf(obligationNames)
	default:
		return nil
	}
}

// Dimensions is the complete lifecycle state of one intent instance.
//
// It has exactly five fields, one per dimension, and deliberately carries no
// universal status field: runtime commit, business completion, external
// consistency and obligations may legitimately differ, and the product renders
// all five.
type Dimensions struct {
	Request     RequestState
	Execution   ExecutionState
	Business    BusinessState
	Consistency ConsistencyState
	Obligation  ObligationState
}

// State returns the canonical state id this tuple carries in one dimension.
func (d Dimensions) State(dim Dimension) StateID {
	switch dim {
	case DimensionRequest:
		return d.Request.StateID()
	case DimensionExecution:
		return d.Execution.StateID()
	case DimensionBusiness:
		return d.Business.StateID()
	case DimensionConsistency:
		return d.Consistency.StateID()
	case DimensionObligation:
		return d.Obligation.StateID()
	default:
		return ""
	}
}

// Validate reports whether every dimension carries a declared value. It does
// not evaluate legality; see [Check].
func (d Dimensions) Validate() error {
	if !d.Request.Valid() {
		return fmt.Errorf("%w: RequestState(%d)", ErrUnknownState, uint8(d.Request))
	}
	if !d.Execution.Valid() {
		return fmt.Errorf("%w: ExecutionState(%d)", ErrUnknownState, uint8(d.Execution))
	}
	if !d.Business.Valid() {
		return fmt.Errorf("%w: BusinessState(%d)", ErrUnknownState, uint8(d.Business))
	}
	if !d.Consistency.Valid() {
		return fmt.Errorf("%w: ConsistencyState(%d)", ErrUnknownState, uint8(d.Consistency))
	}
	if !d.Obligation.Valid() {
		return fmt.Errorf("%w: ObligationState(%d)", ErrUnknownState, uint8(d.Obligation))
	}
	return nil
}

// String renders all five dimensions; no single value substitutes for the set.
func (d Dimensions) String() string {
	return fmt.Sprintf("request=%s execution=%s business=%s consistency=%s obligation=%s",
		d.Request, d.Execution, d.Business, d.Consistency, d.Obligation)
}
