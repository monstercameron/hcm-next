package blockshared

import (
	"encoding/json"
	"testing"
	"time"
)

type strictDecodeFixture struct {
	Name string `json:"name"`
}

func TestDecodeStrictRejectsUnknownFields(t *testing.T) {
	rawInput := json.RawMessage(`{"name":"Jane","unexpected":true}`)

	_, executionError := DecodeStrict[strictDecodeFixture](rawInput, "fixture input does not match the expected contract.")
	if executionError == nil {
		t.Fatalf("expected unknown field to be rejected")
	}

	if executionError.Code != "invalid_input" {
		t.Fatalf("expected invalid_input, got %s", executionError.Code)
	}
}

func TestEffectiveDateValidation(t *testing.T) {
	evaluationDate := time.Date(2026, time.May, 15, 0, 0, 0, 0, time.UTC)

	validationErrors := EffectiveDateValidation("legal_name", "2025-11-15", evaluationDate, 180)
	if len(validationErrors) != 1 {
		t.Fatalf("expected one validation error, got %#v", validationErrors)
	}

	if validationErrors[0].Code != "legal_name.effective_at_too_far_in_past" {
		t.Fatalf("expected too-far-in-past code, got %s", validationErrors[0].Code)
	}
}

func TestRequiredTransactionFields(t *testing.T) {
	validationErrors := RequiredTransactionFields(map[string]string{
		"workerId": "worker_123",
		"reason":   " ",
	})

	if len(validationErrors) != 1 {
		t.Fatalf("expected one validation error, got %#v", validationErrors)
	}

	if validationErrors[0].Field != "reason" {
		t.Fatalf("expected missing reason field, got %s", validationErrors[0].Field)
	}
}

func TestStringHelpers(t *testing.T) {
	email := " USER@Example.COM "

	if NormalizedEmail(&email) != "user@example.com" {
		t.Fatalf("expected normalized email")
	}

	if DigitsOnly("+1 (555) 123-4567") != "15551234567" {
		t.Fatalf("expected only phone digits")
	}

	if PhoneDigitCount("+1 (555) 123-4567") != 11 {
		t.Fatalf("expected phone digit count")
	}
}
