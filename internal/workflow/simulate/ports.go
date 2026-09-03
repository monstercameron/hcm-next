package simulate

import (
	"context"
	"time"

	"github.com/monstercameron/hcm-next/internal/capability"
	"github.com/monstercameron/hcm-next/internal/workflow"
)

// CapabilityRequest is the payload a CAPABILITY or OBSERVE node hands to the
// governed gateway. It is the interpreter's whole contract with a capability
// handler: the node that asked, the exact capability version resolved, the
// declared operation mode, the typed inputs the mappings produced and the
// output fields the node declared it will read back.
//
// The handler never receives the plan, the receipt or the interpreter's state.
// A capability that could see the workflow around it would be able to make a
// decision the workflow author never reviewed.
type CapabilityRequest struct {
	NodeID        string
	Capability    capability.Key
	OperationMode workflow.ExecutionMode
	Inputs        Bag
	Outputs       []workflow.Field
	// At is the simulated instant, from the injected fake clock.
	At time.Time
}

// CapabilityResponse is a handler's typed answer: which of the step type's
// fixed outcomes it produced, the declared output fields, and a short,
// deterministic statement of what it did.
type CapabilityResponse struct {
	Outcome workflow.Outcome
	Outputs Bag
	Detail  string
}

// Authorizer produces the already-made authorization decision the governed
// gateway enforces for one node. This package never authenticates and never
// computes an authorization: it presents the decision a caller made, and the
// gateway refuses when the node's declared authority scopes do not cover the
// capability manifest's required scope.
type Authorizer func(node workflow.CompiledNode) capability.Authorization

// ScopeAuthorizer returns an Authorizer that presents exactly the authority
// scopes the node declared, under the given subject.
//
// It grants nothing the plan did not already declare: a node that binds a
// capability requiring a scope it never declared is refused by the gateway,
// which is what makes the declared scope list load-bearing rather than
// documentation.
func ScopeAuthorizer(subjectRef string) Authorizer {
	return func(node workflow.CompiledNode) capability.Authorization {
		var scopes []string
		if node.Capability != nil {
			scopes = append(scopes, node.Capability.AuthorityScopes...)
		}
		return capability.Authorization{
			Decision:   capability.Allow,
			Scopes:     scopes,
			SubjectRef: subjectRef,
			Reason:     "workflow node declared authority scopes",
		}
	}
}

// DecisionRequest is one DECISION evaluation: the compiled binding (evaluator,
// rule reference, routes, precedence, default) and the pinned input snapshot
// the mappings produced.
type DecisionRequest struct {
	NodeID   string
	Decision workflow.CompiledDecision
	Inputs   Bag
	At       time.Time
}

// DecisionResult is the route a decision selected, plus the evaluation trace
// reference that lets the choice be re-derived.
type DecisionResult struct {
	// RouteKey must be one of the node's declared routes, or the fixed
	// UNKNOWN outcome. A decision that cannot decide says UNKNOWN; it never
	// falls through to whichever edge happens to be first.
	RouteKey string
	// TraceRef cites the evaluation trace, for example the matched row id.
	TraceRef string
	Detail   string
}

// DecisionPort evaluates a DECISION node. The kernel routes; the evaluator
// owns the business rule.
type DecisionPort interface {
	Decide(ctx context.Context, req DecisionRequest) (DecisionResult, error)
}

// TransformRequest is one TRANSFORM evaluation: the compiled binding with its
// normalization profile, pinned lookups, taint lineage and resource limits,
// and the typed inputs.
type TransformRequest struct {
	NodeID    string
	Transform workflow.CompiledTransform
	Inputs    Bag
	Outputs   []workflow.Field
	At        time.Time
}

// TransformResult is a transform's typed output and outcome.
type TransformResult struct {
	Outcome workflow.Outcome
	Outputs Bag
	Detail  string
}

