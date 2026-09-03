package workflow

import (
	"errors"
	"testing"
)

func TestErrors_Smoke(t *testing.T) {
	if ErrCompile == nil {
		t.Fatalf("ErrCompile is nil")
	}
	if CodeTypeMismatch == "" {
		t.Fatalf("CodeTypeMismatch is empty")
	}
	if CodeUnresolvedRef == "" {
		t.Fatalf("CodeUnresolvedRef is empty")
	}
	if CodeInvalidDefinition == "" {
		t.Fatalf("CodeInvalidDefinition is empty")
	}
	if CodePhaseNotImplemented == "" {
		t.Fatalf("CodePhaseNotImplemented is empty")
	}
	if CodeUnreachableNode == "" {
		t.Fatalf("CodeUnreachableNode is empty")
	}
	if CodeImplicitFirstEdge == "" {
		t.Fatalf("CodeImplicitFirstEdge is empty")
	}
	if CodeMissingRoute == "" {
		t.Fatalf("CodeMissingRoute is empty")
	}
	if CodeDuplicateRoute == "" {
		t.Fatalf("CodeDuplicateRoute is empty")
	}
	if CodeUnknownRoute == "" {
		t.Fatalf("CodeUnknownRoute is empty")
	}
	if CodeInvalidTerminalPath == "" {
		t.Fatalf("CodeInvalidTerminalPath is empty")
	}
	if CodeUndeclaredCycle == "" {
		t.Fatalf("CodeUndeclaredCycle is empty")
	}
	if CodeUnboundedFanout == "" {
		t.Fatalf("CodeUnboundedFanout is empty")
	}
	if CodeSourceNotPredecessor == "" {
		t.Fatalf("CodeSourceNotPredecessor is empty")
	}
	if CodeDuplicateMapping == "" {
		t.Fatalf("CodeDuplicateMapping is empty")
	}
	if CodeNonIdempotentRetry == "" {
		t.Fatalf("CodeNonIdempotentRetry is empty")
	}
	if CodeUnobservedEffect == "" {
		t.Fatalf("CodeUnobservedEffect is empty")
	}
	if CodeMutationInSimulation == "" {
		t.Fatalf("CodeMutationInSimulation is empty")
	}
	if CodeEffectDeclarationConflict == "" {
		t.Fatalf("CodeEffectDeclarationConflict is empty")
	}
	if CodeWriteEffectRefusedP1A == "" {
		t.Fatalf("CodeWriteEffectRefusedP1A is empty")
	}
	if CodeMissingGovernanceEvaluation == "" {
		t.Fatalf("CodeMissingGovernanceEvaluation is empty")
	}
	if CodeUnresolvedApprovalScope == "" {
		t.Fatalf("CodeUnresolvedApprovalScope is empty")
	}
	if CodeUnvalidatedAgentOutput == "" {
		t.Fatalf("CodeUnvalidatedAgentOutput is empty")
	}
	if CodeUnresolvedObligation == "" {
		t.Fatalf("CodeUnresolvedObligation is empty")
	}
	if CodeUnauthorizedScope == "" {
		t.Fatalf("CodeUnauthorizedScope is empty")
	}
	if CodeUnrestrictedResultCopy == "" {
		t.Fatalf("CodeUnrestrictedResultCopy is empty")
	}
	if CodeMutableDecisionInput == "" {
		t.Fatalf("CodeMutableDecisionInput is empty")
	}
	if CodeNonExclusiveRoutes == "" {
		t.Fatalf("CodeNonExclusiveRoutes is empty")
	}
	if CodeArbitraryCode == "" {
		t.Fatalf("CodeArbitraryCode is empty")
	}
	if CodeNondeterministicTransform == "" {
		t.Fatalf("CodeNondeterministicTransform is empty")
	}
	if CodeUnpinnedLookup == "" {
		t.Fatalf("CodeUnpinnedLookup is empty")
	}
	if CodeResourceLimitExceeded == "" {
		t.Fatalf("CodeResourceLimitExceeded is empty")
	}
	if CodeUnsatisfiedSanitizer == "" {
		t.Fatalf("CodeUnsatisfiedSanitizer is empty")
	}
	if CodeReceiptIsNotObservation == "" {
		t.Fatalf("CodeReceiptIsNotObservation is empty")
	}
	if CodeStaleObservationAccepted == "" {
		t.Fatalf("CodeStaleObservationAccepted is empty")
	}
	if CodeDegradedCollapsedToPass == "" {
		t.Fatalf("CodeDegradedCollapsedToPass is empty")
	}
	if CodeRetryExhaustionFalseCompletion == "" {
		t.Fatalf("CodeRetryExhaustionFalseCompletion is empty")
	}
	if CodeMissingDimension == "" {
		t.Fatalf("CodeMissingDimension is empty")
	}
	if CodeSixthDimension == "" {
		t.Fatalf("CodeSixthDimension is empty")
	}
	if CodeIllegalTerminalTuple == "" {
		t.Fatalf("CodeIllegalTerminalTuple is empty")
	}
	if CodeTerminalNotInProfile == "" {
		t.Fatalf("CodeTerminalNotInProfile is empty")
	}
	if CodeObligationCollapsed == "" {
		t.Fatalf("CodeObligationCollapsed is empty")
	}
	if CodeDegradedCollapsedToSuccess == "" {
		t.Fatalf("CodeDegradedCollapsedToSuccess is empty")
	}
}

func TestErrors_ErrorsIs(t *testing.T) {
	if !errors.Is(ErrCompile, ErrCompile) {
		t.Fatalf("errors.Is failed for ErrCompile")
	}
	if ErrCompile.Error() == "" {
		t.Fatalf("ErrCompile Error empty")
	}
}
