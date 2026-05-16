package termination

import (
	"strings"
	"time"

	"hcm-next-executor/internal/blockshared"
	"hcm-next-executor/internal/executor"
)

var knownTerminationTypes = map[string]bool{
	"voluntary":        true,
	"involuntary":      true,
	"retirement":       true,
	"mutual_agreement": true,
}

// PreflightInput is the contract for employee termination validation.
type PreflightInput struct {
	WorkerID                string `json:"workerId"`
	CurrentEmploymentStatus string `json:"currentEmploymentStatus"`
	HireDate                string `json:"hireDate"`
	EffectiveAt             string `json:"effectiveAt"`
	TerminationType         string `json:"terminationType"`
	BusinessReason          string `json:"businessReason"`
}

// PreflightOutput is the deterministic validation result for an employee termination.
type PreflightOutput struct {
	executor.BlockOutputContract
	Valid               bool                `json:"valid"`
	RiskLevel           string              `json:"riskLevel"`
	RequiresApproval    bool                `json:"requiresApproval"`
	FinalPayDate        string              `json:"finalPayDate"`
	CobraWindowDays     int                 `json:"cobraWindowDays"`
	StatutoryNoticeDays int                 `json:"statutoryNoticeDays"`
	TenureYears         float64             `json:"tenureYears"`
	Warnings            []ValidationMessage `json:"warnings"`
	Errors              []ValidationMessage `json:"errors"`
}

// ExecutePreflight validates a termination request without side effects.
func ExecutePreflight(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := blockshared.DecodeStrict[PreflightInput](request.Input, "Termination preflight input does not match the expected contract.")
	if inputError != nil {
		return executor.BlockResult{}, inputError
	}

	evaluationDate, evaluationDateError := blockshared.ParseContextEffectiveDate(request.Context.EffectiveAt)
	if evaluationDateError != nil {
		return executor.BlockResult{}, evaluationDateError
	}

	validationWarnings := validatePreflightWarnings(input, evaluationDate)
	validationErrors := validatePreflightInput(input, evaluationDate)
	isValid := len(validationErrors) == 0
	riskLevel := terminationRiskLevel(isValid, input.TerminationType, len(validationWarnings))
	routeKey := executor.RouteKeyForValidation(isValid, len(validationWarnings), true)

	tenureYears := computeTenureYears(input.HireDate, input.EffectiveAt)
	finalPayDate := strings.TrimSpace(input.EffectiveAt)
	statutoryNoticeDays := statutoryNoticeForType(input.TerminationType)

	output := PreflightOutput{
		BlockOutputContract: executor.NewValidationOutputContract(
			routeKey,
			blockshared.PreflightFacts(PreflightBlockName, isValid, riskLevel, false, true, len(validationWarnings)),
			validationErrors,
			validationWarnings,
		),
		Valid:               isValid,
		RiskLevel:           riskLevel,
		RequiresApproval:    true,
		FinalPayDate:        finalPayDate,
		CobraWindowDays:     60,
		StatutoryNoticeDays: statutoryNoticeDays,
		TenureYears:         tenureYears,
		Warnings:            validationWarnings,
		Errors:              validationErrors,
	}

	return executor.BlockResult{
		Output: output,
		Logs: []executor.ExecutionLog{
			{
				Level:   "info",
				Message: "Termination preflight completed.",
				Fields: map[string]any{
					"valid":               isValid,
					"riskLevel":           riskLevel,
					"terminationType":     input.TerminationType,
					"tenureYears":         tenureYears,
					"statutoryNoticeDays": statutoryNoticeDays,
					"warningCount":        len(validationWarnings),
					"errorCount":          len(validationErrors),
				},
			},
		},
	}, nil
}

func validatePreflightInput(input PreflightInput, evaluationDate time.Time) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)

	if strings.TrimSpace(input.WorkerID) == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "termination.worker_id_required",
			Field:   "workerId",
			Message: "Worker ID is required.",
		})
	}

	if strings.TrimSpace(input.BusinessReason) == "" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "termination.business_reason_required",
			Field:   "businessReason",
			Message: "Business reason is required.",
		})
	}

	if !knownTerminationTypes[strings.TrimSpace(input.TerminationType)] {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "termination.termination_type_invalid",
			Field:   "terminationType",
			Message: "Termination type must be voluntary, involuntary, retirement, or mutual_agreement.",
		})
	}

	if strings.TrimSpace(input.CurrentEmploymentStatus) != "active" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "termination.employee_not_active",
			Field:   "currentEmploymentStatus",
			Message: "Only active employees can be terminated through this workflow.",
		})
	}

	validationErrors = append(validationErrors, blockshared.EffectiveDateValidation("termination", input.EffectiveAt, evaluationDate, 30)...)

	return validationErrors
}

func validatePreflightWarnings(input PreflightInput, evaluationDate time.Time) []ValidationMessage {
	warnings := make([]ValidationMessage, 0)

	if strings.TrimSpace(input.TerminationType) == "involuntary" {
		warnings = append(warnings, ValidationMessage{
			Code:    "termination.involuntary_legal_review",
			Field:   "terminationType",
			Message: "Involuntary terminations require legal review and documentation before proceeding.",
		})
	}

	tenureYears := computeTenureYears(input.HireDate, input.EffectiveAt)
	if tenureYears > 0 && tenureYears < 1 {
		warnings = append(warnings, ValidationMessage{
			Code:    "termination.short_tenure",
			Field:   "hireDate",
			Message: "Employee tenure is less than one year; verify FMLA and benefits eligibility.",
		})
	}

	effectiveDate, effectiveDateError := blockshared.ParseDate(strings.TrimSpace(input.EffectiveAt))
	if effectiveDateError == nil && !effectiveDate.After(evaluationDate) {
		warnings = append(warnings, ValidationMessage{
			Code:    "termination.no_notice_period",
			Field:   "effectiveAt",
			Message: "Effective date is today or in the past; no standard notice period is being provided.",
		})
	}

	return warnings
}

func terminationRiskLevel(isValid bool, terminationType string, warningCount int) string {
	if !isValid {
		return blockshared.RiskHigh
	}

	if strings.TrimSpace(terminationType) == "involuntary" {
		return blockshared.RiskHigh
	}

	if warningCount > 0 {
		return blockshared.RiskMedium
	}

	return blockshared.RiskLow
}

func statutoryNoticeForType(terminationType string) int {
	switch strings.TrimSpace(terminationType) {
	case "involuntary":
		return 14
	case "mutual_agreement":
		return 7
	default:
		return 0
	}
}

func computeTenureYears(hireDate string, effectiveAt string) float64 {
	hire, hireError := blockshared.ParseDate(strings.TrimSpace(hireDate))
	effective, effectiveError := blockshared.ParseDate(strings.TrimSpace(effectiveAt))

	if hireError != nil || effectiveError != nil {
		return 0
	}

	if !effective.After(hire) {
		return 0
	}

	duration := effective.Sub(hire)
	return duration.Hours() / (24 * 365.25)
}
