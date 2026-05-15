package blockshared

import (
	"strconv"
	"strings"
	"time"

	"hcm-next-executor/internal/executor"
)

const (
	dateLayout = "2006-01-02"
)

// ParseDate accepts date-only and RFC3339 values, then normalizes them to UTC midnight.
func ParseDate(value string) (time.Time, error) {
	trimmedValue := strings.TrimSpace(value)
	layouts := []string{dateLayout, time.RFC3339}

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

// ParseContextEffectiveDate validates the executor context clock used by preflight checks.
func ParseContextEffectiveDate(value string) (time.Time, *executor.ExecutionError) {
	evaluationDate, evaluationDateError := ParseDate(value)
	if evaluationDateError != nil {
		return time.Time{}, executor.InvalidInputError("context.effectiveAt must be a valid date or RFC3339 timestamp.", map[string]any{
			"field": "context.effectiveAt",
		})
	}

	return evaluationDate, nil
}

// EffectiveDateValidation validates a required workflow effective date.
func EffectiveDateValidation(codePrefix string, effectiveAt string, evaluationDate time.Time, maxPastDays int) []executor.ValidationIssue {
	trimmedEffectiveAt := strings.TrimSpace(effectiveAt)
	validationErrors := make([]executor.ValidationIssue, 0, 1)

	if trimmedEffectiveAt == "" {
		return append(validationErrors, executor.ValidationIssue{
			Code:    codePrefix + ".effective_at_required",
			Field:   "effectiveAt",
			Message: "Effective date is required.",
		})
	}

	effectiveDate, parseError := ParseDate(trimmedEffectiveAt)
	if parseError != nil {
		return append(validationErrors, executor.ValidationIssue{
			Code:    codePrefix + ".effective_at_invalid",
			Field:   "effectiveAt",
			Message: "Effective date must be a valid date.",
		})
	}

	if effectiveDate.Before(evaluationDate.AddDate(0, 0, -maxPastDays)) {
		validationErrors = append(validationErrors, executor.ValidationIssue{
			Code:    codePrefix + ".effective_at_too_far_in_past",
			Field:   "effectiveAt",
			Message: "Effective date cannot be more than " + intString(maxPastDays) + " days in the past.",
		})
	}

	return validationErrors
}

// TransactionEffectiveDateValidation validates an optional transaction-plan effective date.
func TransactionEffectiveDateValidation(effectiveAt string) []executor.ValidationIssue {
	if strings.TrimSpace(effectiveAt) == "" {
		return []executor.ValidationIssue{}
	}

	if _, err := ParseDate(effectiveAt); err != nil {
		return []executor.ValidationIssue{
			{
				Code:    "transaction_plan.effective_at_invalid",
				Field:   "effectiveAt",
				Message: "Effective date must be a valid date.",
			},
		}
	}

	return []executor.ValidationIssue{}
}

func intString(value int) string {
	return strconv.Itoa(value)
}
