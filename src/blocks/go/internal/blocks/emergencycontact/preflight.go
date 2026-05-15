package emergencycontact

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"hcm-next-executor/internal/executor"
)

const (
	preflightRiskLow    = "low"
	preflightRiskMedium = "medium"
	preflightRiskHigh   = "high"
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
	Valid            bool                `json:"valid"`
	RiskLevel        string              `json:"riskLevel"`
	RequiresEvidence bool                `json:"requiresEvidence"`
	RequiresApproval bool                `json:"requiresApproval"`
	Warnings         []ValidationMessage `json:"warnings"`
	Errors           []ValidationMessage `json:"errors"`
}

// ExecutePreflight validates an emergency-contact update request without side effects.
func ExecutePreflight(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := decodePreflightInput(request.Input)
	if inputError != nil {
		return executor.BlockResult{}, inputError
	}

	evaluationDate, evaluationDateError := parseDate(request.Context.EffectiveAt)
	if evaluationDateError != nil {
		return executor.BlockResult{}, executor.InvalidInputError("context.effectiveAt must be a valid date or RFC3339 timestamp.", map[string]any{
			"field": "context.effectiveAt",
		})
	}

	validationWarnings := validateDuplicateContact(input)
	validationErrors := validatePreflightInput(input, evaluationDate)
	isValid := len(validationErrors) == 0
	riskLevel := riskLevelForPreflight(isValid, validationWarnings)

	output := PreflightOutput{
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

func decodePreflightInput(rawInput json.RawMessage) (PreflightInput, *executor.ExecutionError) {
	var input PreflightInput
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		return PreflightInput{}, executor.InvalidInputError("Emergency contact preflight input does not match the expected contract.", map[string]any{
			"decodeError": err.Error(),
		})
	}

	return input, nil
}

func validatePreflightInput(input PreflightInput, evaluationDate time.Time) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)
	contact := input.ProposedEmergencyContact
	businessReason := strings.TrimSpace(input.BusinessReason)
	effectiveAt := strings.TrimSpace(input.EffectiveAt)

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

	if phoneDigitCount(contact.Phone) < 7 {
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

	if effectiveAt == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "emergency_contact.effective_at_required",
			Field:   "effectiveAt",
			Message: "Effective date is required.",
		})
	} else {
		effectiveDate, parseError := parseDate(effectiveAt)
		if parseError != nil {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "emergency_contact.effective_at_invalid",
				Field:   "effectiveAt",
				Message: "Effective date must be a valid date.",
			})
		} else if effectiveDate.Before(evaluationDate.AddDate(0, 0, -180)) {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "emergency_contact.effective_at_too_far_in_past",
				Field:   "effectiveAt",
				Message: "Effective date cannot be more than 180 days in the past.",
			})
		}
	}

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
	proposedPhoneDigits := digitsOnly(proposedContact.Phone)

	for _, currentContact := range input.CurrentEmergencyContacts {
		if strings.TrimSpace(currentContact.ContactID) == strings.TrimSpace(proposedContact.ContactID) {
			continue
		}

		if proposedPhoneDigits != "" && digitsOnly(currentContact.Phone) == proposedPhoneDigits {
			warnings = append(warnings, ValidationMessage{
				Code:    "emergency_contact.duplicate_phone",
				Field:   "proposedEmergencyContact.phone",
				Message: "Another emergency contact already uses this phone number.",
			})
		}
	}

	return warnings
}

func riskLevelForPreflight(isValid bool, warnings []ValidationMessage) string {
	if !isValid {
		return preflightRiskHigh
	}

	if len(warnings) > 0 {
		return preflightRiskMedium
	}

	return preflightRiskLow
}

func emergencyContactUnchanged(currentContacts []EmergencyContact, proposedContact EmergencyContact) bool {
	for _, currentContact := range currentContacts {
		if strings.TrimSpace(currentContact.ContactID) != strings.TrimSpace(proposedContact.ContactID) {
			continue
		}

		return strings.TrimSpace(currentContact.Name) == strings.TrimSpace(proposedContact.Name) &&
			strings.TrimSpace(currentContact.Relationship) == strings.TrimSpace(proposedContact.Relationship) &&
			digitsOnly(currentContact.Phone) == digitsOnly(proposedContact.Phone) &&
			normalizedEmail(currentContact.Email) == normalizedEmail(proposedContact.Email) &&
			currentContact.Priority == proposedContact.Priority
	}

	return false
}

func normalizedEmail(value *string) string {
	if value == nil {
		return ""
	}

	return strings.ToLower(strings.TrimSpace(*value))
}

func phoneDigitCount(value string) int {
	return len(digitsOnly(value))
}

func digitsOnly(value string) string {
	var builder strings.Builder

	for _, runeValue := range value {
		if runeValue >= '0' && runeValue <= '9' {
			builder.WriteRune(runeValue)
		}
	}

	return builder.String()
}

func parseDate(value string) (time.Time, error) {
	trimmedValue := strings.TrimSpace(value)
	layouts := []string{"2006-01-02", time.RFC3339}

	var lastError error
	for _, layout := range layouts {
		parsedTime, err := time.Parse(layout, trimmedValue)
		if err == nil {
			return time.Date(parsedTime.Year(), parsedTime.Month(), parsedTime.Day(), 0, 0, 0, 0, time.UTC), nil
		}

		lastError = err
	}

	return time.Time{}, lastError
}
