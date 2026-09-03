package task

import (
	"errors"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	if ErrInvalidNode == nil {
		t.Fatalf("ErrInvalidNode is nil")
	}
	if ErrInvalidContinuation == nil {
		t.Fatalf("ErrInvalidContinuation is nil")
	}
	if ErrInvalidSubmission == nil {
		t.Fatalf("ErrInvalidSubmission is nil")
	}
	if ErrBindingMismatch == nil {
		t.Fatalf("ErrBindingMismatch is nil")
	}
	if ErrClaimExpired == nil {
		t.Fatalf("ErrClaimExpired is nil")
	}
	if ErrValidationFailed == nil {
		t.Fatalf("ErrValidationFailed is nil")
	}
}

func TestErrors_ErrorsIs(t *testing.T) {
	if !errors.Is(ErrInvalidNode, ErrInvalidNode) {
		t.Fatalf("errors.Is failed for ErrInvalidNode")
	}
	if ErrInvalidNode.Error() == "" {
		t.Fatalf("ErrInvalidNode Error empty")
	}
	if !errors.Is(ErrInvalidContinuation, ErrInvalidContinuation) {
		t.Fatalf("errors.Is failed for ErrInvalidContinuation")
	}
	if ErrInvalidContinuation.Error() == "" {
		t.Fatalf("ErrInvalidContinuation Error empty")
	}
	if !errors.Is(ErrInvalidSubmission, ErrInvalidSubmission) {
		t.Fatalf("errors.Is failed for ErrInvalidSubmission")
	}
	if ErrInvalidSubmission.Error() == "" {
		t.Fatalf("ErrInvalidSubmission Error empty")
	}
	if !errors.Is(ErrBindingMismatch, ErrBindingMismatch) {
		t.Fatalf("errors.Is failed for ErrBindingMismatch")
	}
	if ErrBindingMismatch.Error() == "" {
		t.Fatalf("ErrBindingMismatch Error empty")
	}
	if !errors.Is(ErrClaimExpired, ErrClaimExpired) {
		t.Fatalf("errors.Is failed for ErrClaimExpired")
	}
	if ErrClaimExpired.Error() == "" {
		t.Fatalf("ErrClaimExpired Error empty")
	}
	if !errors.Is(ErrValidationFailed, ErrValidationFailed) {
		t.Fatalf("errors.Is failed for ErrValidationFailed")
	}
	if ErrValidationFailed.Error() == "" {
		t.Fatalf("ErrValidationFailed Error empty")
	}
}
