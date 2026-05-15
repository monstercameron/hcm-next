package executor

import "encoding/json"

const (
	// ExecutionStatusSucceeded means the requested block completed and returned output.
	ExecutionStatusSucceeded = "succeeded"
	// ExecutionStatusFailed means the executor rejected or failed the requested block execution.
	ExecutionStatusFailed = "failed"
)

const (
	// ErrorCodeInvalidJSON identifies malformed request JSON.
	ErrorCodeInvalidJSON = "invalid_json"
	// ErrorCodeInvalidRequest identifies request contract validation failures.
	ErrorCodeInvalidRequest = "invalid_request"
	// ErrorCodeUnknownBlock identifies a block name/version with no registered handler.
	ErrorCodeUnknownBlock = "unknown_block"
	// ErrorCodeInvalidInput identifies block input contract validation failures.
	ErrorCodeInvalidInput = "invalid_input"
	// ErrorCodeExecutionFailed identifies an unexpected block execution failure.
	ErrorCodeExecutionFailed = "execution_failed"
	// ErrorCodeMethodNotAllowed identifies unsupported HTTP methods.
	ErrorCodeMethodNotAllowed = "method_not_allowed"
)

const (
	// RouteKeyValidationFailed routes workflows back to input correction.
	RouteKeyValidationFailed = "validation_failed"
	// RouteKeyApprovalRequired routes valid requests to the approval path.
	RouteKeyApprovalRequired = "approval_required"
	// RouteKeyApprovalWithWarnings routes valid requests to approval with review context.
	RouteKeyApprovalWithWarnings = "approval_with_warnings"
	// RouteKeyNoApprovalRequired routes valid requests directly to execution.
	RouteKeyNoApprovalRequired = "no_approval_required"
	// RouteKeyTransactionPlanReady routes approved requests to transaction execution.
	RouteKeyTransactionPlanReady = "transaction_plan_ready"
)

// BlockReference identifies a deterministic executor block.
type BlockReference struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ExecutionContext carries workflow context needed for deterministic block execution.
type ExecutionContext struct {
	ActorID        string         `json:"actorId"`
	EffectiveAt    string         `json:"effectiveAt"`
	Permissions    map[string]any `json:"permissions"`
	CorrelationID  string         `json:"correlationId"`
	IdempotencyKey string         `json:"idempotencyKey"`
}

// ExecutionRequest is the Node-to-Go executor request contract.
type ExecutionRequest struct {
	TenantID           string           `json:"tenantId"`
	EnvironmentID      string           `json:"environmentId"`
	ChangeRequestID    string           `json:"changeRequestId,omitempty"`
	WorkflowInstanceID string           `json:"workflowInstanceId"`
	WorkflowVersionID  string           `json:"workflowVersionId"`
	Block              BlockReference   `json:"block"`
	Input              json.RawMessage  `json:"input"`
	Context            ExecutionContext `json:"context"`
}

// ProposedEvent is a block-proposed event that Node may convert into ledger entries.
type ProposedEvent struct {
	EventType   string         `json:"eventType"`
	SubjectType string         `json:"subjectType"`
	SubjectID   string         `json:"subjectId"`
	EffectiveAt string         `json:"effectiveAt,omitempty"`
	Payload     map[string]any `json:"payload"`
}

// ExternalCallRequest is a side-effect request spec. Node owns executing it.
type ExternalCallRequest struct {
	ConnectionID    string         `json:"connectionId"`
	Operation       string         `json:"operation"`
	IdempotencyKey  string         `json:"idempotencyKey"`
	Payload         map[string]any `json:"payload"`
	Reconciliation  map[string]any `json:"reconciliation,omitempty"`
	CapabilityHints []string       `json:"capabilityHints,omitempty"`
}

// Fact is a generic deterministic fact emitted by a block for workflow routing and audit.
type Fact struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Source string `json:"source,omitempty"`
}

// ValidationIssue is the generic validation and warning message contract.
type ValidationIssue struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// LedgerFact is a block-proposed business fact that Node may persist as a ledger event.
type LedgerFact struct {
	EventType   string         `json:"eventType"`
	SubjectType string         `json:"subjectType"`
	SubjectID   string         `json:"subjectId"`
	EffectiveAt string         `json:"effectiveAt"`
	Payload     map[string]any `json:"payload"`
}

// ProjectionPatch describes a deterministic projection mutation that Node may apply.
type ProjectionPatch struct {
	Projection string `json:"projection"`
	Operation  string `json:"operation"`
	Path       string `json:"path"`
	Value      any    `json:"value"`
}

