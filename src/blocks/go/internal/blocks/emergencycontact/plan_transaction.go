package emergencycontact

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

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

// InternalWriteSpec describes an internal ledger/projection write that Node may apply.
type InternalWriteSpec struct {
	EventType   string         `json:"eventType"`
	SubjectType string         `json:"subjectType"`
	SubjectID   string         `json:"subjectId"`
	EffectiveAt string         `json:"effectiveAt"`
	Payload     map[string]any `json:"payload"`
}

// PlanTransactionOutput is the deterministic execution plan for an emergency-contact update.
type PlanTransactionOutput struct {
	InternalWrites       []InternalWriteSpec            `json:"internalWrites"`
	ExternalCallRequests []executor.ExternalCallRequest `json:"externalCallRequests"`
}

// ExecutePlanTransaction builds deterministic internal write and external call request specs.
func ExecutePlanTransaction(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := decodePlanTransactionInput(request.Input)
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

	output := PlanTransactionOutput{
		InternalWrites:       internalWrites,
		ExternalCallRequests: externalCallRequests,
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
				},
			},
		},
	}, nil
}

func decodePlanTransactionInput(rawInput json.RawMessage) (PlanTransactionInput, *executor.ExecutionError) {
	var input PlanTransactionInput
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		return PlanTransactionInput{}, executor.InvalidInputError("Emergency contact transaction plan input does not match the expected contract.", map[string]any{
			"decodeError": err.Error(),
		})
	}

	return input, nil
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
