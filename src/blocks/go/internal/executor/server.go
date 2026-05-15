package executor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	defaultMaxBodyBytes = int64(1 << 20)
	defaultReadTimeout  = 10 * time.Second
	defaultWriteTimeout = 10 * time.Second
	defaultIdleTimeout  = 60 * time.Second
)

var generatedRequestSequence atomic.Uint64

// Server owns the executor HTTP handlers.
type Server struct {
	registry     *Registry
	maxBodyBytes int64
}

// NewServer creates a configured executor HTTP service.
func NewServer(registry *Registry) *Server {
	return &Server{
		registry:     registry,
		maxBodyBytes: defaultMaxBodyBytes,
	}
}

// NewHTTPServer creates an HTTP server with bounded request timeouts.
func NewHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}
}

// Routes returns the executor HTTP route mux.
func (server *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.handleHealth)
	mux.HandleFunc("/execute-block", server.handleExecuteBlock)
	return mux
}

func (server *Server) handleHealth(responseWriter http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeErrorResponse(responseWriter, http.StatusMethodNotAllowed, "", "", MethodNotAllowedError(request.Method), 0)
		return
	}

	writeJSON(responseWriter, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "go-executor",
	})
}

func (server *Server) handleExecuteBlock(responseWriter http.ResponseWriter, request *http.Request) {
	startedAt := time.Now()
	requestID := requestIDFromHeader(request)

	if request.Method != http.MethodPost {
		writeErrorResponse(responseWriter, http.StatusMethodNotAllowed, requestID, "", MethodNotAllowedError(request.Method), durationMs(startedAt))
		return
	}

	request.Body = http.MaxBytesReader(responseWriter, request.Body, server.maxBodyBytes)

	var executionRequest ExecutionRequest
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&executionRequest); err != nil {
		writeErrorResponse(responseWriter, http.StatusBadRequest, requestID, "", InvalidJSONError(err.Error()), durationMs(startedAt))
		return
	}

	var trailingValue struct{}
	if err := decoder.Decode(&trailingValue); err != io.EOF {
		writeErrorResponse(responseWriter, http.StatusBadRequest, requestID, executionRequest.Context.CorrelationID, InvalidJSONError("request body contains multiple JSON values"), durationMs(startedAt))
		return
	}

	if validationError := validateExecutionRequest(executionRequest); validationError != nil {
		writeErrorResponse(responseWriter, http.StatusBadRequest, requestID, executionRequest.Context.CorrelationID, validationError, durationMs(startedAt))
		return
	}

	blockResult, executionError := server.registry.Execute(executionRequest)
	if executionError != nil {
		writeErrorResponse(responseWriter, httpStatusForExecutionError(executionError), requestID, executionRequest.Context.CorrelationID, executionError, durationMs(startedAt))
		return
	}

	executionResponse := ExecutionResponse{
		RequestID:            requestID,
		CorrelationID:        executionRequest.Context.CorrelationID,
		Status:               ExecutionStatusSucceeded,
		Output:               blockResult.Output,
		ProposedEvents:       nonNilProposedEvents(blockResult.ProposedEvents),
		ExternalCallRequests: nonNilExternalCallRequests(blockResult.ExternalCallRequests),
		Logs:                 nonNilLogs(blockResult.Logs),
		Metrics: ExecutionMetrics{
			DurationMs: durationMs(startedAt),
		},
	}

	writeJSON(responseWriter, http.StatusOK, executionResponse)
}

func validateExecutionRequest(request ExecutionRequest) *ExecutionError {
	missingFields := make([]string, 0)

	requiredFields := map[string]string{
		"tenantId":               request.TenantID,
		"environmentId":          request.EnvironmentID,
		"workflowInstanceId":     request.WorkflowInstanceID,
		"workflowVersionId":      request.WorkflowVersionID,
		"block.name":             request.Block.Name,
		"block.version":          request.Block.Version,
		"context.actorId":        request.Context.ActorID,
		"context.effectiveAt":    request.Context.EffectiveAt,
		"context.correlationId":  request.Context.CorrelationID,
		"context.idempotencyKey": request.Context.IdempotencyKey,
	}

	for field, value := range requiredFields {
		if strings.TrimSpace(value) == "" {
			missingFields = append(missingFields, field)
		}
	}

	if len(request.Input) == 0 {
		missingFields = append(missingFields, "input")
	}

	if len(missingFields) > 0 {
		return InvalidRequestError("Executor request is missing required fields.", map[string]any{
			"missingFields": missingFields,
		})
	}

	return nil
}

func writeErrorResponse(responseWriter http.ResponseWriter, statusCode int, requestID string, correlationID string, executionError *ExecutionError, duration int64) {
	executionResponse := ExecutionResponse{
		RequestID:            requestID,
		CorrelationID:        correlationID,
		Status:               ExecutionStatusFailed,
		Output:               map[string]any{},
		ProposedEvents:       []ProposedEvent{},
		ExternalCallRequests: []ExternalCallRequest{},
		Logs:                 []ExecutionLog{},
		Metrics: ExecutionMetrics{
			DurationMs: duration,
		},
		Error: executionError,
	}

	writeJSON(responseWriter, statusCode, executionResponse)
}

func writeJSON(responseWriter http.ResponseWriter, statusCode int, payload any) {
	responseWriter.Header().Set("Content-Type", "application/json")
	responseWriter.WriteHeader(statusCode)
	_ = json.NewEncoder(responseWriter).Encode(payload)
}

func requestIDFromHeader(request *http.Request) string {
	requestID := strings.TrimSpace(request.Header.Get("X-Request-Id"))
	if requestID != "" {
		return requestID
	}

	return fmt.Sprintf("req_go_executor_%d_%d", time.Now().UTC().UnixNano(), generatedRequestSequence.Add(1))
}

func httpStatusForExecutionError(executionError *ExecutionError) int {
	switch executionError.Code {
	case ErrorCodeUnknownBlock:
		return http.StatusNotFound
	case ErrorCodeInvalidInput:
		return http.StatusUnprocessableEntity
	case ErrorCodeInvalidRequest, ErrorCodeInvalidJSON:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func durationMs(startedAt time.Time) int64 {
	return time.Since(startedAt).Milliseconds()
}

func nonNilProposedEvents(events []ProposedEvent) []ProposedEvent {
	if events == nil {
		return []ProposedEvent{}
	}

	return events
}

func nonNilExternalCallRequests(requests []ExternalCallRequest) []ExternalCallRequest {
	if requests == nil {
		return []ExternalCallRequest{}
	}

	return requests
}

func nonNilLogs(logs []ExecutionLog) []ExecutionLog {
	if logs == nil {
		return []ExecutionLog{}
	}

	return logs
}
