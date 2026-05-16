package termination

import (
	"hcm-next-executor/internal/blockshared"
	"hcm-next-executor/internal/executor"
)

const (
	terminationExecutedEventType     = "EmployeeTerminationExecuted"
	workerSubjectType                = "worker"
	payrollConnectionID              = "fake_payroll"
	benefitsConnectionID             = "fake_benefits"
	processFinalPayOperation         = "processFinalPay"
	triggerCobraOperation            = "triggerCobra"
)

// PlanTransactionInput is the contract for deterministic termination transaction planning.
type PlanTransactionInput struct {
	ChangeRequestID string `json:"changeRequestId"`
	WorkerID        string `json:"workerId"`
	EffectiveAt     string `json:"effectiveAt"`
	TerminationType string `json:"terminationType"`
	CobraWindowDays int    `json:"cobraWindowDays"`
	FinalPayDate    string `json:"finalPayDate"`
}

// PlanTransactionOutput is the deterministic execution plan for an employee termination.
type PlanTransactionOutput struct {
	executor.BlockOutputContract
	InternalWrites       []InternalWriteSpec            `json:"internalWrites"`
	ExternalCallRequests []executor.ExternalCallRequest `json:"externalCallRequests"`
	ProjectionPatches    []ProjectionPatch              `json:"projectionPatches"`
}

// ExecutePlanTransaction builds deterministic internal write and external call request specs.
func ExecutePlanTransaction(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := blockshared.DecodeStrict[PlanTransactionInput](request.Input, "Termination transaction plan input does not match the expected contract.")
	if inputError != nil {
		return executor.BlockResult{}, inputError
	}

	if validationErrors := validatePlanTransactionInput(input); len(validationErrors) > 0 {
		return executor.BlockResult{}, executor.InvalidInputError("Termination transaction plan input is missing required fields.", map[string]any{
			"errors": validationErrors,
		})
	}

	cobraWindowDays := input.CobraWindowDays
	if cobraWindowDays <= 0 {
		cobraWindowDays = 60
	}

	internalWrites := []InternalWriteSpec{
		{
			EventType:   terminationExecutedEventType,
			SubjectType: workerSubjectType,
			SubjectID:   input.WorkerID,
			EffectiveAt: input.EffectiveAt,
			Payload: map[string]any{
				"changeRequestId": input.ChangeRequestID,
				"terminationType": input.TerminationType,
				"effectiveAt":     input.EffectiveAt,
				"finalPayDate":    input.FinalPayDate,
				"cobraWindowDays": cobraWindowDays,
			},
		},
	}

	externalCallRequests := []executor.ExternalCallRequest{
		{
			ConnectionID:   payrollConnectionID,
			Operation:      processFinalPayOperation,
			IdempotencyKey: "fake_payroll_final_pay_" + input.ChangeRequestID,
			Payload: map[string]any{
				"workerId":        input.WorkerID,
				"terminationType": input.TerminationType,
				"effectiveAt":     input.EffectiveAt,
				"finalPayDate":    input.FinalPayDate,
			},
			Reconciliation: map[string]any{
				"expectedExternalObject": "final_pay_record",
				"expectedFields": map[string]any{
					"workerId":    input.WorkerID,
					"finalPayDate": input.FinalPayDate,
					"status":      "processed",
				},
			},
		},
		{
			ConnectionID:   benefitsConnectionID,
			Operation:      triggerCobraOperation,
			IdempotencyKey: "fake_benefits_cobra_" + input.ChangeRequestID,
			Payload: map[string]any{
				"workerId":        input.WorkerID,
				"effectiveAt":     input.EffectiveAt,
				"cobraWindowDays": cobraWindowDays,
			},
			Reconciliation: map[string]any{
				"expectedExternalObject": "cobra_enrollment",
				"expectedFields": map[string]any{
					"workerId":  input.WorkerID,
					"triggered": true,
				},
			},
		},
	}

	projectionPatches := []ProjectionPatch{
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/employment/status",
			Value:      "terminated",
		},
	}

	output := PlanTransactionOutput{
		BlockOutputContract: executor.NewTransactionOutputContract(
			executor.RouteKeyTransactionPlanReady,
			blockshared.TransactionFacts(PlanTransactionBlockName, len(internalWrites), len(externalCallRequests), len(projectionPatches)),
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
				Message: "Termination transaction plan created.",
				Fields: map[string]any{
					"internalWriteCount":       len(internalWrites),
					"externalCallRequestCount": len(externalCallRequests),
					"projectionPatchCount":     len(projectionPatches),
					"terminationType":          input.TerminationType,
				},
			},
		},
	}, nil
}

func validatePlanTransactionInput(input PlanTransactionInput) []ValidationMessage {
	requiredFields := map[string]string{
		"changeRequestId": input.ChangeRequestID,
		"workerId":        input.WorkerID,
		"effectiveAt":     input.EffectiveAt,
		"terminationType": input.TerminationType,
		"finalPayDate":    input.FinalPayDate,
	}

	validationErrors := blockshared.RequiredTransactionFields(requiredFields)
	validationErrors = append(validationErrors, blockshared.TransactionEffectiveDateValidation(input.EffectiveAt)...)

	return validationErrors
}
