// Package outcome translates a governed operation outcome into participant-safe
// recovery choices. It is deliberately presentation-only: it neither probes a
// provider nor retries an effect.
package outcome

import (
	"errors"
	"slices"
	"time"
)

type Kind string

// OutcomeKind is a descriptive alias for callers that prefer the longer
// contract name.
type OutcomeKind = Kind

const (
	Partial        Kind = "PARTIAL"
	Unknown        Kind = "UNKNOWN"
	Ambiguous      Kind = "AMBIGUOUS"
	RepairRequired Kind = "REPAIR_REQUIRED"
)

const (
	OutcomePartial        = Partial
	OutcomeUnknown        = Unknown
	OutcomeAmbiguous      = Ambiguous
	OutcomeRepairRequired = RepairRequired
)

type Action string

const (
	Wait         Action = "WAIT"
	Refresh      Action = "REFRESH"
	RequestHelp  Action = "REQUEST_HELP"
	CancelIfSafe Action = "CANCEL_IF_SAFE"
	Correct      Action = "CORRECT"
	OpenRepair   Action = "OPEN_REPAIR"
)

var ErrInvalid = errors.New("outcome: invalid recovery input")

// Component describes the portion of business or external truth available to
// the participant. Values are labels only; payloads and provider details do
// not cross this boundary.
type Component struct {
	Name  string
	Known bool
}

// Request is the already-classified operation/reconciliation contract. The
// caller owns classification; this package only selects safe next actions.
type Request struct {
	Kind                Kind
	Components          []Component
	ObservationAt       time.Time
	ObservationDeadline time.Time
	Now                 time.Time
	LastSafeOperation   string
	EffectApplied       bool
	RepairReference     string
}

type RecoveryRequest = Request

type Recovery struct {
	Kind              Kind
	KnownComponents   []string
	UnknownComponents []string
	ObservationFresh  bool
	DeadlineExceeded  bool
	LastSafeOperation string
	Actions           []Action
	Reason            string
}

type RecoveryPlan = Recovery

// Recover deterministically presents participant-safe choices. In particular,
// it never emits RETRY, RESUBMIT, or another effect-producing action: an
// unknown/ambiguous provider result may already have applied the effect.
func Recover(req Request) (Recovery, error) {
	if req.Kind != Partial && req.Kind != Unknown && req.Kind != Ambiguous && req.Kind != RepairRequired {
		return Recovery{}, ErrInvalid
	}
	if req.Now.IsZero() {
		req.Now = time.Now()
	}
	if req.LastSafeOperation == "" {
		return Recovery{}, ErrInvalid
	}
	r := Recovery{Kind: req.Kind, LastSafeOperation: req.LastSafeOperation}
	for _, c := range req.Components {
		if c.Name == "" {
			return Recovery{}, ErrInvalid
		}
		if c.Known {
			r.KnownComponents = append(r.KnownComponents, c.Name)
		} else {
			r.UnknownComponents = append(r.UnknownComponents, c.Name)
		}
	}
	if !req.ObservationAt.IsZero() {
		r.ObservationFresh = req.ObservationDeadline.IsZero() || req.Now.Before(req.ObservationDeadline)
	}
	r.DeadlineExceeded = !req.ObservationDeadline.IsZero() && !req.Now.Before(req.ObservationDeadline)
	// Refresh is a read-only action and is always the first recovery step when
	// the observation is absent/stale; it is never a duplicate effect.
	switch req.Kind {
	case Partial:
		r.Actions = append(r.Actions, Wait, Refresh, RequestHelp)
		r.Reason = "some components are known; verification remains open"
	case Unknown:
		r.Actions = append(r.Actions, Refresh, RequestHelp)
		r.Reason = "outcome is unknown; do not repeat the operation"
	case Ambiguous:
		r.Actions = append(r.Actions, RequestHelp, OpenRepair)
		r.Reason = "business and external truth disagree; reconcile before acting"
	case RepairRequired:
		r.Actions = append(r.Actions, OpenRepair, RequestHelp, Correct)
		r.Reason = "a governed repair is required before completion"
	}
	if r.DeadlineExceeded {
		r.Actions = remove(r.Actions, Wait)
		if !contains(r.Actions, RequestHelp) {
			r.Actions = append(r.Actions, RequestHelp)
		}
		r.Reason += "; observation deadline passed"
	}
	// Cancellation is offered only where no effect was applied and the caller
	// supplied an explicit safe operation boundary. Never infer safety from an
	// error or from a missing observation.
	if req.Kind == Partial && !req.EffectApplied && len(r.UnknownComponents) == 0 {
		r.Actions = append(r.Actions, CancelIfSafe)
	}
	r.Actions = unique(r.Actions)
	return r, nil
}

func contains(xs []Action, x Action) bool { return slices.Contains(xs, x) }
func remove(xs []Action, x Action) []Action {
	out := xs[:0]
	for _, a := range xs {
		if a != x {
			out = append(out, a)
		}
	}
	return out
}
func unique(xs []Action) []Action {
	out := make([]Action, 0, len(xs))
	for _, a := range xs {
		if !contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}
