package approval

import "errors"

// Execution-bound refusal codes used by APPROVAL-003, APPROVAL-005 and
// APPROVAL-006. They are stable machine-readable reasons, not transport
// status codes.
const (
	CodeApprovalExpired       = "APPROVAL_EXPIRED"
	CodeApprovalRevoked       = "APPROVAL_REVOKED"
	CodeApprovalSuperseded    = "APPROVAL_SUPERSEDED"
	CodeApprovalAlreadyUsed   = "APPROVAL_ALREADY_USED"
	CodePlanMismatch          = "APPROVAL_PLAN_MISMATCH"
	CodePlanNotExecutable     = "APPROVAL_PLAN_NOT_EXECUTABLE"
	CodeAtomicityFailure      = "APPROVAL_ATOMICITY_FAILURE"
	CodeReapprovalRequired    = "REAPPROVAL_REQUIRED"
	CodeProposalChanged       = "PROPOSAL_CHANGED"
	CodeExecutionAuthority    = "EXECUTION_AUTHORITY_CHANGED"
	CodeControlRevalidation   = "CONTROL_REVALIDATION_REFUSED"
	CodeUnsafeProjection      = "PROJECTION_NOT_SAFE_TO_DECIDE"
	CodeProjectionMismatch    = "PROJECTION_MISMATCH"
	CodeProjectionTaskStale   = "PROJECTION_TASK_VERSION_STALE"
	CodeInvalidExecutionInput = "INVALID_EXECUTION_INPUT"
)

var (
	ErrApprovalExpired       = errors.New("approval: " + CodeApprovalExpired)
	ErrApprovalRevoked       = errors.New("approval: " + CodeApprovalRevoked)
	ErrApprovalSuperseded    = errors.New("approval: " + CodeApprovalSuperseded)
	ErrApprovalAlreadyUsed   = errors.New("approval: " + CodeApprovalAlreadyUsed)
	ErrPlanMismatch          = errors.New("approval: " + CodePlanMismatch)
	ErrPlanNotExecutable     = errors.New("approval: " + CodePlanNotExecutable)
	ErrAtomicityFailure      = errors.New("approval: " + CodeAtomicityFailure)
	ErrReapprovalRequired    = errors.New("approval: " + CodeReapprovalRequired)
	ErrProposalChanged       = errors.New("approval: " + CodeProposalChanged)
	ErrExecutionAuthority    = errors.New("approval: " + CodeExecutionAuthority)
	ErrControlRevalidation   = errors.New("approval: " + CodeControlRevalidation)
	ErrUnsafeProjection      = errors.New("approval: " + CodeUnsafeProjection)
	ErrProjectionMismatch    = errors.New("approval: " + CodeProjectionMismatch)
	ErrProjectionTaskStale   = errors.New("approval: " + CodeProjectionTaskStale)
	ErrInvalidExecutionInput = errors.New("approval: " + CodeInvalidExecutionInput)
)