// TransactionOperation describes a non-ledger durable operation in a generic transaction plan.
type TransactionOperation struct {
	Operation      string         `json:"operation"`
	Target         string         `json:"target"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
}

// TransactionPlan is the generic deterministic transaction contract emitted by plan blocks.
type TransactionPlan struct {
	PlanType          string                 `json:"planType"`
	IdempotencyKey    string                 `json:"idempotencyKey,omitempty"`
	LedgerFacts       []LedgerFact           `json:"ledgerFacts"`
	ExternalCalls     []ExternalCallRequest  `json:"externalCalls"`
	ProjectionPatches []ProjectionPatch      `json:"projectionPatches"`
	Operations        []TransactionOperation `json:"operations"`
}

// BlockOutputContract is the generic output contract every block should emit.
type BlockOutputContract struct {
	Facts             []Fact                `json:"facts"`
	RouteKey          string                `json:"routeKey,omitempty"`
	ValidationErrors  []ValidationIssue     `json:"validationErrors"`
	Warnings          []ValidationIssue     `json:"warnings"`
	TransactionPlan   *TransactionPlan      `json:"transactionPlan,omitempty"`
	ExternalCalls     []ExternalCallRequest `json:"externalCalls"`
	ProjectionPatches []ProjectionPatch     `json:"projectionPatches"`
	LedgerFacts       []LedgerFact          `json:"ledgerFacts"`
}

// ExecutionLog is a structured log entry returned by a deterministic block.
type ExecutionLog struct {
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

// ExecutionMetrics contains execution measurements for observability.
type ExecutionMetrics struct {
	DurationMs int64 `json:"durationMs"`
}

// ExecutionError is the typed executor error response.
type ExecutionError struct {
	Code        string         `json:"code"`
	Message     string         `json:"message"`
	SafeMessage string         `json:"safeMessage"`
	Details     map[string]any `json:"details,omitempty"`
}

// ExecutionResponse is the Go-to-Node executor response contract.
type ExecutionResponse struct {
	RequestID            string                `json:"requestId,omitempty"`
	CorrelationID        string                `json:"correlationId,omitempty"`
	Status               string                `json:"status"`
	Output               any                   `json:"output"`
	ProposedEvents       []ProposedEvent       `json:"proposedEvents"`
	ExternalCallRequests []ExternalCallRequest `json:"externalCallRequests"`
	Logs                 []ExecutionLog        `json:"logs"`
	Metrics              ExecutionMetrics      `json:"metrics"`
	Error                *ExecutionError       `json:"error,omitempty"`
}

// BlockResult contains block output before the executor wraps it with status and metrics.
type BlockResult struct {
	Output               any
	ProposedEvents       []ProposedEvent
	ExternalCallRequests []ExternalCallRequest
	Logs                 []ExecutionLog
}

// BlockHandler executes a registered deterministic block.
type BlockHandler func(request ExecutionRequest) (BlockResult, *ExecutionError)

// RouteKeyForValidation maps validation status to the generic route key contract.
func RouteKeyForValidation(isValid bool, warningCount int, requiresApproval bool) string {
	if !isValid {
		return RouteKeyValidationFailed
	}

	if warningCount > 0 {
		return RouteKeyApprovalWithWarnings
	}

	if requiresApproval {
		return RouteKeyApprovalRequired
	}

	return RouteKeyNoApprovalRequired
}

// NewValidationOutputContract builds the generic contract fields for validation blocks.
func NewValidationOutputContract(routeKey string, facts []Fact, validationErrors []ValidationIssue, warnings []ValidationIssue) BlockOutputContract {
	return BlockOutputContract{
		Facts:             nonNilFacts(facts),
		RouteKey:          routeKey,
		ValidationErrors:  nonNilValidationIssues(validationErrors),
		Warnings:          nonNilValidationIssues(warnings),
		ExternalCalls:     []ExternalCallRequest{},
		ProjectionPatches: []ProjectionPatch{},
		LedgerFacts:       []LedgerFact{},
	}
}

// NewTransactionOutputContract builds the generic contract fields for transaction planning blocks.
func NewTransactionOutputContract(
	routeKey string,
	facts []Fact,
	ledgerFacts []LedgerFact,
	externalCalls []ExternalCallRequest,
	projectionPatches []ProjectionPatch,
	planType string,
	idempotencyKey string,
	operations []TransactionOperation,
) BlockOutputContract {
	return BlockOutputContract{
		Facts:            nonNilFacts(facts),
		RouteKey:         routeKey,
		ValidationErrors: []ValidationIssue{},
		Warnings:         []ValidationIssue{},
		TransactionPlan: &TransactionPlan{
			PlanType:          planType,
			IdempotencyKey:    idempotencyKey,
			LedgerFacts:       nonNilLedgerFacts(ledgerFacts),
			ExternalCalls:     nonNilExternalCalls(externalCalls),
			ProjectionPatches: nonNilProjectionPatches(projectionPatches),
			Operations:        nonNilTransactionOperations(operations),
		},
		ExternalCalls:     nonNilExternalCalls(externalCalls),
		ProjectionPatches: nonNilProjectionPatches(projectionPatches),
		LedgerFacts:       nonNilLedgerFacts(ledgerFacts),
	}
}

func nonNilFacts(facts []Fact) []Fact {
	if facts == nil {
		return []Fact{}
	}

	return facts
}

func nonNilValidationIssues(issues []ValidationIssue) []ValidationIssue {
	if issues == nil {
		return []ValidationIssue{}
	}

	return issues
}

func nonNilLedgerFacts(facts []LedgerFact) []LedgerFact {
	if facts == nil {
		return []LedgerFact{}
	}

	return facts
}

func nonNilExternalCalls(calls []ExternalCallRequest) []ExternalCallRequest {
	if calls == nil {
		return []ExternalCallRequest{}
	}

	return calls
}

func nonNilProjectionPatches(patches []ProjectionPatch) []ProjectionPatch {
	if patches == nil {
		return []ProjectionPatch{}
	}

	return patches
}

func nonNilTransactionOperations(operations []TransactionOperation) []TransactionOperation {
	if operations == nil {
		return []TransactionOperation{}
	}

	return operations
}
