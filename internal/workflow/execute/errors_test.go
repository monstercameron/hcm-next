package execute

import (
	"errors"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	if ErrInvalidConfiguration == nil {
		t.Fatalf("ErrInvalidConfiguration is nil")
	}
	if ErrUnsupportedContinuation == nil {
		t.Fatalf("ErrUnsupportedContinuation is nil")
	}
	if ErrNoProgress == nil {
		t.Fatalf("ErrNoProgress is nil")
	}
}

func TestErrors_ErrorsIs(t *testing.T) {
	if !errors.Is(ErrInvalidConfiguration, ErrInvalidConfiguration) {
		t.Fatalf("errors.Is failed for ErrInvalidConfiguration")
	}
	if ErrInvalidConfiguration.Error() == "" {
		t.Fatalf("ErrInvalidConfiguration Error empty")
	}
	if !errors.Is(ErrUnsupportedContinuation, ErrUnsupportedContinuation) {
		t.Fatalf("errors.Is failed for ErrUnsupportedContinuation")
	}
	if ErrUnsupportedContinuation.Error() == "" {
		t.Fatalf("ErrUnsupportedContinuation Error empty")
	}
	if !errors.Is(ErrNoProgress, ErrNoProgress) {
		t.Fatalf("errors.Is failed for ErrNoProgress")
	}
	if ErrNoProgress.Error() == "" {
		t.Fatalf("ErrNoProgress Error empty")
	}
}
