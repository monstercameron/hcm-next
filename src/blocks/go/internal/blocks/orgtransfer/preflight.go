package orgtransfer

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

// PreflightInput is the deterministic validation input for an org transfer.
type PreflightInput struct {
	Employee                 PreflightEmployee  `json:"employee"`
	CurrentOrganization      OrganizationInfo   `json:"currentOrganization"`
	TargetLocationOrgUnit    OrgUnit            `json:"targetLocationOrgUnit"`
	TargetTeamOrgUnit        OrgUnit            `json:"targetTeamOrgUnit"`
	TargetCostCenterOrgUnit  OrgUnit            `json:"targetCostCenterOrgUnit"`
	TargetManager            PreflightEmployee  `json:"targetManager"`
	ActiveAssignments        []WorkerAssignment `json:"activeAssignments"`
	EffectiveAt              string             `json:"effectiveAt"`
	BusinessReason           string             `json:"businessReason"`
	TransferReason           string             `json:"transferReason"`
	AccessImpactAcknowledged bool               `json:"accessImpactAcknowledged"`
}

// PreflightEmployee is the minimal worker status contract needed by preflight.
type PreflightEmployee struct {
	EmployeeID       string `json:"employeeId"`
	EmploymentStatus string `json:"employmentStatus"`
}

// PreflightOutput is the deterministic validation result for an org transfer.
type PreflightOutput struct {
	executor.BlockOutputContract
	Valid            bool                `json:"valid"`
	RiskLevel        string              `json:"riskLevel"`
	RequiresEvidence bool                `json:"requiresEvidence"`
	RequiresApproval bool                `json:"requiresApproval"`
	Warnings         []ValidationMessage `json:"warnings"`
	Errors           []ValidationMessage `json:"errors"`
}

// ExecutePreflight validates the requested transfer without side effects.
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
	validationWarnings := validatePreflightWarnings(input)
	isValid := len(validationErrors) == 0
	riskLevel := riskLevelForPreflight(isValid, validationWarnings)
	routeKey := executor.RouteKeyForValidation(isValid, len(validationWarnings), true)

	output := PreflightOutput{
		BlockOutputContract: executor.NewValidationOutputContract(
			routeKey,
			orgTransferPreflightFacts(isValid, riskLevel, false, true, len(validationWarnings), input.TargetTeamOrgUnit.OrgUnitID),
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
				Message: "Org transfer preflight completed.",
				Fields: map[string]any{
					"valid":               isValid,
					"riskLevel":           riskLevel,
					"warningCount":        len(validationWarnings),
					"errorCount":          len(validationErrors),
					"targetTeamOrgUnitId": input.TargetTeamOrgUnit.OrgUnitID,
				},
			},
		},
	}, nil
}

func orgTransferPreflightFacts(isValid bool, riskLevel string, requiresEvidence bool, requiresApproval bool, warningCount int, targetTeamOrgUnitID string) []executor.Fact {
	return []executor.Fact{
		{Key: "valid", Value: isValid, Source: PreflightBlockName},
		{Key: "riskLevel", Value: riskLevel, Source: PreflightBlockName},
		{Key: "requiresEvidence", Value: requiresEvidence, Source: PreflightBlockName},
		{Key: "requiresApproval", Value: requiresApproval, Source: PreflightBlockName},
		{Key: "warningCount", Value: warningCount, Source: PreflightBlockName},
		{Key: "targetTeamOrgUnitId", Value: targetTeamOrgUnitID, Source: PreflightBlockName},
	}
}

func decodePreflightInput(rawInput json.RawMessage) (PreflightInput, *executor.ExecutionError) {
	var input PreflightInput
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		return PreflightInput{}, executor.InvalidInputError("Org transfer preflight input does not match the expected contract.", map[string]any{
			"decodeError": err.Error(),
		})
	}

	return input, nil
}

func validatePreflightInput(input PreflightInput, evaluationDate time.Time) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)

	if strings.TrimSpace(input.Employee.EmployeeID) == "" {
		validationErrors = append(validationErrors, requiredMessage("employee.employeeId"))
	}

	if strings.TrimSpace(input.Employee.EmploymentStatus) != "active" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "org_transfer.employee_not_active",
			Field:   "employee.employmentStatus",
			Message: "Employee must be active to transfer.",
		})
	}

	validateActiveOrgUnit(&validationErrors, input.TargetLocationOrgUnit, "targetLocationOrgUnit", "location")
	validateActiveOrgUnit(&validationErrors, input.TargetTeamOrgUnit, "targetTeamOrgUnit", "team")
	validateActiveOrgUnit(&validationErrors, input.TargetCostCenterOrgUnit, "targetCostCenterOrgUnit", "cost_center")

	if strings.TrimSpace(input.TargetManager.EmployeeID) == "" {
		validationErrors = append(validationErrors, requiredMessage("targetManager.employeeId"))
	}

	if strings.TrimSpace(input.TargetManager.EmploymentStatus) != "active" {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "org_transfer.target_manager_not_active",
			Field:   "targetManager.employmentStatus",
			Message: "Target manager must be active.",
		})
	}

	if input.TargetManager.EmployeeID == input.Employee.EmployeeID {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "org_transfer.self_manager_not_allowed",
			Field:   "targetManager.employeeId",
			Message: "Employee cannot be their own target manager.",
		})
	}

	if strings.TrimSpace(input.BusinessReason) == "" {
		validationErrors = append(validationErrors, requiredMessage("businessReason"))
	}

	if strings.TrimSpace(input.TransferReason) == "" {
		validationErrors = append(validationErrors, requiredMessage("transferReason"))
	}

	validateEffectiveDate(&validationErrors, input.EffectiveAt, evaluationDate)
	validateCostCenterBusinessUnit(&validationErrors, input)

	if !input.AccessImpactAcknowledged {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "org_transfer.access_impact_acknowledgement_required",
			Field:   "accessImpactAcknowledged",
			Message: "Access impact must be acknowledged before submission.",
		})
	}

	if !hasActiveAssignment(input.ActiveAssignments, "primary_team") {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "org_transfer.current_primary_team_missing",
			Field:   "activeAssignments",
			Message: "Employee must have an active primary team assignment.",
		})
	}

	return validationErrors
}

