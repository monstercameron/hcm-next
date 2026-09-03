package wait

import (
	"errors"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	if ErrWorkflowIdentityRequired == nil {
		t.Fatalf("ErrWorkflowIdentityRequired is nil")
	}
	if ErrNoWakeCondition == nil {
		t.Fatalf("ErrNoWakeCondition is nil")
	}
	if ErrConflictingWakeCondition == nil {
		t.Fatalf("ErrConflictingWakeCondition is nil")
	}
	if ErrInvalidRequirement == nil {
		t.Fatalf("ErrInvalidRequirement is nil")
	}
	if ErrDigestMismatch == nil {
		t.Fatalf("ErrDigestMismatch is nil")
	}
	if ErrNowRequired == nil {
		t.Fatalf("ErrNowRequired is nil")
	}
	if ErrEarlyWake == nil {
		t.Fatalf("ErrEarlyWake is nil")
	}
	if ErrUnknownEventKind == nil {
		t.Fatalf("ErrUnknownEventKind is nil")
	}
}

func TestErrors_ErrorsIs(t *testing.T) {
	if !errors.Is(ErrWorkflowIdentityRequired, ErrWorkflowIdentityRequired) {
		t.Fatalf("errors.Is failed for ErrWorkflowIdentityRequired")
	}
	if ErrWorkflowIdentityRequired.Error() == "" {
		t.Fatalf("ErrWorkflowIdentityRequired Error empty")
	}
	if !errors.Is(ErrNoWakeCondition, ErrNoWakeCondition) {
		t.Fatalf("errors.Is failed for ErrNoWakeCondition")
	}
	if ErrNoWakeCondition.Error() == "" {
		t.Fatalf("ErrNoWakeCondition Error empty")
	}
	if !errors.Is(ErrConflictingWakeCondition, ErrConflictingWakeCondition) {
		t.Fatalf("errors.Is failed for ErrConflictingWakeCondition")
	}
	if ErrConflictingWakeCondition.Error() == "" {
		t.Fatalf("ErrConflictingWakeCondition Error empty")
	}
	if !errors.Is(ErrInvalidRequirement, ErrInvalidRequirement) {
		t.Fatalf("errors.Is failed for ErrInvalidRequirement")
	}
	if ErrInvalidRequirement.Error() == "" {
		t.Fatalf("ErrInvalidRequirement Error empty")
	}
	if !errors.Is(ErrDigestMismatch, ErrDigestMismatch) {
		t.Fatalf("errors.Is failed for ErrDigestMismatch")
	}
	if ErrDigestMismatch.Error() == "" {
		t.Fatalf("ErrDigestMismatch Error empty")
	}
	if !errors.Is(ErrNowRequired, ErrNowRequired) {
		t.Fatalf("errors.Is failed for ErrNowRequired")
	}
	if ErrNowRequired.Error() == "" {
		t.Fatalf("ErrNowRequired Error empty")
	}
	if !errors.Is(ErrEarlyWake, ErrEarlyWake) {
		t.Fatalf("errors.Is failed for ErrEarlyWake")
	}
	if ErrEarlyWake.Error() == "" {
		t.Fatalf("ErrEarlyWake Error empty")
	}
	if !errors.Is(ErrUnknownEventKind, ErrUnknownEventKind) {
		t.Fatalf("errors.Is failed for ErrUnknownEventKind")
	}
	if ErrUnknownEventKind.Error() == "" {
		t.Fatalf("ErrUnknownEventKind Error empty")
	}
}
