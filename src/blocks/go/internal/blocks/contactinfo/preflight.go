package contactinfo

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

// PreflightInput is the contract for contact-information update validation.
type PreflightInput struct {
	CurrentContactInfo  ContactInfo `json:"currentContactInfo"`
	ProposedContactInfo ContactInfo `json:"proposedContactInfo"`
	EffectiveAt         string      `json:"effectiveAt"`
	BusinessReason      string      `json:"businessReason"`
}

// PreflightOutput is the deterministic validation result for a contact-information update.
type PreflightOutput struct {
	Valid            bool                `json:"valid"`
	RiskLevel        string              `json:"riskLevel"`
	RequiresEvidence bool                `json:"requiresEvidence"`
	RequiresApproval bool                `json:"requiresApproval"`
	Warnings         []ValidationMessage `json:"warnings"`
	Errors           []ValidationMessage `json:"errors"`
}

// ExecutePreflight validates a contact-information update request without side effects.
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

	validationWarnings := validatePreflightWarnings(input)
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
				Message: "Contact information preflight completed.",
				Fields: map[string]any{
					"valid":        isValid,
					"riskLevel":    riskLevel,
					"warningCount": len(validationWarnings),
					"errorCount":   len(validationErrors),
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
		return PreflightInput{}, executor.InvalidInputError("Contact information preflight input does not match the expected contract.", map[string]any{
			"decodeError": err.Error(),
		})
	}

	return input, nil
}

func validatePreflightInput(input PreflightInput, evaluationDate time.Time) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)
	proposedContactInfo := normalizeContactInfo(input.ProposedContactInfo)
	businessReason := strings.TrimSpace(input.BusinessReason)
	effectiveAt := strings.TrimSpace(input.EffectiveAt)

	if normalizedEmail(proposedContactInfo.PersonalEmail) == "" && digitsOnly(pointerValue(proposedContactInfo.MobilePhone)) == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "contact_info.reachable_contact_required",
			Field:   "proposedContactInfo",
			Message: "At least one personal email or mobile phone is required.",
		})
	}

	if proposedContactInfo.PersonalEmail != nil && !strings.Contains(normalizedEmail(proposedContactInfo.PersonalEmail), "@") {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "contact_info.personal_email_invalid",
			Field:   "proposedContactInfo.personalEmail",
			Message: "Personal email must be a valid email address when provided.",
		})
	}

	if proposedContactInfo.MobilePhone != nil && phoneDigitCount(pointerValue(proposedContactInfo.MobilePhone)) < 7 {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "contact_info.mobile_phone_invalid",
			Field:   "proposedContactInfo.mobilePhone",
			Message: "Mobile phone must include at least seven digits when provided.",
		})
	}

	validationErrors = append(validationErrors, validateAddress(proposedContactInfo.HomeAddress)...)

	if businessReason == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "contact_info.business_reason_required",
			Field:   "businessReason",
			Message: "Business reason is required.",
		})
	}

	if effectiveAt == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "contact_info.effective_at_required",
			Field:   "effectiveAt",
			Message: "Effective date is required.",
		})
	} else {
		effectiveDate, parseError := parseDate(effectiveAt)
		if parseError != nil {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "contact_info.effective_at_invalid",
				Field:   "effectiveAt",
				Message: "Effective date must be a valid date.",
			})
		} else if effectiveDate.Before(evaluationDate.AddDate(0, 0, -180)) {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "contact_info.effective_at_too_far_in_past",
				Field:   "effectiveAt",
				Message: "Effective date cannot be more than 180 days in the past.",
			})
		}
	}

	if contactInfoEqual(input.CurrentContactInfo, input.ProposedContactInfo) {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "contact_info.unchanged",
			Field:   "proposedContactInfo",
			Message: "Contact information must differ from current contact information.",
		})
	}

	return validationErrors
}

func validateAddress(address PostalAddress) []ValidationMessage {
	requiredAddressFields := map[string]string{
		"proposedContactInfo.homeAddress.line1":      address.Line1,
		"proposedContactInfo.homeAddress.city":       address.City,
		"proposedContactInfo.homeAddress.region":     address.Region,
		"proposedContactInfo.homeAddress.postalCode": address.PostalCode,
		"proposedContactInfo.homeAddress.country":    address.Country,
	}
	validationErrors := make([]ValidationMessage, 0, len(requiredAddressFields))

	for field, value := range requiredAddressFields {
		if strings.TrimSpace(value) == "" {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "contact_info.address_required",
				Field:   field,
				Message: "Required address field is missing.",
			})
		}
	}

	if country := strings.TrimSpace(address.Country); country != "" && len(country) != 2 {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "contact_info.country_invalid",
			Field:   "proposedContactInfo.homeAddress.country",
			Message: "Country must be a two-letter country code.",
		})
	}

	return validationErrors
}

