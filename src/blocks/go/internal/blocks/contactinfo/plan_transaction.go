package contactinfo

import (
	"bytes"
	"encoding/json"
	"strings"

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

// InternalWriteSpec describes an internal ledger/projection write that Node may apply.
type InternalWriteSpec struct {
	EventType   string         `json:"eventType"`
	SubjectType string         `json:"subjectType"`
	SubjectID   string         `json:"subjectId"`
	EffectiveAt string         `json:"effectiveAt"`
	Payload     map[string]any `json:"payload"`
}

// ProjectionPatch describes a deterministic projection mutation that Node may apply.
type ProjectionPatch struct {
	Projection string `json:"projection"`
	Operation  string `json:"operation"`
	Path       string `json:"path"`
	Value      any    `json:"value"`
}

// PlanTransactionOutput is the deterministic execution plan for a contact-information update.
type PlanTransactionOutput struct {
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

func decodePlanTransactionInput(rawInput json.RawMessage) (PlanTransactionInput, *executor.ExecutionError) {
	var input PlanTransactionInput
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		return PlanTransactionInput{}, executor.InvalidInputError("Contact information transaction plan input does not match the expected contract.", map[string]any{
			"decodeError": err.Error(),
		})
	}

	return input, nil
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

	if normalizedEmail(normalizedCurrent.PersonalEmail) != normalizedEmail(proposedContactInfo.PersonalEmail) {
		changedFields = append(changedFields, "contact.personalEmail")
	}

	if digitsOnly(pointerValue(normalizedCurrent.MobilePhone)) != digitsOnly(pointerValue(proposedContactInfo.MobilePhone)) {
		changedFields = append(changedFields, "contact.mobilePhone")
	}

	if !addressEqual(normalizedCurrent.HomeAddress, proposedContactInfo.HomeAddress) {
		changedFields = append(changedFields, "contact.homeAddress")
	}

	return changedFields
}
