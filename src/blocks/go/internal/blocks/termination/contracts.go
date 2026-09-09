package termination

import "human-capital-management-suite-executor/internal/executor"

const (
	// PreflightBlockName is the termination deterministic validation block name.
	PreflightBlockName = "system.employee_data.termination.preflight"
	// PlanTransactionBlockName is the termination deterministic transaction planning block name.
	PlanTransactionBlockName = "system.employee_data.termination.plan_transaction"
	// BlockVersionV1 is the V1 termination block version.
	BlockVersionV1 = "1.0.0"
)

// ValidationMessage is the generic block validation message used by termination blocks.
type ValidationMessage = executor.ValidationIssue

// InternalWriteSpec is the termination output name for generic ledger facts.
type InternalWriteSpec = executor.LedgerFact

// ProjectionPatch is the generic projection mutation emitted by termination blocks.
type ProjectionPatch = executor.ProjectionPatch

// RegisterBlocks registers the V1 termination deterministic blocks.
func RegisterBlocks(registry *executor.Registry) error {
	if err := registry.Register(executor.BlockReference{Name: PreflightBlockName, Version: BlockVersionV1}, ExecutePreflight); err != nil {
		return err
	}

	return registry.Register(executor.BlockReference{Name: PlanTransactionBlockName, Version: BlockVersionV1}, ExecutePlanTransaction)
}
