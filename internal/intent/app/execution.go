package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/monstercameron/hcm-next/internal/humanwork/workitem"
	"github.com/monstercameron/hcm-next/internal/trust"
	"github.com/monstercameron/hcm-next/internal/workflow/frontier"
	"github.com/monstercameron/hcm-next/internal/workflow/runtime"
)

// ExecutionResult is the caller-driven workflow driver's outcome, narrowed to
// exactly what [IntentService.ExecuteIntent] needs to build a wire receipt.
//
// Its shape mirrors internal/workflow/execute.Driver's own Result closely
// enough that a composition root can adapt one to the other directly. This
// package deliberately does not import internal/workflow/execute itself: a
// port defined here, rather than that package's own request/result types,
// is what lets the composition root decide whether that import is
// architecturally admitted (tools/policy/archrules) without this package's
// compilation depending on the answer.
type ExecutionResult struct {
	// Parked is true when the driver stopped on durable human work rather
	// than completing the instance outright.
	Parked bool
	// InstanceID is the started or resumed workflow instance.
	InstanceID string
	// InstanceVersion is the instance's own optimistic version after this
	// call's advancement.
	InstanceVersion int64
	// VisitedNodes is every node the driver actually ran before returning,
	// in the order it ran them.
	VisitedNodes []string
	// ParkedContinuations names the continuation(s) the instance is waiting
	// on. Empty when Parked is false.
	ParkedContinuations []string
}

// ExecutionResumeRequest resumes one parked instance from completed
// human-work evidence. It is not yet used by any published RPC; the port
// carries it so a composition root's driver satisfies the same Execute/Resume
// shape internal/workflow/execute.Driver exposes.
type ExecutionResumeRequest struct {
	Start                   runtime.StartRequest
	InstanceID              uuid.UUID
	ExpectedInstanceVersion int64
	WorkItem                workitem.WorkItem
	Outcome                 frontier.NodeOutcome
}

// ProposalExecutor is the narrow part of the caller-driven workflow driver
// [IntentService.ExecuteIntent] needs: start an immutable, approved proposal
// revision, and resume one already parked on completed human-work evidence.
// internal/workflow/execute.Driver satisfies an equivalent shape once a
// composition root adapts its own request/result types to this port's; a
// cell composed with a nil ProposalExecutor leaves EXECUTE unavailable
// regardless of [ExecutionAuthority].
type ProposalExecutor interface {
	Execute(ctx context.Context, start runtime.StartRequest) (ExecutionResult, error)
	Resume(ctx context.Context, req ExecutionResumeRequest) (ExecutionResult, error)
}

// ExecutionAuthority is the explicit P1B gate [IntentService.ExecuteIntent]
// requires before it will run [ProposalExecutor] at all
// (planning/next-steps.md "P1B exists only after a signed Gate A PROCEED";
// definitions/planning/gates/p1b-template.yaml names promote_worker/EXECUTE
// as the first of the six P1B candidate contracts that amendment admits).
//
// This type carries no authority of its own. It is evidence a composition
// root asserts it already holds — an amendment digest, the exact intent
// types it names, the role it additionally requires of the caller — never a
// check against the signed gate document itself: that document lives under
// definitions/ and this package never parses it. A cell composed with a nil
// ExecutionAuthority behaves byte-for-byte like the P1A cell of today:
// ExecuteIntent refuses under the same envelope every other governed write
// already does ([p1aRefusal]).
type ExecutionAuthority struct {
	// AuthorityDigest names the signed P1B authority amendment this gate was
	// configured under. It is carried through to evidence and to a
	// successful receipt; this package never computes or verifies it against
	// the gate document.
	AuthorityDigest string
	// AdmittedIntentTypes is the exact, closed set of intent type ids this
	// authority admits into EXECUTE. Only promotion.IntentType
	// ("promote_worker") is granted in this release (planning/next-steps.md
	// P1B item 1); a composition root that names any other type id is
	// asserting authority this release's gate document does not.
	AdmittedIntentTypes map[string]bool
	// RequiredRole is the principal role ExecuteIntent additionally requires
	// of the caller, on top of the cell itself being authorized. A caller
	// without it is refused even when the cell is authorized.
	RequiredRole string
}

// admitsType reports whether a admits intentTypeID into EXECUTE. A nil a
// admits nothing.
func (a *ExecutionAuthority) admitsType(intentTypeID string) bool {
	return a != nil && a.AdmittedIntentTypes[intentTypeID]
}

// admitsCaller reports whether principal carries a's required role. A nil a,
// or one naming no required role, admits no caller: an authority that names
// no role is a misconfiguration, not an open gate.
func (a *ExecutionAuthority) admitsCaller(principal *trust.Principal) bool {
	if a == nil || a.RequiredRole == "" || principal == nil {
		return false
	}
	return principal.HasRole(a.RequiredRole)
}
