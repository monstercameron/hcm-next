package compensation

import "hcm-next-executor/internal/executor"

const (
	// PreflightBlockName is the compensation deterministic validation block name.
	PreflightBlockName = "system.employee_data.compensation.preflight"
	// PlanTransactionBlockName is the compensation deterministic transaction planning block name.
	PlanTransactionBlockName = "system.employee_data.compensation.plan_transaction"
	// BlockVersionV1 is the V0 compensation block version.
	BlockVersionV1 = "1.0.0"
)

// CompensationInfo is the normalized compensation contract shared by compensation blocks.
type CompensationInfo struct {
	Amount             float64 `json:"amount"`
	Currency           string  `json:"currency"`
	PayFrequency       string  `json:"payFrequency"`
	BonusTargetPercent float64 `json:"bonusTargetPercent"`
	EffectiveDate      string  `json:"effectiveDate"`
}

// ValidationMessage is the generic block validation message used by compensation blocks.
type ValidationMessage = executor.ValidationIssue

// InternalWriteSpec is the legacy compensation output name for generic ledger facts.
type InternalWriteSpec = executor.LedgerFact

// ProjectionPatch is the generic projection mutation emitted by compensation blocks.
type ProjectionPatch = executor.ProjectionPatch

// RegisterBlocks registers the V0 compensation deterministic blocks.
func RegisterBlocks(registry *executor.Registry) error {
	if err := registry.Register(executor.BlockReference{Name: PreflightBlockName, Version: BlockVersionV1}, ExecutePreflight); err != nil {
		return err
	}

	return registry.Register(executor.BlockReference{Name: PlanTransactionBlockName, Version: BlockVersionV1}, ExecutePlanTransaction)
}
