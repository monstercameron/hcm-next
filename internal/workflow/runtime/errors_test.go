package runtime

import (
	"errors"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	if ErrRuntime == nil {
		t.Fatalf("ErrRuntime is nil")
	}
	if CodeStaleInstance == "" {
		t.Fatalf("CodeStaleInstance is empty")
	}
	if CodeIllegalTransition == "" {
		t.Fatalf("CodeIllegalTransition is empty")
	}
	if CodeInstanceNotFound == "" {
		t.Fatalf("CodeInstanceNotFound is empty")
	}
	if CodeNodeExecutionNotFound == "" {
		t.Fatalf("CodeNodeExecutionNotFound is empty")
	}
	if CodeInvalidRecord == "" {
		t.Fatalf("CodeInvalidRecord is empty")
	}
	if CodeStorageFailed == "" {
		t.Fatalf("CodeStorageFailed is empty")
	}
}

func TestErrors_ErrorsIs(t *testing.T) {
	if !errors.Is(ErrRuntime, ErrRuntime) {
		t.Fatalf("errors.Is failed for ErrRuntime")
	}
	if ErrRuntime.Error() == "" {
		t.Fatalf("ErrRuntime Error empty")
	}
}
