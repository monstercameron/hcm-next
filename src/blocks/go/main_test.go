package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewExecutorHandlerHealth(t *testing.T) {
	handler, err := newExecutorHandler()
	if err != nil {
		t.Fatalf("expected handler, got error: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	responseRecorder := httptest.NewRecorder()

	handler.ServeHTTP(responseRecorder, request)

	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", responseRecorder.Code)
	}
}
