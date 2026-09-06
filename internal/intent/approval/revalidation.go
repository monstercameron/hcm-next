package approval

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/hcm-next/internal/governance/revalidate"
	"github.com/monstercameron/hcm-next/internal/kernel/values"
	transactionplan "github.com/monstercameron/hcm-next/internal/transaction/plan"
)

// AuthorityRequest is the exact immutable decision context an authority port
// must check again immediately before execution.
type AuthorityRequest struct {
	Decision    ApprovalDecision
	Plan        transactionplan.TransactionPlan
	EvaluatedAt values.Instant
}

// AuthorityResult is the port's current answer. Valid is not inferred from a
// principal id: the port must re-evaluate the current candidate and route.
type AuthorityResult struct {
	Valid         bool
	RequirementID string
	PrincipalID   string
	DecisionRef   string
	Reason        string
}

// AuthorityPort owns the current approver/requirement authority lookup.
type AuthorityPort interface {
	RevalidateApprovalAuthority(context.Context, AuthorityRequest) (AuthorityResult, error)
}

// ProposalState is the server-held proposal identity at the execution
// boundary. It is deliberately smaller than a proposal payload.
type ProposalState struct {
	IntentID           string
	ProposalRevisionID string
	MaterialDigest     string
}

// ProposalPort reads the current server-held proposal binding.
type ProposalPort interface {
	CurrentProposal(context.Context, string) (ProposalState, error)
}

// ControlPort routes current governance facts through GOVERN-003's typed
// revalidator. Implementations may load durable facts before calling it.
type ControlPort interface {
	RevalidateApproval(context.Context, revalidate.HistoricalApproval, revalidate.Facts, string) (revalidate.Result, error)
}

// ExecutionRevalidationRequest is the complete pre-execution input. The
// historical approval and current facts are evidence inputs to ControlPort;
// neither is treated as proof merely because it came from the caller.
type ExecutionRevalidationRequest struct {
	Decision     ApprovalDecision
	Plan         transactionplan.TransactionPlan
	Historical   revalidate.HistoricalApproval
	CurrentFacts revalidate.Facts
	EvaluatedAt  values.Instant
}

// ExecutionRevalidationPorts are the three current-truth seams required at
// the pre-execution boundary.
type ExecutionRevalidationPorts struct {
	Authority AuthorityPort
	Proposal  ProposalPort
	Control   ControlPort
}

// ExecutionAdmission is returned only after all three ports confirm the
// exact decision, proposal and prepared plan.
type ExecutionAdmission struct {
	Decision  ApprovalDecision
	Plan      transactionplan.TransactionPlan
	Authority AuthorityResult
	Controls  revalidate.Result
}

// RevalidateBeforeExecution performs the final authority, proposal and
// governance checks. Any refusal happens before the caller can execute an
// effect; no port is optional.
func RevalidateBeforeExecution(ctx context.Context, ports ExecutionRevalidationPorts, req ExecutionRevalidationRequest) (ExecutionAdmission, error) {
	if ctx == nil {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "context", CodeInvalidExecutionInput, ErrInvalidExecutionInput, "context is nil")
	}
	if req.EvaluatedAt.Validate() != nil || req.Decision.DecidedAt.Validate() != nil {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "evaluated_at", CodeInvalidExecutionInput, ErrInvalidExecutionInput, "execution time and decision time are required")
	}
	if !req.Decision.Outcome.Valid() || req.Decision.Outcome != OutcomeApproved {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "decision.outcome", CodeReapprovalRequired, ErrReapprovalRequired, "only an approved decision may execute")
	}
	if err := req.Plan.VerifyDigest(); err != nil {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "plan", CodePlanNotExecutable, ErrPlanNotExecutable, "%v", err)
	}
	binding := req.Decision.Binding
	if binding.ProposalRevisionID != req.Plan.ProposalRevisionID || binding.ProposalDigest.Digest != req.Plan.ProposalDigest {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "decision.binding", CodePlanMismatch, ErrPlanMismatch,
			"decision %q is not bound to plan %q", req.Decision.DecisionID, req.Plan.Digest)
	}
	if strings.TrimSpace(binding.IntentID) == "" || strings.TrimSpace(req.Decision.DecisionID) == "" {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "decision.binding", CodeInvalidExecutionInput, ErrInvalidExecutionInput, "decision identity is incomplete")
	}
	if ports.Proposal == nil {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "proposal_port", CodeProposalChanged, ErrProposalChanged, "proposal port is required")
	}
	current, err := ports.Proposal.CurrentProposal(ctx, binding.IntentID)
	if err != nil {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "proposal", CodeProposalChanged, ErrProposalChanged, "%v", err)
	}
	if current.IntentID != binding.IntentID || current.ProposalRevisionID != binding.ProposalRevisionID || current.MaterialDigest != req.Plan.ProposalDigest {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "proposal", CodeProposalChanged, ErrProposalChanged,
			"server proposal no longer matches decision %q", req.Decision.DecisionID)
	}
	if ports.Authority == nil {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "authority_port", CodeExecutionAuthority, ErrExecutionAuthority, "authority port is required")
	}
	authority, err := ports.Authority.RevalidateApprovalAuthority(ctx, AuthorityRequest{Decision: req.Decision, Plan: req.Plan, EvaluatedAt: req.EvaluatedAt})
	if err != nil {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "authority", CodeExecutionAuthority, ErrExecutionAuthority, "%v", err)
	}
	if !authority.Valid || authority.RequirementID != binding.RequirementID || authority.PrincipalID != req.Decision.Approver.PrincipalID {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "authority", CodeExecutionAuthority, ErrExecutionAuthority,
			"current authority no longer authorizes decision %q: %s", req.Decision.DecisionID, authority.Reason)
	}
	if ports.Control == nil {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "control_port", CodeControlRevalidation, ErrControlRevalidation, "control port is required")
	}
	controls, err := ports.Control.RevalidateApproval(ctx, req.Historical, req.CurrentFacts, req.Plan.Digest)
	if err != nil {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "controls", CodeControlRevalidation, ErrControlRevalidation, "%v", err)
	}
	if err := controls.VerifyBoundPlan(req.Plan.Digest); err != nil {
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "controls.plan_digest", CodePlanMismatch, ErrPlanMismatch, "%v", err)
	}
	if !controls.Confirmed {
		code, cause := CodeReapprovalRequired, ErrReapprovalRequired
		if controls.Requirement == revalidate.RequirementBlock || controls.Requirement == revalidate.RequirementReplanRequired {
			code, cause = CodeControlRevalidation, ErrControlRevalidation
		}
		return ExecutionAdmission{}, refusal("RevalidateBeforeExecution", "controls", code, cause,
			"current governance requires %s: %s", controls.Requirement, controls.Explanation)
	}
	return ExecutionAdmission{Decision: req.Decision, Plan: req.Plan, Authority: authority, Controls: controls}, nil
}

// Explain describes the pre-execution boundary without exposing proposal
// contents or authority evidence.
func (a ExecutionAdmission) Explain() string {
	return fmt.Sprintf("execution admission decision=%s plan=%s governance=%t authority=%t", a.Decision.DecisionID, a.Plan.Digest, a.Controls.Confirmed, a.Authority.Valid)
}
