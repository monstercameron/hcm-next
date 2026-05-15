package blockshared

import (
	"strings"

	"hcm-next-executor/internal/executor"
)

const (
	transactionRequiredCode    = "transaction_plan.required"
	transactionRequiredMessage = "Required transaction planning field is missing."
)

// RequiredTransactionFields converts blank required string fields into standard transaction validation issues.
func RequiredTransactionFields(requiredFields map[string]string) []executor.ValidationIssue {
	return RequiredFields(requiredFields, transactionRequiredCode, transactionRequiredMessage)
}

// RequiredFields converts blank required string fields into validation issues.
func RequiredFields(requiredFields map[string]string, code string, message string) []executor.ValidationIssue {
	validationErrors := make([]executor.ValidationIssue, 0, len(requiredFields))

	for field, value := range requiredFields {
		if strings.TrimSpace(value) == "" {
			validationErrors = append(validationErrors, executor.ValidationIssue{
				Code:    code,
				Field:   field,
				Message: message,
			})
		}
	}

	return validationErrors
}
