package legalname

import (
	"strings"
	"time"

	"human-capital-management-suite-executor/internal/blockshared"
	"human-capital-management-suite-executor/internal/executor"
)

// PreflightInput is the contract for legal-name change validation.
type PreflightInput struct {
	CurrentLegalName  LegalName `json:"currentLegalName"`
	ProposedLegalName LegalName `json:"proposedLegalName"`
	EffectiveAt       string    `json:"effectiveAt"`
	BusinessReason    string    `json:"businessReason"`
}

// PreflightOutput is the deterministic validation result for a legal-name change.
type PreflightOutput struct {
	executor.BlockOutputContract
	Valid            bool                `json:"valid"`
	RiskLevel        string              `json:"riskLevel"`
	RequiresEvidence bool                `json:"requiresEvidence"`
	RequiresApproval bool                `json:"requiresApproval"`
	Warnings         []ValidationMessage `json:"warnings"`
	Errors           []ValidationMessage `json:"errors"`
}

// ExecutePreflight validates a legal-name change request without side effects.
func ExecutePreflight(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := blockshared.DecodeStrict[PreflightInput](request.Input, "Legal name preflight input does not match the expected contract.")
	if inputError != nil {
		return executor.BlockResult{}, inputError
	}

	evaluationDate, evaluationDateError := blockshared.ParseContextEffectiveDate(request.Context.EffectiveAt)
	if evaluationDateError != nil {
		return executor.BlockResult{}, evaluationDateError
	}

	validationErrors := validatePreflightInput(input, evaluationDate)
	isValid := len(validationErrors) == 0
	riskLevel := blockshared.RiskLevelForValidation(isValid, 0)

	warnings := []ValidationMessage{}
	routeKey := executor.RouteKeyForValidation(isValid, len(warnings), true)
	output := PreflightOutput{
		BlockOutputContract: executor.NewValidationOutputContract(
			routeKey,
			blockshared.CorePreflightFacts(PreflightBlockName, isValid, riskLevel, true, true),
			validationErrors,
			warnings,
		),
		Valid:            isValid,
		RiskLevel:        riskLevel,
		RequiresEvidence: true,
		RequiresApproval: true,
		Warnings:         warnings,
		Errors:           validationErrors,
	}

	return executor.BlockResult{
		Output: output,
		Logs: []executor.ExecutionLog{
			{
				Level:   "info",
				Message: "Legal name preflight completed.",
				Fields: map[string]any{
					"valid":     isValid,
					"riskLevel": riskLevel,
				},
			},
		},
	}, nil
}

func validatePreflightInput(input PreflightInput, evaluationDate time.Time) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)
	proposedFirstName := strings.TrimSpace(input.ProposedLegalName.First)
	proposedLastName := strings.TrimSpace(input.ProposedLegalName.Last)
	businessReason := strings.TrimSpace(input.BusinessReason)

	if proposedFirstName == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "legal_name.first_required",
			Field:   "proposedLegalName.first",
			Message: "First name is required.",
		})
	}

	if proposedLastName == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "legal_name.last_required",
			Field:   "proposedLegalName.last",
			Message: "Last name is required.",
		})
	}

	if businessReason == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "legal_name.business_reason_required",
			Field:   "businessReason",
			Message: "Business reason is required.",
		})
	}

	validationErrors = append(validationErrors, blockshared.EffectiveDateValidation("legal_name", input.EffectiveAt, evaluationDate, 180)...)

	if legalNamesMatch(input.CurrentLegalName, input.ProposedLegalName) {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "legal_name.unchanged",
			Field:   "proposedLegalName",
			Message: "New legal name must differ from the current legal name.",
		})
	}

	return validationErrors
}

func legalNamesMatch(currentName LegalName, proposedName LegalName) bool {
	return strings.TrimSpace(currentName.First) == strings.TrimSpace(proposedName.First) &&
		normalizedMiddleName(currentName.Middle) == normalizedMiddleName(proposedName.Middle) &&
		strings.TrimSpace(currentName.Last) == strings.TrimSpace(proposedName.Last)
}

func normalizedMiddleName(value *string) string {
	if value == nil {
		return ""
	}

	return strings.TrimSpace(*value)
}
