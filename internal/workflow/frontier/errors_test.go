package frontier

import (
	"errors"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	if ErrFrontier == nil {
		t.Fatalf("ErrFrontier is nil")
	}
	if CodeInvalidPlan == "" {
		t.Fatalf("CodeInvalidPlan is empty")
	}
	if CodePlanMismatch == "" {
		t.Fatalf("CodePlanMismatch is empty")
	}
	if CodeUnknownNode == "" {
		t.Fatalf("CodeUnknownNode is empty")
	}
	if CodeNodeNotActive == "" {
		t.Fatalf("CodeNodeNotActive is empty")
	}
	if CodeAlreadyComplete == "" {
		t.Fatalf("CodeAlreadyComplete is empty")
	}
	if CodeUnknownOutcome == "" {
		t.Fatalf("CodeUnknownOutcome is empty")
	}
	if CodeMissingRoute == "" {
		t.Fatalf("CodeMissingRoute is empty")
	}
	if CodeNoMatchingRoute == "" {
		t.Fatalf("CodeNoMatchingRoute is empty")
	}
	if CodeNoFailureRoute == "" {
		t.Fatalf("CodeNoFailureRoute is empty")
	}
	if CodeAwaitNotAdmitted == "" {
		t.Fatalf("CodeAwaitNotAdmitted is empty")
	}
	if CodeJoinNotDeclared == "" {
		t.Fatalf("CodeJoinNotDeclared is empty")
	}
	if CodeInvalidJoinDeclaration == "" {
		t.Fatalf("CodeInvalidJoinDeclaration is empty")
	}
	if CodeMissingTerminal == "" {
		t.Fatalf("CodeMissingTerminal is empty")
	}
	if CodeTerminalMismatch == "" {
		t.Fatalf("CodeTerminalMismatch is empty")
	}
	if CodeTerminalFrontierRemains == "" {
		t.Fatalf("CodeTerminalFrontierRemains is empty")
	}
	if CodeInvalidState == "" {
		t.Fatalf("CodeInvalidState is empty")
	}
}

func TestErrors_ErrorsIs(t *testing.T) {
	if !errors.Is(ErrFrontier, ErrFrontier) {
		t.Fatalf("errors.Is failed for ErrFrontier")
	}
	if ErrFrontier.Error() == "" {
		t.Fatalf("ErrFrontier Error empty")
	}
}
