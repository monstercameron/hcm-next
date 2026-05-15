package compensation

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
	Valid            bool                `json:"valid"`
	RiskLevel        string              `json:"riskLevel"`
	RequiresEvidence bool                `json:"requiresEvidence"`
	RequiresApproval bool                `json:"requiresApproval"`
	Warnings         []ValidationMessage `json:"warnings"`
	Errors           []ValidationMessage `json:"errors"`
}

// ExecutePreflight validates a compensation change request without side effects.
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
				Message: "Compensation preflight completed.",
				Fields: map[string]any{
					"valid":           isValid,
					"riskLevel":       riskLevel,
					"warningCount":    len(validationWarnings),
					"errorCount":      len(validationErrors),
					"increasePercent": compensationIncreasePercent(input.CurrentCompensation, input.ProposedCompensation),
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
		return PreflightInput{}, executor.InvalidInputError("Compensation preflight input does not match the expected contract.", map[string]any{
			"decodeError": err.Error(),
		})
	}

	return input, nil
}

func validatePreflightInput(input PreflightInput, evaluationDate time.Time) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)
	businessReason := strings.TrimSpace(input.BusinessReason)
	effectiveAt := strings.TrimSpace(input.EffectiveAt)

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

	if effectiveAt == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "compensation.effective_at_required",
			Field:   "effectiveAt",
			Message: "Effective date is required.",
		})
	} else {
		effectiveDate, parseError := parseDate(effectiveAt)
		if parseError != nil {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "compensation.effective_at_invalid",
				Field:   "effectiveAt",
				Message: "Effective date must be a valid date.",
			})
		} else if effectiveDate.Before(evaluationDate.AddDate(0, 0, -180)) {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "compensation.effective_at_too_far_in_past",
				Field:   "effectiveAt",
				Message: "Effective date cannot be more than 180 days in the past.",
			})
		}
	}

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

func riskLevelForPreflight(isValid bool, warnings []ValidationMessage) string {
	if !isValid {
		return preflightRiskHigh
	}

	if len(warnings) > 0 {
		return preflightRiskMedium
	}

	return preflightRiskLow
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
