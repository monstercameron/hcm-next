package approval

import (
	"errors"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	if ErrInvalidContinuation == nil {
		t.Fatalf("ErrInvalidContinuation is nil")
	}
	if ErrBindingMismatch == nil {
		t.Fatalf("ErrBindingMismatch is nil")
	}
	if ErrInvalidEvent == nil {
		t.Fatalf("ErrInvalidEvent is nil")
	}
	if ErrInvalidEvidence == nil {
		t.Fatalf("ErrInvalidEvidence is nil")
	}
}

func TestErrors_ErrorsIs(t *testing.T) {
	if !errors.Is(ErrInvalidContinuation, ErrInvalidContinuation) {
		t.Fatalf("errors.Is failed for ErrInvalidContinuation")
	}
	if ErrInvalidContinuation.Error() == "" {
		t.Fatalf("ErrInvalidContinuation Error empty")
	}
	if !errors.Is(ErrBindingMismatch, ErrBindingMismatch) {
		t.Fatalf("errors.Is failed for ErrBindingMismatch")
	}
	if ErrBindingMismatch.Error() == "" {
		t.Fatalf("ErrBindingMismatch Error empty")
	}
	if !errors.Is(ErrInvalidEvent, ErrInvalidEvent) {
		t.Fatalf("errors.Is failed for ErrInvalidEvent")
	}
	if ErrInvalidEvent.Error() == "" {
		t.Fatalf("ErrInvalidEvent Error empty")
	}
	if !errors.Is(ErrInvalidEvidence, ErrInvalidEvidence) {
		t.Fatalf("errors.Is failed for ErrInvalidEvidence")
	}
	if ErrInvalidEvidence.Error() == "" {
		t.Fatalf("ErrInvalidEvidence Error empty")
	}
}
