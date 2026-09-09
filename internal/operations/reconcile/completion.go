package reconcile

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
)

// CompletionStatus is RECON-002's policy result. It is separate from the
// durable job lifecycle [Status] because WAIVED is a policy decision, not a
// value that may be written into the existing job-state vocabulary.
type CompletionStatus string

const (
	CompletionPass           CompletionStatus = "PASS"
	CompletionMismatch       CompletionStatus = "MISMATCH"
	CompletionPartial        CompletionStatus = "PARTIAL"
	CompletionUnknown        CompletionStatus = "UNKNOWN"
	CompletionWaived         CompletionStatus = "WAIVED"
	CompletionRepairRequired CompletionStatus = "REPAIR_REQUIRED"
	CompletionExpired        CompletionStatus = "EXPIRED"
)

// CompletionAction tells the scheduler what durable evidence says to do next.
type CompletionAction string

const (
	ActionNone    CompletionAction = "NONE"
	ActionObserve CompletionAction = "OBSERVE"
	ActionRepair  CompletionAction = "REPAIR"
	ActionExpire  CompletionAction = "EXPIRE"
	ActionWaive   CompletionAction = "WAIVE"
)

// Route is the closed promotion observe_reconciliation route vocabulary.
type Route string

const (
	RouteConsistent Route = "CONSISTENT"
	RouteDegraded   Route = "DEGRADED"
)

// CompletionDimension is the exact terminal dimension contributed by a
// completed reconciliation evaluation.
const CompletionDimension = "EXTERNAL_CONSISTENCY"

// CompletionRequest is the complete durable evidence input to RECON-002. No
// connector, database, wall clock or provider acceptance is consulted here.
type CompletionRequest struct {
	Job        Job
	Found      bool
	Freshness  observe.Freshness
	Complete   bool
	Comparison observe.PilotComparison
	Now        time.Time
	Waived     bool
	WaiverRef  string
}

// CompletionDecision is the policy answer and the exact workflow consequence
// derived from it.
type CompletionDecision struct {
	Status               CompletionStatus
	Terminal             bool
	Route                Route
	TerminalContribution string
	NextAction           CompletionAction
	Reason               string
}

func (r CompletionRequest) validate() error {
	switch {
	case r.Job.TenantID == uuid.Nil:
		return fmt.Errorf("reconcile: completion requires a tenant")
	case r.Job.JobID == uuid.Nil:
		return fmt.Errorf("reconcile: completion requires a job")
	case r.Now.IsZero():
		return fmt.Errorf("reconcile: completion requires the caller's instant")
	case r.Job.Deadline.IsZero():
		return fmt.Errorf("reconcile: completion requires the job deadline")
	case !r.Freshness.Valid():
		return fmt.Errorf("reconcile: completion freshness %q is not declared", r.Freshness)
	case !r.Job.RequiredFreshness.Valid():
		return fmt.Errorf("reconcile: job required freshness %q is not declared", r.Job.RequiredFreshness)
	case r.Waived && r.WaiverRef == "":
		return fmt.Errorf("reconcile: a waiver requires an evidence reference")
	}
	return nil
}

// EvaluateCompletion evaluates freshness before comparison and deadline
// before another observation is spent. This ordering means a stale or
// incomplete provider read can never become a false PASS, and a mandatory
// effect cannot disappear at deadline without becoming EXPIRED or
// REPAIR_REQUIRED.
func EvaluateCompletion(req CompletionRequest) (CompletionDecision, error) {
	if err := req.validate(); err != nil {
		return CompletionDecision{}, err
	}
	if req.Waived {
		return CompletionDecision{
			Status: CompletionWaived, Terminal: true, Route: RouteDegraded,
			TerminalContribution: CompletionDimension, NextAction: ActionWaive,
			Reason: "completion was explicitly waived under " + req.WaiverRef,
		}, nil
	}
	if !req.Now.Before(req.Job.Deadline) {
		if req.Job.RepairPolicy == "" || req.Job.RepairPolicy == noRepairPolicy {
			return CompletionDecision{
				Status: CompletionExpired, Terminal: true, Route: RouteDegraded,
				TerminalContribution: CompletionDimension, NextAction: ActionExpire,
				Reason: "deadline reached without determinate evidence",
			}, nil
		}
		return CompletionDecision{
			Status: CompletionRepairRequired, Terminal: true, Route: RouteDegraded,
			TerminalContribution: CompletionDimension, NextAction: ActionRepair,
			Reason: "deadline reached; the effect declares repair policy " + req.Job.RepairPolicy,
		}, nil
	}
	if !req.Found {
		return pendingDecision(CompletionUnknown, ActionObserve, "observation did not identify the watched external state"), nil
	}
	if !meetsFreshness(req.Freshness, req.Job.RequiredFreshness) {
		return pendingDecision(StatusToCompletion(StatusObserving), ActionObserve,
			"observation freshness "+string(req.Freshness)+" does not meet required "+string(req.Job.RequiredFreshness)), nil
	}
	if !req.Complete || len(req.Comparison.Fields) != len(observe.AllPilotFields()) {
		return pendingDecision(CompletionPartial, ActionObserve,
			"observation does not cover all Promotion pilot fields"), nil
	}
	switch req.Comparison.Verdict {
	case observe.Match:
		return CompletionDecision{
			Status: CompletionPass, Terminal: true, Route: RouteConsistent,
			TerminalContribution: CompletionDimension, NextAction: ActionNone,
			Reason: "fresh complete observation matches intended and canonical values",
		}, nil
	case observe.Mismatch:
		return CompletionDecision{
			Status: CompletionMismatch, Terminal: true, Route: RouteDegraded,
			TerminalContribution: CompletionDimension, NextAction: ActionRepair,
			Reason: "fresh complete observation disagrees with intended or canonical values",
		}, nil
	case observe.Unknown:
		return pendingDecision(CompletionUnknown, ActionObserve,
			"fresh observation did not contain enough readable values to decide"), nil
	default:
		return CompletionDecision{}, fmt.Errorf("reconcile: comparison verdict %q is not declared", req.Comparison.Verdict)
	}
}

func pendingDecision(status CompletionStatus, action CompletionAction, reason string) CompletionDecision {
	return CompletionDecision{
		Status: status, Terminal: false, Route: RouteDegraded,
		TerminalContribution: "", NextAction: action, Reason: reason,
	}
}

// StatusToCompletion maps the resting job status into RECON-002's policy
// vocabulary without changing the durable RECON-001 status set.
func StatusToCompletion(status Status) CompletionStatus {
	switch status {
	case StatusPass:
		return CompletionPass
	case StatusMismatch:
		return CompletionMismatch
	case StatusPartial:
		return CompletionPartial
	case StatusUnknown:
		return CompletionUnknown
	case StatusExpired:
		return CompletionExpired
	case StatusRepairRequired:
		return CompletionRepairRequired
	default:
		return CompletionUnknown
	}
}

// Evaluate is the concise spelling used by policy ports.
func Evaluate(req CompletionRequest) (CompletionDecision, error) {
	return EvaluateCompletion(req)
}

// Explain returns the bounded policy outcome used by telemetry and promotion
// step adapters; it contains no payload or sensitive field values.
func Explain(decision CompletionDecision) string {
	return fmt.Sprintf("reconciliation completion status=%s terminal=%t route=%s contribution=%s next=%s reason=%s",
		decision.Status, decision.Terminal, decision.Route, decision.TerminalContribution,
		decision.NextAction, decision.Reason)
}
