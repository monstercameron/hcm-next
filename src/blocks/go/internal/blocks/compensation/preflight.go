package compensation

import (
	"strings"
	"time"

	"human-capital-management-suite-executor/internal/blockshared"
	"human-capital-management-suite-executor/internal/executor"
)

// PreflightInput is the contract for compensation change validation.
type PreflightInput struct {
	CurrentCompensation  CompensationInfo `json:"currentCompensation"`
	ProposedCompensation CompensationInfo `json:"proposedCompensation"`
	CurrentJob           map[string]any   `json:"currentJob"`
	CurrentOrganization  map[string]any   `json:"currentOrganization"`
	EffectiveAt          string           `json:"effectiveAt"`
	BusinessReason       string           `json:"businessReason"`
}

// PreflightOutput is the deterministic validation result for a compensation change.
type PreflightOutput struct {
	executor.BlockOutputContract
	Valid            bool                `json:"valid"`
	RiskLevel        string              `json:"riskLevel"`
	RequiresEvidence bool                `json:"requiresEvidence"`
	RequiresApproval bool                `json:"requiresApproval"`
	Warnings         []ValidationMessage `json:"warnings"`
	Errors           []ValidationMessage `json:"errors"`
}

// ExecutePreflight validates a compensation change request without side effects.
func ExecutePreflight(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := blockshared.DecodeStrict[PreflightInput](request.Input, "Compensation preflight input does not match the expected contract.")
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
	increasePercent := compensationIncreasePercent(input.CurrentCompensation, input.ProposedCompensation)
	routeKey := executor.RouteKeyForValidation(isValid, len(validationWarnings), true)

	output := PreflightOutput{
		BlockOutputContract: executor.NewValidationOutputContract(
			routeKey,
			blockshared.PreflightFacts(
				PreflightBlockName,
				isValid,
				riskLevel,
				false,
				true,
				len(validationWarnings),
				executor.Fact{Key: "increasePercent", Value: increasePercent, Source: PreflightBlockName},
			),
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
				Message: "Compensation preflight completed.",
				Fields: map[string]any{
					"valid":           isValid,
					"riskLevel":       riskLevel,
					"warningCount":    len(validationWarnings),
					"errorCount":      len(validationErrors),
					"increasePercent": increasePercent,
				},
			},
		},
	}, nil
}

func validatePreflightInput(input PreflightInput, evaluationDate time.Time) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)
	businessReason := strings.TrimSpace(input.BusinessReason)

	if input.CurrentCompensation.Amount <= 0 {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "compensation.current_amount_invalid",
			Field:   "currentCompensation.amount",
			Message: "Current compensation amount must be greater than zero.",
		})
	}

	if input.ProposedCompensation.Amount <= 0 {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "compensation.proposed_amount_invalid",
			Field:   "proposedCompensation.amount",
			Message: "Proposed compensation amount must be greater than zero.",
		})
	}

	if strings.TrimSpace(input.ProposedCompensation.Currency) == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "compensation.currency_required",
			Field:   "proposedCompensation.currency",
			Message: "Proposed compensation currency is required.",
		})
	}

	if strings.TrimSpace(input.ProposedCompensation.PayFrequency) == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "compensation.pay_frequency_required",
			Field:   "proposedCompensation.payFrequency",
			Message: "Proposed compensation pay frequency is required.",
		})
	}

	if strings.TrimSpace(input.ProposedCompensation.Currency) != strings.TrimSpace(input.CurrentCompensation.Currency) {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "compensation.currency_change_not_supported_v0",
			Field:   "proposedCompensation.currency",
			Message: "Currency changes are not supported in the V0 compensation workflow.",
		})
	}

	if input.ProposedCompensation.Amount <= input.CurrentCompensation.Amount {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "compensation.not_a_raise",
			Field:   "proposedCompensation.amount",
			Message: "The proposed amount must be greater than the current amount.",
		})
	}

	if businessReason == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "compensation.business_reason_required",
			Field:   "businessReason",
			Message: "Business reason is required.",
		})
	}

	validationErrors = append(validationErrors, blockshared.EffectiveDateValidation("compensation", input.EffectiveAt, evaluationDate, 180)...)

	if strings.TrimSpace(input.ProposedCompensation.EffectiveDate) == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "compensation.proposed_effective_date_required",
			Field:   "proposedCompensation.effectiveDate",
			Message: "Proposed compensation effective date is required.",
		})
	}

	return validationErrors
}

func validatePreflightWarnings(input PreflightInput) []ValidationMessage {
	warnings := make([]ValidationMessage, 0)
	increasePercent := compensationIncreasePercent(input.CurrentCompensation, input.ProposedCompensation)

	if increasePercent > 10 {
		warnings = append(warnings, ValidationMessage{
			Code:    "compensation.increase_over_ten_percent",
			Field:   "proposedCompensation.amount",
			Message: "Raise exceeds ten percent and requires careful compensation review.",
		})
	}

	return warnings
}
