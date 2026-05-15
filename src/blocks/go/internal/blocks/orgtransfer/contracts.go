package orgtransfer

import "hcm-next-executor/internal/executor"

const (
	// PreflightBlockName is the org transfer deterministic validation block name.
	PreflightBlockName = "system.employee_data.org_transfer_compensation_change.preflight"
	// PlanTransactionBlockName is the org transfer deterministic transaction planning block name.
	PlanTransactionBlockName = "system.employee_data.org_transfer_compensation_change.plan_transaction"
	// BlockVersionV1 is the V0 org transfer block version.
	BlockVersionV1 = "1.0.0"
)

// OrganizationInfo is the employee projection organization slice needed by org transfer blocks.
type OrganizationInfo struct {
	LegalEntity  string `json:"legalEntity"`
	BusinessUnit string `json:"businessUnit"`
	Department   string `json:"department"`
	Team         string `json:"team"`
	Location     string `json:"location"`
	PayZone      string `json:"payZone"`
	CostCenter   string `json:"costCenter"`
}

// JobInfo is the employee projection job slice needed by org transfer blocks.
type JobInfo struct {
	JobCode string `json:"jobCode"`
	Title   string `json:"title"`
	Family  string `json:"family"`
	Level   string `json:"level"`
}

// CompensationInfo is the employee projection compensation slice needed by org transfer blocks.
type CompensationInfo struct {
	Amount             float64 `json:"amount"`
	Currency           string  `json:"currency"`
	PayFrequency       string  `json:"payFrequency"`
	BonusTargetPercent float64 `json:"bonusTargetPercent"`
	EffectiveDate      string  `json:"effectiveDate"`
}

// OrgUnit is the normalized org-unit contract supplied by Node from the data store.
type OrgUnit struct {
	OrgUnitID string         `json:"orgUnitId"`
	Type      string         `json:"type"`
	Name      string         `json:"name"`
	Status    string         `json:"status"`
	Metadata  map[string]any `json:"metadata"`
}

// WorkerAssignment is the normalized worker-assignment contract supplied by Node.
type WorkerAssignment struct {
	WorkerAssignmentID string         `json:"workerAssignmentId"`
	EmployeeID         string         `json:"employeeId"`
	OrgUnitID          string         `json:"orgUnitId"`
	AssignmentType     string         `json:"assignmentType"`
	RoleType           string         `json:"roleType,omitempty"`
	ManagerEmployeeID  string         `json:"managerEmployeeId,omitempty"`
	AllocationPercent  float64        `json:"allocationPercent"`
	Status             string         `json:"status"`
	EffectiveStart     string         `json:"effectiveStart"`
	EffectiveEnd       string         `json:"effectiveEnd,omitempty"`
	Metadata           map[string]any `json:"metadata"`
}

// ValidationMessage is the generic block validation message used by org-transfer blocks.
type ValidationMessage = executor.ValidationIssue

// InternalWriteSpec is the legacy org-transfer output name for generic ledger facts.
type InternalWriteSpec = executor.LedgerFact

// ProjectionPatch is the generic projection mutation emitted by org-transfer blocks.
type ProjectionPatch = executor.ProjectionPatch

// RegisterBlocks registers the V0 org transfer deterministic blocks.
func RegisterBlocks(registry *executor.Registry) error {
	if err := registry.Register(executor.BlockReference{Name: PreflightBlockName, Version: BlockVersionV1}, ExecutePreflight); err != nil {
		return err
	}

	return registry.Register(executor.BlockReference{Name: PlanTransactionBlockName, Version: BlockVersionV1}, ExecutePlanTransaction)
}