func validatePreflightWarnings(input PreflightInput) []ValidationMessage {
	currentContactInfo := normalizeContactInfo(input.CurrentContactInfo)
	proposedContactInfo := normalizeContactInfo(input.ProposedContactInfo)
	warnings := make([]ValidationMessage, 0)

	if strings.TrimSpace(currentContactInfo.HomeAddress.Country) != "" &&
		strings.TrimSpace(proposedContactInfo.HomeAddress.Country) != "" &&
		!strings.EqualFold(currentContactInfo.HomeAddress.Country, proposedContactInfo.HomeAddress.Country) {
		warnings = append(warnings, ValidationMessage{
			Code:    "contact_info.country_changed",
			Field:   "proposedContactInfo.homeAddress.country",
			Message: "Country changed; payroll, tax, and benefits integrations may need review.",
		})
	}

	if currentContactInfo.HomeAddress.Region != proposedContactInfo.HomeAddress.Region {
		warnings = append(warnings, ValidationMessage{
			Code:    "contact_info.region_changed",
			Field:   "proposedContactInfo.homeAddress.region",
			Message: "Region changed; local payroll or tax rules may be affected.",
		})
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

func contactInfoEqual(left ContactInfo, right ContactInfo) bool {
	normalizedLeft := normalizeContactInfo(left)
	normalizedRight := normalizeContactInfo(right)

	return normalizedEmail(normalizedLeft.PersonalEmail) == normalizedEmail(normalizedRight.PersonalEmail) &&
		digitsOnly(pointerValue(normalizedLeft.MobilePhone)) == digitsOnly(pointerValue(normalizedRight.MobilePhone)) &&
		addressEqual(normalizedLeft.HomeAddress, normalizedRight.HomeAddress)
}

func addressEqual(left PostalAddress, right PostalAddress) bool {
	return strings.EqualFold(strings.TrimSpace(left.Line1), strings.TrimSpace(right.Line1)) &&
		strings.EqualFold(strings.TrimSpace(pointerValue(left.Line2)), strings.TrimSpace(pointerValue(right.Line2))) &&
		strings.EqualFold(strings.TrimSpace(left.City), strings.TrimSpace(right.City)) &&
		strings.EqualFold(strings.TrimSpace(left.Region), strings.TrimSpace(right.Region)) &&
		strings.EqualFold(strings.TrimSpace(left.PostalCode), strings.TrimSpace(right.PostalCode)) &&
		strings.EqualFold(strings.TrimSpace(left.Country), strings.TrimSpace(right.Country))
}

func normalizeContactInfo(contactInfo ContactInfo) ContactInfo {
	return ContactInfo{
		PersonalEmail: trimOptionalLowerString(contactInfo.PersonalEmail),
		MobilePhone:   trimOptionalString(contactInfo.MobilePhone),
		HomeAddress: PostalAddress{
			Line1:      strings.TrimSpace(contactInfo.HomeAddress.Line1),
			Line2:      trimOptionalString(contactInfo.HomeAddress.Line2),
			City:       strings.TrimSpace(contactInfo.HomeAddress.City),
			Region:     strings.TrimSpace(contactInfo.HomeAddress.Region),
			PostalCode: strings.TrimSpace(contactInfo.HomeAddress.PostalCode),
			Country:    strings.ToUpper(strings.TrimSpace(contactInfo.HomeAddress.Country)),
		},
	}
}

func normalizedEmail(value *string) string {
	if value == nil {
		return ""
	}

	return strings.ToLower(strings.TrimSpace(*value))
}

func trimOptionalLowerString(value *string) *string {
	if value == nil {
		return nil
	}

	trimmedValue := strings.ToLower(strings.TrimSpace(*value))
	if trimmedValue == "" {
		return nil
	}

	return &trimmedValue
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}

	trimmedValue := strings.TrimSpace(*value)
	if trimmedValue == "" {
		return nil
	}

	return &trimmedValue
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
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