func validatePreflightWarnings(input PreflightInput) []ValidationMessage {
	warnings := make([]ValidationMessage, 0)

	if input.CurrentOrganization.Location != "" && input.CurrentOrganization.Location != input.TargetLocationOrgUnit.Name {
		warnings = append(warnings, ValidationMessage{
			Code:    "org_transfer.location_change",
			Field:   "targetLocationOrgUnitId",
			Message: "Transfer changes the employee work location.",
		})
	}

	return warnings
}

func validateActiveOrgUnit(validationErrors *[]ValidationMessage, orgUnit OrgUnit, field string, expectedType string) {
	if strings.TrimSpace(orgUnit.OrgUnitID) == "" {
		*validationErrors = append(*validationErrors, requiredMessage(field+".orgUnitId"))
		return
	}

	if orgUnit.Status != "active" {
		*validationErrors = append(*validationErrors, ValidationMessage{
			Code:    "org_transfer.target_org_unit_inactive",
			Field:   field + ".status",
			Message: "Target organization unit must be active.",
		})
	}

	if orgUnit.Type != expectedType {
		*validationErrors = append(*validationErrors, ValidationMessage{
			Code:    "org_transfer.target_org_unit_type_invalid",
			Field:   field + ".type",
			Message: "Target organization unit type is not compatible with the requested transfer field.",
		})
	}
}

func validateEffectiveDate(validationErrors *[]ValidationMessage, effectiveAt string, evaluationDate time.Time) {
	if strings.TrimSpace(effectiveAt) == "" {
		*validationErrors = append(*validationErrors, requiredMessage("effectiveAt"))
		return
	}

	effectiveDate, parseError := parseDate(effectiveAt)
	if parseError != nil {
		*validationErrors = append(*validationErrors, ValidationMessage{
			Code:    "org_transfer.effective_at_invalid",
			Field:   "effectiveAt",
			Message: "Effective date must be a valid date.",
		})
		return
	}

	if effectiveDate.Before(evaluationDate.AddDate(0, 0, -30)) {
		*validationErrors = append(*validationErrors, ValidationMessage{
			Code:    "org_transfer.effective_at_too_far_in_past",
			Field:   "effectiveAt",
			Message: "Effective date cannot be more than 30 days in the past.",
		})
	}
}

func validateCostCenterBusinessUnit(validationErrors *[]ValidationMessage, input PreflightInput) {
	targetBusinessUnit := metadataString(input.TargetTeamOrgUnit.Metadata, "businessUnit")
	costCenterBusinessUnit := metadataString(input.TargetCostCenterOrgUnit.Metadata, "businessUnit")
	allowsCrossCharge := metadataBool(input.TargetCostCenterOrgUnit.Metadata, "allowCrossCharge")

	if targetBusinessUnit == "" || costCenterBusinessUnit == "" || targetBusinessUnit == costCenterBusinessUnit || allowsCrossCharge {
		return
	}

	*validationErrors = append(*validationErrors, ValidationMessage{
		Code:    "org_transfer.cost_center_business_unit_mismatch",
		Field:   "targetCostCenterOrgUnitId",
		Message: "Target cost center must belong to the target business unit unless cross-charge is allowed.",
	})
}

func requiredMessage(field string) ValidationMessage {
	return ValidationMessage{
		Code:    "org_transfer.required",
		Field:   field,
		Message: "Required org transfer field is missing.",
	}
}

func hasActiveAssignment(assignments []WorkerAssignment, assignmentType string) bool {
	for _, assignment := range assignments {
		if assignment.AssignmentType == assignmentType && assignment.Status == "active" {
			return true
		}
	}

	return false
}

func metadataString(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}

	stringValue, ok := value.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(stringValue)
}

func metadataBool(metadata map[string]any, key string) bool {
	value, ok := metadata[key]
	if !ok {
		return false
	}

	boolValue, ok := value.(bool)
	return ok && boolValue
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
