package legalname

import (
	"bytes"
	"encoding/json"
	"strings"

	"hcm-next-executor/internal/executor"
)

const (
	personLegalNameChangedEventType = "PersonLegalNameChanged"
	workerSubjectType               = "worker"
	fakeHRISConnectionID            = "fake_hris"
	updateLegalNameOperation        = "updateLegalName"
)

// PlanTransactionInput is the contract for deterministic legal-name transaction planning.
type PlanTransactionInput struct {
	ChangeRequestID   string    `json:"changeRequestId"`
	WorkerID          string    `json:"workerId"`
	PersonID          string    `json:"personId"`
	CurrentLegalName  LegalName `json:"currentLegalName"`
	ProposedLegalName LegalName `json:"proposedLegalName"`
	EffectiveAt       string    `json:"effectiveAt"`
}

// PlanTransactionOutput is the deterministic execution plan for a legal-name change.
type PlanTransactionOutput struct {
	executor.BlockOutputContract
	InternalWrites       []InternalWriteSpec            `json:"internalWrites"`
	ExternalCallRequests []executor.ExternalCallRequest `json:"externalCallRequests"`
	ProjectionPatches    []ProjectionPatch              `json:"projectionPatches"`
}

// ExecutePlanTransaction builds deterministic internal write and external call request specs.
func ExecutePlanTransaction(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := decodePlanTransactionInput(request.Input)
	if inputError != nil {
		return executor.BlockResult{}, inputError
	}

	if validationErrors := validatePlanTransactionInput(input); len(validationErrors) > 0 {
		return executor.BlockResult{}, executor.InvalidInputError("Legal name transaction plan input is missing required fields.", map[string]any{
			"errors": validationErrors,
		})
	}

	internalWrites := []InternalWriteSpec{
		{
			EventType:   personLegalNameChangedEventType,
			SubjectType: workerSubjectType,
			SubjectID:   input.WorkerID,
			EffectiveAt: input.EffectiveAt,
			Payload: map[string]any{
				"personId":          input.PersonID,
				"previousLegalName": input.CurrentLegalName,
				"newLegalName":      input.ProposedLegalName,
			},
		},
	}

	externalCallRequests := []executor.ExternalCallRequest{
		{
			ConnectionID:   fakeHRISConnectionID,
			Operation:      updateLegalNameOperation,
			IdempotencyKey: "fake_hris_legal_name_" + input.ChangeRequestID,
			Payload: map[string]any{
				"workerId":    input.WorkerID,
				"legalName":   input.ProposedLegalName,
				"effectiveAt": input.EffectiveAt,
			},
			Reconciliation: map[string]any{
				"expectedExternalObject": "worker_legal_name",
				"expectedFields": map[string]any{
					"workerId":  input.WorkerID,
					"legalName": input.ProposedLegalName,
				},
			},
		},
	}

	projectionPatches := legalNameProjectionPatches(input.ProposedLegalName)
	output := PlanTransactionOutput{
		BlockOutputContract: executor.NewTransactionOutputContract(
			executor.RouteKeyTransactionPlanReady,
			legalNameTransactionFacts(len(internalWrites), len(externalCallRequests), len(projectionPatches)),
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
				Message: "Legal name transaction plan created.",
				Fields: map[string]any{
					"internalWriteCount":       len(internalWrites),
					"externalCallRequestCount": len(externalCallRequests),
					"projectionPatchCount":     len(projectionPatches),
				},
			},
		},
	}, nil
}

func legalNameTransactionFacts(internalWriteCount int, externalCallCount int, projectionPatchCount int) []executor.Fact {
	return []executor.Fact{
		{Key: "ledgerFactCount", Value: internalWriteCount, Source: PlanTransactionBlockName},
		{Key: "externalCallCount", Value: externalCallCount, Source: PlanTransactionBlockName},
		{Key: "projectionPatchCount", Value: projectionPatchCount, Source: PlanTransactionBlockName},
	}
}

func decodePlanTransactionInput(rawInput json.RawMessage) (PlanTransactionInput, *executor.ExecutionError) {
	var input PlanTransactionInput
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		return PlanTransactionInput{}, executor.InvalidInputError("Legal name transaction plan input does not match the expected contract.", map[string]any{
			"decodeError": err.Error(),
		})
	}

	return input, nil
}

func validatePlanTransactionInput(input PlanTransactionInput) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)
	requiredFields := map[string]string{
		"changeRequestId":         input.ChangeRequestID,
		"workerId":                input.WorkerID,
		"personId":                input.PersonID,
		"currentLegalName.first":  input.CurrentLegalName.First,
		"currentLegalName.last":   input.CurrentLegalName.Last,
		"proposedLegalName.first": input.ProposedLegalName.First,
		"proposedLegalName.last":  input.ProposedLegalName.Last,
		"effectiveAt":             input.EffectiveAt,
	}

	for field, value := range requiredFields {
		if strings.TrimSpace(value) == "" {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "transaction_plan.required",
				Field:   field,
				Message: "Required transaction planning field is missing.",
			})
		}
	}

	if strings.TrimSpace(input.EffectiveAt) != "" {
		if _, err := parseDate(input.EffectiveAt); err != nil {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "transaction_plan.effective_at_invalid",
				Field:   "effectiveAt",
				Message: "Effective date must be a valid date.",
			})
		}
	}

	return validationErrors
}

func legalNameProjectionPatches(proposedLegalName LegalName) []ProjectionPatch {
	return []ProjectionPatch{
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/person/legalName",
			Value:      proposedLegalName,
		},
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/person/displayName",
			Value:      displayNameFromLegalName(proposedLegalName),
		},
	}
}

func displayNameFromLegalName(legalName LegalName) string {
	nameParts := []string{
		strings.TrimSpace(legalName.First),
	}

	if legalName.Middle != nil {
		nameParts = append(nameParts, strings.TrimSpace(*legalName.Middle))
	}

	nameParts = append(nameParts, strings.TrimSpace(legalName.Last))

	displayNameParts := make([]string, 0, len(nameParts))
	for _, namePart := range nameParts {
		if namePart != "" {
			displayNameParts = append(displayNameParts, namePart)
		}
	}

	return strings.Join(displayNameParts, " ")
}
