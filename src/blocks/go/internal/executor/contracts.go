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