// TransformPort evaluates a TRANSFORM node. Implementations are pure: no
// clock, no randomness, no network. The instant is supplied rather than read
// so a transform that legitimately needs "now" still replays identically.
type TransformPort interface {
	Transform(ctx context.Context, req TransformRequest) (TransformResult, error)
}

// ObservationRequest is one OBSERVE evaluation: the compiled observation
// contract (evidence kind, source authority, expected state fields, required
// watermarks, freshness bound, comparison profile) and the typed inputs.
type ObservationRequest struct {
	NodeID  string
	Observe workflow.CompiledObserve
	Inputs  Bag
	Outputs []workflow.Field
	At      time.Time
}

// Observation is an authoritative read's answer.
//
// A submission receipt is not an observation, and neither is an HTTP 200: the
// outcome must be PASS, FAIL, PARTIAL or UNKNOWN, and an implementation that
// cannot see the source says UNKNOWN rather than PASS.
type Observation struct {
	Outcome workflow.Outcome
	Outputs Bag
	// Watermark is the source position the read was taken at, cited so two
	// observations can be proved to have come from one consistent snapshot.
	Watermark string
	Detail    string
}

// ReadPort answers an OBSERVE node. It is injected rather than reached for, so
// a simulation reads a supplied projection and never a database or a network.
type ReadPort interface {
	Observe(ctx context.Context, req ObservationRequest) (Observation, error)
}

// ApprovalRequest is the question an APPROVAL or TASK node - or a terminal
// that declares an outstanding approval requirement - asks of human work.
//
// It carries the run's typed state so a port can derive the requirement from
// what the simulation actually computed (the raise, the band position, the
// budget authority) rather than from a constant.
type ApprovalRequest struct {
	NodeID string
	// RequirementRefs are the approval requirement ids the node's governance
	// surface declares, sorted.
	RequirementRefs []string
	// WorkflowInputs are the run's declared inputs.
	WorkflowInputs Bag
	// NodeOutputs maps an already-executed node id to the outputs it produced.
	NodeOutputs map[string]Bag
	At          time.Time
}

// ApprovalPort derives approval requirements and resolves candidate approvers
// without waiting for any of them.
//
// The "without waiting" is the contract, not an optimization: P1B owns durable
// human tasks, so a P1A simulation records what would have been awaited and
// keeps walking. An implementation that blocked would be an execution, and one
// that returned a decision would be a forgery.
type ApprovalPort interface {
	WouldAwait(ctx context.Context, req ApprovalRequest) ([]WorkItem, error)
}

// noApprovals is the default ApprovalPort: a plan whose nodes declare no
// approval requirement raises no work item, and one that does gets an explicit
// refusal rather than a silently empty list.
type noApprovals struct{}

func (noApprovals) WouldAwait(_ context.Context, req ApprovalRequest) ([]WorkItem, error) {
	if len(req.RequirementRefs) == 0 {
		return nil, nil
	}
	return nil, refuse(CodeInvalidOptions, req.NodeID,
		"node declares approval requirements %v but no ApprovalPort was supplied", req.RequirementRefs)
}

// unboundTransforms is the default TransformPort: it refuses rather than
// echoing its input, because a transform that returned its input unchanged
// would silently produce a proposal nobody computed.
type unboundTransforms struct{}

func (unboundTransforms) Transform(_ context.Context, req TransformRequest) (TransformResult, error) {
	return TransformResult{}, refuse(CodeInvalidOptions, req.NodeID,
		"transform %s has no bound implementation", req.Transform.TransformRef)
}

// unboundReads is the default ReadPort. It answers UNKNOWN rather than
// refusing: "nobody wired an observation source" is exactly the degraded case
// an OBSERVE node's UNKNOWN route exists for, and routing it there proves the
// plan handles it.
type unboundReads struct{}

func (unboundReads) Observe(_ context.Context, _ ObservationRequest) (Observation, error) {
	return Observation{
		Outcome: workflow.OutcomeUnknown,
		Outputs: Bag{},
		Detail:  "no read port is bound; the source could not be observed",
	}, nil
}
