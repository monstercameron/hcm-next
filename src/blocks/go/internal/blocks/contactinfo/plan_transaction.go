package contactinfo

import (
	"hcm-next-executor/internal/blockshared"
	"hcm-next-executor/internal/executor"
)

const (
	contactInfoUpdatedEventType = "EmployeeContactInfoUpdated"
	workerSubjectType           = "worker"
	fakeHRISConnectionID        = "fake_hris"
	updateContactInfoOperation  = "updateContactInfo"
)

// PlanTransactionInput is the contract for deterministic contact-information transaction planning.
type PlanTransactionInput struct {
	ChangeRequestID     string      `json:"changeRequestId"`
	WorkerID            string      `json:"workerId"`
	CurrentContactInfo  ContactInfo `json:"currentContactInfo"`
	ProposedContactInfo ContactInfo `json:"proposedContactInfo"`
	EffectiveAt         string      `json:"effectiveAt"`
}

// PlanTransactionOutput is the deterministic execution plan for a contact-information update.
type PlanTransactionOutput struct {
	executor.BlockOutputContract
	InternalWrites       []InternalWriteSpec            `json:"internalWrites"`
	ExternalCallRequests []executor.ExternalCallRequest `json:"externalCallRequests"`
	ProjectionPatches    []ProjectionPatch              `json:"projectionPatches"`
}

// ExecutePlanTransaction builds deterministic internal write and external call request specs.
func ExecutePlanTransaction(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := blockshared.DecodeStrict[PlanTransactionInput](request.Input, "Contact information transaction plan input does not match the expected contract.")
	if inputError != nil {
		return executor.BlockResult{}, inputError
	}

	if validationErrors := validatePlanTransactionInput(input); len(validationErrors) > 0 {
		return executor.BlockResult{}, executor.InvalidInputError("Contact information transaction plan input is missing required fields.", map[string]any{
			"errors": validationErrors,
		})
	}

	proposedContactInfo := normalizeContactInfo(input.ProposedContactInfo)
	internalWrites := []InternalWriteSpec{
		{
			EventType:   contactInfoUpdatedEventType,
			SubjectType: workerSubjectType,
			SubjectID:   input.WorkerID,
			EffectiveAt: input.EffectiveAt,
			Payload: map[string]any{
				"previousContactInfo": input.CurrentContactInfo,
				"newContactInfo":      proposedContactInfo,
				"changedFields":       changedContactInfoFields(input.CurrentContactInfo, proposedContactInfo),
			},
		},
	}

	externalCallRequests := []executor.ExternalCallRequest{
		{
			ConnectionID:   fakeHRISConnectionID,
			Operation:      updateContactInfoOperation,
			IdempotencyKey: "fake_hris_contact_info_" + input.ChangeRequestID,
			Payload: map[string]any{
				"workerId":    input.WorkerID,
				"contactInfo": proposedContactInfo,
				"effectiveAt": input.EffectiveAt,
			},
			Reconciliation: map[string]any{
				"expectedExternalObject": "worker_contact_info",
				"expectedFields": map[string]any{
					"workerId":    input.WorkerID,
					"contactInfo": proposedContactInfo,
				},
			},
		},
	}

	projectionPatches := contactInfoProjectionPatches(proposedContactInfo)
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
				Message: "Contact information transaction plan created.",
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
		"changeRequestId":                            input.ChangeRequestID,
		"workerId":                                   input.WorkerID,
		"proposedContactInfo.homeAddress.line1":      input.ProposedContactInfo.HomeAddress.Line1,
		"proposedContactInfo.homeAddress.city":       input.ProposedContactInfo.HomeAddress.City,
		"proposedContactInfo.homeAddress.region":     input.ProposedContactInfo.HomeAddress.Region,
		"proposedContactInfo.homeAddress.postalCode": input.ProposedContactInfo.HomeAddress.PostalCode,
		"proposedContactInfo.homeAddress.country":    input.ProposedContactInfo.HomeAddress.Country,
		"effectiveAt":                                input.EffectiveAt,
	}

	validationErrors = append(validationErrors, blockshared.RequiredTransactionFields(requiredFields)...)
	validationErrors = append(validationErrors, blockshared.TransactionEffectiveDateValidation(input.EffectiveAt)...)

	return validationErrors
}

func contactInfoProjectionPatches(proposedContactInfo ContactInfo) []ProjectionPatch {
	return []ProjectionPatch{
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/contact/personalEmail",
			Value:      proposedContactInfo.PersonalEmail,
		},
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/contact/mobilePhone",
			Value:      proposedContactInfo.MobilePhone,
		},
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/contact/homeAddress",
			Value:      proposedContactInfo.HomeAddress,
		},
	}
}

func changedContactInfoFields(currentContactInfo ContactInfo, proposedContactInfo ContactInfo) []string {
	changedFields := make([]string, 0, 3)
	normalizedCurrent := normalizeContactInfo(currentContactInfo)

	if blockshared.NormalizedEmail(normalizedCurrent.PersonalEmail) != blockshared.NormalizedEmail(proposedContactInfo.PersonalEmail) {
		changedFields = append(changedFields, "contact.personalEmail")
	}

	if blockshared.DigitsOnly(blockshared.StringValue(normalizedCurrent.MobilePhone)) != blockshared.DigitsOnly(blockshared.StringValue(proposedContactInfo.MobilePhone)) {
		changedFields = append(changedFields, "contact.mobilePhone")
	}

	if !addressEqual(normalizedCurrent.HomeAddress, proposedContactInfo.HomeAddress) {
		changedFields = append(changedFields, "contact.homeAddress")
	}

	return changedFields
}
