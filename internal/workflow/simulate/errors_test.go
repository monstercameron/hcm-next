package simulate

import (
	"errors"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	if ErrSimulate == nil {
		t.Fatalf("ErrSimulate is nil")
	}
	if ErrValueType == nil {
		t.Fatalf("ErrValueType is nil")
	}
	if ErrMissingInput == nil {
		t.Fatalf("ErrMissingInput is nil")
	}
	if CodeSimulationSideEffectForbidden == "" {
		t.Fatalf("CodeSimulationSideEffectForbidden is empty")
	}
	if CodeWriteEffectInSimulate == "" {
		t.Fatalf("CodeWriteEffectInSimulate is empty")
	}
	if CodeModeNotAdmitted == "" {
		t.Fatalf("CodeModeNotAdmitted is empty")
	}
	if CodePlanNotZeroEffect == "" {
		t.Fatalf("CodePlanNotZeroEffect is empty")
	}
	if CodePlanUnverified == "" {
		t.Fatalf("CodePlanUnverified is empty")
	}
	if CodeStepNotImplemented == "" {
		t.Fatalf("CodeStepNotImplemented is empty")
	}
	if CodeUnresolvedSource == "" {
		t.Fatalf("CodeUnresolvedSource is empty")
	}
	if CodeOutputTypeMismatch == "" {
		t.Fatalf("CodeOutputTypeMismatch is empty")
	}
	if CodeMissingRoute == "" {
		t.Fatalf("CodeMissingRoute is empty")
	}
	if CodeStepBudgetExceeded == "" {
		t.Fatalf("CodeStepBudgetExceeded is empty")
	}
	if CodeHandlerFailed == "" {
		t.Fatalf("CodeHandlerFailed is empty")
	}
	if CodeIllegalTerminal == "" {
		t.Fatalf("CodeIllegalTerminal is empty")
	}
	if CodeNoTerminal == "" {
		t.Fatalf("CodeNoTerminal is empty")
	}
	if CodeReceiptInvalid == "" {
		t.Fatalf("CodeReceiptInvalid is empty")
	}
	if CodeInvalidOptions == "" {
		t.Fatalf("CodeInvalidOptions is empty")
	}
}

func TestErrors_ErrorsIs(t *testing.T) {
	if !errors.Is(ErrSimulate, ErrSimulate) {
		t.Fatalf("errors.Is failed for ErrSimulate")
	}
	if ErrSimulate.Error() == "" {
		t.Fatalf("ErrSimulate Error empty")
	}
	if !errors.Is(ErrValueType, ErrValueType) {
		t.Fatalf("errors.Is failed for ErrValueType")
	}
	if ErrValueType.Error() == "" {
		t.Fatalf("ErrValueType Error empty")
	}
	if !errors.Is(ErrMissingInput, ErrMissingInput) {
		t.Fatalf("errors.Is failed for ErrMissingInput")
	}
	if ErrMissingInput.Error() == "" {
		t.Fatalf("ErrMissingInput Error empty")
	}
}
