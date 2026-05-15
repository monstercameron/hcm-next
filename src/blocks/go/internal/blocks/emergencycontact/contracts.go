package emergencycontact

import "hcm-next-executor/internal/executor"

const (
	// PreflightBlockName is the emergency-contact deterministic validation block name.
	PreflightBlockName = "system.employee_data.emergency_contact.preflight"
	// PlanTransactionBlockName is the emergency-contact deterministic transaction planning block name.
	PlanTransactionBlockName = "system.employee_data.emergency_contact.plan_transaction"
	// BlockVersionV1 is the V0 emergency-contact block version.
	BlockVersionV1 = "1.0.0"
)

// EmergencyContact is the normalized contact contract shared by emergency-contact blocks.
type EmergencyContact struct {
	ContactID    string  `json:"contactId"`
	Name         string  `json:"name"`
	Relationship string  `json:"relationship"`
	Phone        string  `json:"phone"`
	Email        *string `json:"email"`
	Priority     int     `json:"priority"`
}

// ValidationMessage is the generic block validation message used by emergency-contact blocks.
type ValidationMessage = executor.ValidationIssue

// InternalWriteSpec is the legacy emergency-contact output name for generic ledger facts.
type InternalWriteSpec = executor.LedgerFact

// ProjectionPatch is the generic projection mutation emitted by emergency-contact blocks.
type ProjectionPatch = executor.ProjectionPatch

// RegisterBlocks registers the V0 emergency-contact deterministic blocks.
func RegisterBlocks(registry *executor.Registry) error {
	if err := registry.Register(executor.BlockReference{Name: PreflightBlockName, Version: BlockVersionV1}, ExecutePreflight); err != nil {
		return err
	}

	return registry.Register(executor.BlockReference{Name: PlanTransactionBlockName, Version: BlockVersionV1}, ExecutePlanTransaction)
}
