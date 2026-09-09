package emergencycontact

import (
	"strings"
	"time"

	"human-capital-management-suite-executor/internal/blockshared"
	"human-capital-management-suite-executor/internal/executor"
)

// PreflightInput is the contract for emergency-contact update validation.
type PreflightInput struct {
	CurrentEmergencyContacts []EmergencyContact `json:"currentEmergencyContacts"`
	ProposedEmergencyContact EmergencyContact   `json:"proposedEmergencyContact"`
	EffectiveAt              string             `json:"effectiveAt"`
	BusinessReason           string             `json:"businessReason"`
}

// PreflightOutput is the deterministic validation result for an emergency-contact update.
type PreflightOutput struct {
	executor.BlockOutputContract
	Valid            bool                `json:"valid"`
	RiskLevel        string              `json:"riskLevel"`
	RequiresEvidence bool                `json:"requiresEvidence"`
	RequiresApproval bool                `json:"requiresApproval"`
	Warnings         []ValidationMessage `json:"warnings"`
	Errors           []ValidationMessage `json:"errors"`
}

// ExecutePreflight validates an emergency-contact update request without side effects.
func ExecutePreflight(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := blockshared.DecodeStrict[PreflightInput](request.Input, "Emergency contact preflight input does not match the expected contract.")
	if inputError != nil {
		return executor.BlockResult{}, inputError
	}

	evaluationDate, evaluationDateError := blockshared.ParseContextEffectiveDate(request.Context.EffectiveAt)
	if evaluationDateError != nil {
		return executor.BlockResult{}, evaluationDateError
	}

	validationWarnings := validateDuplicateContact(input)
	validationErrors := validatePreflightInput(input, evaluationDate)
	isValid := len(validationErrors) == 0
	riskLevel := blockshared.RiskLevelForValidation(isValid, len(validationWarnings))
	routeKey := executor.RouteKeyForValidation(isValid, len(validationWarnings), true)

	output := PreflightOutput{
		BlockOutputContract: executor.NewValidationOutputContract(
			routeKey,
			blockshared.PreflightFacts(PreflightBlockName, isValid, riskLevel, false, true, len(validationWarnings)),
			validationErrors,
			validationWarnings,
		),
		Valid:            isValid,
		RiskLevel:        riskLevel,
		RequiresEvidence: false,
		RequiresApproval: true,
		Warnings:         validationWarnings,
		Errors:           validationErrors,
	}

	return executor.BlockResult{
		Output: output,
		Logs: []executor.ExecutionLog{
			{
				Level:   "info",
				Message: "Emergency contact preflight completed.",
				Fields: map[string]any{
					"valid":        isValid,
					"riskLevel":    riskLevel,
					"warningCount": len(validationWarnings),
				},
			},
		},
	}, nil
}

func validatePreflightInput(input PreflightInput, evaluationDate time.Time) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)
	contact := input.ProposedEmergencyContact
	businessReason := strings.TrimSpace(input.BusinessReason)

	if strings.TrimSpace(contact.ContactID) == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "emergency_contact.contact_id_required",
			Field:   "proposedEmergencyContact.contactId",
			Message: "Contact ID is required.",
		})
	}

	if strings.TrimSpace(contact.Name) == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "emergency_contact.name_required",
			Field:   "proposedEmergencyContact.name",
			Message: "Contact name is required.",
		})
	}

	if strings.TrimSpace(contact.Relationship) == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "emergency_contact.relationship_required",
			Field:   "proposedEmergencyContact.relationship",
			Message: "Relationship is required.",
		})
	}

	if blockshared.PhoneDigitCount(contact.Phone) < 7 {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "emergency_contact.phone_invalid",
			Field:   "proposedEmergencyContact.phone",
			Message: "Phone must include at least seven digits.",
		})
	}

	if contact.Email != nil && !strings.Contains(strings.TrimSpace(*contact.Email), "@") {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "emergency_contact.email_invalid",
			Field:   "proposedEmergencyContact.email",
			Message: "Email must be a valid email address when provided.",
		})
	}

	if contact.Priority < 1 {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "emergency_contact.priority_invalid",
			Field:   "proposedEmergencyContact.priority",
			Message: "Priority must be at least 1.",
		})
	}

	if businessReason == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "emergency_contact.business_reason_required",
			Field:   "businessReason",
			Message: "Business reason is required.",
		})
	}

	validationErrors = append(validationErrors, blockshared.EffectiveDateValidation("emergency_contact", input.EffectiveAt, evaluationDate, 180)...)

	if emergencyContactUnchanged(input.CurrentEmergencyContacts, contact) {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "emergency_contact.unchanged",
			Field:   "proposedEmergencyContact",
			Message: "Emergency contact must differ from the current contact.",
		})
	}

	return validationErrors
}

func validateDuplicateContact(input PreflightInput) []ValidationMessage {
	warnings := make([]ValidationMessage, 0)
	proposedContact := input.ProposedEmergencyContact
	proposedPhoneDigits := blockshared.DigitsOnly(proposedContact.Phone)

	for _, currentContact := range input.CurrentEmergencyContacts {
		if strings.TrimSpace(currentContact.ContactID) == strings.TrimSpace(proposedContact.ContactID) {
			continue
		}

		if proposedPhoneDigits != "" && blockshared.DigitsOnly(currentContact.Phone) == proposedPhoneDigits {
			warnings = append(warnings, ValidationMessage{
				Code:    "emergency_contact.duplicate_phone",
				Field:   "proposedEmergencyContact.phone",
				Message: "Another emergency contact already uses this phone number.",
			})
		}
	}

	return warnings
}

func emergencyContactUnchanged(currentContacts []EmergencyContact, proposedContact EmergencyContact) bool {
	for _, currentContact := range currentContacts {
		if strings.TrimSpace(currentContact.ContactID) != strings.TrimSpace(proposedContact.ContactID) {
			continue
		}

		return strings.TrimSpace(currentContact.Name) == strings.TrimSpace(proposedContact.Name) &&
			strings.TrimSpace(currentContact.Relationship) == strings.TrimSpace(proposedContact.Relationship) &&
			blockshared.DigitsOnly(currentContact.Phone) == blockshared.DigitsOnly(proposedContact.Phone) &&
			blockshared.NormalizedEmail(currentContact.Email) == blockshared.NormalizedEmail(proposedContact.Email) &&
			currentContact.Priority == proposedContact.Priority
	}

	return false
}
