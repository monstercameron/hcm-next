package orgtransfer

import (
	"bytes"
	"encoding/json"
	"strings"

	"hcm-next-executor/internal/executor"
)

const (
	workerSubjectType                = "worker"
	workerAssignmentSupersededEvent  = "WorkerAssignmentSuperseded"
	workerAssignmentCreatedEvent     = "WorkerAssignmentCreated"
	roleBindingsRecalculatedEvent    = "RoleBindingsRecalculated"
	employeeOrgProjectionUpdatedEvent = "EmployeeOrgProjectionUpdated"
	employeeCompensationUpdatedEvent = "EmployeeCompensationUpdated"
	hrisTransferConnectionID         = "hris_worker_profile"
	payrollCostCenterConnectionID    = "payroll_cost_center"
	compensationVendorConnectionID   = "compensation_vendor"
)

// PlanTransactionInput is the deterministic transaction planning input for org transfer.
type PlanTransactionInput struct {
	ChangeRequestID            string             `json:"changeRequestId"`
	WorkerID                   string             `json:"workerId"`
	CurrentOrganization        OrganizationInfo   `json:"currentOrganization"`
	ProposedOrganization       OrganizationInfo   `json:"proposedOrganization"`
	CurrentJob                 JobInfo            `json:"currentJob"`
	ProposedJob                JobInfo            `json:"proposedJob"`
	CurrentCompensation        CompensationInfo   `json:"currentCompensation"`
	ProposedCompensation       CompensationInfo   `json:"proposedCompensation"`
	CurrentAssignments         []WorkerAssignment `json:"currentAssignments"`
	TargetTeamOrgUnit          OrgUnit            `json:"targetTeamOrgUnit"`
	TargetLocationOrgUnit      OrgUnit            `json:"targetLocationOrgUnit"`
	TargetCostCenterOrgUnit    OrgUnit            `json:"targetCostCenterOrgUnit"`
	SourceManagerEmployeeID    string             `json:"sourceManagerEmployeeId"`
	TargetManagerEmployeeID    string             `json:"targetManagerEmployeeId"`
	EffectiveAt                string             `json:"effectiveAt"`
	BusinessReason             string             `json:"businessReason"`
}

// InternalWriteSpec describes an internal ledger write that Node may apply.
type InternalWriteSpec struct {
	EventType   string         `json:"eventType"`
	SubjectType string         `json:"subjectType"`
	SubjectID   string         `json:"subjectId"`
	EffectiveAt string         `json:"effectiveAt"`
	Payload     map[string]any `json:"payload"`
}

// ProjectionPatch describes a deterministic employee projection mutation.
type ProjectionPatch struct {
	Projection string `json:"projection"`
	Operation  string `json:"operation"`
	Path       string `json:"path"`
	Value      any    `json:"value"`
}

// AssignmentOperation describes a durable worker-assignment write for Node to apply.
type AssignmentOperation struct {
	Operation                 string         `json:"operation"`
	AssignmentType            string         `json:"assignmentType"`
	CurrentWorkerAssignmentID string         `json:"currentWorkerAssignmentId,omitempty"`
	OrgUnitID                 string         `json:"orgUnitId,omitempty"`
	RoleType                  string         `json:"roleType,omitempty"`
	ManagerEmployeeID         string         `json:"managerEmployeeId,omitempty"`
	AllocationPercent         float64        `json:"allocationPercent,omitempty"`
	EffectiveStart            string         `json:"effectiveStart,omitempty"`
	EffectiveEnd              string         `json:"effectiveEnd,omitempty"`
	IdempotencyKey            string         `json:"idempotencyKey"`
	Metadata                  map[string]any `json:"metadata,omitempty"`
}

