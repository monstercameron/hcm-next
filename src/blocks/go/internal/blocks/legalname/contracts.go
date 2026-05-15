package legalname

import "hcm-next-executor/internal/executor"

const (
	// PreflightBlockName is the legal-name deterministic validation block name.
	PreflightBlockName = "system.employee_data.legal_name.preflight"
	// PlanTransactionBlockName is the legal-name deterministic transaction planning block name.
	PlanTransactionBlockName = "system.employee_data.legal_name.plan_transaction"
	// BlockVersionV1 is the V0 legal-name block version.
	BlockVersionV1 = "1.0.0"
)

// LegalName is the normalized legal-name contract shared by legal-name blocks.
type LegalName struct {
	First  string  `json:"first"`
	Middle *string `json:"middle"`
	Last   string  `json:"last"`
}

// ValidationMessage is a typed business validation message returned by preflight.
type ValidationMessage struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Message string `json:"message"`
}

// RegisterBlocks registers the V0 legal-name deterministic blocks.
func RegisterBlocks(registry *executor.Registry) error {
	if err := registry.Register(executor.BlockReference{Name: PreflightBlockName, Version: BlockVersionV1}, ExecutePreflight); err != nil {
		return err
	}

	return registry.Register(executor.BlockReference{Name: PlanTransactionBlockName, Version: BlockVersionV1}, ExecutePlanTransaction)
}
