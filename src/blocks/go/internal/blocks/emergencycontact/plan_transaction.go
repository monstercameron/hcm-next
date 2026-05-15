package emergencycontact

import (
	"sort"
	"strings"

	"hcm-next-executor/internal/blockshared"
	"hcm-next-executor/internal/executor"
)

const (
	emergencyContactUpdatedEventType = "EmployeeEmergencyContactUpdated"
	workerSubjectType                = "worker"
	fakeHRISConnectionID             = "fake_hris"
	updateEmergencyContactsOperation = "updateEmergencyContacts"
)

// PlanTransactionInput is the contract for deterministic emergency-contact transaction planning.
type PlanTransactionInput struct {
	ChangeRequestID          string             `json:"changeRequestId"`
	WorkerID                 string             `json:"workerId"`
	CurrentEmergencyContacts []EmergencyContact `json:"currentEmergencyContacts"`
	ProposedEmergencyContact EmergencyContact   `json:"proposedEmergencyContact"`
	EffectiveAt              string             `json:"effectiveAt"`
}

// PlanTransactionOutput is the deterministic execution plan for an emergency-contact update.
type PlanTransactionOutput struct {
	executor.BlockOutputContract
	InternalWrites       []InternalWriteSpec            `json:"internalWrites"`
	ExternalCallRequests []executor.ExternalCallRequest `json:"externalCallRequests"`
	ProjectionPatches    []ProjectionPatch              `json:"projectionPatches"`
}

// ExecutePlanTransaction builds deterministic internal write and external call request specs.
func ExecutePlanTransaction(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := blockshared.DecodeStrict[PlanTransactionInput](request.Input, "Emergency contact transaction plan input does not match the expected contract.")
	if inputError != nil {
		return executor.BlockResult{}, inputError
	}

	if validationErrors := validatePlanTransactionInput(input); len(validationErrors) > 0 {
		return executor.BlockResult{}, executor.InvalidInputError("Emergency contact transaction plan input is missing required fields.", map[string]any{
			"errors": validationErrors,
		})
	}

	newEmergencyContacts := upsertEmergencyContact(input.CurrentEmergencyContacts, input.ProposedEmergencyContact)
	internalWrites := []InternalWriteSpec{
		{
			EventType:   emergencyContactUpdatedEventType,
			SubjectType: workerSubjectType,
			SubjectID:   input.WorkerID,
			EffectiveAt: input.EffectiveAt,
			Payload: map[string]any{
				"previousEmergencyContacts": input.CurrentEmergencyContacts,
				"changedEmergencyContact":   input.ProposedEmergencyContact,
				"newEmergencyContacts":      newEmergencyContacts,
			},
		},
	}

	externalCallRequests := []executor.ExternalCallRequest{
		{
			ConnectionID:   fakeHRISConnectionID,
			Operation:      updateEmergencyContactsOperation,
			IdempotencyKey: "fake_hris_emergency_contact_" + input.ChangeRequestID,
			Payload: map[string]any{
				"workerId":          input.WorkerID,
				"emergencyContacts": newEmergencyContacts,
				"effectiveAt":       input.EffectiveAt,
			},
			Reconciliation: map[string]any{
				"expectedExternalObject": "worker_emergency_contacts",
				"expectedFields": map[string]any{
					"workerId":          input.WorkerID,
					"emergencyContacts": newEmergencyContacts,
				},
			},
		},
	}

	projectionPatches := emergencyContactProjectionPatches(newEmergencyContacts)
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
				Message: "Emergency contact transaction plan created.",
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
		"changeRequestId":                       input.ChangeRequestID,
		"workerId":                              input.WorkerID,
		"proposedEmergencyContact.contactId":    input.ProposedEmergencyContact.ContactID,
		"proposedEmergencyContact.name":         input.ProposedEmergencyContact.Name,
		"proposedEmergencyContact.relationship": input.ProposedEmergencyContact.Relationship,
		"proposedEmergencyContact.phone":        input.ProposedEmergencyContact.Phone,
		"effectiveAt":                           input.EffectiveAt,
	}

	validationErrors = append(validationErrors, blockshared.RequiredTransactionFields(requiredFields)...)
	validationErrors = append(validationErrors, blockshared.TransactionEffectiveDateValidation(input.EffectiveAt)...)

	return validationErrors
}

func emergencyContactProjectionPatches(newEmergencyContacts []EmergencyContact) []ProjectionPatch {
	return []ProjectionPatch{
		{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/emergencyContacts",
			Value:      newEmergencyContacts,
		},
	}
}

func upsertEmergencyContact(currentContacts []EmergencyContact, proposedContact EmergencyContact) []EmergencyContact {
	updatedContacts := make([]EmergencyContact, 0, len(currentContacts)+1)
	didReplaceContact := false

	for _, currentContact := range currentContacts {
		if strings.TrimSpace(currentContact.ContactID) == strings.TrimSpace(proposedContact.ContactID) {
			updatedContacts = append(updatedContacts, proposedContact)
			didReplaceContact = true
			continue
		}

		updatedContacts = append(updatedContacts, currentContact)
	}

	if !didReplaceContact {
		updatedContacts = append(updatedContacts, proposedContact)
	}

	sort.SliceStable(updatedContacts, func(leftIndex int, rightIndex int) bool {
		if updatedContacts[leftIndex].Priority == updatedContacts[rightIndex].Priority {
			return updatedContacts[leftIndex].ContactID < updatedContacts[rightIndex].ContactID
		}

		return updatedContacts[leftIndex].Priority < updatedContacts[rightIndex].Priority
	})

	return updatedContacts
}