// RoleBindingOperation describes an RBAC recalculation operation for Node to apply.
type RoleBindingOperation struct {
	Operation       string         `json:"operation"`
	ActorEmployeeID string         `json:"actorEmployeeId,omitempty"`
	RoleKey         string         `json:"roleKey,omitempty"`
	ScopeType       string         `json:"scopeType,omitempty"`
	EffectiveStart  string         `json:"effectiveStart,omitempty"`
	IdempotencyKey   string         `json:"idempotencyKey"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

// PlanTransactionOutput is the deterministic execution plan for an org transfer.
type PlanTransactionOutput struct {
	InternalWrites        []InternalWriteSpec            `json:"internalWrites"`
	ProjectionPatches     []ProjectionPatch              `json:"projectionPatches"`
	AssignmentOperations  []AssignmentOperation          `json:"assignmentOperations"`
	RoleBindingOperations []RoleBindingOperation         `json:"roleBindingOperations"`
	ExternalCallRequests  []executor.ExternalCallRequest `json:"externalCallRequests"`
}

// ExecutePlanTransaction builds deterministic assignment, projection, ledger, and outbox specs.
func ExecutePlanTransaction(request executor.ExecutionRequest) (executor.BlockResult, *executor.ExecutionError) {
	input, inputError := decodePlanTransactionInput(request.Input)
	if inputError != nil {
		return executor.BlockResult{}, inputError
	}

	if validationErrors := validatePlanTransactionInput(input); len(validationErrors) > 0 {
		return executor.BlockResult{}, executor.InvalidInputError("Org transfer transaction plan input is missing required fields.", map[string]any{
			"errors": validationErrors,
		})
	}

	assignmentOperations := buildAssignmentOperations(input)
	roleBindingOperations := buildRoleBindingOperations(input)
	projectionPatches := buildProjectionPatches(input)
	internalWrites := buildInternalWrites(input, assignmentOperations, roleBindingOperations, projectionPatches)
	externalCallRequests := buildExternalCallRequests(input)
	output := PlanTransactionOutput{
		InternalWrites:        internalWrites,
		ProjectionPatches:     projectionPatches,
		AssignmentOperations:  assignmentOperations,
		RoleBindingOperations: roleBindingOperations,
		ExternalCallRequests:  externalCallRequests,
	}

	return executor.BlockResult{
		Output:               output,
		ExternalCallRequests: externalCallRequests,
		Logs: []executor.ExecutionLog{
			{
				Level:   "info",
				Message: "Org transfer transaction plan created.",
				Fields: map[string]any{
					"assignmentOperationCount":  len(assignmentOperations),
					"roleBindingOperationCount": len(roleBindingOperations),
					"projectionPatchCount":      len(projectionPatches),
					"externalCallRequestCount":  len(externalCallRequests),
				},
			},
		},
	}, nil
}

func decodePlanTransactionInput(rawInput json.RawMessage) (PlanTransactionInput, *executor.ExecutionError) {
	var input PlanTransactionInput
	decoder := json.NewDecoder(bytes.NewReader(rawInput))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&input); err != nil {
		return PlanTransactionInput{}, executor.InvalidInputError("Org transfer transaction plan input does not match the expected contract.", map[string]any{
			"decodeError": err.Error(),
		})
	}

	return input, nil
}

func validatePlanTransactionInput(input PlanTransactionInput) []ValidationMessage {
	validationErrors := make([]ValidationMessage, 0)
	requiredFields := map[string]string{
		"changeRequestId":         input.ChangeRequestID,
		"workerId":                input.WorkerID,
		"targetTeamOrgUnitId":     input.TargetTeamOrgUnit.OrgUnitID,
		"targetLocationOrgUnitId": input.TargetLocationOrgUnit.OrgUnitID,
		"targetCostCenterOrgUnitId": input.TargetCostCenterOrgUnit.OrgUnitID,
		"targetManagerEmployeeId": input.TargetManagerEmployeeID,
		"effectiveAt":             input.EffectiveAt,
		"businessReason":          input.BusinessReason,
	}

	for field, value := range requiredFields {
		if strings.TrimSpace(value) == "" {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "transaction_plan.required",
				Field:   field,
				Message: "Required transaction planning field is missing.",
			})
		}
	}

	if _, err := parseDate(input.EffectiveAt); strings.TrimSpace(input.EffectiveAt) != "" && err != nil {
		validationErrors = append(validationErrors, ValidationMessage{
			Code:    "transaction_plan.effective_at_invalid",
			Field:   "effectiveAt",
			Message: "Effective date must be a valid date.",
		})
	}

	for _, assignmentType := range []string{"primary_team", "work_location", "cost_center"} {
		if currentAssignmentForType(input.CurrentAssignments, assignmentType) == nil {
			validationErrors = append(validationErrors, ValidationMessage{
				Code:    "transaction_plan.current_assignment_missing",
				Field:   "currentAssignments",
				Message: "Required current assignment is missing.",
			})
		}
	}

	return validationErrors
}

func buildAssignmentOperations(input PlanTransactionInput) []AssignmentOperation {
	assignmentOperations := make([]AssignmentOperation, 0, 6)
	targets := []struct {
		assignmentType string
		orgUnitID      string
		roleType       string
		managerID      string
		metadata       map[string]any
	}{
		{
			assignmentType: "primary_team",
			orgUnitID:      input.TargetTeamOrgUnit.OrgUnitID,
			roleType:       input.ProposedJob.Title,
			managerID:      input.TargetManagerEmployeeID,
			metadata: map[string]any{
				"team":         input.ProposedOrganization.Team,
				"department":   input.ProposedOrganization.Department,
				"businessUnit": input.ProposedOrganization.BusinessUnit,
			},
		},
		{
			assignmentType: "work_location",
			orgUnitID:      input.TargetLocationOrgUnit.OrgUnitID,
			metadata: map[string]any{
				"location": input.ProposedOrganization.Location,
			},
		},
		{
			assignmentType: "cost_center",
			orgUnitID:      input.TargetCostCenterOrgUnit.OrgUnitID,
			metadata: map[string]any{
				"costCenter": input.ProposedOrganization.CostCenter,
			},
		},
	}

	for _, target := range targets {
		currentAssignment := currentAssignmentForType(input.CurrentAssignments, target.assignmentType)
		if currentAssignment != nil {
			assignmentOperations = append(assignmentOperations, AssignmentOperation{
				Operation:                 "supersede",
				AssignmentType:            target.assignmentType,
				CurrentWorkerAssignmentID: currentAssignment.WorkerAssignmentID,
				EffectiveEnd:              input.EffectiveAt,
				IdempotencyKey:            "assignment_supersede_" + input.ChangeRequestID + "_" + target.assignmentType,
			})
		}

		assignmentOperations = append(assignmentOperations, AssignmentOperation{
			Operation:         "create",
			AssignmentType:    target.assignmentType,
			OrgUnitID:         target.orgUnitID,
			RoleType:          target.roleType,
			ManagerEmployeeID: target.managerID,
			AllocationPercent: 100,
			EffectiveStart:    input.EffectiveAt,
			IdempotencyKey:    "assignment_create_" + input.ChangeRequestID + "_" + target.assignmentType,
			Metadata:          target.metadata,
		})
	}

	return assignmentOperations
}

func buildRoleBindingOperations(input PlanTransactionInput) []RoleBindingOperation {
	return []RoleBindingOperation{
		{
			Operation:       "ensure_direct_reports_binding",
			ActorEmployeeID: input.TargetManagerEmployeeID,
			RoleKey:         "manager",
			ScopeType:       "direct_reports",
			EffectiveStart:  input.EffectiveAt,
			IdempotencyKey:   "role_binding_destination_manager_" + input.ChangeRequestID,
			Metadata: map[string]any{
				"sourceManagerEmployeeId": input.SourceManagerEmployeeID,
				"targetManagerEmployeeId": input.TargetManagerEmployeeID,
			},
		},
		{
			Operation:     "record_recalculation",
			EffectiveStart: input.EffectiveAt,
			IdempotencyKey: "role_binding_recalculation_" + input.ChangeRequestID,
			Metadata: map[string]any{
				"sourceManagerEmployeeId": input.SourceManagerEmployeeID,
				"targetManagerEmployeeId": input.TargetManagerEmployeeID,
			},
		},
	}
}

func buildProjectionPatches(input PlanTransactionInput) []ProjectionPatch {
	projectionPatches := []ProjectionPatch{
		{Projection: "employee", Operation: "replace", Path: "/organization/businessUnit", Value: input.ProposedOrganization.BusinessUnit},
		{Projection: "employee", Operation: "replace", Path: "/organization/department", Value: input.ProposedOrganization.Department},
		{Projection: "employee", Operation: "replace", Path: "/organization/team", Value: input.ProposedOrganization.Team},
		{Projection: "employee", Operation: "replace", Path: "/organization/location", Value: input.ProposedOrganization.Location},
		{Projection: "employee", Operation: "replace", Path: "/organization/costCenter", Value: input.ProposedOrganization.CostCenter},
		{Projection: "employee", Operation: "replace", Path: "/manager/employeeId", Value: input.TargetManagerEmployeeID},
	}

	if input.ProposedJob.JobCode != "" {
		projectionPatches = append(projectionPatches, ProjectionPatch{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/job",
			Value:      input.ProposedJob,
		})
	}

	if input.ProposedCompensation.Amount > 0 {
		projectionPatches = append(projectionPatches, ProjectionPatch{
			Projection: "employee",
			Operation:  "replace",
			Path:       "/compensation",
			Value:      input.ProposedCompensation,
		})
	}

	return projectionPatches
}

func buildInternalWrites(
	input PlanTransactionInput,
	assignmentOperations []AssignmentOperation,
	roleBindingOperations []RoleBindingOperation,
	projectionPatches []ProjectionPatch,
) []InternalWriteSpec {
	internalWrites := make([]InternalWriteSpec, 0, len(assignmentOperations)+len(roleBindingOperations)+2)

	for _, operation := range assignmentOperations {
		eventType := workerAssignmentCreatedEvent
		if operation.Operation == "supersede" {
			eventType = workerAssignmentSupersededEvent
		}

		internalWrites = append(internalWrites, InternalWriteSpec{
			EventType:   eventType,
			SubjectType: workerSubjectType,
			SubjectID:   input.WorkerID,
			EffectiveAt: input.EffectiveAt,
			Payload: map[string]any{
				"operation": operation,
			},
		})
	}

	internalWrites = append(internalWrites, InternalWriteSpec{
		EventType:   roleBindingsRecalculatedEvent,
		SubjectType: workerSubjectType,
		SubjectID:   input.WorkerID,
		EffectiveAt: input.EffectiveAt,
		Payload: map[string]any{
			"operations": roleBindingOperations,
		},
	})

	internalWrites = append(internalWrites, InternalWriteSpec{
		EventType:   employeeOrgProjectionUpdatedEvent,
		SubjectType: workerSubjectType,
		SubjectID:   input.WorkerID,
		EffectiveAt: input.EffectiveAt,
		Payload: map[string]any{
			"previousOrganization": input.CurrentOrganization,
			"newOrganization":      input.ProposedOrganization,
			"projectionPatches":    projectionPatches,
		},
	})

	if input.ProposedCompensation.Amount > 0 {
		internalWrites = append(internalWrites, InternalWriteSpec{
			EventType:   employeeCompensationUpdatedEvent,
			SubjectType: workerSubjectType,
			SubjectID:   input.WorkerID,
			EffectiveAt: input.EffectiveAt,
			Payload: map[string]any{
				"previousCompensation": input.CurrentCompensation,
				"newCompensation":      input.ProposedCompensation,
			},
		})
	}

	return internalWrites
}

func buildExternalCallRequests(input PlanTransactionInput) []executor.ExternalCallRequest {
	externalCalls := []executor.ExternalCallRequest{
		{
			ConnectionID:   hrisTransferConnectionID,
			Operation:      "syncWorkerOrgTransfer",
			IdempotencyKey: "hris_org_transfer_" + input.ChangeRequestID,
			Payload: map[string]any{
				"workerId":             input.WorkerID,
				"changeRequestId":      input.ChangeRequestID,
				"proposedOrganization": input.ProposedOrganization,
				"targetManagerEmployeeId": input.TargetManagerEmployeeID,
				"effectiveAt":          input.EffectiveAt,
			},
			Reconciliation: map[string]any{
				"expectedExternalObject": "worker_assignment",
			},
		},
		{
			ConnectionID:   payrollCostCenterConnectionID,
			Operation:      "syncCostCenter",
			IdempotencyKey: "payroll_cost_center_" + input.ChangeRequestID,
			Payload: map[string]any{
				"workerId":        input.WorkerID,
				"changeRequestId": input.ChangeRequestID,
				"costCenter":      input.ProposedOrganization.CostCenter,
				"effectiveAt":     input.EffectiveAt,
			},
			Reconciliation: map[string]any{
				"expectedExternalObject": "payroll_cost_center",
			},
		},
	}

	if input.ProposedCompensation.Amount > 0 {
		externalCalls = append(externalCalls, executor.ExternalCallRequest{
			ConnectionID:   compensationVendorConnectionID,
			Operation:      "syncCompensation",
			IdempotencyKey: "compensation_vendor_" + input.ChangeRequestID,
			Payload: map[string]any{
				"workerId":             input.WorkerID,
				"changeRequestId":      input.ChangeRequestID,
				"proposedCompensation": input.ProposedCompensation,
				"effectiveAt":          input.EffectiveAt,
			},
			Reconciliation: map[string]any{
				"expectedExternalObject": "compensation_record",
			},
		})
	}

	return externalCalls
}

func currentAssignmentForType(assignments []WorkerAssignment, assignmentType string) *WorkerAssignment {
	for index := range assignments {
		if assignments[index].AssignmentType == assignmentType && assignments[index].Status == "active" {
			return &assignments[index]
		}
	}

	return nil
}
