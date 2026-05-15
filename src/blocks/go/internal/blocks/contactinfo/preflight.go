package contactinfo

import (
	"strings"
	"time"

	"hcm-next-executor/internal/blockshared"
	"hcm-next-executor/internal/executor"
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
	executor.BlockOutputContract
	Valid            bool                `json:"valid"`
	RiskLevel        string              `json:"riskLevel"`
	RequiresEvidence bool                `json:"requiresEvidence"`
	RequiresApproval bool                `json:"requiresApproval"`
	Warnings         []ValidationMessage `json:"warnings"`
	Errors           []ValidationMessage `json:"errors"`
}

// ExecutePreflight validates a contact-information update request without side effects.
func ExecutePreflight(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := blockshared.DecodeStrict[PreflightInput](request.Input, "Contact information preflight input does not match the expected contract.")
	if inputError != nil {
		return executor.BlockResult{}, inputError
	}

	evaluationDate, evaluationDateError := blockshared.ParseContextEffectiveDate(request.Context.EffectiveAt)
	if evaluationDateError != nil {
		return executor.BlockResult{}, evaluationDateError
	}

	validationWarnings := validatePreflightWarnings(input)
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

func validatePreflightInput(input PreflightInput, evaluationDate time.Time) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)
	proposedContactInfo := normalizeContactInfo(input.ProposedContactInfo)
	businessReason := strings.TrimSpace(input.BusinessReason)

	if blockshared.NormalizedEmail(proposedContactInfo.PersonalEmail) == "" && blockshared.DigitsOnly(blockshared.StringValue(proposedContactInfo.MobilePhone)) == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "contact_info.reachable_contact_required",
			Field:   "proposedContactInfo",
			Message: "At least one personal email or mobile phone is required.",
		})
	}

	if proposedContactInfo.PersonalEmail != nil && !strings.Contains(blockshared.NormalizedEmail(proposedContactInfo.PersonalEmail), "@") {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "contact_info.personal_email_invalid",
			Field:   "proposedContactInfo.personalEmail",
			Message: "Personal email must be a valid email address when provided.",
		})
	}

	if proposedContactInfo.MobilePhone != nil && blockshared.PhoneDigitCount(blockshared.StringValue(proposedContactInfo.MobilePhone)) < 7 {
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

	validationErrors = append(validationErrors, blockshared.EffectiveDateValidation("contact_info", input.EffectiveAt, evaluationDate, 180)...)

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

func contactInfoEqual(left ContactInfo, right ContactInfo) bool {
	normalizedLeft := normalizeContactInfo(left)
	normalizedRight := normalizeContactInfo(right)

	return blockshared.NormalizedEmail(normalizedLeft.PersonalEmail) == blockshared.NormalizedEmail(normalizedRight.PersonalEmail) &&
		blockshared.DigitsOnly(blockshared.StringValue(normalizedLeft.MobilePhone)) == blockshared.DigitsOnly(blockshared.StringValue(normalizedRight.MobilePhone)) &&
		addressEqual(normalizedLeft.HomeAddress, normalizedRight.HomeAddress)
}

func addressEqual(left PostalAddress, right PostalAddress) bool {
	return strings.EqualFold(strings.TrimSpace(left.Line1), strings.TrimSpace(right.Line1)) &&
		strings.EqualFold(strings.TrimSpace(blockshared.StringValue(left.Line2)), strings.TrimSpace(blockshared.StringValue(right.Line2))) &&
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
