package legalname

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"hcm-next-executor/internal/executor"
)

const (
	preflightRiskLow  = "low"
	preflightRiskHigh = "high"
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
	Valid            bool                `json:"valid"`
	RiskLevel        string              `json:"riskLevel"`
	RequiresEvidence bool                `json:"requiresEvidence"`
	RequiresApproval bool                `json:"requiresApproval"`
	Warnings         []ValidationMessage `json:"warnings"`
	Errors           []ValidationMessage `json:"errors"`
}

// ExecutePreflight validates a legal-name change request without side effects.
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

	validationErrors := validatePreflightInput(input, evaluationDate)
	isValid := len(validationErrors) == 0
	riskLevel := preflightRiskLow
	if !isValid {
		riskLevel = preflightRiskHigh
	}

	output := PreflightOutput{
		Valid:            isValid,
		RiskLevel:        riskLevel,
		RequiresEvidence: true,
		RequiresApproval: true,
		Warnings:         []ValidationMessage{},
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

func decodePreflightInput(rawInput json.RawMessage) (PreflightInput, *executor.ExecutionError) {
	var input PreflightInput
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		return PreflightInput{}, executor.InvalidInputError("Legal name preflight input does not match the expected contract.", map[string]any{
			"decodeError": err.Error(),
		})
	}

	return input, nil
}

func validatePreflightInput(input PreflightInput, evaluationDate time.Time) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)
	proposedFirstName := strings.TrimSpace(input.ProposedLegalName.First)
	proposedLastName := strings.TrimSpace(input.ProposedLegalName.Last)
	businessReason := strings.TrimSpace(input.BusinessReason)
	effectiveAt := strings.TrimSpace(input.EffectiveAt)

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

	if effectiveAt == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "legal_name.effective_at_required",
			Field:   "effectiveAt",
			Message: "Effective date is required.",
		})
	} else {
		effectiveDate, parseError := parseDate(effectiveAt)
		if parseError != nil {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "legal_name.effective_at_invalid",
				Field:   "effectiveAt",
				Message: "Effective date must be a valid date.",
			})
		} else if effectiveDate.Before(evaluationDate.AddDate(0, 0, -180)) {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "legal_name.effective_at_too_far_in_past",
				Field:   "effectiveAt",
				Message: "Effective date cannot be more than 180 days in the past.",
			})
		}
	}

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
