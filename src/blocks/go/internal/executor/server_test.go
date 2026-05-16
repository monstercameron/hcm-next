package executor_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"hcm-next-executor/internal/blocks/legalname"
	"hcm-next-executor/internal/executor"
)

func TestHealthEndpoint(t *testing.T) {
	testServer := newTestServer(t)
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	responseRecorder := httptest.NewRecorder()

	testServer.ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", responseRecorder.Code)
	}

	var response map[string]string
	if err := json.Unmarshal(responseRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["status"] != "ok" {
		t.Fatalf("expected ok health status, got %#v", response)
	}
}

func TestExecuteBlockRunsPreflight(t *testing.T) {
	testServer := newTestServer(t)
	payload := validExecutePayload(legalname.PreflightBlockName, map[string]any{
		"currentLegalName": map[string]any{
			"first": "Jane",
			"last":  "Doe",
		},
		"proposedLegalName": map[string]any{
			"first": "Jane",
			"last":  "Rivera",
		},
		"effectiveAt":    "2026-06-01",
		"businessReason": "legal_name_change",
	})

	response := executeJSONRequest(t, testServer, payload)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	var executionResponse executor.ExecutionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &executionResponse); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if executionResponse.Status != executor.ExecutionStatusSucceeded {
		t.Fatalf("expected succeeded status, got %#v", executionResponse)
	}
}

func TestExecuteBlockRunsPlanTransaction(t *testing.T) {
	testServer := newTestServer(t)
	payload := validExecutePayload(legalname.PlanTransactionBlockName, map[string]any{
		"changeRequestId": "cr_123",
		"workerId":        "emp_123",
		"personId":        "person_123",
		"currentLegalName": map[string]any{
			"first": "Jane",
			"last":  "Doe",
		},
		"proposedLegalName": map[string]any{
			"first": "Jane",
			"last":  "Rivera",
		},
		"effectiveAt": "2026-06-01",
	})

	response := executeJSONRequest(t, testServer, payload)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}

	var executionResponse executor.ExecutionResponse
	if err := json.Unmarshal(response.Body.Bytes(), &executionResponse); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(executionResponse.ExternalCallRequests) != 1 {
		t.Fatalf("expected one top-level external call request, got %#v", executionResponse.ExternalCallRequests)
	}
}

func TestExecuteBlockUnknownBlock(t *testing.T) {
	testServer := newTestServer(t)
	payload := validExecutePayload("system.unknown", map[string]any{})
	response := executeJSONRequest(t, testServer, payload)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", response.Code, response.Body.String())
	}

	assertErrorCode(t, response.Body.Bytes(), executor.ErrorCodeUnknownBlock)
}

func TestExecuteBlockInvalidJSONInput(t *testing.T) {
	testServer := newTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "/execute-block", bytes.NewBufferString("{"))
	responseRecorder := httptest.NewRecorder()

	testServer.ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", responseRecorder.Code, responseRecorder.Body.String())
	}

	assertErrorCode(t, responseRecorder.Body.Bytes(), executor.ErrorCodeInvalidJSON)
}

func newTestServer(t *testing.T) http.Handler {
	t.Helper()

	registry := executor.NewRegistry()
	if err := legalname.RegisterBlocks(registry); err != nil {
		t.Fatalf("failed to register blocks: %v", err)
	}

	testLogger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return executor.NewServer(registry, testLogger).Routes()
}

func validExecutePayload(blockName string, input map[string]any) map[string]any {
	return map[string]any{
		"tenantId":           "tenant_demo",
		"environmentId":      "env_demo",
		"changeRequestId":    "cr_123",
		"workflowInstanceId": "wfi_test",
		"workflowVersionId":  "wfv_test",
		"block": map[string]any{
			"name":    blockName,
			"version": legalname.BlockVersionV1,
		},
		"input": input,
		"context": map[string]any{
			"actorId":        "actor_employee_jane",
			"effectiveAt":    "2026-05-15",
			"permissions":    map[string]any{},
			"correlationId":  "corr_test",
			"idempotencyKey": "idem_test",
		},
	}
}

func executeJSONRequest(t *testing.T, handler http.Handler, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()

	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/execute-block", bytes.NewReader(encodedPayload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-Id", "req_test")
	responseRecorder := httptest.NewRecorder()

	handler.ServeHTTP(responseRecorder, request)
	return responseRecorder
}

func assertErrorCode(t *testing.T, responseBody []byte, expectedCode string) {
	t.Helper()

	var executionResponse executor.ExecutionResponse
	if err := json.Unmarshal(responseBody, &executionResponse); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if executionResponse.Error == nil {
		t.Fatalf("expected error response, got %#v", executionResponse)
	}

	if executionResponse.Error.Code != expectedCode {
		t.Fatalf("expected error code %s, got %s", expectedCode, executionResponse.Error.Code)
	}
}
