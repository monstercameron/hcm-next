// Package sdk exposes the stable block contracts and helpers for customer-authored blocks.
package sdk

import (
	"bytes"
	"encoding/json"

	"human-capital-management-suite-executor/internal/executor"
)

// ExecutionRequest is the deterministic block execution request.
type ExecutionRequest = executor.ExecutionRequest

// BlockResult is the deterministic block result returned to the executor.
type BlockResult = executor.BlockResult

// ExecutionError is the typed block execution error.
type ExecutionError = executor.ExecutionError

// OutputContract is the generic block output contract expected from customer blocks.
type OutputContract = executor.BlockOutputContract

// Fact is a deterministic business fact emitted by a block.
type Fact = executor.Fact

// ValidationIssue is the generic validation or warning message shape.
type ValidationIssue = executor.ValidationIssue

// LedgerFact is a block-proposed ledger fact.
type LedgerFact = executor.LedgerFact

// ProjectionPatch is a deterministic projection mutation.
type ProjectionPatch = executor.ProjectionPatch

// ExternalCallRequest is a side-effect request for Node to enqueue and execute.
type ExternalCallRequest = executor.ExternalCallRequest

// TransactionOperation is a durable non-ledger operation in a generic transaction plan.
type TransactionOperation = executor.TransactionOperation

// TransactionPlan is the generic deterministic transaction plan shape.
type TransactionPlan = executor.TransactionPlan

const (
	// RouteKeyValidationFailed routes workflows back to input correction.
	RouteKeyValidationFailed = executor.RouteKeyValidationFailed
	// RouteKeyApprovalRequired routes valid requests to approval.
	RouteKeyApprovalRequired = executor.RouteKeyApprovalRequired
	// RouteKeyApprovalWithWarnings routes valid requests to approval with review context.
	RouteKeyApprovalWithWarnings = executor.RouteKeyApprovalWithWarnings
	// RouteKeyNoApprovalRequired routes valid requests directly to execution.
	RouteKeyNoApprovalRequired = executor.RouteKeyNoApprovalRequired
	// RouteKeyTransactionPlanReady routes approved requests to transaction execution.
	RouteKeyTransactionPlanReady = executor.RouteKeyTransactionPlanReady
)

// DecodeInput decodes a block input with unknown-field rejection for deterministic contracts.
func DecodeInput[T any](rawInput json.RawMessage, contractName string) (T, *ExecutionError) {
	var input T
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		return input, executor.InvalidInputError(contractName+" input does not match the expected contract.", map[string]any{
			"decodeError": err.Error(),
		})
	}

	return input, nil
}

// RouteKeyForValidation returns the standard route key for validation block outputs.
func RouteKeyForValidation(isValid bool, warningCount int, requiresApproval bool) string {
	return executor.RouteKeyForValidation(isValid, warningCount, requiresApproval)
}

// NewValidationOutputContract builds generic output fields for validation blocks.
func NewValidationOutputContract(routeKey string, facts []Fact, validationErrors []ValidationIssue, warnings []ValidationIssue) OutputContract {
	return executor.NewValidationOutputContract(routeKey, facts, validationErrors, warnings)
}

// NewTransactionOutputContract builds generic output fields for transaction planning blocks.
func NewTransactionOutputContract(
	routeKey string,
	facts []Fact,
	ledgerFacts []LedgerFact,
	externalCalls []ExternalCallRequest,
	projectionPatches []ProjectionPatch,
	planType string,
	idempotencyKey string,
	operations []TransactionOperation,
) OutputContract {
	return executor.NewTransactionOutputContract(
		routeKey,
		facts,
		ledgerFacts,
		externalCalls,
		projectionPatches,
		planType,
		idempotencyKey,
		operations,
	)
}
