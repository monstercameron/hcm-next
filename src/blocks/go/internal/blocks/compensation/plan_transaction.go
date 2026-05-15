package compensation

import (
	"math"
	"strings"

	"hcm-next-executor/internal/blockshared"
	"hcm-next-executor/internal/executor"
)

const (
	compensationUpdatedEventType      = "EmployeeCompensationUpdated"
	workerSubjectType                 = "worker"
	compensationDecisionConnectionID  = "third_party_compensation_decision"
	submitCompensationChangeOperation = "submitCompensationChange"
)

// PlanTransactionInput is the contract for deterministic compensation transaction planning.
type PlanTransactionInput struct {
	ChangeRequestID      string           `json:"changeRequestId"`
	WorkerID             string           `json:"workerId"`
	CurrentCompensation  CompensationInfo `json:"currentCompensation"`
	ProposedCompensation CompensationInfo `json:"proposedCompensation"`
	EffectiveAt          string           `json:"effectiveAt"`
	BusinessReason       string           `json:"businessReason"`
}

// PlanTransactionOutput is the deterministic execution plan for a compensation change.
type PlanTransactionOutput struct {
	executor.BlockOutputContract
	InternalWrites       []InternalWriteSpec            `json:"internalWrites"`
	ExternalCallRequests []executor.ExternalCallRequest `json:"externalCallRequests"`
	ProjectionPatches    []ProjectionPatch              `json:"projectionPatches"`
}

// ExecutePlanTransaction builds deterministic internal write and external call request specs.
func ExecutePlanTransaction(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := blockshared.DecodeStrict[PlanTransactionInput](request.Input, "Compensation transaction plan input does not match the expected contract.")
	if inputError != nil {
		return executor.BlockResult{}, inputError
	}

	if validationErrors := validatePlanTransactionInput(input); len(validationErrors) > 0 {
		return executor.BlockResult{}, executor.InvalidInputError("Compensation transaction plan input is missing required fields.", map[string]any{
			"errors": validationErrors,
		})
	}

	increasePercent := compensationIncreasePercent(input.CurrentCompensation, input.ProposedCompensation)
	internalWrites := []InternalWriteSpec{
		{
			EventType:   compensationUpdatedEventType,
			SubjectType: workerSubjectType,
			SubjectID:   input.WorkerID,
			EffectiveAt: input.EffectiveAt,
			Payload: map[string]any{
				"previousCompensation": input.CurrentCompensation,
				"newCompensation":      input.ProposedCompensation,
				"changedFields":        changedCompensationFields(input.CurrentCompensation, input.ProposedCompensation),
				"increasePercent":      increasePercent,
			},
		},
	}

	externalCallRequests := []executor.ExternalCallRequest{
		{
			ConnectionID:   compensationDecisionConnectionID,
			Operation:      submitCompensationChangeOperation,
			IdempotencyKey: "third_party_comp_decision_" + input.ChangeRequestID,
			Payload: map[string]any{
				"workerId":             input.WorkerID,
				"changeRequestId":      input.ChangeRequestID,
				"currentCompensation":  input.CurrentCompensation,
				"proposedCompensation": input.ProposedCompensation,
				"effectiveAt":          input.EffectiveAt,
				"businessReason":       input.BusinessReason,
			},
			Reconciliation: map[string]any{
				"expectedExternalObject": "compensation_decision",
				"acceptedStatuses":       []string{"accepted"},
			},
			CapabilityHints: []string{"synchronous_decision_required"},
		},
	}

	projectionPatches := []ProjectionPatch{
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/compensation",
			Value:      input.ProposedCompensation,
		},
	}
	output := PlanTransactionOutput{
		BlockOutputContract: executor.NewTransactionOutputContract(
			executor.RouteKeyTransactionPlanReady,
			blockshared.TransactionFacts(
				PlanTransactionBlockName,
				len(internalWrites),
				len(externalCallRequests),
				len(projectionPatches),
				executor.Fact{Key: "increasePercent", Value: increasePercent, Source: PlanTransactionBlockName},
			),
			internalWrites,
			externalCallRequests,
			projectionPatches,
			PlanTransactionBlockName,
			request.Context.IdempotencyKey,
			nil,
		),
		InternalWrites:       internalWrites,
		ExternalCallRequests: externalCallRequests,
		ProjectionPatches:    projectionPatches,
	}

	return executor.BlockResult{
		Output:               output,
		ExternalCallRequests: externalCallRequests,
		Logs: []executor.ExecutionLog{
			{
				Level:   "info",
				Message: "Compensation transaction plan created.",
				Fields: map[string]any{
					"internalWriteCount":       len(internalWrites),
					"externalCallRequestCount": len(externalCallRequests),
					"projectionPatchCount":     len(projectionPatches),
				},
			},
		},
	}, nil
}

func validatePlanTransactionInput(input PlanTransactionInput) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)
	requiredFields := map[string]string{
		"changeRequestId":                    input.ChangeRequestID,
		"workerId":                           input.WorkerID,
		"proposedCompensation.currency":      input.ProposedCompensation.Currency,
		"proposedCompensation.payFrequency":  input.ProposedCompensation.PayFrequency,
		"proposedCompensation.effectiveDate": input.ProposedCompensation.EffectiveDate,
		"effectiveAt":                        input.EffectiveAt,
		"businessReason":                     input.BusinessReason,
	}

	validationErrors = append(validationErrors, blockshared.RequiredTransactionFields(requiredFields)...)

	if input.ProposedCompensation.Amount <= 0 {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "transaction_plan.proposed_amount_invalid",
			Field:   "proposedCompensation.amount",
			Message: "Proposed compensation amount must be greater than zero.",
		})
	}

	validationErrors = append(validationErrors, blockshared.TransactionEffectiveDateValidation(input.EffectiveAt)...)

	return validationErrors
}

func changedCompensationFields(currentCompensation CompensationInfo, proposedCompensation CompensationInfo) []string {
	changedFields := make([]string, 0, 5)

	if !equalCurrencyAmount(currentCompensation.Amount, proposedCompensation.Amount) {
		changedFields = append(changedFields, "compensation.amount")
	}

	if strings.TrimSpace(currentCompensation.Currency) != strings.TrimSpace(proposedCompensation.Currency) {
		changedFields = append(changedFields, "compensation.currency")
	}

	if strings.TrimSpace(currentCompensation.PayFrequency) != strings.TrimSpace(proposedCompensation.PayFrequency) {
		changedFields = append(changedFields, "compensation.payFrequency")
	}

	if !equalCurrencyAmount(currentCompensation.BonusTargetPercent, proposedCompensation.BonusTargetPercent) {
		changedFields = append(changedFields, "compensation.bonusTargetPercent")
	}

	if strings.TrimSpace(currentCompensation.EffectiveDate) != strings.TrimSpace(proposedCompensation.EffectiveDate) {
		changedFields = append(changedFields, "compensation.effectiveDate")
	}

	return changedFields
}

func compensationIncreasePercent(currentCompensation CompensationInfo, proposedCompensation CompensationInfo) float64 {
	if currentCompensation.Amount <= 0 {
		return 0
	}

	return ((proposedCompensation.Amount - currentCompensation.Amount) / currentCompensation.Amount) * 100
}

func equalCurrencyAmount(left float64, right float64) bool {
	return math.Abs(left-right) < 0.0001
}
